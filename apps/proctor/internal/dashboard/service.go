package dashboard

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"exam-platform/shared/clientwire"
	"exam-platform/shared/examdoc"
)

type Options struct {
	Server string
	// ServerSet means Server was given on purpose (a command-line flag), so the dashboard connects to it at startup and
	// it wins over the address remembered last time.
	ServerSet bool
	Token     string
	// ExamID is monitored right after connecting; empty monitors the published exam, if any.
	ExamID  string
	Poll    time.Duration
	Timeout time.Duration
	// Choose asks the operator for a local file and returns its path, or "" when they cancel.
	Choose func(FileDialog) (string, error)
	// TokenFile is the path of the credential file given at launch (the token itself is Token); it only tells the token
	// file dialog which folder to open in.
	TokenFile string
	// ConfigFile is where the dashboard remembers the server address and the last token file; empty remembers
	// nothing.
	ConfigFile string
	// Quit closes the application when the operator asks for it.
	Quit func()
	// OpenWindow shows a page of the dashboard in a window of its own (see WindowRequest).
	OpenWindow func(WindowRequest) error
	// Save asks the operator where to save a file and returns the path, or "" when they cancel.
	Save func(SaveDialog) (string, error)
	// ChooseFolder asks the operator for a folder and returns its path, or "" when they cancel.
	ChooseFolder func(FolderDialog) (string, error)
	// OpenFolder opens a folder in the system's file manager and does not wait for it.
	OpenFolder func(path string) error
}

// FolderDialog describes a native folder dialog.
type FolderDialog struct {
	Title, Directory string
}

// FileDialog describes a native file dialog. An empty Pattern allows any file; Directory, when set, is the folder it opens in.
type FileDialog struct {
	Title, FilterName, Pattern, Directory string
}

func DefaultOptions() Options {
	return Options{Server: "http://127.0.0.1:8080", Poll: 2 * time.Second, Timeout: 5 * time.Second}
}

type View struct {
	Observation         uint64 `json:"observation"`
	Server              string `json:"server"`
	ExamID              string `json:"exam_id"`
	CredentialAvailable bool   `json:"credential_available"`
	// Connecting is true while a connection attempt (including the one at startup) is waiting for the server's answer.
	Connecting bool `json:"connecting"`
	// ConnectError says why the last connection attempt failed; empty after a success or before any attempt.
	ConnectError string `json:"connect_error"`
	// Unreachable is true while an established connection gets no answer from the server (it stopped, or the network did).
	Unreachable bool `json:"unreachable"`
	// KnownServers are addresses worth offering in the connection dialog: the current one and the remembered one.
	KnownServers []string `json:"known_servers"`
	// CredentialFile is the folder and name (never the token) of the file the proctor chose in the window, once Connect will use it or
	// is using it; empty when the credential came from the launch option.
	CredentialFile string `json:"credential_file"`
	// Notice says why remembered settings were not used or saved; empty when there is nothing to report.
	Notice      string     `json:"notice"`
	Connected   bool       `json:"connected"`
	Loaded      bool       `json:"loaded"`
	Loading     bool       `json:"loading"`
	Stale       bool       `json:"stale"`
	Busy        bool       `json:"busy"`
	Error       string     `json:"error"`
	LastSuccess *time.Time `json:"last_success"`
	Roster      Roster     `json:"roster"`
	// Catalog is every exam the server stores; CatalogLoaded is false until the first answer.
	Catalog       Catalog `json:"catalog"`
	CatalogLoaded bool    `json:"catalog_loaded"`
	// Classes are the classes stored on the server; ClassesLoaded is false until the first answer.
	Classes       []ClassEntry `json:"classes"`
	ClassesLoaded bool         `json:"classes_loaded"`
}
type Service struct {
	mu         sync.Mutex
	opts       Options
	client     *http.Client
	ctx        context.Context
	cancel     context.CancelFunc
	connection context.Context
	disconnect context.CancelFunc
	wake       chan struct{}
	images     chan struct{}
	wg         sync.WaitGroup
	gen        uint64
	epoch      uint64
	// link counts connections. Review and image windows depend only on it, not on which exam is monitored.
	link    uint64
	stopped bool
	token   string
	view    View
	// pendingCredential is the token read from the file the proctor chose; the next Connect uses it. credentialFile names the
	// file behind the token of the live connection.
	pendingCredential *chosenCredential
	credentialFile    string
	// config is what is remembered between runs. tokenPath is the token file the dialog last opened in.
	config     Config
	tokenPath  string
	configNote string
	// preview is the calculator state of the exam the preview dialog shows (owner decision 2026-10-23); nil
	// when nothing is previewed or the exam has no calculator.
	preview   *previewCalculatorState
	tokenNote string
	// autoSelect picks the published exam once the catalog arrives, until the operator picks one.
	autoSelect bool
	// picking is set while a native file dialog is open; pendingExam and pendingClass hold what a preview showed. The exam was
	// previewed for the class pendingExamClass, which is the one it is imported into.
	picking          bool
	pendingExam      *examdoc.Exam
	pendingExamClass string
	pendingClass     *pendingFile
	// pendingRoster is the file previewed as the new roster of the existing class pendingRosterClass.
	pendingRoster      *pendingFile
	pendingRosterClass string
	// pendingExamSource is the Markdown the previewed exam was compiled from; ImportExam sends it to the server.
	pendingExamSource []byte
	// downloadParent is the parent folder the proctor chose for the output folder; downloadFolder is the folder of the last
	// download that wrote at least one student's file, which OpenDownloadFolder opens. downloadProgress is read while a
	// download runs.
	downloadParent   string
	downloadFolder   string
	downloadProgress DownloadProgress
	// autoConnect is set when the dashboard knows both a server and a token at startup and should connect on its own.
	autoConnect bool
}

