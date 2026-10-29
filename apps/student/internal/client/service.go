package client

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"

	"exam-platform/shared/calc"
)

type Options struct {
	Server                                                       string
	OnCompletion, OnClose                                        func()
	Monitoring                                                   MonitorOptions
	Screenshots                                                  CaptureOptions
	Sound                                                        SoundOptions
	Debounce, Periodic, Poll, RequestTimeout, RetryMin, RetryMax time.Duration
	HTTPClient                                                   *http.Client
}

func DefaultOptions() Options {
	return Options{Screenshots: DefaultCaptureOptions(), Monitoring: DefaultMonitorOptions(), Sound: DefaultSoundOptions(), Server: "http://127.0.0.1:8080", Debounce: 2 * time.Second, Periodic: 20 * time.Second, Poll: 3 * time.Second, RequestTimeout: 5 * time.Second, RetryMin: time.Second, RetryMax: 8 * time.Second}
}

type View struct {
	RulesAccepted        bool              `json:"rules_accepted"`
	Monitoring           MonitoringView    `json:"monitoring"`
	Capture              CaptureView       `json:"capture"`
	Sound                SoundReport       `json:"sound"`
	SoundNotice          *SoundNotice      `json:"sound_notice,omitempty"`
	Observation          uint64            `json:"observation"`
	Phase                string            `json:"phase"`
	Server               string            `json:"server"`
	Account              string            `json:"account"`
	Exam                 *Exam             `json:"exam,omitempty"`
	Answers              map[string]string `json:"answers,omitempty"`
	Revision             int64             `json:"revision"`
	AcknowledgedRevision int64             `json:"acknowledged_revision"`
	Saved                *Saved            `json:"saved,omitempty"`
	Status               *Status           `json:"status,omitempty"`
	TimingStale          bool              `json:"timing_stale"`
	SaveState            string            `json:"save_state"`
	SaveError            string            `json:"save_error"`
	StatusError          string            `json:"status_error"`
	Submitting           bool              `json:"submitting"`
	SubmitError          string            `json:"submit_error"`
	// Exercise is the exercise a forward-only student is on; Advancing means a move to the next one awaits the server.
	Exercise     int    `json:"exercise"`
	Advancing    bool   `json:"advancing"`
	AdvanceError string `json:"advance_error"`
	Final        *Final `json:"final,omitempty"`
	// Locked says that the exam is locked, since when and why; absent while it is not.
	Locked *Lock `json:"locked,omitempty"`
	// UnlockNote says that a proctor unlocked the exam; it is shown once, with the time given back.
	UnlockNote   string `json:"unlock_note,omitempty"`
	AddedSeconds int64  `json:"added_seconds,omitempty"`
}
type snapshot struct {
	answers  map[string]string
	revision int64
}

// advance is a forward-only student's request to leave exercise from, with the answers to save on the way.
type advance struct {
	snapshot
	from int
}
type Service struct {
	mu                                      sync.Mutex
	loginMu                                 sync.Mutex
	options                                 Options
	ctx                                     context.Context
	cancel                                  context.CancelFunc
	wg                                      sync.WaitGroup
	http                                    *http.Client
	wake, pollNow                           chan struct{}
	phase, server, account, token           string
	exam                                    *Exam
	answers                                 map[string]string
	revision, acknowledged, blockedRevision int64
	observation                             uint64
	saved                                   *Saved
	status                                  *Status
	stale                                   bool
	statusError, saveError, submitError     string
	advanceError                            string
	inflight                                bool
	pendingSubmit                           *snapshot
	pendingAdvance                          *advance
	exercise                                int
	final                                   *Final
	// locked is the server's lock of the exam; the monitor knows the application's own lock as well (examLocked).
	locked       *Lock
	unlockNote   string
	addedSeconds int64
	// loadAfterUnlock says that a proctor unlocked an exam the application never had the questions of: it started on the
	// lock screen. The poll loads the exam until it succeeds, and the next snapshot hands it to the window (handOver).
	loadAfterUnlock, handOver      bool
	lastEdit, lastAttempt, retryAt time.Time
	backoff                        time.Duration
	monitor                        *monitor
	capture                        *captureManager
	sound                          *soundManager
	// calcAns is the scientific panel's last result, a number, a vector or a matrix, in memory only (D10); the
	// panel serves scientific and neural mode (Phase 7). It is cleared when the exam ends.
	calcAns calc.Quantity
	// calcAnsProgrammer is the programmer mode's ans with its kind: an integer's bits or a real's exact value; a
	// float64 cannot carry either exactly.
	calcAnsProgrammer calc.Value
	// tools is the calculator's own event queue (D12), separate from the monitor's focus queue.
	tools              *toolQueue
	completionNotified bool
	closeRequested     bool
	rulesAccepted      bool
}

