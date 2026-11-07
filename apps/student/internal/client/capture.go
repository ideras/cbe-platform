package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const screenshotBytes = 16 << 20

type CapturePermission struct {
	Supported    bool   `json:"supported"`
	Granted      bool   `json:"granted"`
	TestRequired bool   `json:"test_required"`
	Message      string `json:"message"`
}
type CaptureOptions struct {
	Required bool
	// Test must validate/discard a real PNG and honor cancellation.
	Test func(context.Context) (CaptureTestResult, error)
	// Capture must honor cancellation and return an owned PNG buffer.
	Capture              func(context.Context) ([]byte, error)
	Permission           func(bool) CapturePermission
	Timeout, Lifetime    time.Duration
	Attempts, MaxPending int
	// UnboundGrace is how long the screenshots of a lock that ended are kept while the server has not named its record. They can
	// only be sent under it, so past that they are let go, rather than shown as pending for the rest of the exam.
	UnboundGrace time.Duration
}

// defaultUnboundGrace is how long the screenshots of a lock that ended wait for the server to name its record, unless the options say
// otherwise.
const defaultUnboundGrace = 20 * time.Second

func DefaultCaptureOptions() CaptureOptions {
	return CaptureOptions{Required: true, Timeout: 30 * time.Second, Lifetime: 45 * time.Second, Attempts: 3, MaxPending: 2, UnboundGrace: defaultUnboundGrace}
}

type CaptureView struct {
	Required       bool              `json:"required"`
	Ready          bool              `json:"ready"`
	Blocked        bool              `json:"blocked"`
	Testing        bool              `json:"testing"`
	PendingReports int               `json:"pending_reports"`
	ReportError    string            `json:"report_error"`
	BlockReason    string            `json:"block_reason"`
	Enabled        bool              `json:"enabled"`
	Permission     CapturePermission `json:"permission"`
	State          string            `json:"state"`
	Incident       uint64            `json:"local_incident"`
	EventID        int64             `json:"event_log_id"`
	ScreenshotID   int64             `json:"screenshot_id"`
	Pending        int               `json:"pending"`
	Failures       uint64            `json:"failures"`
	Error          string            `json:"error"`
}

// captureKey names one screenshot: the index-th capture of a focus loss.
type captureKey struct {
	sequence uint64
	index    int
}

func (k captureKey) after(other captureKey) bool {
	return k.sequence > other.sequence || k.sequence == other.sequence && k.index > other.index
}

type captureJob struct {
	key       captureKey
	incident  Incident
	data      []byte
	at        time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	expiry    *time.Timer
	uploading bool
}

// heldShot is a screenshot taken while a lock the student asked for lasts. The application holds it until the server has
// recorded the lock, which can be long after: the student asks for that lock with the server out of reach.
type heldShot struct {
	lock  uint64
	index int
	at    time.Time
	data  []byte
}
type captureManager struct {
	mu                 sync.Mutex
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	options            CaptureOptions
	http               *http.Client
	retryMin, retryMax time.Duration
	base, token        string
	wake               chan struct{}
	jobs               map[captureKey]*captureJob
	held               []*heldShot
	lockRecords        map[uint64]int64     // the record the server made of each lock, once it has answered
	lockEnds           map[uint64]time.Time // when each watched lock ended
	capturing, closed  bool
	attempted          captureKey
	started            bool
	state              CaptureView
	reports            []*captureReport
	reportWake         chan struct{}
	sessionAway        atomic.Value // what took the student's session off screen (a string), "" while it is on screen
}

