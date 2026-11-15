package workspace

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)

func TestTheLiveLogIsReadFromAnOffsetAndNothingIsSentTwice(t *testing.T) {
	var l liveLog
	l.add("Base image: cbe/cpp20\n", t0)
	text, start, next := l.read(0)
	if text != "Base image: cbe/cpp20\n" || start != 0 || next != int64(len(text)) {
		t.Fatalf("%q %d %d", text, start, next)
	}
	// Nothing new: nothing sent.
	if again, from, n := l.read(next); again != "" || from != next || n != next {
		t.Errorf("a look with nothing new: %q %d %d", again, from, n)
	}
	l.add("STEP 1/2: RUN make\n", t0.Add(time.Second))
	more, from, n := l.read(next)
	if more != "STEP 1/2: RUN make\n" || from != next || n != next+int64(len(more)) {
		t.Errorf("what is new: %q %d %d", more, from, n)
	}
	// An offset this log does not have (a look made before a restart, or nonsense) starts again from the beginning.
	for _, bad := range []int64{-1, 1 << 40} {
		if all, from, _ := l.read(bad); from != 0 || !strings.HasPrefix(all, "Base image") || !strings.HasSuffix(all, "RUN make\n") {
			t.Errorf("offset %d: %q from %d", bad, all, from)
		}
	}
	if snapshot := l.snapshot(); snapshot != "Base image: cbe/cpp20\nSTEP 1/2: RUN make\n" {
		t.Errorf("the snapshot: %q", snapshot)
	}
}

// A character that a chunk of output cuts in two is not sent in halves: it is sent whole, when the rest of it has come.
func TestACharacterCutBetweenTwoChunksIsSentWhole(t *testing.T) {
	var l liveLog
	l.add("caf\xc3", t0)
	text, _, next := l.read(0)
	if text != "caf" || next != 3 {
		t.Fatalf("the first look: %q %d", text, next)
	}
	l.add("\xa9 ok\n", t0)
	text, _, next = l.read(next)
	if text != "é ok\n" || strings.ContainsRune(text, '�') {
		t.Errorf("the second look: %q", text)
	}
	// Euro sign: three bytes, cut after the first and after the second.
	var e liveLog
	e.add("a\xe2", t0)
	if text, _, n := e.read(0); text != "a" || n != 1 {
		t.Errorf("one byte of three: %q %d", text, n)
	}
	e.add("\x82", t0)
	if text, _, n := e.read(0); text != "a" || n != 1 {
		t.Errorf("two bytes of three: %q %d", text, n)
	}
	e.add("\xac\n", t0)
	if text, _, _ := e.read(1); text != "€\n" {
		t.Errorf("all three: %q", text)
	}
	// Bytes that are not UTF-8 at all do not stop the log: they are shown as the replacement character.
	var bad liveLog
	bad.add("x\xffy\n", t0)
	if text, _, _ := bad.read(0); text != "x�y\n" {
		t.Errorf("not UTF-8: %q", text)
	}
}

func TestTheBuildersStepsAreReadEvenWhenALineIsCutBetweenChunks(t *testing.T) {
	var l liveLog
	if step, steps, text, last := l.progress(); step != 0 || steps != 0 || text != "" || !last.IsZero() {
		t.Errorf("before anything: %d %d %q %v", step, steps, text, last)
	}
	l.add("STEP 1/9: FROM x\nsome output\nSTEP 2/", t0)
	if step, steps, _, _ := l.progress(); step != 1 || steps != 9 {
		t.Errorf("a line cut in two is not read yet: %d of %d", step, steps)
	}
	l.add("9: RUN apt-get install -y make\r\n", t0.Add(3*time.Second))
	step, steps, text, last := l.progress()
	if step != 2 || steps != 9 || text != "RUN apt-get install -y make" || !last.Equal(t0.Add(3*time.Second)) {
		t.Errorf("%d of %d %q %v", step, steps, text, last)
	}
	// Output that looks like a step but is not at the start of a line is not one; a long step is cut.
	l.add("see STEP 7/9: RUN x\n", t0)
	if step, _, _, _ := l.progress(); step != 2 {
		t.Errorf("a step in the middle of a line: %d", step)
	}
	l.add("STEP 3/9: "+strings.Repeat("y", 2000)+"\n", t0)
	if _, _, text, _ := l.progress(); len(text) > 310 {
		t.Errorf("a step of %d bytes", len(text))
	}
	// A line with no end is not kept for ever.
	l.add(strings.Repeat("z", 100000), t0)
	l.mu.Lock()
	pending := len(l.pending)
	l.mu.Unlock()
	if pending > maxPendingLine {
		t.Errorf("the unfinished line is %d bytes", pending)
	}
}

func TestTheLiveLogStopsAtItsLimitAndSaysSoButGoesOnReadingProgress(t *testing.T) {
	var l liveLog
	chunk := strings.Repeat("x", 99) + "\n"
	for i := 0; i < maxLiveLog/len(chunk)+100; i++ {
		l.add(chunk, t0)
	}
	l.add("STEP 5/5: RUN last\n", t0)
	text := l.snapshot()
	if len(text) > maxLiveLog+len(liveCapNote) || !strings.HasSuffix(text, liveCapNote) {
		t.Fatalf("%d bytes, ends %q", len(text), text[max(0, len(text)-80):])
	}
	if step, steps, _, _ := l.progress(); step != 5 || steps != 5 {
		t.Errorf("the progress after the cap: %d of %d", step, steps)
	}
	read, _, next := l.read(0)
	if len(read) != len(text) || next != int64(len(text)) {
		t.Errorf("a look: %d of %d bytes, next %d", len(read), len(text), next)
	}
}
