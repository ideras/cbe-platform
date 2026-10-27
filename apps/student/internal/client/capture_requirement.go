package client

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"time"
)

type CaptureTestResult struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type captureReport struct {
	id, kind, reason string
	at               time.Time
	incident         uint64
}

func (m *captureManager) reportLocked(kind, reason string) {
	if m.token == "" || m.closed {
		return
	}
	m.reports = append(m.reports, &captureReport{id: rand.Text(), kind: kind, reason: reason, at: time.Now().UTC(), incident: m.state.Incident})
	signal(m.reportWake)
}

// Capture-health reports are not dropped after the ordinary focus FIFO's retry limit.
func (m *captureManager) reportLoop() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.reportWake:
		}
		delay := m.retryMin
		for {
			m.mu.Lock()
			if len(m.reports) == 0 || m.closed {
				m.mu.Unlock()
				break
			}
			report, base, token := m.reports[0], m.base, m.token
			m.mu.Unlock()
			var reply eventAck
			err := requestJSON(m.ctx, m.http, base, token, http.MethodPost, "/event", struct {
				Type   string         `json:"event_type"`
				Detail map[string]any `json:"detail"`
			}{report.kind, map[string]any{"client_report_id": report.id, "client_time": report.at.Format(time.RFC3339Nano), "source": "capture_health", "reason": report.reason, "local_incident": report.incident}}, &reply)
			if err == nil && (reply.ID <= 0 || reply.ServerTime.IsZero()) {
				err = errors.New("invalid capture-log acknowledgment")
			}
			m.mu.Lock()
			if err == nil {
				m.reports = m.reports[1:]
				m.state.ReportError = ""
				delay = m.retryMin
			} else {
				m.state.ReportError = "Capture status has not been acknowledged by the server. Reporting will retry while this app remains open; editing stays locked."
			}
			m.mu.Unlock()
			if err != nil {
				timer := time.NewTimer(delay)
				select {
				case <-m.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				delay = min(delay*2, m.retryMax)
			}
		}
	}
}

func (m *captureManager) test(ctx context.Context) (CaptureTestResult, error) {
	var result CaptureTestResult
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil || m.options.Test == nil || m.options.Capture == nil {
		m.mu.Unlock()
		return result, errors.New("Native screenshot verification is unavailable.")
	}
	if m.capturing || m.state.Testing || len(m.reports) > 0 {
		m.mu.Unlock()
		return result, errors.New("Wait for the current capture or server log acknowledgment before retesting.")
	}
	m.state.Testing = true
	m.wg.Add(1)
	m.mu.Unlock()
	defer m.wg.Done()
	request, cancel := context.WithTimeout(m.ctx, 2*time.Minute)
	stop := context.AfterFunc(ctx, cancel)
	if ctx.Err() != nil {
		cancel()
	}
	defer func() { stop(); cancel() }()
	result, err := m.options.Test(request)
	if err == nil {
		err = request.Err()
	}
	if err == nil && (result.Width <= 0 || result.Height <= 0 || result.Width > 16384 || result.Height > 16384 || int64(result.Width)*int64(result.Height) > 40000000) {
		err = errors.New("invalid screenshot test result")
	}
	var permission CapturePermission
	if err == nil && m.options.Permission != nil {
		permission = m.options.Permission(false)
		if !permission.Supported || (!permission.Granted && !permission.TestRequired) {
			err = errors.New("automatic capture access is unavailable")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Testing = false
	if m.closed {
		return CaptureTestResult{}, errors.New("Screenshot test cancelled during shutdown.")
	}
	if err == nil {
		err = request.Err()
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && len(m.reports) > 0 {
		err = errors.New("capture status changed during verification")
	}
	if err != nil {
		m.failLocked("Screenshot verification failed or was cancelled. Editing requires a successful retest; current answers are preserved.")
		return CaptureTestResult{}, errors.New("Screenshot verification failed or was cancelled. Check permission/display support and retry.")
	}
	m.state.Ready = true
	m.state.Permission = permission
	m.state.Error = ""
	m.reportLocked("capture_restored", "successful_screenshot_retest")
	return result, nil
}

func (s *Service) TestScreenshot(ctx context.Context) (CaptureTestResult, error) {
	if !s.loginMu.TryLock() {
		return CaptureTestResult{}, errors.New("A login, load or screenshot test is already running.")
	}
	defer s.loginMu.Unlock()
	s.mu.Lock()
	allowed := s.phase != "complete" && s.pendingSubmit == nil
	s.mu.Unlock()
	if !allowed {
		return CaptureTestResult{}, errors.New("Screenshot retesting is unavailable for a finalized or submitting exam.")
	}
	return s.capture.test(ctx)
}