func New(options Options) (*Service, error) {
	server, err := validateServer(options.Server)
	if err != nil {
		return nil, err
	}
	if options.Debounce <= 0 || options.Periodic <= 0 || options.Poll <= 0 || options.RequestTimeout <= 0 || options.RetryMin <= 0 || options.RetryMax < options.RetryMin {
		return nil, errors.New("Client intervals must be positive and retry bounds ordered.")
	}
	if err := options.Monitoring.validate(); err != nil {
		return nil, err
	}
	if err := options.Sound.validate(); err != nil {
		return nil, err
	}
	if options.Screenshots.Required && (options.Screenshots.Capture == nil || options.Screenshots.Test == nil || options.Screenshots.Permission == nil) {
		return nil, errors.New("Required screenshot verification and capture adapters must be configured.")
	}
	if options.Screenshots.Capture != nil && (options.Screenshots.Timeout <= 0 || options.Screenshots.Lifetime < options.Screenshots.Timeout || options.Screenshots.Attempts < 1 || options.Screenshots.Attempts > 3 || options.Screenshots.MaxPending < 1 || options.Screenshots.MaxPending > 2) {
		return nil, errors.New("Capture intervals/attempts/queue limits are invalid.")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{options: options, ctx: ctx, cancel: cancel, server: server, phase: "login", stale: true, wake: make(chan struct{}, 1), pollNow: make(chan struct{}, 1), blockedRevision: -1}
	s.http = options.HTTPClient
	if s.http == nil {
		s.http = newHTTPClient(options.RequestTimeout)
	}
	s.capture = newCaptureManager(s.ctx, s.http, options.Screenshots, options.RetryMin, options.RetryMax)
	// The student's own sound settings are recorded here, before anything can change them.
	s.sound = newSoundManager(options.Sound)
	s.sound.check()
	monitoring := options.Monitoring
	monitoring.Sound = s.sound.report
	monitoring.SoundCheck = s.sound.refresh
	ack, captured := monitoring.OnLossAcknowledged, monitoring.OnCapture
	monitoring.OnCapture = func(i Incident, index int) {
		s.capture.start(i, index, s.monitor.view())
		if captured != nil {
			captured(i, index)
		}
	}
	monitoring.OnLossAcknowledged = func(i Incident) {
		s.capture.bind(i)
		if ack != nil {
			ack(i)
		}
	}
	s.monitor = newMonitor(s.ctx, s.http, monitoring, options.RetryMin, options.RetryMax, s.pollNow)
	s.tools = newToolQueue(s.ctx, s.http, options.RetryMin, options.RetryMax)
	s.wg.Add(4)
	go s.writeLoop()
	go s.pollLoop()
	go func() { defer s.wg.Done(); s.tools.run() }()
	go func() { defer s.wg.Done(); s.sound.run(s.ctx) }()
	return s, nil
}
func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	context.AfterFunc(ctx, s.cancel)
	return nil
}
func (s *Service) ServiceShutdown() error {
	_ = s.sound.restore()
	s.monitor.close()
	s.capture.close()
	s.cancel()
	s.wg.Wait()
	s.http.CloseIdleConnections()
	return nil
}
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
func (s *Service) viewLocked(loaded bool) View {
	s.observation++
	v := View{Capture: s.capture.view(), Sound: s.sound.report(), SoundNotice: s.sound.latestNotice(), RulesAccepted: s.rulesAccepted, Monitoring: s.monitor.view(), Observation: s.observation, Phase: s.phase, Server: s.server, Account: s.account, Revision: s.revision, AcknowledgedRevision: s.acknowledged, TimingStale: s.stale, SaveError: s.saveError, StatusError: s.statusError, SubmitError: s.submitError, Submitting: s.pendingSubmit != nil, Exercise: s.exercise, Advancing: s.pendingAdvance != nil, AdvanceError: s.advanceError, SaveState: "not_saved", UnlockNote: s.unlockNote, AddedSeconds: s.addedSeconds}
	if s.saved != nil {
		copy := *s.saved
		v.Saved = &copy
		v.SaveState = "saved"
	}
	if s.revision > s.acknowledged {
		v.SaveState = "pending"
	}
	if s.saveError != "" {
		v.SaveState = "failed"
	}
	if s.inflight {
		v.SaveState = "saving"
	}
	if s.status != nil {
		copy := *s.status
		v.Status = &copy
	}
	if s.final != nil {
		copy := *s.final
		v.Final = &copy
	}
	if s.phase != "complete" {
		// The lock screen shows the server's lock, or the application's own while the server has not reported it yet.
		if s.locked != nil {
			copy := *s.locked
			v.Locked = &copy
		} else if v.Monitoring.Locked {
			v.Locked = &Lock{Reason: v.Monitoring.LockReason}
			if v.Monitoring.LockSince != nil {
				v.Locked.Since = *v.Monitoring.LockSince
			}
		}
	}
	if v.Locked != nil {
		// The note of an earlier unlock is over once the exam is locked again.
		v.UnlockNote, v.AddedSeconds = "", 0
	}
	if loaded && s.exam != nil {
		// The exam never changes once loaded, so the copy may share its content.
		copy := *s.exam
		v.Exam = &copy
		v.Answers = maps.Clone(s.answers)
	}
	return v
}
func (s *Service) Snapshot() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	view := s.viewLocked(s.handOver)
	s.handOver = false
	return view
}

