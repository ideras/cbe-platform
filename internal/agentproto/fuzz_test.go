package agentproto

import (
	"bytes"
	"encoding/json"
	"testing"
)

// The agent's decoder is the first thing any bytes on the socket meet, and the socket is open to the student. These targets feed it
// whatever the fuzzer invents. Plain `go test` runs the seeds; the Phase 2 acceptance runs each for a minute:
//
//	go test ./internal/agentproto -run '^$' -fuzz FuzzDecodeRequest -fuzztime 60s
func FuzzDecodeRequest(f *testing.F) {
	for _, seed := range []string{
		`{"type":"fs.list","path":""}`, `{"id":3,"type":"fs.read","path":"src/a.cpp"}`, `{"type":"fs.write","path":"a","content":"x","base_version":"v"}`,
		`{"type":"fs.rename","path":"a","to":"b"}`, `{"type":"pty.open","cols":80,"rows":24,"cmd":"make"}`, `{"type":"pty.resize","term":1,"cols":1,"rows":1}`,
		`{"type":"fs.read","path":"../../etc/passwd"}`, `{"type":"fs.read","path":"/etc/passwd"}`, `{"type":"fs.read","path":"a\u0000b"}`,
		`{"type":"fs.delete","path":""}`, `{"type":"agent.ping"}{"type":"agent.ping"}`, `[]`, `null`, `"x"`, `{"type":1}`, `{"type":"pty.close","term":-1}`,
		"\xef\xbb\xbf{}", `{"type":"fs.read","path":"` + "\xff" + `"}`, `{"id":18446744073709551615,"type":"agent.ping"}`, `{"id":1e30,"type":"agent.ping"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		request, err := DecodeRequest(payload)
		if err != nil {
			if request != nil {
				t.Fatalf("a request and an error: %v, %v", request, err)
			}
			if _, isError := err.(*Error); !isError {
				t.Fatalf("the error is not an *Error: %T", err)
			}
			return
		}
		// Whatever it accepted must be what the protocol defines: it validates again, its paths pass, and it encodes to JSON the
		// decoder accepts once more.
		if err := request.Validate(); err != nil {
			t.Fatalf("an accepted request does not validate: %v", err)
		}
		again, err := Encode(request)
		if err != nil {
			t.Fatalf("an accepted request does not encode: %v", err)
		}
		if _, err := DecodeRequest(again); err != nil {
			t.Fatalf("%s was accepted but its encoding %s was not: %v", payload, again, err)
		}
		if !json.Valid(payload) {
			t.Fatalf("invalid JSON was accepted: %q", payload)
		}
	})
}

func FuzzReadFrame(f *testing.F) {
	var seed bytes.Buffer
	_ = WriteFrame(&seed, Frame{Kind: KindJSON, Data: []byte(`{"type":"agent.ping"}`)})
	_ = WriteFrame(&seed, Frame{Kind: KindBinary, Term: 2, Data: []byte("ls\n")})
	f.Add(seed.Bytes())
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{0, 0, 0, 3, 2, 0, 1})
	f.Fuzz(func(t *testing.T, wire []byte) {
		reader := bytes.NewReader(wire)
		for range 64 {
			frame, err := ReadFrame(reader)
			if err != nil {
				return
			}
			if frame.Kind != KindJSON && frame.Kind != KindBinary {
				t.Fatalf("kind %d", frame.Kind)
			}
			var back bytes.Buffer
			if err := WriteFrame(&back, frame); err != nil {
				t.Fatalf("a frame that was read cannot be written: %v", err)
			}
		}
	})
}

func FuzzCheckPath(f *testing.F) {
	for _, seed := range []string{"", "a/b", "../a", "/a", "a//b", "a\x00", ".cbe-x", "a/./b", "\xff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, path string) {
		if CheckPath(path) != nil {
			return
		}
		// A path that passes has no way out of the folder it is relative to.
		for _, name := range bytes.Split([]byte(path), []byte("/")) {
			if path != "" && (len(name) == 0 || string(name) == "." || string(name) == "..") {
				t.Fatalf("%q passed", path)
			}
		}
		if bytes.ContainsAny([]byte(path), "\x00\\") || len(path) > MaxPathBytes || (len(path) > 0 && path[0] == '/') {
			t.Fatalf("%q passed", path)
		}
	})
}
