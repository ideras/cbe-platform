package workspace

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// castWriter writes one terminal's transcript as an asciinema v2 file (https://docs.asciinema.org/manual/asciicast/v2/): a line with
// a JSON header, then one line for each piece of output, [seconds since the start, "o", text]. Any player of the format replays it,
// and so does the proctor's. Only output is recorded, not the keys the student typed: what they typed shows in it anyway when the
// terminal echoes it, and a program that does not echo (a password prompt) is not given away.
//
// The text of an event must be valid UTF-8, and a program's output is bytes: a character cut between two pieces is held for the next
// one, and a byte that is not part of any character becomes U+FFFD.
type castWriter struct {
	file      *os.File
	start     time.Time
	limit     int64
	written   int64
	truncated bool
	pending   []byte
}

// maxTranscriptBytes is how much output of one terminal a transcript keeps. A program that prints without end makes a transcript of
// this size and a line saying so, not a file without a limit.
const maxTranscriptBytes = 4 << 20

func newCastWriter(path string, start time.Time, cols, rows int, title string, limit int64) (*castWriter, error) {
	if err := os.MkdirAll(dirOf(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	header, err := json.Marshal(struct {
		Version   int               `json:"version"`
		Width     int               `json:"width"`
		Height    int               `json:"height"`
		Timestamp int64             `json:"timestamp"`
		Title     string            `json:"title,omitempty"`
		Env       map[string]string `json:"env"`
	}{2, max(cols, 1), max(rows, 1), start.Unix(), title, map[string]string{"SHELL": "/bin/sh", "TERM": "xterm-256color"}})
	if err != nil {
		file.Close()
		return nil, err
	}
	if _, err := file.Write(append(header, '\n')); err != nil {
		file.Close()
		return nil, err
	}
	return &castWriter{file: file, start: start, limit: limit}, nil
}

func dirOf(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return "."
}

// Write adds what a program printed at a time. Once the limit is passed nothing more is written but a line saying so.
func (w *castWriter) Write(at time.Time, data []byte) {
	if w.truncated || len(data) == 0 {
		return
	}
	if w.written+int64(len(data)) > w.limit {
		data = data[:max(0, w.limit-w.written)]
		w.written = w.limit
		w.truncated = true
		w.event(at, string(w.take(data))+"\r\n[The transcript stops here: this terminal printed more than the transcript keeps.]\r\n")
		return
	}
	w.written += int64(len(data))
	if text := w.take(data); text != "" {
		w.event(at, text)
	}
}

// take turns bytes into whole text, keeping a character that is cut at the end for the next piece.
func (w *castWriter) take(data []byte) string {
	buf := append(w.pending, data...)
	end := len(buf)
	for i := 1; i <= 3 && i <= len(buf); i++ {
		if utf8.RuneStart(buf[len(buf)-i]) {
			if !utf8.FullRune(buf[len(buf)-i:]) {
				end = len(buf) - i
			}
			break
		}
	}
	w.pending = append([]byte(nil), buf[end:]...)
	return strings.ToValidUTF8(string(buf[:end]), "�")
}

func (w *castWriter) event(at time.Time, text string) {
	seconds := math.Round(at.Sub(w.start).Seconds()*1e6) / 1e6
	if seconds < 0 {
		seconds = 0
	}
	line, err := json.Marshal([]any{seconds, "o", text})
	if err != nil {
		return
	}
	_, _ = w.file.Write(append(line, '\n'))
}

// Close ends the file: a character that was cut and never finished is written as the replacement character. It returns how much
// output was written and whether the limit cut it.
func (w *castWriter) Close(at time.Time) (int64, bool) {
	if len(w.pending) > 0 && !w.truncated {
		w.event(at, "�")
		w.pending = nil
	}
	_ = w.file.Close()
	return w.written, w.truncated
}

// ReplayCast reads a transcript back: its header and the text of its output events in order, with the time of each. It is what the
// proctor's player does, and what the tests hold a transcript to.
func ReplayCast(data []byte) (header CastHeader, events []CastEvent, err error) {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return header, nil, fmt.Errorf("the transcript is empty")
	}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil || header.Version != 2 || header.Width < 1 || header.Height < 1 {
		return header, nil, fmt.Errorf("the transcript has no valid header")
	}
	last := 0.0
	for number, line := range lines[1:] {
		var raw []json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil || len(raw) != 3 {
			return header, events, fmt.Errorf("line %d is not an event", number+2)
		}
		var event CastEvent
		var kind string
		if json.Unmarshal(raw[0], &event.At) != nil || json.Unmarshal(raw[1], &kind) != nil || json.Unmarshal(raw[2], &event.Text) != nil || event.At < last {
			return header, events, fmt.Errorf("line %d is not an event in time order", number+2)
		}
		last = event.At
		if kind == "o" {
			events = append(events, event)
		}
	}
	return header, events, nil
}

// CastHeader and CastEvent are the parts of an asciinema v2 transcript the server reads.
type CastHeader struct {
	Version   int    `json:"version"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Timestamp int64  `json:"timestamp"`
	Title     string `json:"title"`
}
type CastEvent struct {
	At   float64
	Text string
}
