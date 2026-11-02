package service_test

import (
	"errors"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// refusedAt tries to log in at the given time after the epoch and says how many seconds the session is still assumed open.
func (f *fixture) refusedAt(t *testing.T, after time.Duration, account string) int64 {
	t.Helper()
	f.clock.Set(epoch.Add(after))
	_, err := f.sessions.Login(t.Context(), account, "exam-secret")
	var open *service.SessionOpenError
	if !errors.As(err, &open) || !errors.Is(err, service.ErrSessionOpen) {
		t.Fatalf("login at +%s = %v, want the session to be open", after, err)
	}
	return open.Seconds
}

func (f *fixture) acceptedAt(t *testing.T, after time.Duration, account string) service.LoginResult {
	t.Helper()
	f.clock.Set(epoch.Add(after))
	return f.login(t, account)
}

func TestASessionIsOpenUntilItsApplicationHasBeenSilentForTheOverdueThreshold(t *testing.T) {
	f := newFixture(t, 0)
	first := f.login(t, "0012")
	// The policy's threshold is 30 seconds; the login itself counts as the first sign of life.
	if left := f.refusedAt(t, 5*time.Second, "0012"); left != 25 {
		t.Fatalf("seconds left at +5s = %d", left)
	}
	if left := f.refusedAt(t, 30*time.Second, "0012"); left != 1 {
		t.Fatalf("seconds left at the threshold = %d", left)
	}
	// Refused attempts are not signs of life: they never keep the session open for the next one.
	f.acceptedAt(t, 30*time.Second+time.Millisecond, "0012")
	if principal, err := f.sessions.Authenticate(t.Context(), first.Token); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("the application that was silent kept its token: %+v, %v", principal.Session.ID, err)
	}
}

func TestHeartbeatsKeepASessionOpen(t *testing.T) {
	f := newFixture(t, 0)
	first := f.login(t, "0012")
	f.clock.Set(epoch.Add(20 * time.Second))
	heartbeat(t, f, first.Token)
	// 45 seconds after the login but only 25 after the heartbeat: still open, for 5 more seconds.
	if left := f.refusedAt(t, 45*time.Second, "0012"); left != 5 {
		t.Fatalf("seconds left = %d", left)
	}
	f.acceptedAt(t, 51*time.Second, "0012")
}

func TestOnlyTheAccountThatIsOpenIsRefused(t *testing.T) {
	f := newFixture(t, 0)
	f.login(t, "0012")
	f.refusedAt(t, time.Second, "0012")
	// Another student, or the same one in an exam they have not started, is not affected.
	f.acceptedAt(t, time.Second, "0034")
}

