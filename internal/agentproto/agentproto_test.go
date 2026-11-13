package agentproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFramesRoundTrip(t *testing.T) {
	var wire bytes.Buffer
	for _, frame := range []Frame{
		{Kind: KindJSON, Data: []byte(`{"type":"agent.ping"}`)},
		{Kind: KindBinary, Term: 7, Data: []byte("hello\x00\xff")},
		{Kind: KindBinary, Term: 1},
		{Kind: KindJSON, Data: nil},
	} {
		if err := WriteFrame(&wire, frame); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []Frame{{KindJSON, 0, []byte(`{"type":"agent.ping"}`)}, {KindBinary, 7, []byte("hello\x00\xff")}, {KindBinary, 1, []byte{}}, {KindJSON, 0, []byte{}}} {
		got, err := ReadFrame(&wire)
		if err != nil || got.Kind != want.Kind || got.Term != want.Term || !bytes.Equal(got.Data, want.Data) {
			t.Fatalf("got %+v, %v; want %+v", got, err, want)
		}
	}
	if _, err := ReadFrame(&wire); err != io.EOF {
		t.Fatalf("at the end: %v, want io.EOF", err)
	}
}

func TestMalformedFramesAreRefused(t *testing.T) {
	prefixed := func(size uint32, body ...byte) []byte {
		return append(binary.BigEndian.AppendUint32(nil, size), body...)
	}
	for name, test := range map[string]struct {
		wire    []byte
		framing bool // the error is ErrFrame, not a short read
	}{
		"empty frame":         {prefixed(0), true},
		"over the limit":      {prefixed(MaxFrame + 1), true},
		"huge length":         {prefixed(0xffffffff), true},
		"unknown kind":        {prefixed(2, 9, 'x'), true},
		"terminal without id": {prefixed(3, KindBinary, 0, 1), true},
		"terminal data limit": {append(prefixed(1+4+MaxTerminalData+1, KindBinary, 0, 0, 0, 1), make([]byte, MaxTerminalData+1)...), true},
		"cut in the body":     {prefixed(10, KindJSON, '{'), false},
		"cut in the prefix":   {[]byte{0, 0}, false},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ReadFrame(bytes.NewReader(test.wire))
			if err == nil {
				t.Fatal("no error")
			}
			if test.framing != errors.Is(err, ErrFrame) {
				t.Fatalf("err = %v, ErrFrame = %v, want %v", err, errors.Is(err, ErrFrame), test.framing)
			}
		})
	}
	for name, frame := range map[string]Frame{"unknown kind": {Kind: 7}, "too much terminal data": {Kind: KindBinary, Data: make([]byte, MaxTerminalData+1)}, "too big": {Kind: KindJSON, Data: make([]byte, MaxFrame)}} {
		if err := WriteFrame(io.Discard, frame); !errors.Is(err, ErrFrame) {
			t.Errorf("WriteFrame(%s) = %v", name, err)
		}
	}
}

