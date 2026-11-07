package sessionwatch

import "testing"

// The session is on screen while it is the one attached to the console, and not once another is.
func TestTheSessionIsOnScreenWhileItHoldsTheConsole(t *testing.T) {
	p := &consoleProbe{own: 2}
	for _, step := range []struct {
		console uint32
		on      bool
	}{{2, true}, {2, true}, {3, false}, {3, false}, {2, true}, {7, false}, {2, true}} {
		on, known := p.onScreen(step.console)
		if !known || on != step.on {
			t.Fatalf("console %d: on = %v, known = %v; want on = %v, known", step.console, on, known, step.on)
		}
	}
}

// A moment with no session on the console, as between two users, says nothing; a console that stays without one is not ours.
func TestAMomentWithoutASessionOnTheConsoleSaysNothing(t *testing.T) {
	p := &consoleProbe{own: 2}
	for i := 1; i <= unattachedReadings; i++ {
		if _, known := p.onScreen(noSession); known {
			t.Fatalf("reading %d without a session was taken as an answer", i)
		}
	}
	if on, known := p.onScreen(noSession); !known || on {
		t.Fatalf("a console that stayed without a session: on = %v, known = %v; want off and known", on, known)
	}
	if on, known := p.onScreen(2); !known || !on {
		t.Fatalf("the session back on the console: on = %v, known = %v", on, known)
	}
}

// A single gap between two readings of the session itself does not add up with an earlier one.
func TestGapsOnTheConsoleAreCountedInARow(t *testing.T) {
	p := &consoleProbe{own: 2}
	for round := 0; round < 3; round++ {
		for i := 0; i < unattachedReadings; i++ {
			p.onScreen(noSession)
		}
		if on, known := p.onScreen(2); !known || !on {
			t.Fatalf("round %d: on = %v, known = %v", round, on, known)
		}
	}
}
