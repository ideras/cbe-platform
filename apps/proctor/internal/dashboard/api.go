package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"exam-platform/shared/calc"
	"exam-platform/shared/clientwire"
)

type Student struct {
	Account string `json:"account_number"`
	Name    string `json:"student_name"`
}
type Focus struct {
	State             string     `json:"state"`
	Since             *time.Time `json:"since"`
	Duration          *int64     `json:"unfocused_duration_ms"`
	ThresholdExceeded bool       `json:"threshold_exceeded"`
}
type Presence struct {
	Monitoring   bool       `json:"monitoring"`
	Connectivity string     `json:"connectivity"`
	Hostname     string     `json:"hostname"`
	RemoteAddr   string     `json:"remote_addr"`
	LastSeen     *time.Time `json:"last_seen"`
	Age          *int64     `json:"heartbeat_age_ms"`
	Missed       int64      `json:"missed_intervals"`
	Overdue      bool       `json:"overdue"`
	EverOverdue  bool       `json:"ever_overdue"`
	Focus        Focus      `json:"focus"`
	Volume       *int       `json:"volume_percent"`
	Muted        *bool      `json:"volume_muted"`
}
type FinalSummary struct {
	VersionID      int64     `json:"final_version_id"`
	Sequence       int64     `json:"version_seq"`
	SavedAt        time.Time `json:"saved_at"`
	SubmittedAt    time.Time `json:"submitted_at"`
	Reason         string    `json:"reason"`
	OperatorReason string    `json:"operator_reason,omitempty"`
}
type Session struct {
	ID     string `json:"session_id"`
	ExamID string `json:"exam_id"`
	Student
	RosterActive        bool      `json:"roster_active"`
	Status              string    `json:"status"`
	PersistedStatus     string    `json:"persisted_status"`
	FinalizationPending bool      `json:"finalization_pending"`
	StartedAt           time.Time `json:"started_at"`
	DeadlineAt          time.Time `json:"deadline_at"`
	GraceUntil          time.Time `json:"grace_until"`
	Remaining           int64     `json:"seconds_remaining"`
	Presence            Presence  `json:"presence"`
	KnownUnfocused      *int64    `json:"last_known_unfocused_duration_ms"`
	NetworkWarning      bool      `json:"network_warning"`
	FocusWarning        bool      `json:"focus_warning"`
	// Locked says that the exam is locked, since when and why; nil while it is not.
	Locked *Lock `json:"locked"`
	// Locks are the locks and unlocks of the session, in the order they happened.
	Locks []LockRecord  `json:"locks"`
	Final *FinalSummary `json:"final_version"`
}

// Lock says that a student's exam is locked: since when, and why.
type Lock struct {
	Since  time.Time `json:"since"`
	Reason string    `json:"reason"`
}