func TestARefusedLoginIsRecordedAndShownToTheProctor(t *testing.T) {
	f := newFixture(t, 0)
	first := f.login(t, "0012")
	rows := func() domain.LoginGate {
		t.Helper()
		var gate domain.LoginGate
		if err := f.store.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
			var err error
			gate, err = tx.LoginGate(t.Context(), first.SessionID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return gate
	}
	list, err := f.sessions.ProctorSessions(t.Context(), testExamID)
	if err != nil || len(list.Sessions) != 1 || list.Sessions[0].LoginGate != nil {
		t.Fatalf("a session nobody tried to open reports a gate: %+v, %v", list.Sessions, err)
	}
	f.clock.Set(epoch.Add(4 * time.Second))
	for _, address := range []string{"192.0.2.77", "192.0.2.88"} {
		if _, err := f.sessions.Login(service.WithRemoteAddr(t.Context(), address), "0012", "exam-secret"); !errors.Is(err, service.ErrSessionOpen) {
			t.Fatalf("login = %v", err)
		}
		f.clock.Set(f.clock.Now().Add(time.Second))
	}
	list, err = f.sessions.ProctorSessions(t.Context(), testExamID)
	if err != nil {
		t.Fatal(err)
	}
	gate := list.Sessions[0].LoginGate
	if gate == nil || gate.Refusals != 2 || gate.LastRefusedAddr != "192.0.2.88" || gate.LastRefusedAt == nil || !gate.LastRefusedAt.Equal(epoch.Add(5*time.Second)) || gate.ReleasedAt != nil {
		t.Fatalf("roster gate = %+v", gate)
	}
	// The refusals are in the session's history, with the address they came from.
	if f.count(t, `SELECT count(*) FROM event_log WHERE session_id = ? AND event_type = 'login_refused' AND json_extract(detail, '$.remote_addr') = '192.0.2.77'`, first.SessionID) != 1 {
		t.Fatal("the first refusal is not in the history")
	}
	// An accepted login ends the matter: the next report starts from nothing.
	f.acceptedAt(t, time.Minute, "0012")
	if g := rows(); g.Refusals != 0 || g.ReleasedAt != nil {
		t.Fatalf("after an accepted login: %+v", g)
	}
}

func TestAReleaseLetsExactlyOneLoginIn(t *testing.T) {
	f := newFixture(t, 0)
	first := f.login(t, "0012")
	f.refusedAt(t, 5*time.Second, "0012")
	released, err := f.sessions.ReleaseLogin(t.Context(), first.SessionID, "the student's laptop went flat")
	if err != nil || released.SessionID != first.SessionID || !released.ReleasedAt.Equal(epoch.Add(5*time.Second)) {
		t.Fatalf("release = %+v, %v", released, err)
	}
	// A second click while the release waits for its login changes nothing, and is not another history row.
	f.clock.Set(epoch.Add(6 * time.Second))
	again, err := f.sessions.ReleaseLogin(t.Context(), first.SessionID, "clicked twice")
	if err != nil || !again.ReleasedAt.Equal(released.ReleasedAt) || f.count(t, `SELECT count(*) FROM event_log WHERE event_type = 'login_released'`) != 1 {
		t.Fatalf("second release = %+v, %v", again, err)
	}
	list, err := f.sessions.ProctorSessions(t.Context(), testExamID)
	if err != nil || list.Sessions[0].LoginGate == nil || list.Sessions[0].LoginGate.ReleasedAt == nil {
		t.Fatalf("the roster does not show the release: %+v, %v", list.Sessions[0].LoginGate, err)
	}
	// The login that follows is accepted although the first application was heard from a second ago; it replaces the token.
	second := f.acceptedAt(t, 7*time.Second, "0012")
	if second.SessionID != first.SessionID || second.Token == first.Token {
		t.Fatalf("the released login = %+v", second)
	}
	if _, err := f.sessions.Heartbeat(t.Context(), first.Token, "student-pc", "192.0.2.1", domain.Sound{}); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("the application that was released still reports in: %v", err)
	}
	// The release was used up: the next login finds the session open again.
	f.refusedAt(t, 8*time.Second, "0012")
	var detail string
	if err := f.raw.QueryRow(`SELECT detail FROM event_log WHERE event_type = 'login' ORDER BY id DESC LIMIT 1`).Scan(&detail); err != nil || detail != `{"resumed":true,"released":true}` {
		t.Fatalf("login detail = %s, %v", detail, err)
	}
}

func TestOnlyARunningSessionCanBeReleased(t *testing.T) {
	f := newFixture(t, 0)
	first := f.login(t, "0012")
	for name, input := range map[string][2]string{
		"no reason":        {first.SessionID, "  "},
		"a control char":   {first.SessionID, "bad\x00reason"},
		"not a session id": {"nope", "reason"},
	} {
		if _, err := f.sessions.ReleaseLogin(t.Context(), input[0], input[1]); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := f.sessions.ReleaseLogin(t.Context(), "00000000-0000-4000-8000-0000000000ff", "reason"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown session: %v", err)
	}
	if _, err := f.sessions.Submit(t.Context(), first.Token, domain.Answers{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.ReleaseLogin(t.Context(), first.SessionID, "too late"); !errors.Is(err, service.ErrAlreadySubmitted) {
		t.Fatalf("submitted session: %v", err)
	}
}

// A student who closes the application and opens it again does not wait: the application announces its closing, and only the
// holder of the session's token can do that.
func TestAnApplicationThatAnnouncedItsClosingNoLongerHoldsTheSession(t *testing.T) {
	f := newFixture(t, 0)
	first := f.login(t, "0012")
	f.clock.Set(epoch.Add(2 * time.Second))
	heartbeat(t, f, first.Token)
	clientEvent(t, f, first.Token, "app_started")
	f.refusedAt(t, 3*time.Second, "0012")
	clientEvent(t, f, first.Token, "app_closed")
	// It was heard from a second ago, well inside the 30 seconds, yet the session is free.
	second := f.acceptedAt(t, 4*time.Second, "0012")
	if second.SessionID != first.SessionID || second.Token == first.Token {
		t.Fatalf("the login after a clean close = %+v", second)
	}
	// The new login holds the session from that moment, before its application has said anything: a closing that came before
	// it does not free it.
	f.refusedAt(t, 5*time.Second, "0012")
	clientEvent(t, f, second.Token, "app_started")
	f.refusedAt(t, 6*time.Second, "0012")
}