func newCaptureManager(ctx context.Context, h *http.Client, options CaptureOptions, min, max time.Duration) *captureManager {
	ctx, cancel := context.WithCancel(ctx)
	m := &captureManager{ctx: ctx, cancel: cancel, http: h, options: options, retryMin: min, retryMax: max, wake: make(chan struct{}, 1), jobs: map[captureKey]*captureJob{}, lockRecords: map[uint64]int64{}, lockEnds: map[uint64]time.Time{}, reportWake: make(chan struct{}, 1), state: CaptureView{Required: options.Required, Ready: !options.Required, Enabled: options.Capture != nil, State: "idle"}}
	if options.Permission != nil {
		m.state.Permission = options.Permission(false)
	}
	m.wg.Add(2)
	go m.uploadLoop()
	go m.reportLoop()
	return m
}
func (m *captureManager) auth(base, token string) {
	m.mu.Lock()
	m.base, m.token = base, token
	if m.options.Required && m.state.Ready {
		m.reportLocked("capture_ready", "verified_before_admission")
	}
	m.mu.Unlock()
}
func (m *captureManager) view() CaptureView {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.state
	v.Pending = len(m.jobs) + len(m.held)
	v.PendingReports = len(m.reports)
	v.Blocked = v.Required && (!v.Ready || v.Testing || v.PendingReports > 0)
	if v.Blocked {
		switch {
		case v.Testing:
			v.BlockReason = "Screenshot retest required: editing is paused until a screenshot test succeeds. Current answers are preserved; the server timer continues."
		case v.PendingReports > 0:
			v.BlockReason = "Screenshot retest required: editing is paused until the server acknowledges the capture status. Existing answers continue autosaving."
		default:
			v.BlockReason = "Screenshot retest required: editing is paused until a screenshot test succeeds. Current answers are preserved and continue autosaving."
		}
	}
	if v.Permission.TestRequired && v.Ready {
		v.Permission.Granted = true
		v.Permission.Message = "Screenshot test passed. Automatic incident capture is armed; complete monitor coverage still requires instructor verification."
	}
	return v
}
func (m *captureManager) permission(request bool) {
	if m.options.Permission == nil {
		return
	}
	permission := m.options.Permission(request)
	m.mu.Lock()
	m.state.Permission = permission
	if m.options.Required && m.state.Ready && (!permission.Supported || (!permission.Granted && !permission.TestRequired)) {
		m.failLocked("Screen-recording access is unavailable. A successful retest is required; current answers are preserved.")
	}
	m.mu.Unlock()
}
func (m *captureManager) failLocked(message string) {
	m.state.Failures++
	m.state.Error = message
	if m.options.Required && m.state.Ready {
		m.state.Ready = false
		m.state.Permission.Granted = false
		m.state.Permission.Message = "Capture failed. A screenshot retest is required."
		m.reportLocked("capture_failed", message)
	}
}

// start takes the index-th screenshot of a sustained focus loss. The first one of a loss must succeed: when capture is
// busy or failing, a required retest follows. A later one is skipped when it would take the last pending slot, so it can
// never stand in the way of the first capture of a new focus loss.
func (m *captureManager) start(incident Incident, index int, current MonitoringView) {
	if !current.Active || current.Focus != "unfocused" || current.Incident == nil || current.Incident.Sequence != incident.Sequence {
		return
	}
	if current.Incident.LossEventID > 0 {
		incident.LossEventID = current.Incident.LossEventID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.state.Enabled || m.closed || m.ctx.Err() != nil || (m.options.Required && (!m.state.Ready || m.state.Testing)) {
		return
	}
	key := captureKey{incident.Sequence, index}
	if m.started && !key.after(m.attempted) {
		return
	}
	if index > 0 && (m.capturing || len(m.jobs) >= max(m.options.MaxPending-1, 1)) {
		return
	}
	m.attempted, m.started = key, true
	if m.capturing || len(m.jobs) >= m.options.MaxPending {
		m.failLocked("Capture is busy or its bounded pending queue is full. This incident has no image; current answers are preserved and required capture must be retested.")
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, m.options.Lifetime)
	job := &captureJob{key: key, incident: incident, ctx: ctx, cancel: cancel}
	m.jobs[key] = job
	m.capturing = true
	m.state.State = "capturing"
	m.state.Incident = incident.Sequence
	m.state.EventID = incident.LossEventID
	job.expiry = time.AfterFunc(m.options.Lifetime, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.jobs[key] != job {
			return
		}
		delete(m.jobs, key)
		job.data = nil
		job.cancel()
		m.state.State = "failed"
		m.state.Incident = job.incident.Sequence
		m.state.EventID = job.incident.LossEventID
		m.failLocked("Pending capture/upload expired without confirmation. Current answers are preserved; required capture must be retested.")
		signal(m.wake)
	})
	m.wg.Add(1)
	go m.capture(job)
}
func (m *captureManager) capture(job *captureJob) {
	defer m.wg.Done()
	ctx, cancel := context.WithTimeout(job.ctx, m.options.Timeout)
	defer cancel()
	away := m.awayNow() != ""
	data, err := m.options.Capture(ctx)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		data, err = prepareCapture(data, away || m.awayNow() != "")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capturing = false
	if m.closed || m.jobs[job.key] != job {
		return
	}
	if err != nil {
		delete(m.jobs, job.key)
		job.expiry.Stop()
		job.cancel()
		m.state.State = "failed"
		m.state.Incident = job.incident.Sequence
		m.state.EventID = job.incident.LossEventID
		m.failLocked("Screen capture failed. A successful retest is required before editing can resume; current answers remain preserved.")
		return
	}
	job.data = data
	job.at = time.Now().UTC()
	m.state.State = "waiting_for_event"
	m.state.Incident = job.incident.Sequence
	m.state.EventID = job.incident.LossEventID
	signal(m.wake)
}