func New(opts Options) (*Service, error) {
	server, err := clientwire.ValidateServer(opts.Server)
	if err != nil {
		return nil, err
	}
	if opts.Poll <= 0 || opts.Timeout <= 0 {
		return nil, errors.New("Poll and request timeout must be positive.")
	}
	if opts.Token != "" && !validToken(opts.Token) {
		return nil, errors.New("Invalid proctor credential.")
	}
	if opts.ExamID != "" && !validID(opts.ExamID) {
		return nil, errors.New("Exam ID must be a canonical UUID.")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{opts: opts, client: clientwire.NewHTTPClient(opts.Timeout), ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), images: make(chan struct{}, 2)}
	s.view = View{Server: server, ExamID: opts.ExamID, CredentialAvailable: opts.Token != "", Stale: true}
	s.loadRemembered()
	s.wg.Add(1)
	go s.poll()
	if s.autoConnect {
		server := s.view.Server
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			_ = s.connect(server) // a failure is shown in the window; the operator can retry from the dialog
		}()
	}
	return s, nil
}
func validToken(value string) bool {
	return len(value) >= 32 && len(value) <= 4096 && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}
func (s *Service) ServiceShutdown() {
	s.mu.Lock()
	s.stopped = true
	s.cancel()
	if s.disconnect != nil {
		s.disconnect()
	}
	s.token, s.credentialFile, s.pendingCredential = "", "", nil
	s.opts.Token = ""
	s.pendingExam, s.pendingExamClass, s.pendingClass, s.pendingRoster, s.pendingRosterClass = nil, "", nil, nil, ""
	s.pendingExamSource, s.downloadParent, s.downloadFolder = nil, "", ""
	s.downloadProgress = DownloadProgress{}
	s.view.Connected = false
	s.view.Stale = true
	s.view.CredentialAvailable = false
	s.mu.Unlock()
	s.wg.Wait()
	s.client.CloseIdleConnections()
}
func (s *Service) Snapshot() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view.Observation++
	v := s.view
	v.CredentialFile = s.credentialFile
	if s.pendingCredential != nil {
		v.CredentialFile = s.pendingCredential.name
	}
	v.Notice = strings.TrimSpace(s.tokenNote + " " + s.configNote)
	v.KnownServers = s.knownServers(v.Server)
	if v.LastSuccess != nil && time.Since(*v.LastSuccess) > 2*s.opts.Poll+s.opts.Timeout {
		v.Stale = true
	}
	return v
}

// knownServers lists the current address first, then the remembered one.
func (s *Service) knownServers(current string) []string {
	known := []string{current}
	if s.config.Server != "" && s.config.Server != current {
		known = append(known, s.config.Server)
	}
	return known
}

