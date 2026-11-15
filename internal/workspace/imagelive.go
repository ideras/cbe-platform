package workspace

import (
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The log of an image build while it goes (decision P7-10). The runner streams what the build prints; the exam server keeps it here, in memory, for the
// proctor's dialog to read from an offset (so that each look transfers only what is new), and writes it to the database every few seconds, so that a
// server that stops during a build still has the log up to there. When the build ends, the runner's own log (the start and the end of it, cut to its
// limit) replaces this one.

const (
	// maxLiveLog is the most of a build's live log that is kept in memory: past it the live view stops (and says so), and the proctor reads the start and
	// the end of the whole log when the build ends.
	maxLiveLog = 2 << 20
	// maxPendingLine bounds the end of the text that is not a whole line yet, which is read for the builder's STEP lines.
	maxPendingLine = 4 << 10
	liveCapNote    = "\n[… the live view of the log stops here, at 2 MiB: the start and the end of the whole log appear when the build ends …]\n"
	// defaultPersistEvery is how often a build's log so far is written to the database.
	defaultPersistEvery = 10 * time.Second
)

var stepLine = regexp.MustCompile(`^STEP (\d+)/(\d+): (.*)$`)

// liveLog is the log of one build that is going.
type liveLog struct {
	mu      sync.Mutex
	buf     []byte
	capped  bool
	pending string
	step    int
	steps   int
	text    string
	lastAt  time.Time
}

// add appends what the build printed and reads the builder's progress from it: "STEP 4/9: RUN make" says it is at step 4 of 9.
func (l *liveLog) add(text string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastAt = now
	if !l.capped {
		if len(l.buf)+len(text) > maxLiveLog {
			l.buf = append(l.buf, liveCapNote...)
			l.capped = true
		} else {
			l.buf = append(l.buf, text...)
		}
	}
	l.pending += text
	for {
		end := strings.IndexByte(l.pending, '\n')
		if end < 0 {
			break
		}
		line := strings.TrimRight(l.pending[:end], "\r")
		l.pending = l.pending[end+1:]
		if m := stepLine.FindStringSubmatch(line); m != nil {
			l.step, l.steps, l.text = atoiSmall(m[1]), atoiSmall(m[2]), clip(m[3], 300)
		}
	}
	if len(l.pending) > maxPendingLine {
		l.pending = l.pending[len(l.pending)-maxPendingLine:]
	}
}

func atoiSmall(digits string) int {
	n := 0
	for _, c := range digits {
		if n = n*10 + int(c-'0'); n > 1<<20 {
			return 1 << 20
		}
	}
	return n
}

// read is the log from an offset on: the text, the offset it really starts at (0 when the one asked for is not one this log has, as after a stale
// look) and the offset to ask for next. The text ends at a whole character, so a character that a chunk of output cut in two is sent whole next time.
func (l *liveLog) read(from int64) (text string, start, next int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if from < 0 || from > int64(len(l.buf)) {
		from = 0
	}
	end := len(l.buf)
	for i := 1; i <= 3 && i <= end-int(from); i++ {
		if utf8.RuneStart(l.buf[end-i]) {
			if !utf8.FullRune(l.buf[end-i : end]) {
				end -= i
			}
			break
		}
	}
	return strings.ToValidUTF8(string(l.buf[from:end]), "�"), from, int64(end)
}

// snapshot is everything the live log holds, for the database.
func (l *liveLog) snapshot() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.ToValidUTF8(string(l.buf), "�")
}

// progress is where the build is: its step and the number of steps, what the step is, and when the build last printed anything.
func (l *liveLog) progress() (step, steps int, text string, last time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.step, l.steps, l.text, l.lastAt
}