// examLocked says whether the exam is locked: by the server, or by the application itself before the server knows.
func (s *Service) examLocked() bool { return s.locked != nil || s.monitor.isLocked() }
func (s *Service) callContext(ctx context.Context) (context.Context, func()) {
	request, cancel := context.WithCancel(s.ctx)
	stop := context.AfterFunc(ctx, cancel)
	if ctx.Err() != nil {
		cancel()
	}
	return request, func() { stop(); cancel() }
}
func (s *Service) Login(ctx context.Context, server, account, pin string) (View, error) {
	if !s.loginMu.TryLock() {
		return View{}, errors.New("A login or load is already running.")
	}
	defer s.loginMu.Unlock()
	base, err := validateServer(server)
	if err != nil {
		return View{}, err
	}
	account = strings.TrimSpace(account)
	if account == "" || strings.TrimSpace(pin) == "" || !utf8.ValidString(account) || !utf8.ValidString(pin) {
		return View{}, errors.New("Account number and exam PIN are required.")
	}
	s.mu.Lock()
	if !s.rulesAccepted {
		s.mu.Unlock()
		return View{}, errors.New("Read and acknowledge the monitoring disclosure before login.")
	}
	if s.token != "" {
		s.mu.Unlock()
		return View{}, errors.New("A session is already open. Retry loading it instead of logging in again.")
	}
	s.mu.Unlock()
	request, cancel := s.callContext(ctx)
	defer cancel()
	// A real screenshot is taken before the server is asked for a session: no deadline starts while the student is still
	// answering the system's capture permission dialog. Neither the service lock nor the monitor's is held meanwhile.
	if s.capture.options.Required {
		if err := s.capture.admit(request); err != nil {
			return View{}, err
		}
	}
	var reply loginReply
	err = requestJSON(request, s.http, base, "", http.MethodPost, "/login", struct {
		Account string `json:"account_number"`
		PIN     string `json:"exam_pin"`
	}{account, pin}, &reply)
	if err != nil {
		return View{}, err
	}
	if reply.Token == "" || reply.SessionID == "" || reply.DeadlineAt.IsZero() || reply.ServerTime.IsZero() {
		return View{}, errors.New("Login response is invalid. Retry login to resume; the server may have started the session.")
	}
	s.mu.Lock()
	s.server, s.account, s.token, s.phase = base, account, reply.Token, "loading"
	s.mu.Unlock()
	s.capture.auth(base, reply.Token)
	if s.options.Monitoring.OnParticipation != nil {
		s.options.Monitoring.OnParticipation()
	}
	if reply.MinVolumePercent != nil {
		s.sound.activate(*reply.MinVolumePercent)
	}
	s.monitor.start(base, reply.Token, reply.Policy)
	s.tools.start(base, reply.Token)
	s.mu.Lock()
	s.locked = reply.Locked
	s.monitor.setServerLock(reply.Locked)
	s.mu.Unlock()
	return s.load(request)
}
func (s *Service) RetryLoad(ctx context.Context) (View, error) {
	if !s.loginMu.TryLock() {
		return View{}, errors.New("A login or load is already running.")
	}
	defer s.loginMu.Unlock()
	s.mu.Lock()
	ready := s.phase == "loading" && s.token != ""
	s.mu.Unlock()
	if !ready {
		return View{}, errors.New("No pending session to load.")
	}
	request, cancel := s.callContext(ctx)
	defer cancel()
	return s.load(request)
}
func (s *Service) load(ctx context.Context) (View, error) {
	defer s.notifyCompletion()
	s.mu.Lock()
	base, token := s.server, s.token
	s.mu.Unlock()
	var reply examReply
	examErr := requestJSON(ctx, s.http, base, token, http.MethodGet, "/exam", nil, &reply)
	var status Status
	statusErr := requestJSON(ctx, s.http, base, token, http.MethodGet, "/session-status", nil, &status)
	if statusErr == nil {
		statusErr = validateStatus(status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if statusErr != nil {
		s.stale = true
		s.statusError = statusErr.Error()
		return s.viewLocked(true), statusErr
	}
	s.status = &status
	s.monitor.setPolicy(status.Policy)
	if finalStatus(status.State) {
		s.monitor.stop("Session finalized; ordinary telemetry stopped.")
	}
	s.stale = false
	s.statusError = ""
	s.locked = status.Locked
	s.monitor.setServerLock(status.Locked)
	if examErr != nil {
		if finalStatus(status.State) {
			s.phase = "complete"
			s.calcAns, s.calcAnsProgrammer = calc.Quantity{}, calc.Value{} // the exam ended: the calculator's ans goes with it (D11)
			return s.viewLocked(true), nil
		}
		if failureCode(examErr) == 423 {
			// The exam is locked: the questions are not sent while it is, and the lock screen takes their place.
			if s.locked == nil {
				// Locked between the two requests; the next status says since when and why.
				s.locked = &Lock{Since: time.Now()}
			}
			s.phase = "locked"
			return s.viewLocked(true), nil
		}
		return s.viewLocked(true), examErr
	}
	exercises := len(reply.Content.Exercises)
	if reply.ID == "" || exercises == 0 || reply.Answers == nil || reply.Navigation != "free" && reply.Navigation != "forward_only" || reply.CurrentExercise < 0 || reply.CurrentExercise >= exercises {
		return s.viewLocked(true), errors.New("Exam response is invalid. Retry loading the session.")
	}
	if reply.Latest != nil && (reply.Latest.Sequence <= 0 || reply.Latest.SavedAt.IsZero()) {
		return s.viewLocked(true), errors.New("Saved version metadata is invalid.")
	}
	answers := map[string]string{}
	for _, exercise := range reply.Content.Exercises {
		slots := slices.Clone(exercise.Slots)
		for _, item := range exercise.Items {
			slots = append(slots, item.Slots...)
		}
		for _, slot := range slots {
			if slot.ID == "" {
				return s.viewLocked(true), errors.New("Exam answer field identity is invalid.")
			}
			if _, duplicate := answers[slot.ID]; duplicate {
				return s.viewLocked(true), errors.New("Exam answer field identity is duplicated.")
			}
			answers[slot.ID] = reply.Answers[slot.ID]
		}
	}
	s.exam = &reply.Exam
	s.answers = answers
	s.saved = reply.Latest
	s.exercise = max(s.exercise, reply.CurrentExercise)
	s.phase = "exam"
	s.lastAttempt = time.Now()
	if finalStatus(status.State) {
		s.phase = "complete"
		s.calcAns, s.calcAnsProgrammer = calc.Quantity{}, calc.Value{} // the exam ended: the calculator's ans goes with it (D11)
	}
	if s.locked != nil {
		s.phase = "locked"
	}
	signal(s.pollNow)
	return s.viewLocked(true), nil
}
func (s *Service) checkAnswers(answers map[string]string) (map[string]string, error) {
	if s.exam == nil || answers == nil {
		return nil, errors.New("Exam answers are not loaded.")
	}
	slots := s.exam.slotExercises()
	copy := make(map[string]string, len(slots))
	for id := range slots {
		copy[id] = answers[id]
	}
	for id, text := range answers {
		if _, ok := copy[id]; !ok || !utf8.ValidString(text) {
			return nil, errors.New("Answer identity or text is invalid.")
		}
	}
	return copy, nil
}
func (s *Service) UpdateAnswers(answers map[string]string, revision int64) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.examLocked() {
		return s.viewLocked(false), errors.New("Your exam is locked. Call your instructor.")
	}
	if s.phase != "exam" || s.pendingSubmit != nil || s.pendingAdvance != nil {
		return s.viewLocked(false), errors.New("Editing is locked while submitted, or while submitting or moving on is pending.")
	}
	if revision < 1 || revision > 9007199254740991 {
		return s.viewLocked(false), errors.New("Editor revision is invalid.")
	}
	if revision <= s.revision {
		return s.viewLocked(false), nil
	}
	copy, err := s.checkAnswers(answers)
	if err != nil {
		return s.viewLocked(false), err
	}
	s.answers, s.revision, s.lastEdit = copy, revision, time.Now()
	s.saveError = ""
	s.blockedRevision = -1
	signal(s.wake)
	return s.viewLocked(false), nil
}
func (s *Service) Submit(answers map[string]string, revision int64) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingSubmit != nil {
		return s.viewLocked(false), nil
	}
	if s.examLocked() {
		return s.viewLocked(false), errors.New("Your exam is locked. Call your instructor.")
	}
	if s.phase != "exam" || s.pendingAdvance != nil || revision < s.revision || revision < 0 || revision > 9007199254740991 {
		return s.viewLocked(false), errors.New("Submission is locked or the editor snapshot is stale.")
	}
	copy, err := s.checkAnswers(answers)
	if err != nil {
		return s.viewLocked(false), err
	}
	s.answers, s.revision = copy, revision
	s.pendingSubmit = &snapshot{maps.Clone(copy), revision}
	s.retryAt = time.Time{}
	s.submitError = ""
	s.saveError = ""
	s.backoff = 0
	signal(s.wake)
	return s.viewLocked(false), nil
}