// Connect connects to the server with the token from the file chosen with ChooseCredential, else the live connection's
// token when the server is unchanged, else the one given at launch. It waits for the server to accept the token, so a
// wrong address or token comes back as an error here and the dashboard stays disconnected. The token never passes
// through the window.
func (s *Service) Connect(server string) (View, error) {
	err := s.connect(server)
	return s.Snapshot(), err
}

func (s *Service) connect(server string) error {
	normalized, err := clientwire.ValidateServer(server)
	if err != nil {
		return errors.New("Invalid server address. " + err.Error())
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return errors.New("The dashboard is closing.")
	}
	if s.view.Busy {
		s.mu.Unlock()
		return errors.New("A control action is in progress. Wait for it to finish.")
	}
	if s.view.Connecting {
		s.mu.Unlock()
		return errors.New("A connection attempt is already in progress.")
	}
	var credential, file string
	pending := s.pendingCredential
	switch {
	case pending != nil:
		credential, file = pending.token, pending.name
	case normalized == s.view.Server && s.token != "":
		credential, file = s.token, s.credentialFile
	}
	if credential == "" {
		credential, file = s.opts.Token, ""
	}
	if !validToken(credential) {
		s.mu.Unlock()
		return errors.New("Choose the proctor token file first, or launch with an admin-token file.")
	}
	s.view.Connecting, s.view.ConnectError = true, ""
	ctx := s.ctx
	s.mu.Unlock()

	var catalog Catalog
	err = getJSON(ctx, s.client, normalized, credential, http.MethodGet, "/admin/exams", nil, &catalog)
	if err == nil {
		err = validateCatalog(catalog)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.view.Connecting = false
	if s.stopped {
		return errors.New("The dashboard is closing.")
	}
	if err != nil {
		s.view.ConnectError = connectionProblem(normalized, err)
		return errors.New(s.view.ConnectError)
	}
	if s.disconnect != nil {
		s.disconnect()
	}
	s.connection, s.disconnect = context.WithCancel(s.ctx)
	s.gen++
	s.epoch++
	s.link++
	s.token, s.credentialFile = credential, file
	// The server accepted the token, so the address and a token file chosen in the window are worth remembering.
	s.remember(func(c *Config) {
		c.Server = normalized
		if pending != nil && pending.confirm {
			c.TokenFile = pending.path
		}
	})
	if s.pendingCredential == pending {
		s.pendingCredential = nil
	}
	s.tokenNote = ""
	s.pendingExam, s.pendingExamClass, s.pendingClass, s.pendingRoster, s.pendingRosterClass = nil, "", nil, nil, ""
	s.autoSelect = s.opts.ExamID == ""
	observation := s.view.Observation
	s.view = View{Observation: observation, Server: normalized, ExamID: s.opts.ExamID, CredentialAvailable: true, Connected: true, Loading: true, Stale: true, Catalog: catalog, CatalogLoaded: true}
	s.signalLocked()
	return nil
}

// connectionProblem says in the operator's terms why a connection attempt failed.
func connectionProblem(server string, err error) string {
	host := server
	if u, parseErr := url.Parse(server); parseErr == nil && u.Host != "" {
		host = u.Host
	}
	switch {
	case errors.Is(err, errUnauthorized):
		return "The server at " + host + " rejected the token. Choose the token file the server was started with (its -admin-token-file)."
	case errors.Is(err, errUnreachable):
		return "Cannot reach " + host + ". Check the address, that the server is running and that this computer is on the same network."
	}
	return "The server at " + host + " did not answer as expected: " + err.Error()
}

// Disconnect ends the connection: Classes and Exams become unavailable and nothing is asked of the server any more. The address
// and the token stay known, so Connect can connect again without choosing the file again; the settings are not touched, so the
// next start still connects by itself.
func (s *Service) Disconnect() (View, error) {
	s.mu.Lock()
	switch {
	case s.stopped:
		s.mu.Unlock()
		return s.Snapshot(), errors.New("The dashboard is closing.")
	case s.view.Connecting:
		s.mu.Unlock()
		return s.Snapshot(), errors.New("A connection attempt is in progress. Wait for it to finish.")
	case s.view.Busy:
		s.mu.Unlock()
		return s.Snapshot(), errors.New("A control action is in progress. Wait for it to finish.")
	case !s.view.Connected:
		s.mu.Unlock()
		return s.Snapshot(), nil
	}
	if s.disconnect != nil {
		s.disconnect()
	}
	s.connection, s.disconnect = nil, nil
	s.gen++ // answers still on their way belong to the connection that has just ended
	s.epoch++
	s.link++
	s.autoSelect = false
	s.pendingExam, s.pendingExamClass, s.pendingClass, s.pendingRoster, s.pendingRosterClass = nil, "", nil, nil, ""
	observation := s.view.Observation
	s.view = View{Observation: observation, Server: s.view.Server, ExamID: s.opts.ExamID, CredentialAvailable: s.token != "" || s.opts.Token != "", Stale: true}
	s.mu.Unlock()
	return s.Snapshot(), nil
}

// SelectExam monitors another stored exam. It never changes which exam students are admitted to.
func (s *Service) SelectExam(examID string) (View, error) {
	examID = strings.TrimSpace(examID)
	if !validID(examID) {
		return s.Snapshot(), errors.New("Exam ID must be a canonical lowercase UUID.")
	}
	s.mu.Lock()
	if s.stopped || !s.view.Connected || s.view.Busy {
		s.mu.Unlock()
		return s.Snapshot(), errors.New("Connect first and wait for the current action to finish.")
	}
	if problem := monitorProblem(s.view.Catalog, s.view.CatalogLoaded, examID); problem != "" {
		s.mu.Unlock()
		return s.Snapshot(), errors.New(problem)
	}
	s.selectLocked(examID)
	s.mu.Unlock()
	s.signal()
	return s.Snapshot(), nil
}

// monitorProblem says why an exam cannot be monitored, or "" when it can: it must be the published exam or one students
// have taken, since closing admission does not end the sessions already running. An exam the catalog does not list yet is
// left to the server, which answers for unknown IDs.
func monitorProblem(catalog Catalog, loaded bool, examID string) string {
	if !loaded {
		return ""
	}
	for _, exam := range catalog.Exams {
		if exam.ID == examID {
			if exam.Published || exam.TotalSessions > 0 {
				return ""
			}
			return "Nobody has taken this exam yet, so there is nothing to monitor. Publish it first."
		}
	}
	return ""
}

// selectLocked switches the monitored exam and discards anything still in flight for the previous one.
func (s *Service) selectLocked(examID string) {
	s.gen++
	s.epoch++
	s.autoSelect = false
	s.view.ExamID = examID
	s.view.Roster = Roster{}
	s.view.Loaded, s.view.Loading, s.view.Stale = false, true, true
}
func (s *Service) Refresh() { s.signal() }
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) poll() {
	defer s.wg.Done()
	timer := time.NewTimer(s.opts.Poll)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-timer.C:
		}
		s.refresh()
		timer.Reset(s.opts.Poll)
	}
}

