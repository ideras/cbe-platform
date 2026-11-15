package loadtest

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// student is one simulated student: the application's side of the exam server's routes, without the window.
type student struct {
	cfg     *Config
	rec     *Recorder
	account string
	rnd     *rand.Rand
	http    *http.Client

	token, session string

	mu      sync.Mutex
	conn    *websocket.Conn
	nextID  uint64
	pending map[uint64]chan map[string]any
	output  map[uint32]*bytes.Buffer
	state   string
	closed  chan struct{} // closed when the connection ends
	changed chan struct{} // told (without waiting) whenever something arrived

	term     uint32
	haveTerm bool
	versions map[string]string
	units    []string
	counter  int
	built    bool
	gone     bool
}

func newStudent(cfg *Config, rec *Recorder, account string, seed int64) *student {
	return &student{cfg: cfg, rec: rec, account: account, rnd: rand.New(rand.NewSource(seed)), http: cfg.HTTP, versions: map[string]string{}, changed: make(chan struct{}, 1)}
}

func (s *student) note() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func failure(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 80 {
		text = text[:80]
	}
	return text
}

// post sends a JSON request to one of the server's routes and decodes the answer.
func (s *student) post(ctx context.Context, path string, body any, token string, into any) error {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", s.cfg.Server+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != 200 {
		return fmt.Errorf("%s: %d %.60s", path, response.StatusCode, strings.TrimSpace(string(data)))
	}
	if into != nil {
		return json.Unmarshal(data, into)
	}
	return nil
}

func (s *student) login(ctx context.Context) error {
	var out struct {
		Token   string `json:"session_token"`
		Session string `json:"session_id"`
	}
	start := time.Now()
	err := s.post(ctx, "/login", map[string]string{"account_number": s.account, "exam_pin": s.cfg.PIN}, "", &out)
	s.rec.Add(OpLogin, time.Since(start), failure(err))
	if err != nil {
		return err
	}
	s.token, s.session = out.Token, out.Session
	return nil
}

func (s *student) heartbeats(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			start := time.Now()
			err := s.post(ctx, "/heartbeat", map[string]string{"hostname": "loadtest-" + s.account}, s.token, nil)
			if ctx.Err() == nil {
				s.rec.Add(OpHeartbeat, time.Since(start), failure(err))
			}
		}
	}
}

// dial opens the WebSocket and starts reading it.
func (s *student) dial(ctx context.Context) error {
	url := "ws" + strings.TrimPrefix(s.cfg.Server, "http") + "/workspace/ws"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + s.token}}, HTTPClient: s.http})
	if err != nil {
		return err
	}
	conn.SetReadLimit(16 << 20)
	s.mu.Lock()
	s.conn = conn
	s.pending = map[uint64]chan map[string]any{}
	s.output = map[uint32]*bytes.Buffer{}
	s.state = ""
	closed := make(chan struct{})
	s.closed = closed
	s.mu.Unlock()
	go func() {
		defer close(closed)
		for {
			kind, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			if kind == websocket.MessageBinary {
				if len(data) >= 4 {
					term := binary.BigEndian.Uint32(data[:4])
					s.mu.Lock()
					if s.output[term] == nil {
						s.output[term] = &bytes.Buffer{}
					}
					s.output[term].Write(data[4:])
					s.mu.Unlock()
					s.note()
				}
				continue
			}
			var message map[string]any
			if json.Unmarshal(data, &message) != nil {
				continue
			}
			s.mu.Lock()
			if id, isReply := message["id"].(float64); isReply && message["ok"] != nil {
				if wait := s.pending[uint64(id)]; wait != nil {
					wait <- message
					delete(s.pending, uint64(id))
				}
			} else {
				switch message["type"] {
				case "ws.status":
					s.state, _ = message["state"].(string)
				case "ws.agent_restarted":
					s.rec.Event("workspace restarted (agent lost)")
					s.haveTerm = false
				case "agent.error":
					if message["code"] == "RATE" {
						s.rec.Event("keystrokes dropped (RATE)")
					}
				}
			}
			s.mu.Unlock()
			s.note()
		}
	}()
	return nil
}

func (s *student) connection() (*websocket.Conn, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn, s.closed
}