func TestCheckPath(t *testing.T) {
	for _, good := range []string{"", "a", "src/list.cpp", "a/b/c/d", "名前/ファイル.txt", "with space/and-dash_under.dot", ".hidden", "dir/.hidden/file", strings.Repeat("a", 255), "a..b", "...", "a/..b/c"} {
		if err := CheckPath(good); err != nil {
			t.Errorf("CheckPath(%q) = %v", good, err)
		}
	}
	for path, want := range map[string]string{
		"/etc/passwd": "absolute", "/": "absolute", "..": `".."`, "../x": `".."`, "a/../b": `".."`, "a/..": `".."`, ".": `"."`, "./a": `"."`, "a/./b": `"."`,
		"a//b": "empty name", "a/": "empty name", "a\x00b": "NUL", "a\\b": `\`, "a\nb": "control character", "a\x7fb": "control character", "a\tb": "control character",
		"\xff": "UTF-8", ".cbe-write-1": "kept for the system", "a/.cbe-x/b": "kept for the system",
		strings.Repeat("a", 256): "longer than 255", strings.Repeat("a/", 2049): "longer than 4096",
	} {
		err := CheckPath(path)
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeBadPath || !strings.Contains(e.Message, want) {
			t.Errorf("CheckPath(%q) = %v, want BAD_PATH with %q", path, err, want)
		}
	}
	for path, want := range map[string][2]string{"a": {"", "a"}, "a/b": {"a", "b"}, "a/b/c.txt": {"a/b", "c.txt"}} {
		if folder, name := SplitPath(path); folder != want[0] || name != want[1] {
			t.Errorf("SplitPath(%q) = %q, %q", path, folder, name)
		}
	}
}

func TestDecodeRequest(t *testing.T) {
	for payload, want := range map[string]string{
		`{"type":"fs.list","path":""}`:                                           TypeFSList,
		`{"type":"fs.list"}`:                                                     TypeFSList,
		`{"id":3,"type":"fs.read","path":"src/a.cpp"}`:                           TypeFSRead,
		`{"type":"fs.write","path":"a","content":"x","base_version":"sha256:1"}`: TypeFSWrite,
		`{"type":"fs.create","path":"a"}`:                                        TypeFSCreate,
		`{"type":"fs.create","path":"a","content":"text\nmore"}`:                 TypeFSCreate,
		`{"type":"fs.mkdir","path":"d"}`:                                         TypeFSMkdir,
		`{"type":"fs.rename","path":"a","to":"b"}`:                               TypeFSRename,
		`{"type":"fs.delete","path":"a"}`:                                        TypeFSDelete,
		`{"type":"pty.open","cols":80,"rows":24}`:                                TypePTYOpen,
		`{"type":"pty.open","cols":80,"rows":24,"cmd":"make run","cwd":"src"}`:   TypePTYOpen,
		`{"type":"pty.attach","term":2}`:                                         TypePTYAttach,
		`{"type":"pty.list"}`:                                                    TypePTYList,
		`{"type":"pty.resize","term":1,"cols":100,"rows":30}`:                    TypePTYResize,
		`{"type":"pty.close","term":1}`:                                          TypePTYClose,
		`{"type":"agent.ping"}`:                                                  TypePing,
		" \n{\"type\":\"agent.ping\"}\n ":                                        TypePing,
	} {
		request, err := DecodeRequest([]byte(payload))
		if err != nil || request.Head().Type != want {
			t.Errorf("DecodeRequest(%s) = %v, %v", payload, request, err)
		}
	}
	if request, _ := DecodeRequest([]byte(`{"id":3,"type":"fs.read","path":"src/a.cpp"}`)); request.Head().ID != 3 || request.(*FSRead).Path != "src/a.cpp" {
		t.Errorf("fields were not read: %+v", request)
	}
}

func TestDecodeRequestRefusals(t *testing.T) {
	long := strings.Repeat("x", MaxFileBytes+1)
	for name, test := range map[string]struct{ payload, code, want string }{
		"not json":            {`nope`, CodeBadRequest, "not a JSON object"},
		"empty":               {``, CodeBadRequest, "not a JSON object"},
		"array":               {`[]`, CodeBadRequest, "not a JSON object"},
		"no type":             {`{}`, CodeBadRequest, "unknown message type"},
		"unknown type":        {`{"type":"fs.chmod","path":"a"}`, CodeBadRequest, `unknown message type "fs.chmod"`},
		"a reply type":        {`{"type":"fs.event","kind":"create","path":"a"}`, CodeBadRequest, "unknown message type"},
		"unknown field":       {`{"type":"fs.read","path":"a","mode":"rw"}`, CodeBadRequest, "not valid"},
		"wrong type":          {`{"type":"fs.read","path":7}`, CodeBadRequest, "not valid"},
		"negative term":       {`{"type":"pty.close","term":-1}`, CodeBadRequest, "not valid"},
		"huge term":           {`{"type":"pty.close","term":99999999999}`, CodeBadRequest, "not valid"},
		"trailing data":       {`{"type":"agent.ping"}{"type":"agent.ping"}`, CodeBadRequest, "not a JSON object"},
		"trailing garbage":    {`{"type":"agent.ping"} x`, CodeBadRequest, "not a JSON object"},
		"null":                {`null`, CodeBadRequest, "unknown message type"},
		"no path":             {`{"type":"fs.read"}`, CodeBadPath, "needs a path"},
		"read the root":       {`{"type":"fs.read","path":""}`, CodeBadPath, "needs a path"},
		"delete the root":     {`{"type":"fs.delete","path":""}`, CodeBadPath, "needs a path"},
		"absolute":            {`{"type":"fs.read","path":"/etc/passwd"}`, CodeBadPath, "absolute"},
		"parent":              {`{"type":"fs.list","path":"../.."}`, CodeBadPath, `".."`},
		"rename target":       {`{"type":"fs.rename","path":"a","to":"../b"}`, CodeBadPath, `".."`},
		"rename no target":    {`{"type":"fs.rename","path":"a"}`, CodeBadPath, "needs a path"},
		"write no base":       {`{"type":"fs.write","path":"a","content":"x"}`, CodeBadRequest, "base_version"},
		"write too large":     {`{"type":"fs.write","path":"a","base_version":"v","content":"` + long + `"}`, CodeTooLarge, "larger than"},
		"create too large":    {`{"type":"fs.create","path":"a","content":"` + long + `"}`, CodeTooLarge, "larger than"},
		"write a NUL":         {`{"type":"fs.write","path":"a","base_version":"v","content":"a\u0000b"}`, CodeBinary, "NUL"},
		"pty no size":         {`{"type":"pty.open"}`, CodeBadRequest, "cols must be"},
		"pty big":             {`{"type":"pty.open","cols":501,"rows":24}`, CodeBadRequest, "cols must be"},
		"pty rows":            {`{"type":"pty.open","cols":80,"rows":0}`, CodeBadRequest, "cols must be"},
		"pty cwd":             {`{"type":"pty.open","cols":80,"rows":24,"cwd":"../x"}`, CodeBadPath, `".."`},
		"pty cmd newline":     {`{"type":"pty.open","cols":80,"rows":24,"cmd":"a\nb"}`, CodeBadRequest, "one line"},
		"pty cmd long":        {`{"type":"pty.open","cols":80,"rows":24,"cmd":"` + strings.Repeat("a", 1001) + `"}`, CodeBadRequest, "longer than"},
		"attach no term":      {`{"type":"pty.attach"}`, CodeBadRequest, "terminal ID"},
		"resize no term":      {`{"type":"pty.resize","cols":80,"rows":24}`, CodeBadRequest, "terminal ID"},
		"resize bad size":     {`{"type":"pty.resize","term":1,"cols":0,"rows":24}`, CodeBadRequest, "cols must be"},
		"reserved name":       {`{"type":"fs.create","path":".cbe-write-1"}`, CodeBadPath, "kept for the system"},
		"type with null byte": {"{\"type\":\"fs.\\u0000read\"}", CodeBadRequest, "unknown message type"},
	} {
		t.Run(name, func(t *testing.T) {
			request, err := DecodeRequest([]byte(test.payload))
			var e *Error
			if request != nil || !errors.As(err, &e) || e.Code != test.code || !strings.Contains(e.Message, test.want) {
				t.Fatalf("DecodeRequest = %v, %v; want %s with %q", request, err, test.code, test.want)
			}
		})
	}
}

func TestMessagesRoundTrip(t *testing.T) {
	yes, no := true, false
	code := 2
	content := ""
	for _, message := range []Message{
		{ID: 4, Type: TypeFSRead, OK: &yes, Content: &content, Version: "sha256:0"},
		{ID: 5, Type: TypeFSRead, OK: &no, Code: CodeNotFound, Message: "no such file"},
		{Type: TypeFSEvent, Kind: "rename", Path: "b", From: "a", IsDir: true},
		{Type: TypePTYExit, Term: 3, ExitCode: &code, Signal: "SIGKILL", OOMKilled: true},
		{Type: TypeFSList, ID: 1, OK: &yes, Entries: []Entry{{Name: "a", Kind: "file", Size: 3, MTime: 7}}},
	} {
		payload, err := Encode(message)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.ContainsAny(payload, "\n") {
			t.Errorf("an encoded message has a newline: %s", payload)
		}
		again, err := DecodeMessage(payload)
		if err != nil {
			t.Fatal(err)
		}
		second, _ := Encode(again)
		if !bytes.Equal(payload, second) {
			t.Errorf("round trip changed %s into %s", payload, second)
		}
	}
	if m := (Message{OK: &no, Code: CodeConflict, Message: "changed"}); m.Err() == nil || !m.Failed() || m.Err().(*Error).Code != CodeConflict {
		t.Error("a failed reply is not an error")
	}
	if m := (Message{OK: &yes}); m.Err() != nil || m.Failed() {
		t.Error("a good reply is an error")
	}
	if m := (Message{}); m.Err() != nil || m.Failed() {
		t.Error("an event is an error")
	}
	// An empty file's content is "", not absent.
	payload, _ := Encode(Message{ID: 1, Type: TypeFSRead, OK: &yes, Content: &content})
	if !strings.Contains(string(payload), `"content":""`) {
		t.Errorf("an empty content was left out: %s", payload)
	}
	// Text with < and & is not escaped to <.
	text := "<a & b>"
	payload, _ = Encode(Message{Type: TypeFSRead, Content: &text})
	if !strings.Contains(string(payload), "<a & b>") {
		t.Errorf("content was HTML-escaped: %s", payload)
	}
}