// refresh asks the server for the exam catalog, the student count and, when an exam is selected, its live roster. Each
// answer replaces the previous observation only if the connection and selection are unchanged; any failure leaves the
// previous observations in place and marks them stale.
func (s *Service) refresh() {
	s.mu.Lock()
	ctx, base, token, id, gen, epoch := s.connection, s.view.Server, s.token, s.view.ExamID, s.gen, s.epoch
	connected := s.view.Connected && !s.view.Busy
	s.mu.Unlock()
	if !connected || ctx == nil {
		return
	}
	var catalog Catalog
	catalogErr := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/exams", nil, &catalog)
	if catalogErr == nil {
		catalogErr = validateCatalog(catalog)
	}
	var classes ClassList
	classesErr := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/classes", nil, &classes)
	if classesErr == nil {
		classesErr = validateClasses(classes)
	}
	failures := []error{catalogErr, classesErr}
	selectedKnown := id != "" && catalogErr == nil && catalogHas(catalog, id)
	if id != "" && catalogErr == nil && !selectedKnown {
		failures = append(failures, errors.New("The selected exam is not stored on this server."))
	}
	var roster Roster
	rosterLoaded := false
	if id != "" && (catalogErr != nil || selectedKnown) {
		err := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/sessions?exam_id="+id, nil, &roster)
		if err == nil {
			err = validateRoster(roster, id)
		}
		rosterLoaded = err == nil
		failures = append(failures, err)
	}
	failure := joinDistinct(failures...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen || epoch != s.epoch || s.stopped {
		return
	}
	s.view.Loading = false
	s.view.Unreachable = errors.Is(catalogErr, errUnreachable) || errors.Is(classesErr, errUnreachable)
	if catalogErr == nil {
		s.view.Catalog, s.view.CatalogLoaded = catalog, true
		if s.autoSelect && s.view.ExamID == "" && catalog.CurrentExamID != "" {
			// Monitor the published exam as soon as it is known; its roster loads on the next pass.
			s.selectLocked(catalog.CurrentExamID)
			s.view.Loading = true
			s.signalLocked()
			return
		}
	}
	if classesErr == nil {
		s.view.Classes, s.view.ClassesLoaded = classes.Classes, true
	}
	if rosterLoaded {
		s.view.Roster, s.view.Loaded = roster, true
	}
	if failure != nil {
		s.view.Stale = true
		s.view.Error = failure.Error()
		return
	}
	now := time.Now().UTC()
	s.view.LastSuccess = &now
	s.view.Stale = false
	s.view.Error = ""
}

// joinDistinct reports each distinct failure once: requests that all fail for one reason, such as a rejected credential,
// read as a single problem.
func joinDistinct(errs ...error) error {
	seen := map[string]bool{}
	var distinct []error
	for _, err := range errs {
		if err != nil && !seen[err.Error()] {
			seen[err.Error()] = true
			distinct = append(distinct, err)
		}
	}
	return errors.Join(distinct...)
}

func (s *Service) signalLocked() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// maxStudentList bounds what a server may send for one class; it is more than the server ever lists.
const maxStudentList = 10000

func validateClasses(list ClassList) error {
	invalid := errors.New("Invalid class list; previous observations retained.")
	if list.Classes == nil || len(list.Classes) > 10000 {
		return invalid
	}
	seen := make(map[string]bool, len(list.Classes))
	for _, class := range list.Classes {
		if !validID(class.ID) || seen[class.ID] || strings.TrimSpace(class.Name) == "" || len(class.Name) > 1024 || class.StudentCount < 0 || class.ExamCount < 0 || class.CreatedAt.IsZero() {
			return invalid
		}
		if exam := class.PublishedExam; exam != nil && (class.ExamCount < 1 || !validID(exam.ExamID) || len(exam.Title) > 1024) {
			return invalid
		}
		seen[class.ID] = true
	}
	return nil
}

// validateClassRoster checks one class's student list against the class that was asked for.
func validateClassRoster(roster ClassRoster, id string) error {
	invalid := errors.New("Invalid class roster response.")
	if roster.ID != id || roster.Total < len(roster.Students) || roster.Students == nil || len(roster.Students) > maxStudentList || len(roster.Name) > 1024 {
		return invalid
	}
	seen := make(map[string]bool, len(roster.Students))
	for _, student := range roster.Students {
		if student.Account == "" || len(student.Account) > 256 || len(student.Name) > 1024 || seen[student.Account] {
			return invalid
		}
		seen[student.Account] = true
	}
	return nil
}

func validateCatalog(catalog Catalog) error {
	if catalog.ServerTime.IsZero() || catalog.Exams == nil || catalog.CurrentExamID != "" && !validID(catalog.CurrentExamID) ||
		(catalog.CurrentExamID == "") != (catalog.CurrentClassID == "") || catalog.CurrentClassID != "" && !validID(catalog.CurrentClassID) {
		return errors.New("Invalid exam catalog; previous observations retained.")
	}
	seen := map[string]bool{}
	published := 0
	for _, exam := range catalog.Exams {
		if !validID(exam.ID) || !validID(exam.ClassID) || strings.TrimSpace(exam.ClassName) == "" || len(exam.ClassName) > 1024 || seen[exam.ID] || exam.ExerciseCount < 0 || exam.Navigation != "free" && exam.Navigation != "forward_only" || exam.ActiveSessions < 0 || exam.TotalSessions < exam.ActiveSessions || exam.Published != (exam.ID == catalog.CurrentExamID) {
			return errors.New("Invalid exam catalog entry; previous observations retained.")
		}
		seen[exam.ID] = true
		if exam.Published {
			published++
			if exam.ClassID != catalog.CurrentClassID || exam.ClassName != catalog.CurrentClassName {
				return errors.New("Invalid exam catalog: the published exam is not in the class admitted to it.")
			}
		}
	}
	if published > 1 {
		return errors.New("Invalid exam catalog: several exams are published.")
	}
	return nil
}

func catalogHas(catalog Catalog, id string) bool {
	for _, exam := range catalog.Exams {
		if exam.ID == id {
			return true
		}
	}
	return false
}
func validateSession(row Session, examID string) error {
	if !validID(row.ID) || row.ExamID != examID || row.Account == "" || row.Remaining < 0 {
		return errors.New("Invalid server session summary.")
	}
	switch row.Status {
	case "active", "grace", "submitted", "force_submitted":
	default:
		return errors.New("Unknown server lifecycle status.")
	}
	if row.FinalizationPending && row.Final != nil {
		return errors.New("Inconsistent pending finalization.")
	}
	if row.Final != nil && (row.Final.VersionID <= 0 || row.Final.Sequence <= 0) {
		return errors.New("Invalid final-version identity.")
	}
	return nil
}
func validateRoster(roster Roster, id string) error {
	if roster.ExamID != id || roster.ServerTime.IsZero() || roster.Sessions == nil || roster.Missing == nil {
		return errors.New("Invalid roster response; previous observations retained.")
	}
	seen := map[string]bool{}
	for _, row := range roster.Sessions {
		if err := validateSession(row, id); err != nil {
			return err
		}
		if seen[row.ID] {
			return errors.New("Duplicate session identity.")
		}
		seen[row.ID] = true
	}
	return nil
}
func (s *Service) connectionState() (context.Context, string, string, string, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || !s.view.Connected || !s.view.Loaded {
		return nil, "", "", "", 0, errors.New("Connect and load an exam first.")
	}
	return s.connection, s.view.Server, s.token, s.view.ExamID, s.gen, nil
}

// linkState is the live connection for reads that do not depend on the monitored exam, such as a session's history.
func (s *Service) linkState() (context.Context, string, string, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || !s.view.Connected {
		return nil, "", "", 0, errors.New("Connect to the server first.")
	}
	return s.connection, s.view.Server, s.token, s.link, nil
}
func (s *Service) stillLinked(link uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.link != link {
		return errors.New("The server connection changed; response discarded.")
	}
	return nil
}
func (s *Service) stillCurrent(gen uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.gen != gen {
		return errors.New("Exam selection changed; response discarded.")
	}
	return nil
}

// Timeline reads one page of a session's history. It works for any session of the server, whichever exam is monitored, so
// each review window can stay open on its own student.
func (s *Service) Timeline(id, cursor string) (Page, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return Page{}, err
	}
	if !validID(id) || len(cursor) > 1024 {
		return Page{}, errors.New("Invalid history selection.")
	}
	var page Page
	err = getJSON(ctx, s.client, base, token, http.MethodGet, timelinePath(id, cursor), nil, &page)
	if err != nil {
		return Page{}, err
	}
	if err = s.stillLinked(link); err != nil {
		return Page{}, err
	}
	if page.ServerTime.IsZero() || page.Session.ID != id || page.Items == nil || page.Next != nil && (*page.Next == "" || len(*page.Next) > 1024) {
		return Page{}, errors.New("Invalid timeline response.")
	}
	if err = validateSession(page.Session, page.Session.ExamID); err != nil || !validID(page.Session.ExamID) {
		return Page{}, errors.New("Invalid server session summary.")
	}
	seen := map[string]bool{}
	for _, item := range page.Items {
		if seen[item.ID] || item.At.IsZero() {
			return Page{}, errors.New("Invalid timeline identity or time.")
		}
		seen[item.ID] = true
		switch item.Kind {
		case "event":
			if item.Event == nil || item.Version != nil || item.Event.ID <= 0 {
				return Page{}, errors.New("Invalid event response.")
			}
			for _, image := range item.Event.Screenshots {
				if image.ID <= 0 || image.EventID != item.Event.ID {
					return Page{}, errors.New("Image does not match its originating event.")
				}
			}
		case "answer_version":
			if item.Version == nil || item.Event != nil || item.Version.ID <= 0 || item.Version.Sequence <= 0 || item.Version.Answers == nil {
				return Page{}, errors.New("Invalid answer-version response.")
			}
		default:
			return Page{}, errors.New("Unknown timeline item.")
		}
	}
	return page, nil
}
func (s *Service) Image(id int64, thumbnail bool) (Image, error) {
	data, contentType, err := s.imageBytes(id, thumbnail)
	if err != nil {
		return Image{}, err
	}
	return Image{ID: id, Data: "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)}, nil
}

// imageBytes reads a protected screenshot, one at a time, for whichever window asks.
func (s *Service) imageBytes(id int64, thumbnail bool) ([]byte, string, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return nil, "", err
	}
	if id <= 0 {
		return nil, "", errors.New("Invalid image selection.")
	}
	select {
	case s.images <- struct{}{}:
		defer func() { <-s.images }()
	case <-ctx.Done():
		return nil, "", errors.New("Image loading cancelled.")
	}
	data, contentType, err := readImage(ctx, s.client, base, token, id, thumbnail)
	if err != nil {
		return nil, "", err
	}
	if err = s.stillLinked(link); err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}