// call sends a request and waits for its reply.
func (s *student) call(ctx context.Context, kind string, fields map[string]any, within time.Duration) (map[string]any, time.Duration, error) {
	conn, closed := s.connection()
	if conn == nil {
		return nil, 0, errors.New("not connected")
	}
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	wait := make(chan map[string]any, 1)
	s.pending[id] = wait
	s.mu.Unlock()
	request := map[string]any{"id": id, "type": kind}
	for key, value := range fields {
		request[key] = value
	}
	payload, _ := json.Marshal(request)
	start := time.Now()
	writeCtx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, payload); err != nil {
		return nil, 0, fmt.Errorf("write: %w", err)
	}
	select {
	case reply := <-wait:
		if reply["ok"] != true {
			code, _ := reply["code"].(string)
			return reply, time.Since(start), fmt.Errorf("%s: %s", kind, code)
		}
		return reply, time.Since(start), nil
	case <-closed:
		return nil, 0, errors.New("the connection ended")
	case <-time.After(within):
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return nil, 0, fmt.Errorf("%s: no answer in %v", kind, within)
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}

// timed makes a call and records it.
func (s *student) timed(ctx context.Context, op, kind string, fields map[string]any) (map[string]any, error) {
	reply, took, err := s.call(ctx, kind, fields, 60*time.Second)
	if ctx.Err() != nil {
		return reply, err
	}
	s.rec.Add(op, took, failure(err))
	return reply, err
}

func (s *student) keys(ctx context.Context, term uint32, text string) error {
	conn, _ := s.connection()
	if conn == nil {
		return errors.New("not connected")
	}
	frame := binary.BigEndian.AppendUint32(nil, term)
	return conn.Write(ctx, websocket.MessageBinary, append(frame, text...))
}

// waitOutput waits until what a terminal printed matches, and takes it (and what came before it) out of the buffer.
func (s *student) waitOutput(ctx context.Context, term uint32, pattern *regexp.Regexp, within time.Duration) ([]string, bool) {
	deadline := time.After(within)
	_, closed := s.connection()
	for {
		s.mu.Lock()
		if buffer := s.output[term]; buffer != nil {
			if match := pattern.FindSubmatchIndex(buffer.Bytes()); match != nil {
				text := buffer.String()
				groups := make([]string, 0, len(match)/2)
				for i := 0; i+1 < len(match); i += 2 {
					groups = append(groups, text[match[i]:match[i+1]])
				}
				rest := append([]byte(nil), buffer.Bytes()[match[1]:]...)
				buffer.Reset()
				buffer.Write(rest)
				s.mu.Unlock()
				return groups, true
			}
			if buffer.Len() > 256<<10 {
				keep := append([]byte(nil), buffer.Bytes()[buffer.Len()-64<<10:]...)
				buffer.Reset()
				buffer.Write(keep)
			}
		}
		s.mu.Unlock()
		select {
		case <-s.changed:
		case <-deadline:
			return nil, false
		case <-closed:
			return nil, false
		case <-ctx.Done():
			return nil, false
		}
	}
}