// Advance saves the answers and moves a forward-only student from exercise from to the next one. Once the server confirms,
// the student cannot return. Editing is locked until then; a request that cannot be confirmed is retried with the same
// snapshot, which the server accepts only once.
func (s *Service) Advance(answers map[string]string, revision int64, from int) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingAdvance != nil {
		return s.viewLocked(false), nil
	}
	if s.examLocked() {
		return s.viewLocked(false), errors.New("Your exam is locked. Call your instructor.")
	}
	if s.phase != "exam" || s.pendingSubmit != nil || s.exam == nil || !s.exam.forwardOnly() {
		return s.viewLocked(false), errors.New("Moving to the next exercise is not available now.")
	}
	if from != s.exercise || from >= len(s.exam.Content.Exercises)-1 {
		return s.viewLocked(false), errors.New("The exam is no longer on that exercise.")
	}
	if revision < s.revision || revision < 0 || revision > 9007199254740991 {
		return s.viewLocked(false), errors.New("The editor snapshot is stale.")
	}
	copy, err := s.checkAnswers(answers)
	if err != nil {
		return s.viewLocked(false), err
	}
	s.answers, s.revision = copy, revision
	s.pendingAdvance = &advance{snapshot{maps.Clone(copy), revision}, from}
	s.retryAt = time.Time{}
	s.advanceError = ""
	s.saveError = ""
	s.backoff = 0
	signal(s.wake)
	return s.viewLocked(false), nil
}
func (s *Service) Retry() {
	s.mu.Lock()
	s.retryAt = time.Now()
	s.blockedRevision = -1
	s.mu.Unlock()
	signal(s.wake)
	signal(s.pollNow)
}

