package workspace

import (
	"context"
	"time"
)

// The events a workspace adds to a session's history (plan, Phase 5), written to the same table as the monitoring's. The details are
// the plan's, without the time: the record has one.
const (
	EventOpened         = "workspace.opened"
	EventDisconnected   = "workspace.disconnected"
	EventFileSaved      = "file.saved"
	EventFileCreated    = "file.created"
	EventFileRenamed    = "file.renamed"
	EventFileDeleted    = "file.deleted"
	EventPTYOpened      = "pty.opened"
	EventPTYClosed      = "pty.closed"
	EventRunStarted     = "run.started"
	EventTodoSet        = "todo.set"
	EventPasteBlocked   = "paste.blocked"
	EventSubmitted      = "workspace.submitted"
	EventAgentRestarted = "workspace.agent_restarted"
	// The events of grading (Phase 6): a run that ended (done or failed), and a proctor's confirmation of a grade or its withdrawal.
	EventGradingFinished    = "grading.finished"
	EventGradingConfirmed   = "grading.confirmed"
	EventGradingUnconfirmed = "grading.unconfirmed"
)

// event records one in a session's history. A history that cannot be written to is logged and goes on: nothing a student does waits
// for it.
func (m *Manager) event(session, kind string, detail any) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.opts.Repo.RecordEvent(ctx, session, kind, m.opts.Now().UTC(), detail); err != nil {
		m.opts.Logger.Error("workspace: an event could not be recorded", "session", session, "event", kind, "error", err)
	}
}

// touch notes that the student did something now (the roster's "last activity").
func (m *Manager) touch(session string) {
	m.mu.Lock()
	m.activity[session] = m.opts.Now()
	m.mu.Unlock()
}

// LastActivity is when the student last did something through the workspace in this server's life, or the zero time.
func (m *Manager) LastActivity(session string) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activity[session]
}