// prepareCapture checks a captured PNG and makes the JPEG that is uploaded. offScreen says the student's session was not the one on
// screen while it was taken: an entirely black image is then a screen that could not be read (X11 gives that while another console
// has the display), and fails like any other capture that could not be taken. A session that is off screen but shows something, as
// when a remote person has taken it over, is captured as ever; with the session on screen black is a real picture.
func prepareCapture(data []byte, offScreen bool) ([]byte, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || len(data) > screenshotBytes || config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 40000000 {
		return nil, errors.New("Capture returned an invalid or oversized PNG. Notify the instructor.")
	}
	encoded, err := encodeCapture(data, offScreen)
	if errors.Is(err, errBlankCapture) {
		return nil, errBlankCapture
	}
	if err != nil {
		return nil, errors.New("The capture could not be prepared for upload. Notify the instructor.")
	}
	return encoded, nil
}

// takeLockShot takes the index-th screenshot of a lock the student asked for and holds it; limit is the most one lock keeps.
// One that cannot be taken now, because the screen is being captured already or capture is off, is skipped, not made up.
func (m *captureManager) takeLockShot(lock uint64, index, limit int) {
	m.mu.Lock()
	if !m.state.Enabled || m.closed || m.ctx.Err() != nil || m.capturing || (m.options.Required && (!m.state.Ready || m.state.Testing)) {
		m.mu.Unlock()
		return
	}
	held := 0
	for _, shot := range m.held {
		if shot.lock == lock {
			held++
		}
	}
	if held >= limit {
		m.mu.Unlock()
		return
	}
	m.capturing = true
	m.wg.Add(1)
	m.mu.Unlock()
	go m.captureLockShot(lock, index)
}
func (m *captureManager) captureLockShot(lock uint64, index int) {
	defer m.wg.Done()
	ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
	defer cancel()
	away := m.awayNow() != ""
	data, err := m.options.Capture(ctx)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		data, err = prepareCapture(data, away || m.awayNow() != "")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capturing = false
	if m.closed {
		return
	}
	if err != nil {
		// A screen that cannot be captured needs the same retest as for an absence, once the exam is unlocked.
		m.state.State = "failed"
		m.failLocked("Screen capture failed. A successful retest is required before editing can resume; current answers remain preserved.")
		return
	}
	m.held = append(m.held, &heldShot{lock: lock, index: index, at: time.Now().UTC(), data: data})
	signal(m.wake)
}

// lockEnded says that a watched lock has ended. The screenshots it has no record for are let go after a while, in case the record
// is still on its way.
func (m *captureManager) lockEnded(lock uint64) {
	m.mu.Lock()
	m.lockEnds[lock] = time.Now()
	m.mu.Unlock()
	time.AfterFunc(m.unboundGrace()+10*time.Millisecond, func() { signal(m.wake) })
}