func (s *Service) writeLoop() {
	defer s.wg.Done()
	interval := min(s.options.Debounce/4, 100*time.Millisecond)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		s.writeOnce()
	}
}
func (s *Service) writeOnce() {
	defer s.notifyCompletion()
	s.mu.Lock()
	now := time.Now()
	if s.token == "" || s.exam == nil || s.phase == "loading" || !s.retryAt.IsZero() && now.Before(s.retryAt) {
		s.mu.Unlock()
		return
	}
	submit := s.pendingSubmit != nil
	moving := !submit && s.pendingAdvance != nil && s.phase == "exam"
	if !submit && !moving && (s.phase != "exam" || s.revision <= s.acknowledged || s.revision == s.blockedRevision || now.Sub(s.lastEdit) < s.options.Debounce && now.Sub(s.lastAttempt) < s.options.Periodic && s.retryAt.IsZero()) {
		s.mu.Unlock()
		return
	}
	work := snapshot{maps.Clone(s.answers), s.revision}
	from := 0
	if submit {
		work = *s.pendingSubmit
	} else if moving {
		work, from = s.pendingAdvance.snapshot, s.pendingAdvance.from
	}
	base, token := s.server, s.token
	s.inflight = true
	s.lastAttempt = now
	s.mu.Unlock()
	path := "/autosave"
	var input, output any = struct {
		Answers map[string]string `json:"answers"`
	}{work.answers}, nil
	var saved Saved
	var final Final
	var moved advanceReply
	switch {
	case submit:
		path = "/submit"
		output = &final
	case moving:
		path = "/advance"
		input = struct {
			Answers map[string]string `json:"answers"`
			From    int               `json:"from_exercise"`
		}{work.answers, from}
		output = &moved
	default:
		output = &saved
	}
	err := requestJSON(s.ctx, s.http, base, token, http.MethodPost, path, input, output)
	if err == nil {
		switch {
		case submit && (final.VersionID <= 0 || final.Sequence <= 0 || final.SubmittedAt.IsZero() || !finalStatus(final.Status)),
			moving && (moved.CurrentExercise != from+1 || moved.Sequence <= 0 || moved.SavedAt.IsZero()),
			!submit && !moving && (saved.Sequence <= 0 || saved.SavedAt.IsZero()):
			err = &apiFailure{message: "Write response is invalid. Acknowledgment is unavailable."}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight = false
	if err != nil {
		if failureCode(err) == 423 {
			// The exam is locked: nothing is lost. The answers stay and are sent once a proctor unlocks the exam; the next
			// status says since when and why.
			if s.locked == nil {
				s.locked = &Lock{Since: time.Now()}
			}
			s.phase = "locked"
			s.retryAt = time.Now().Add(s.options.Poll)
			signal(s.pollNow)
			return
		}
		switch {
		case submit:
			s.submitError = err.Error()
		case moving:
			s.advanceError = err.Error()
		default:
			s.saveError = err.Error()
		}
		if retryable(err) {
			s.backoff = min(max(s.backoff*2, s.options.RetryMin), s.options.RetryMax)
			s.retryAt = time.Now().Add(s.backoff)
		} else {
			s.retryAt = time.Time{}
			switch {
			case submit:
				s.blockedRevision = work.revision
				s.pendingSubmit = nil
			case moving:
				// The server refused to move on (it may already have the student elsewhere); editing resumes and the next
				// status poll brings the exercise up to date.
				s.pendingAdvance = nil
				signal(s.pollNow)
			default:
				s.blockedRevision = work.revision
			}
		}
		if failureCode(err) == 403 || failureCode(err) == 401 {
			s.stale = true
			signal(s.pollNow)
		}
		return
	}
	s.retryAt = time.Time{}
	s.backoff = 0
	s.saveError = ""
	s.submitError = ""
	s.advanceError = ""
	if submit {
		s.final = &final
		s.monitor.stop("Session finalized; ordinary telemetry stopped.")
		s.phase = "complete"
		s.calcAns, s.calcAnsProgrammer = calc.Quantity{}, calc.Value{} // the exam ended: the calculator's ans goes with it (D11)
		s.pendingSubmit = nil
		// A final response may select earlier expiry/proctor work, not this local snapshot.
		signal(s.pollNow)
	} else if moving {
		s.saved = &Saved{Sequence: moved.Sequence, SavedAt: moved.SavedAt}
		s.acknowledged = max(s.acknowledged, work.revision)
		s.exercise = max(s.exercise, moved.CurrentExercise)
		s.pendingAdvance = nil
	} else {
		s.saved = &saved
		s.acknowledged = max(s.acknowledged, work.revision)
	}
}
func (s *Service) pollLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.options.Poll)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.pollNow:
		case <-ticker.C:
		}
		s.pollOnce()
	}
}
func (s *Service) pollOnce() {
	defer s.notifyCompletion()
	s.mu.Lock()
	if s.phase != "loading" {
		// A load by hand already brought the exam back.
		s.loadAfterUnlock = false
	}
	if s.token == "" || s.phase == "loading" && !s.loadAfterUnlock {
		s.mu.Unlock()
		return
	}
	if s.loadAfterUnlock {
		s.mu.Unlock()
		s.loadUnlockedExam()
		return
	}
	base, token := s.server, s.token
	checkCapture := s.phase != "complete" && s.options.Screenshots.Required
	s.mu.Unlock()
	if checkCapture {
		s.capture.permission(false)
	}
	var status Status
	err := requestJSON(s.ctx, s.http, base, token, http.MethodGet, "/session-status", nil, &status)
	if err == nil {
		err = validateStatus(status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.stale = true
		s.statusError = err.Error()
		return
	}
	if s.phase == "complete" && !finalStatus(status.State) {
		return
	}
	previousDeadline := time.Time{}
	if s.status != nil {
		previousDeadline = s.status.DeadlineAt
	}
	s.status = &status
	s.monitor.setPolicy(status.Policy)
	s.stale = false
	s.statusError = ""
	// The server only ever moves a student forward, so a status read before an advance was confirmed cannot move them back.
	if s.exam != nil && s.exam.forwardOnly() && status.CurrentExercise < len(s.exam.Content.Exercises) {
		s.exercise = max(s.exercise, status.CurrentExercise)
	}
	switch {
	case finalStatus(status.State):
		// The end of the exam ends its lock too: a time that ran out while the exam was locked is no unlock.
		s.locked = nil
		s.monitor.stop("Session finalized; ordinary telemetry stopped.")
		s.phase = "complete"
		s.calcAns, s.calcAnsProgrammer = calc.Quantity{}, calc.Value{} // the exam ended: the calculator's ans goes with it (D11)
		signal(s.wake)
	case status.Locked != nil:
		if s.locked == nil {
			s.unlockNote, s.addedSeconds = "", 0
		}
		s.locked = status.Locked
		s.monitor.setServerLock(status.Locked)
		s.phase = "locked"
	default:
		s.monitor.setServerLock(nil)
		if s.locked == nil {
			return
		}
		// A proctor unlocked the exam. The time given back is how far the deadline moved.
		s.locked = nil
		s.unlockNote, s.addedSeconds = "Unlocked by your instructor", 0
		if !previousDeadline.IsZero() {
			s.addedSeconds = int64(status.DeadlineAt.Sub(previousDeadline) / time.Second)
		}
		if s.exam == nil {
			// The application started on the lock screen and never had the questions: the server sends them now.
			s.phase, s.loadAfterUnlock = "loading", true
			signal(s.pollNow)
			return
		}
		// The questions and the unsaved answers were kept: the exam comes back where the student was.
		s.phase = "exam"
		signal(s.wake)
	}
}

// loadUnlockedExam loads the exam after a proctor unlocked it, for an application that started on the lock screen. A failure
// is retried on the next poll, and the student can retry it by hand.
func (s *Service) loadUnlockedExam() {
	if !s.loginMu.TryLock() {
		return
	}
	defer s.loginMu.Unlock()
	request, cancel := s.callContext(s.ctx)
	defer cancel()
	s.load(request)
	s.mu.Lock()
	s.loadAfterUnlock = s.phase == "loading"
	s.handOver = s.exam != nil
	s.mu.Unlock()
}