// LockRecord is one lock or unlock of a session, as the history and the downloads show it.
type LockRecord struct {
	Type           string    `json:"type"`
	At             time.Time `json:"at"`
	Reason         string    `json:"reason"`
	OperatorReason string    `json:"operator_reason"`
	LockedMillis   int64     `json:"locked_ms"`
	ExtendedMillis int64     `json:"extended_ms"`
}
type Policy struct {
	Heartbeat int64 `json:"heartbeat_interval_ms"`
	Overdue   int64 `json:"overdue_threshold_ms"`
	Focus     int64 `json:"focus_threshold_ms"`
	Alert     int64 `json:"alert_interval_ms"`
	Capture   int64 `json:"capture_interval_ms"`
	// LockAway is how long one absence may last before the exam locks; 0 means the lock is off.
	LockAway int64 `json:"lock_away_after_ms"`
	// LockNoContact is how long the application may be out of contact before the exam locks; 0 means off.
	LockNoContact int64 `json:"lock_no_contact_after_ms"`
}
type Roster struct {
	ExamID     string    `json:"exam_id"`
	ServerTime time.Time `json:"server_time"`
	Policy     Policy    `json:"policy"`
	Sessions   []Session `json:"sessions"`
	Missing    []Student `json:"students_without_session"`
}
type Screenshot struct {
	ID         int64      `json:"screenshot_id"`
	EventID    int64      `json:"event_log_id"`
	CapturedAt time.Time  `json:"captured_at"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	Bytes      int64      `json:"byte_size"`
	ClientTime *time.Time `json:"client_captured_at_untrusted"`
}
type Event struct {
	ID          int64          `json:"event_log_id"`
	Type        string         `json:"event_type"`
	Detail      map[string]any `json:"detail"`
	Screenshots []Screenshot   `json:"screenshots"`
}
type Version struct {
	ID       int64             `json:"version_id"`
	Sequence int64             `json:"version_seq"`
	Answers  map[string]string `json:"answers"`
	IsFinal  bool              `json:"is_final"`
}
type Item struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	At      time.Time `json:"occurred_at"`
	Event   *Event    `json:"event,omitempty"`
	Version *Version  `json:"answer_version,omitempty"`
}
type Page struct {
	ServerTime time.Time `json:"server_time"`
	Session    Session   `json:"session"`
	Policy     Policy    `json:"policy"`
	Items      []Item    `json:"items"`
	Next       *string   `json:"next_cursor"`
}
type Image struct {
	ID   int64  `json:"id"`
	Data string `json:"data"`
}
type Purge struct {
	ExamID   string  `json:"exam_id"`
	Deleted  int64   `json:"deleted"`
	Failed   []int64 `json:"failed_screenshot_ids"`
	Complete bool    `json:"complete"`
}

// ExamEntry is one stored exam as the server lists it: never the PIN or the exam's text. An exam belongs to the class it was
// imported into.
type ExamEntry struct {
	ID            string `json:"exam_id"`
	ClassID       string `json:"class_id"`
	ClassName     string `json:"class_name"`
	Title         string `json:"title"`
	ExerciseCount int    `json:"exercise_count"`
	// Navigation is free, or forward_only when students cannot return to an exercise they left.
	Navigation       string     `json:"navigation"`
	DurationSeconds  int64      `json:"duration_seconds"`
	GraceSeconds     int64      `json:"grace_seconds"`
	MinVolumePercent int        `json:"min_volume_percent"`
	CreatedAt        time.Time  `json:"created_at"`
	OpensAt          *time.Time `json:"opens_at"`
	ClosesAt         *time.Time `json:"closes_at"`
	Published        bool       `json:"published"`
	ActiveSessions   int        `json:"active_sessions"`
	TotalSessions    int        `json:"total_sessions"`
	// LockedSessions are the running sessions a proctor has to unlock.
	LockedSessions int `json:"locked_sessions"`
	// Tools is the exam's effective tools configuration; omitted when the exam has no calculator.
	Tools *calc.Tools `json:"tools,omitempty"`
}

// SettingsValues are the monitoring values the Settings page edits, in whole milliseconds.
type SettingsValues struct {
	HeartbeatInterval  int64 `json:"heartbeat_interval_ms"`
	OverdueThreshold   int64 `json:"overdue_threshold_ms"`
	FocusThreshold     int64 `json:"focus_threshold_ms"`
	AlertInterval      int64 `json:"alert_interval_ms"`
	CaptureInterval    int64 `json:"capture_interval_ms"`
	LockAwayAfter      int64 `json:"lock_away_after_ms"`
	LockNoContactAfter int64 `json:"lock_no_contact_after_ms"`
	IncidentCaptures   int   `json:"incident_captures"`
	SessionCaptures    int   `json:"session_captures"`
	DefaultGrace       int64 `json:"default_grace_ms"`
}

// Settings are the monitoring settings of the server, as the Settings page shows them.
type Settings struct {
	SettingsValues
	// UpdatedAt is when a proctor last saved the settings; empty when nobody has.
	UpdatedAt string `json:"updated_at"`
	// AverageScreenshotBytes is the mean size of the stored screenshots; nil when there are none yet.
	AverageScreenshotBytes *int64 `json:"average_screenshot_bytes"`
}

// UnlockResult is what lifting a lock did to a session.
type UnlockResult struct {
	SessionID    string    `json:"session_id"`
	DeadlineAt   time.Time `json:"deadline_at"`
	GraceUntil   time.Time `json:"grace_until"`
	SecondsAdded int64     `json:"seconds_added"`
	LockedMillis int64     `json:"locked_ms"`
}

// ClassExam names an exam of a class.
type ClassExam struct {
	ExamID string `json:"exam_id"`
	Title  string `json:"title"`
}

// ClassEntry is one class on the server: a name, how many students and exams it has, and the exam students are being admitted
// to when that one is in this class.
type ClassEntry struct {
	ID            string     `json:"class_id"`
	Name          string     `json:"name"`
	StudentCount  int        `json:"student_count"`
	ExamCount     int        `json:"exam_count"`
	CreatedAt     time.Time  `json:"created_at"`
	PublishedExam *ClassExam `json:"published_exam"`
}

type ClassList struct {
	Classes []ClassEntry `json:"classes"`
}

// ClassRoster is the roster of one class: account number and full name of each student, nothing else.
type ClassRoster struct {
	ID       string    `json:"class_id"`
	Name     string    `json:"name"`
	Total    int       `json:"total"`
	Students []Student `json:"students"`
}

// Catalog lists the server's exams and which one new students are admitted to, with its class.
type Catalog struct {
	ServerTime       time.Time   `json:"server_time"`
	CurrentExamID    string      `json:"current_exam_id"`
	CurrentClassID   string      `json:"current_class_id"`
	CurrentClassName string      `json:"current_class_name"`
	Exams            []ExamEntry `json:"exams"`
}

func validID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// The two ways a connection attempt usually fails, told apart so the dialog can say what to do.
var (
	errUnauthorized = errors.New("Proctor authorization was rejected. Reconnect with the correct credential.")
	errUnreachable  = errors.New("Server unreachable or request timed out. Data or action outcome is unconfirmed; refresh before retrying.")
)

func apiError(status int) error {
	switch status {
	case 401, 403:
		return errUnauthorized
	case 404, 410:
		return errors.New("Requested record or image is unavailable, missing or purged.")
	case 400, 413, 415:
		return errors.New("The server rejected the request. Check the selected exam and input.")
	}
	return errors.New("Server request failed. Data or action outcome is unconfirmed; refresh before retrying.")
}
func request(ctx context.Context, client *http.Client, base, token, method, path string, input any) (*http.Response, error) {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, errors.New("Cannot encode request.")
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, errors.New("Cannot create request.")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errUnreachable
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, apiError(resp.StatusCode)
	}
	return resp, nil
}
func getJSON(ctx context.Context, client *http.Client, base, token, method, path string, input, output any) error {
	resp, err := request(ctx, client, base, token, method, path, input)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil || len(data) > 16<<20 || !utf8.Valid(data) || json.Unmarshal(data, output) != nil {
		return errors.New("Server response invalid, incomplete or above the 16 MiB dashboard limit. Confirmation unavailable.")
	}
	return nil
}

// readImage fetches a protected screenshot and checks it is the PNG or JPEG image it claims to be.
func readImage(ctx context.Context, client *http.Client, base, token string, id int64, thumbnail bool) ([]byte, string, error) {
	path := "/admin/screenshot/" + strconv.FormatInt(id, 10)
	limit := int64(16 << 20)
	if thumbnail {
		path += "/thumbnail"
		limit = 2 << 20
	}
	resp, err := request(ctx, client, base, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	contentType := resp.Header.Get("Content-Type")
	if err != nil || int64(len(data)) > limit || contentType != "image/png" && contentType != "image/jpeg" {
		return nil, "", errors.New("Image response invalid or too large.")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || "image/"+format != contentType || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 16384 || cfg.Height > 16384 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
		return nil, "", errors.New("Image dimensions invalid.")
	}
	return data, contentType, nil
}
func timelinePath(id, cursor string) string {
	p := "/admin/session/" + id + "/timeline?limit=100"
	if cursor != "" {
		p += "&cursor=" + url.QueryEscape(cursor)
	}
	return p
}
func validFinal(value clientwire.Final) bool {
	return (value.Status == "submitted" || value.Status == "force_submitted") && value.VersionID > 0 && value.Sequence > 0 && !value.SubmittedAt.IsZero()
}

// rejection is a definite answer from the server: it processed the request and refused it. Network failures are not
// rejections, because their outcome is unknown.
type rejection struct {
	Status   int
	Message  string
	Problems []string
	Active   int
}

func (r *rejection) Error() string {
	if len(r.Problems) > 0 {
		return r.Message + " " + strings.Join(r.Problems, "; ") + "."
	}
	return r.Message
}

// adminSend sends an administrator write whose body is already encoded and decodes a successful JSON answer.
func adminSend(ctx context.Context, client *http.Client, base, token, method, path, contentType string, body []byte, output any) error {
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("Cannot create request.")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Server unreachable or request timed out. The outcome is unconfirmed; check the server state before repeating the action.")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return rejected(resp)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil || len(data) > 16<<20 || !utf8.Valid(data) || json.Unmarshal(data, output) != nil {
		return errors.New("Server response invalid or incomplete. The outcome is unconfirmed; check the server state before repeating the action.")
	}
	return nil
}

func rejected(resp *http.Response) error {
	var detail struct {
		Error    string   `json:"error"`
		Problems []string `json:"problems"`
		Active   int      `json:"active_sessions"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if json.Unmarshal(data, &detail) != nil || !utf8.ValidString(detail.Error) {
		detail.Error, detail.Problems = "", nil
	}
	reject := &rejection{Status: resp.StatusCode, Problems: detail.Problems, Active: detail.Active}
	switch resp.StatusCode {
	case 401, 403:
		reject.Message = "Proctor authorization was rejected. Reconnect with the correct credential."
	case 400:
		reject.Message = "The server rejected the document:"
		if len(reject.Problems) == 0 {
			reject.Message = "The server rejected the request."
		}
	case 404:
		reject.Message = "The server does not have that exam or class."
	case 409:
		reject.Message = "The server refused the change: " + detail.Error + "."
		if detail.Error == "" {
			reject.Message = "The server refused the change because the situation changed. Refresh and try again."
		}
	case 413:
		reject.Message = "The file is larger than the server accepts."
	case 415:
		reject.Message = "The server does not accept this kind of file."
	default:
		reject.Message = "Server request failed. The outcome is unconfirmed; check the server state before repeating the action."
	}
	return reject
}
