package dashboard

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// The dashboard's main window prepares classes and exams; monitoring, each student's review and each screenshot open in
// windows of their own. All of them are pages of the same frontend, served by the same Service, so the credential never
// leaves Go and every window shares one server connection.

// WindowRequest asks the application for a window showing one page of the dashboard. A window with the same Name is
// reused: it is brought to the front showing URL.
type WindowRequest struct {
	Name, Title, URL string
	Width, Height    int
}

// SaveDialog describes a native save dialog; Filename is the suggested name.
type SaveDialog struct {
	Title, Filename, FilterName, Pattern string
}

// maxIncidentImages bounds the screenshots one image window steps through.
const maxIncidentImages = 200

// OpenMonitoring shows the live roster of the monitored exam in its own window.
func (s *Service) OpenMonitoring() error {
	s.mu.Lock()
	connected, examID, title := s.view.Connected, s.view.ExamID, ""
	for _, exam := range s.view.Catalog.Exams {
		if exam.ID == examID {
			title = exam.Title + " (" + exam.ClassName + ")"
		}
	}
	s.mu.Unlock()
	if !connected || examID == "" {
		return errors.New("Choose an exam to monitor first.")
	}
	if title == "" {
		title = examID
	}
	return s.open(WindowRequest{Name: "monitoring", Title: "Monitoring — " + title, URL: "/#/monitoring", Width: 1280, Height: 860})
}

// OpenReview shows one session's history in its own window; each student gets one window.
func (s *Service) OpenReview(sessionID string) error {
	if !validID(sessionID) {
		return errors.New("Invalid session selection.")
	}
	s.mu.Lock()
	connected, title := s.view.Connected, "Session review"
	for _, row := range s.view.Roster.Sessions {
		if row.ID == sessionID {
			title = "Review — " + row.Student.Name + " (" + row.Student.Account + ")"
		}
	}
	s.mu.Unlock()
	if !connected {
		return errors.New("Connect to the server first.")
	}
	return s.open(WindowRequest{Name: "review-" + sessionID, Title: title, URL: "/#/review/" + sessionID, Width: 1180, Height: 860})
}

// OpenImage shows screenshot ids[index] in an image window that can step through all of ids, the captures of one focus
// loss. One window serves one focus loss. times carries the capture times, so the window can show when each one was taken.
func (s *Service) OpenImage(ids []int64, times []string, index int) error {
	if len(ids) == 0 || len(ids) > maxIncidentImages || index < 0 || index >= len(ids) {
		return errors.New("Invalid image selection.")
	}
	if len(times) != 0 && len(times) != len(ids) {
		return errors.New("Invalid image selection.")
	}
	list := make([]string, len(ids))
	for i, id := range ids {
		if id <= 0 {
			return errors.New("Invalid image selection.")
		}
		list[i] = strconv.FormatInt(id, 10)
	}
	if !s.Snapshot().Connected {
		return errors.New("Connect to the server first.")
	}
	query := url.Values{"set": {strings.Join(list, ",")}, "at": {strconv.Itoa(index)}}
	if len(times) > 0 {
		query.Set("times", strings.Join(times, ","))
	}
	return s.open(WindowRequest{Name: "image-" + list[0], Title: "Screenshot #" + list[index], URL: "/#/image?" + query.Encode(), Width: 1280, Height: 900})
}

func (s *Service) open(request WindowRequest) error {
	if s.opts.OpenWindow == nil {
		return errors.New("Opening windows is not available in this build.")
	}
	if err := s.opts.OpenWindow(request); err != nil {
		return errors.New("The window could not be opened.")
	}
	return nil
}

// SaveImage writes the full screenshot to a file the proctor chooses, and returns its path, or "" when they cancel. The
// saved copy is outside the server's protected store, so purging the exam's screenshots does not delete it.
func (s *Service) SaveImage(id int64) (string, error) {
	if s.opts.Save == nil {
		return "", errors.New("Saving files is not available in this build.")
	}
	data, contentType, err := s.imageBytes(id, false)
	if err != nil {
		return "", err
	}
	extension, filter := ".png", "PNG image"
	if contentType == "image/jpeg" {
		extension, filter = ".jpg", "JPEG image"
	}
	path, err := s.pickSave(SaveDialog{Title: "Save screenshot", Filename: "screenshot-" + strconv.FormatInt(id, 10) + extension, FilterName: filter, Pattern: "*" + extension})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", errors.New("The screenshot could not be written to that file.")
	}
	return path, nil
}

// pickSave opens the save dialog once at a time; an empty path means cancelled.
func (s *Service) pickSave(dialog SaveDialog) (string, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return "", errors.New("Dashboard is closing.")
	}
	if s.picking {
		s.mu.Unlock()
		return "", errors.New("A file dialog is already open.")
	}
	s.picking = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.picking = false; s.mu.Unlock() }()
	path, err := s.opts.Save(dialog)
	if err != nil {
		return "", errors.New("The file dialog failed.")
	}
	return path, nil
}
