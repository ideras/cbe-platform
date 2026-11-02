package client

import (
	"strings"
	"testing"
)

// The server refuses a login for an account that is open on another computer with a conflict that says how long to wait. The
// student reads that, not the text of the other conflicts, and no session is started.
func TestALoginForAnAccountOpenElsewhereSaysWhenToTryAgain(t *testing.T) {
	for _, test := range []struct{ retryAfter, wait string }{
		{"1", "in 1 second,"},
		{"18", "in 18 seconds,"},
		{"90", "in 90 seconds,"},
		{"91", "in 2 minutes,"},
		{"300", "in 5 minutes,"},
	} {
		f := newFixture(t)
		f.loginCode, f.loginRetryAfter = 409, test.retryAfter
		s := newClient(t, f, nil)
		_, err := s.Login(t.Context(), f.server.URL, "00123", f.pin)
		if err == nil || !strings.Contains(err.Error(), "already open on another computer") || !strings.Contains(err.Error(), test.wait) || !strings.Contains(err.Error(), "call your instructor") {
			t.Fatalf("Retry-After %s: error = %v", test.retryAfter, err)
		}
		if retryable(err) || failureCode(err) != 409 {
			t.Fatalf("the refusal is %d, retryable %v", failureCode(err), retryable(err))
		}
		if view := s.Snapshot(); view.Phase == "exam" || view.Exam != nil {
			t.Fatalf("a refused login started a session: %+v", view.Phase)
		}
	}
}

// The other conflicts keep their own text: a conflict without the header is a submitted session or a moved exercise.
func TestAConflictWithoutRetryAfterKeepsTheOldMessage(t *testing.T) {
	for _, header := range []string{"", "soon", "0", "-5"} {
		f := newFixture(t)
		f.loginCode, f.loginRetryAfter = 409, header
		s := newClient(t, f, nil)
		_, err := s.Login(t.Context(), f.server.URL, "00123", f.pin)
		if err == nil || strings.Contains(err.Error(), "another computer") || !strings.Contains(err.Error(), "already been submitted") {
			t.Fatalf("Retry-After %q: error = %v", header, err)
		}
	}
}

// An application whose session was taken over by another login is not told to check its PIN: its token simply stopped being
// recognised, and what it can do about that is to call the instructor.
func TestAReplacedTokenSaysTheAccountWasOpenedElsewhere(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	f.mu.Lock()
	f.token = "the token another login replaced this one with"
	f.mu.Unlock()
	view := waitView(t, s, func(v View) bool { return v.StatusError != "" })
	if !strings.Contains(view.StatusError, "opened on another computer") || strings.Contains(view.StatusError, "PIN") {
		t.Fatalf("status error = %q", view.StatusError)
	}
}