// ready waits for the workspace to be running, as the application's page does before it shows the IDE.
func (s *student) ready(ctx context.Context, within time.Duration) error {
	start := time.Now()
	deadline := time.After(within)
	_, closed := s.connection()
	for {
		s.mu.Lock()
		state := s.state
		s.mu.Unlock()
		switch state {
		case "running":
			s.rec.Add(OpReady, time.Since(start), "")
			return nil
		case "failed", "ended", "frozen", "locked":
			s.rec.Add(OpReady, 0, "the workspace is "+state)
			return fmt.Errorf("the workspace is %s", state)
		}
		select {
		case <-s.changed:
		case <-deadline:
			s.rec.Add(OpReady, 0, fmt.Sprintf("not running after %v (state %q)", within, state))
			return errors.New("the workspace did not come up")
		case <-closed:
			s.rec.Add(OpReady, 0, "the connection ended")
			return errors.New("the connection ended")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// openTerminal opens a shell (or finds the one the student has after a reconnection).
func (s *student) openTerminal(ctx context.Context) error {
	reply, err := s.timed(ctx, "terminal open", "pty.open", map[string]any{"cols": 120, "rows": 30})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.term, s.haveTerm = uint32(reply["term"].(float64)), true
	s.mu.Unlock()
	// The prompt, so that what follows is not mixed with it.
	time.Sleep(300 * time.Millisecond)
	return nil
}

func (s *student) terminal(ctx context.Context) (uint32, error) {
	s.mu.Lock()
	term, have := s.term, s.haveTerm
	s.mu.Unlock()
	if have {
		return term, nil
	}
	if err := s.openTerminal(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.term, nil
}

var numbers = regexp.MustCompile(`\d+`)

// make types a build into the terminal and waits for it to end, as a student does; it returns how long it took and the exit code.
func (s *student) make(ctx context.Context, op, command string) error {
	term, err := s.terminal(ctx)
	if err != nil {
		return err
	}
	s.counter++
	tag := fmt.Sprintf("__b%d", s.counter)
	pattern := regexp.MustCompile(regexp.QuoteMeta(tag) + `_(\d+)`)
	start := time.Now()
	if err := s.keys(ctx, term, command+" >/dev/null 2>&1; echo \""+tag+"_$?\"\n"); err != nil {
		s.rec.Add(op, 0, failure(err))
		return err
	}
	groups, ok := s.waitOutput(ctx, term, pattern, 10*time.Minute)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	switch {
	case !ok:
		s.rec.Add(op, 0, "the build did not end")
		return errors.New("the build did not end")
	case groups[1] != "0":
		s.rec.Add(op, 0, "make failed with "+groups[1])
		return errors.New("make failed")
	}
	s.rec.Add(op, time.Since(start), "")
	return nil
}

// echo measures how long a key takes to come back from the terminal.
func (s *student) echo(ctx context.Context) {
	term, err := s.terminal(ctx)
	if err != nil {
		return
	}
	s.counter++
	tag := fmt.Sprintf("#e%d", s.counter)
	start := time.Now()
	if err := s.keys(ctx, term, tag); err != nil {
		s.rec.Add(OpEcho, 0, failure(err))
		return
	}
	_, ok := s.waitOutput(ctx, term, regexp.MustCompile(regexp.QuoteMeta(tag)), 30*time.Second)
	if ctx.Err() != nil {
		return
	}
	if !ok {
		s.rec.Add(OpEcho, 0, "no echo in 30 s")
	} else {
		s.rec.Add(OpEcho, time.Since(start), "")
	}
	_ = s.keys(ctx, term, "\x15") // Ctrl-U: the line is empty again
}

// typist is a student's writing of a file of notes while their programs build, as the IDE's autosave goes on whatever the terminals are doing: a
// save every few seconds, with no relation to the stretches of work, so that the autosave is measured while the machine is busy too.
func (s *student) typist(ctx context.Context) {
	rnd := rand.New(rand.NewSource(s.rnd.Int63()))
	path := "notes-" + s.account + ".txt"
	if _, err := s.timed(ctx, OpCreate, "fs.create", map[string]any{"path": path, "content": "Notes of " + s.account + "\n"}); err != nil {
		return
	}
	reply, err := s.timed(ctx, OpOpen, "fs.read", map[string]any{"path": path})
	if err != nil {
		return
	}
	content, _ := reply["content"].(string)
	version, _ := reply["version"].(string)
	for line := 0; ctx.Err() == nil; line++ {
		if !s.sleep(ctx, 2*time.Second+time.Duration(rnd.Int63n(int64(4*time.Second)))) {
			return
		}
		if line%40 == 39 {
			content = "Notes of " + s.account + "\n"
		}
		content += fmt.Sprintf("line %d: the list is sorted when insert_sorted keeps it in order\n", line)
		reply, err := s.timed(ctx, OpSave, "fs.write", map[string]any{"path": path, "content": content, "base_version": version})
		if err != nil {
			// The connection ended or the file changed under us (a reconnection): read it again and go on.
			if again, err := s.timed(ctx, OpOpen, "fs.read", map[string]any{"path": path}); err == nil {
				content, _ = again["content"].(string)
				version, _ = again["version"].(string)
			}
			continue
		}
		version, _ = reply["version"].(string)
	}
}

func (s *student) sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *student) between(low, high time.Duration) time.Duration {
	if high <= low {
		return low
	}
	return low + time.Duration(s.rnd.Int63n(int64(high-low)))
}

// discover lists the project, as the tree does when the IDE opens.
func (s *student) discover(ctx context.Context) error {
	if _, err := s.timed(ctx, OpList, "fs.list", map[string]any{"path": ""}); err != nil {
		return err
	}
	reply, err := s.timed(ctx, OpList, "fs.list", map[string]any{"path": "src"})
	if err != nil {
		return err
	}
	entries, _ := reply["entries"].([]any)
	for _, entry := range entries {
		item, _ := entry.(map[string]any)
		if name, _ := item["name"].(string); strings.HasPrefix(name, "unit") && strings.HasSuffix(name, ".cpp") {
			s.units = append(s.units, "src/"+name)
		}
	}
	if len(s.units) == 0 {
		return errors.New("the project has no src/unit*.cpp: this is not the load test's package")
	}
	return nil
}

// edit opens a unit and saves it several times, a second apart, as autosave does while a student types.
func (s *student) edit(ctx context.Context) error {
	path := s.units[s.rnd.Intn(len(s.units))]
	reply, err := s.timed(ctx, OpOpen, "fs.read", map[string]any{"path": path})
	if err != nil {
		return err
	}
	content, _ := reply["content"].(string)
	version, _ := reply["version"].(string)
	saves := 3 + s.rnd.Intn(4)
	for i := 0; i < saves; i++ {
		s.counter++
		first, rest, _ := strings.Cut(content, "\n")
		if !strings.HasPrefix(first, "// edit ") {
			rest = content
		}
		content = fmt.Sprintf("// edit %s %d\n%s", s.account, s.counter, rest)
		reply, err := s.timed(ctx, OpSave, "fs.write", map[string]any{"path": path, "content": content, "base_version": version})
		if err != nil {
			return err
		}
		version, _ = reply["version"].(string)
		if !s.sleep(ctx, s.between(s.cfg.SaveGap/2, s.cfg.SaveGap*3/2)) {
			return ctx.Err()
		}
	}
	return nil
}

// touchEverything makes every unit newer, as a branch change or a fresh checkout would, so the next make builds all of it.
func (s *student) touchEverything(ctx context.Context) error {
	term, err := s.terminal(ctx)
	if err != nil {
		return err
	}
	s.counter++
	tag := fmt.Sprintf("__t%d", s.counter)
	if err := s.keys(ctx, term, "rm -rf build; echo "+tag+"_ok\n"); err != nil {
		return err
	}
	_, ok := s.waitOutput(ctx, term, regexp.MustCompile(regexp.QuoteMeta(tag)+`_ok\s`), time.Minute)
	if !ok {
		return errors.New("the shell did not answer")
	}
	return nil
}

// cycle is a stretch of work: look at the project, edit and save, build, and now and then a file or a tick.
func (s *student) cycle(ctx context.Context) error {
	if s.rnd.Intn(3) == 0 {
		if _, err := s.timed(ctx, OpList, "fs.list", map[string]any{"path": "src"}); err != nil {
			return err
		}
	}
	if err := s.edit(ctx); err != nil {
		return err
	}
	s.echo(ctx)
	// The first make of a student builds everything (nothing is built in a new container); the ones after an edit build a unit and link.
	op := OpBuild
	if !s.built {
		op, s.built = OpFirstBuild, true
	}
	if err := s.make(ctx, op, "make"); err != nil && ctx.Err() == nil {
		return err
	}
	switch s.rnd.Intn(6) {
	case 0:
		name := fmt.Sprintf("notes-%d.txt", s.counter)
		if _, err := s.timed(ctx, OpCreate, "fs.create", map[string]any{"path": name, "content": "a note\n"}); err == nil {
			_, _ = s.timed(ctx, OpDelete, "fs.delete", map[string]any{"path": name})
		}
	case 1:
		_, _ = s.timed(ctx, OpTodo, "todo.set", map[string]any{"item": "t2", "done": s.rnd.Intn(2) == 0})
	}
	return nil
}

// reconnect opens the WebSocket again, as the application does when it drops. It says whether it could.
func (s *student) reconnect(ctx context.Context) bool {
	for attempt := 0; attempt < 5 && ctx.Err() == nil; attempt++ {
		if !s.sleep(ctx, time.Duration(attempt+1)*time.Second) {
			return false
		}
		if err := s.dial(ctx); err != nil {
			continue
		}
		if s.ready(ctx, 30*time.Second) == nil {
			s.rec.Event("connection lost and made again")
			s.mu.Lock()
			s.haveTerm = false
			s.mu.Unlock()
			return true
		}
	}
	return false
}

func (s *student) close() {
	if conn, _ := s.connection(); conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "the load test is over")
	}
}