// unboundGrace is the option, or the default for an application that does not set one.
func (m *captureManager) unboundGrace() time.Duration {
	if m.options.UnboundGrace > 0 {
		return m.options.UnboundGrace
	}
	return defaultUnboundGrace
}

// releaseUnbound lets go of the screenshots held for locks the server never named a record of: with the exam over there is no lock
// left to learn it from.
func (m *captureManager) releaseUnbound(expired func(lock uint64) bool) {
	m.held = slices.DeleteFunc(m.held, func(shot *heldShot) bool {
		if m.lockRecords[shot.lock] > 0 || !expired(shot.lock) {
			return false
		}
		shot.data = nil
		return true
	})
}

// releaseUnboundAtEnd is releaseUnbound for the end of the exam.
func (m *captureManager) releaseUnboundAtEnd() {
	m.mu.Lock()
	m.releaseUnbound(func(uint64) bool { return true })
	m.mu.Unlock()
}

// bindLock tells the manager which record the server made of a lock: from then on, the screenshots held for it are sent.
func (m *captureManager) bindLock(lock uint64, event int64) {
	if event <= 0 {
		return
	}
	m.mu.Lock()
	m.lockRecords[lock] = event
	m.mu.Unlock()
	signal(m.wake)
}

// uploadHeld sends the screenshots held for the locks the server has recorded. One the server cannot be asked about now stays
// held and is tried again soon; one the server refuses is released, since nothing the application does would change that.
func (m *captureManager) uploadHeld() {
	for {
		m.mu.Lock()
		m.releaseUnbound(func(lock uint64) bool {
			ended, ok := m.lockEnds[lock]
			return ok && time.Since(ended) >= m.unboundGrace()
		})
		var shot *heldShot
		var event int64
		for _, candidate := range m.held {
			if id := m.lockRecords[candidate.lock]; id > 0 && (shot == nil || candidate.lock < shot.lock || candidate.lock == shot.lock && candidate.index < shot.index) {
				shot, event = candidate, id
			}
		}
		base, token := m.base, m.token
		if shot == nil || m.closed || base == "" || token == "" {
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		var id int64
		var err error
		delay := m.retryMin
		for attempt := 0; attempt < m.options.Attempts; attempt++ {
			ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
			var wait time.Duration
			id, wait, err = m.upload(ctx, base, token, event, shot.index, shot.at, shot.data)
			cancel()
			if err == nil || (!retryable(err) && failureCode(err) != 425) || attempt+1 == m.options.Attempts || m.ctx.Err() != nil {
				break
			}
			select {
			case <-m.ctx.Done():
			case <-time.After(max(delay, wait)):
			}
			delay = min(delay*2, m.retryMax)
		}
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return
		}
		if err != nil && (retryable(err) || failureCode(err) == 425) {
			m.mu.Unlock()
			time.AfterFunc(m.retryMax, func() { signal(m.wake) })
			return
		}
		m.held = slices.DeleteFunc(m.held, func(other *heldShot) bool { return other == shot })
		shot.data = nil
		if err == nil {
			m.state.ScreenshotID = id
		}
		m.mu.Unlock()
	}
}
func (m *captureManager) bind(incident Incident) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobs {
		if job.key.sequence == incident.Sequence && incident.LossEventID > 0 && job.incident.LossEventID == 0 {
			job.incident.LossEventID = incident.LossEventID
			signal(m.wake)
		}
	}
}
func (m *captureManager) uploadLoop() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		}
		for {
			m.mu.Lock()
			var job *captureJob
			for _, candidate := range m.jobs {
				if len(candidate.data) > 0 && candidate.incident.LossEventID > 0 && !candidate.uploading && (job == nil || job.key.after(candidate.key)) {
					job = candidate
				}
			}
			if job == nil || m.closed {
				m.mu.Unlock()
				break
			}
			job.uploading = true
			base, token := m.base, m.token
			data := job.data
			incident := job.incident
			m.state.State = "uploading"
			m.state.Incident = incident.Sequence
			m.state.EventID = incident.LossEventID
			m.mu.Unlock()
			var id int64
			var err error
			delay := m.retryMin
			for attempt := 0; attempt < m.options.Attempts; attempt++ {
				var wait time.Duration
				id, wait, err = m.upload(job.ctx, base, token, incident.LossEventID, job.key.index, job.at, data)
				if err == nil || (!retryable(err) && failureCode(err) != 425) || attempt+1 == m.options.Attempts {
					break
				}
				timer := time.NewTimer(max(delay, wait))
				select {
				case <-job.ctx.Done():
					timer.Stop()
					err = job.ctx.Err()
				case <-timer.C:
				}
				if job.ctx.Err() != nil {
					break
				}
				delay = min(delay*2, m.retryMax)
			}
			m.mu.Lock()
			if m.jobs[job.key] == job {
				m.state.Incident = incident.Sequence
				m.state.EventID = incident.LossEventID
				delete(m.jobs, job.key)
				job.expiry.Stop()
				job.cancel()
				job.data = nil
				if err != nil && job.key.index > 0 && failureCode(err) == 403 {
					// The focus loss ended or reached the server's capture limit before this later capture: nothing failed.
					m.state.State = "uploaded"
				} else if err != nil {
					m.state.State = "failed"
					m.failLocked("Screenshot upload was not confirmed after bounded attempts. The image was released; current answers are preserved and required capture must be retested.")
				} else {
					m.state.State = "uploaded"
					m.state.ScreenshotID = id
				}
			}
			m.mu.Unlock()
		}
		m.uploadHeld()
	}
}
func (m *captureManager) upload(ctx context.Context, base, token string, event int64, index int, at time.Time, data []byte) (int64, time.Duration, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("event_log_id", strconv.FormatInt(event, 10))
	_ = writer.WriteField("capture_index", strconv.Itoa(index))
	_ = writer.WriteField("client_captured_at", at.Format(time.RFC3339Nano))
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="image"; filename="capture.jpg"`)
	header.Set("Content-Type", "image/jpeg")
	part, err := writer.CreatePart(header)
	if err != nil {
		return 0, 0, err
	}
	if _, err := part.Write(data); err != nil {
		return 0, 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/screenshot", bytes.NewReader(body.Bytes()))
	if err != nil {
		return 0, 0, &apiFailure{message: "Cannot prepare screenshot upload."}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := m.http.Do(request)
	if err != nil {
		return 0, 0, &apiFailure{message: "Screenshot upload could not be confirmed."}
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		seconds, _ := strconv.Atoi(response.Header.Get("Retry-After"))
		wait := time.Duration(max(0, min(seconds, 3600))) * time.Second
		return 0, wait, &apiFailure{code: response.StatusCode, message: fmt.Sprintf("Screenshot upload rejected (%d).", response.StatusCode)}
	}
	var reply struct {
		ID    int64     `json:"screenshot_id"`
		Event int64     `json:"event_log_id"`
		At    time.Time `json:"captured_at"`
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(encoded) > 8192 || json.Unmarshal(encoded, &reply) != nil || reply.ID <= 0 || reply.Event != event || reply.At.IsZero() {
		return 0, 0, &apiFailure{message: "Screenshot acknowledgment is invalid."}
	}
	return reply.ID, 0, nil
}
func (m *captureManager) close() {
	m.mu.Lock()
	m.closed = true
	m.state.State = "stopped"
	m.cancel()
	for _, job := range m.jobs {
		job.expiry.Stop()
		job.cancel()
		job.data = nil
	}
	m.jobs = map[captureKey]*captureJob{}
	m.held = nil
	m.mu.Unlock()
	m.wg.Wait()
}
func (s *Service) CheckCapturePermission() View {
	if !s.loginMu.TryLock() {
		return s.Snapshot()
	}
	defer s.loginMu.Unlock()
	s.mu.Lock()
	allowed := s.token == ""
	s.mu.Unlock()
	if allowed {
		s.capture.permission(true)
	}
	return s.Snapshot()
}