func (s *Service) beginAction() (context.Context, string, string, string, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || !s.view.Loaded || !s.view.Connected || s.view.Busy {
		return nil, "", "", "", 0, errors.New("Load an exam and wait for the current action to finish.")
	}
	s.view.Busy = true
	s.epoch++
	return s.connection, s.view.Server, s.token, s.view.ExamID, s.gen, nil
}
func (s *Service) endAction() {
	s.mu.Lock()
	s.view.Busy = false
	s.view.Stale = true
	s.view.Error = "Refreshing after control action; displayed rows are previous observations."
	s.epoch++
	s.mu.Unlock()
	s.signal()
}

// endAdmin finishes an administrator change that touches no session, such as storing the monitoring settings: the rows on
// screen stay current, so they are not marked stale.
func (s *Service) endAdmin() {
	s.mu.Lock()
	s.view.Busy = false
	s.mu.Unlock()
	s.signal()
}
func (s *Service) ForceSubmit(id, reason string, confirmed bool) (clientwire.Final, error) {
	reason = strings.TrimSpace(reason)
	if !confirmed {
		return clientwire.Final{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(id) || len(reason) == 0 || len(reason) > 1024 || !utf8.ValidString(reason) {
		return clientwire.Final{}, errors.New("A selected session and a reason of 1–1024 UTF-8 bytes are required.")
	}
	ctx, base, token, _, gen, err := s.beginAction()
	if err != nil {
		return clientwire.Final{}, err
	}
	defer s.endAction()
	s.mu.Lock()
	found := false
	for _, row := range s.view.Roster.Sessions {
		if row.ID == id {
			found = true
		}
	}
	s.mu.Unlock()
	if !found {
		return clientwire.Final{}, errors.New("Session is not in this exam's loaded roster.")
	}
	var result clientwire.Final
	err = getJSON(ctx, s.client, base, token, http.MethodPost, "/admin/force-submit/"+id, map[string]string{"reason": reason}, &result)
	if err != nil {
		return result, err
	}
	if err = s.stillCurrent(gen); err != nil {
		return clientwire.Final{}, err
	}
	if !validFinal(result) {
		return clientwire.Final{}, errors.New("Finalization acknowledgment invalid; refresh to establish the outcome.")
	}
	return result, nil
}

// PurgeScreenshots deletes the screenshots of one exam on the server's list, after grading. It does not depend on which
// exam is monitored.
func (s *Service) PurgeScreenshots(examID string, confirmed bool) (Purge, error) {
	if !confirmed {
		return Purge{}, errors.New("Confirm grading completion and irreversible image deletion; nothing was sent.")
	}
	if !validID(examID) {
		return Purge{}, errors.New("Invalid exam selection; nothing was sent.")
	}
	s.mu.Lock()
	listed := false
	for _, exam := range s.view.Catalog.Exams {
		listed = listed || exam.ID == examID
	}
	s.mu.Unlock()
	if !listed {
		return Purge{}, errors.New("That exam is not on the server's list; nothing was sent. Look at the exam list again.")
	}
	ctx, base, token, err := s.beginAdmin()
	if err != nil {
		return Purge{}, err
	}
	defer s.endAction()
	var result Purge
	err = getJSON(ctx, s.client, base, token, http.MethodPost, "/admin/purge-screenshots/"+examID, map[string]bool{"confirm": true}, &result)
	if err != nil {
		return Purge{}, err
	}
	if result.ExamID != examID || result.Deleted < 0 || result.Failed == nil || result.Complete && len(result.Failed) != 0 {
		return Purge{}, errors.New("Purge acknowledgment invalid; verify retention state before retrying.")
	}
	return result, nil
}
