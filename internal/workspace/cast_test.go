package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCast(t *testing.T, limit int64, pieces ...string) ([]byte, int64, bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s", "1.cast")
	start := time.Unix(1_800_000_000, 0)
	w, err := newCastWriter(path, start, 80, 24, "Terminal", limit)
	if err != nil {
		t.Fatal(err)
	}
	for i, piece := range pieces {
		w.Write(start.Add(time.Duration(i+1)*250*time.Millisecond), []byte(piece))
	}
	bytes, truncated := w.Close(start.Add(time.Hour))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data, bytes, truncated
}

func replayText(t *testing.T, data []byte) string {
	t.Helper()
	_, events, err := ReplayCast(data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	var out strings.Builder
	for _, e := range events {
		out.WriteString(e.Text)
	}
	return out.String()
}

func TestATranscriptIsAnAsciinemaV2File(t *testing.T) {
	data, written, truncated := writeCast(t, 1<<20, "hello ", "world\r\n")
	header, events, err := ReplayCast(data)
	if err != nil || header.Version != 2 || header.Width != 80 || header.Height != 24 || header.Timestamp != 1_800_000_000 || header.Title != "Terminal" {
		t.Fatalf("header = %+v, %v", header, err)
	}
	if len(events) != 2 || events[0].At != 0.25 || events[1].At != 0.5 || events[0].Text != "hello " || events[1].Text != "world\r\n" || written != 13 || truncated {
		t.Errorf("events = %+v, %d bytes, truncated %v", events, written, truncated)
	}
	// Each line is a JSON array of the format's three things, which is all a player needs.
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[1], `[0.25,"o","hello "]`) {
		t.Errorf("lines = %q", lines)
	}
}

func TestACharacterCutBetweenTwoPiecesIsKeptWhole(t *testing.T) {
	// "é" is C3 A9, "世" is E4 B8 96, and the emoji is F0 9F 98 80: each is cut at every place it can be.
	whole := "café 世界 \U0001F600 end"
	bytes := []byte(whole)
	for cut := 1; cut < len(bytes); cut++ {
		data, _, _ := writeCast(t, 1<<20, string(bytes[:cut]), string(bytes[cut:]))
		if got := replayText(t, data); got != whole {
			t.Fatalf("cut at %d: %q", cut, got)
		}
	}
	// Three pieces, a character in each of two joins.
	data, _, _ := writeCast(t, 1<<20, string(bytes[:4]), string(bytes[4:5]), string(bytes[5:]))
	if got := replayText(t, data); got != whole {
		t.Errorf("three pieces: %q", got)
	}
}

func TestBytesThatAreNoCharacterBecomeTheReplacementCharacter(t *testing.T) {
	data, _, _ := writeCast(t, 1<<20, "a\xffb", "\xe4\xb8", "c")
	if got := replayText(t, data); got != "a�b�c" {
		t.Errorf("got %q", got)
	}
	// A character that is never finished is written when the transcript ends, so it is not lost silently.
	data, _, _ = writeCast(t, 1<<20, "end\xe4\xb8")
	if got := replayText(t, data); got != "end�" {
		t.Errorf("an unfinished character at the end: %q", got)
	}
}

func TestATranscriptStopsAtItsLimitAndSaysSo(t *testing.T) {
	data, written, truncated := writeCast(t, 10, "12345", "6789012345", "never written")
	text := replayText(t, data)
	if !truncated || written != 10 || !strings.HasPrefix(text, "123456789012345"[:10]) || !strings.Contains(text, "The transcript stops here") || strings.Contains(text, "never written") {
		t.Errorf("truncated %v, %d bytes: %q", truncated, written, text)
	}
	// Exactly at the limit is not past it.
	if _, _, truncated := writeCast(t, 5, "12345"); truncated {
		t.Error("a transcript at its limit was cut")
	}
}

func TestACastThatIsNotOneIsRefused(t *testing.T) {
	for name, data := range map[string]string{
		"empty":             "",
		"no header":         `[0.1,"o","x"]` + "\n",
		"version 1":         `{"version":1,"width":80,"height":24}` + "\n",
		"no size":           `{"version":2}` + "\n",
		"not an event":      `{"version":2,"width":80,"height":24}` + "\nnope\n",
		"a short event":     `{"version":2,"width":80,"height":24}` + "\n" + `[0.1,"o"]` + "\n",
		"time going back":   `{"version":2,"width":80,"height":24}` + "\n" + `[0.5,"o","a"]` + "\n" + `[0.1,"o","b"]` + "\n",
		"a text that isn't": `{"version":2,"width":80,"height":24}` + "\n" + `[0.5,"o",7]` + "\n",
	} {
		if _, _, err := ReplayCast([]byte(data)); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
	// Input events are skipped, not refused: a player's own recordings have them.
	if _, events, err := ReplayCast([]byte(`{"version":2,"width":80,"height":24}` + "\n" + `[0.1,"i","k"]` + "\n" + `[0.2,"o","out"]` + "\n")); err != nil || len(events) != 1 || events[0].Text != "out" {
		t.Errorf("events = %+v, %v", events, err)
	}
}
