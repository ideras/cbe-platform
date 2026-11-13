package agentproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// ProtocolVersion is the version a peer reports in agent.hello.
const ProtocolVersion = 1

// Error codes. The first ten are those of section 6 of the workspace plan. BAD_REQUEST (a malformed message), DENIED (the file's
// permissions refuse it) and INTERNAL (the agent failed) are the protocol's own additions.
const (
	CodeNotFound    = "NOT_FOUND"
	CodeExists      = "EXISTS"
	CodeConflict    = "CONFLICT"
	CodeTooLarge    = "TOO_LARGE"
	CodeBinary      = "BINARY"
	CodeQuota       = "QUOTA"
	CodeLimit       = "LIMIT"
	CodeBadPath     = "BAD_PATH"
	CodeUnavailable = "UNAVAILABLE"
	CodeRate        = "RATE"
	CodeBadRequest  = "BAD_REQUEST"
	CodeDenied      = "DENIED"
	CodeInternal    = "INTERNAL"
)

// Limits of what a request may hold.
const (
	// MaxFileBytes is the largest file the editor opens or saves (plan, Appendix A).
	MaxFileBytes = 1 << 20
	// MaxTerminals is how many terminals one student may have open at once.
	MaxTerminals = 4
	// MaxCommandBytes is the longest command a terminal may be opened with.
	MaxCommandBytes = 1000
	MaxCols         = 500
	MaxRows         = 200
	// MaxListEntries is the most names one folder listing returns.
	MaxListEntries = 5000
)

// Error is a failure with the code a client acts on and a message for a person.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// CodeOf is the protocol code of err, or "" when err is not an *Error.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Errorf makes an Error.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Header is what every request starts with. ID ties a reply to its request; it is chosen by the sender and may be 0 when no reply
// is wanted.
type Header struct {
	ID   uint64 `json:"id,omitempty"`
	Type string `json:"type"`
}

// Request is a message to the agent.
type Request interface {
	// Head gives the header, for the ID and the type.
	Head() *Header
	// Validate checks the fields; it returns an *Error with CodeBadPath or CodeBadRequest.
	Validate() error
}

func (h *Header) Head() *Header { return h }

// Message types.
const (
	TypeFSList     = "fs.list"
	TypeFSRead     = "fs.read"
	TypeFSWrite    = "fs.write"
	TypeFSCreate   = "fs.create"
	TypeFSMkdir    = "fs.mkdir"
	TypeFSRename   = "fs.rename"
	TypeFSDelete   = "fs.delete"
	TypeFSEvent    = "fs.event"
	TypePTYOpen    = "pty.open"
	TypePTYAttach  = "pty.attach"
	TypePTYList    = "pty.list"
	TypePTYResize  = "pty.resize"
	TypePTYClose   = "pty.close"
	TypePTYExit    = "pty.exit"
	TypePing       = "agent.ping"
	TypeHello      = "agent.hello"
	TypeHeartbeat  = "agent.hb"
	TypeSessionErr = "agent.error"
)

// FSList lists one folder.
type FSList struct {
	Header
	Path string `json:"path"`
}

// FSRead reads a text file of at most MaxFileBytes. The reply has its content and its version.
type FSRead struct {
	Header
	Path string `json:"path"`
}

// FSWrite replaces a file's content. It fails with CONFLICT unless BaseVersion is the file's version now.
type FSWrite struct {
	Header
	Path        string `json:"path"`
	Content     string `json:"content"`
	BaseVersion string `json:"base_version"`
}

// FSCreate creates a file, with Content in it when given. It fails with EXISTS when there is something at the path.
type FSCreate struct {
	Header
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
}

// FSMkdir creates one folder; its parent must exist.
type FSMkdir struct {
	Header
	Path string `json:"path"`
}

// FSRename moves a file or folder. It fails with EXISTS when To is taken.
type FSRename struct {
	Header
	Path string `json:"path"`
	To   string `json:"to"`
}

// FSDelete deletes a file, a link or a folder with everything in it.
type FSDelete struct {
	Header
	Path string `json:"path"`
}

// PTYOpen opens a terminal in /work, or in Cwd. With Cmd it runs that command with sh -c and the terminal ends with it, for the
// Run and Check buttons; without, it runs a login shell. The opener is attached to the terminal.
type PTYOpen struct {
	Header
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	Cmd  string `json:"cmd,omitempty"`
	Cwd  string `json:"cwd,omitempty"`
}

// PTYAttach attaches the connection to an open terminal. The reply is followed by the output the terminal still holds.
type PTYAttach struct {
	Header
	Term uint32 `json:"term"`
}

// PTYList lists the open terminals.
type PTYList struct{ Header }

// PTYResize changes the size of a terminal.
type PTYResize struct {
	Header
	Term uint32 `json:"term"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// PTYClose ends a terminal: its processes are hung up, then killed, and it is forgotten.
type PTYClose struct {
	Header
	Term uint32 `json:"term"`
}

// Ping asks the agent to answer, to show it is alive.
type Ping struct{ Header }

var requests = map[string]func() Request{
	TypeFSList:    func() Request { return new(FSList) },
	TypeFSRead:    func() Request { return new(FSRead) },
	TypeFSWrite:   func() Request { return new(FSWrite) },
	TypeFSCreate:  func() Request { return new(FSCreate) },
	TypeFSMkdir:   func() Request { return new(FSMkdir) },
	TypeFSRename:  func() Request { return new(FSRename) },
	TypeFSDelete:  func() Request { return new(FSDelete) },
	TypePTYOpen:   func() Request { return new(PTYOpen) },
	TypePTYAttach: func() Request { return new(PTYAttach) },
	TypePTYList:   func() Request { return new(PTYList) },
	TypePTYResize: func() Request { return new(PTYResize) },
	TypePTYClose:  func() Request { return new(PTYClose) },
	TypePing:      func() Request { return new(Ping) },
}

func bad(format string, args ...any) error { return Errorf(CodeBadRequest, format, args...) }

// DecodeRequest reads one JSON request. Whatever the bytes are, it either returns a validated request or an *Error; it never
// panics. An unknown type, an unknown field, a field of the wrong type, trailing bytes and any value the request's rules refuse are
// all errors, so what the agent acts on is exactly what the protocol defines.
func DecodeRequest(payload []byte) (Request, error) {
	var header Header
	if err := json.Unmarshal(payload, &header); err != nil {
		return nil, bad("the message is not a JSON object with a type")
	}
	build, found := requests[header.Type]
	if !found {
		return nil, bad("unknown message type %q", abbreviate(header.Type))
	}
	request := build()
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(request); err != nil {
		return nil, bad("the %s message is not valid: %s", header.Type, abbreviate(err.Error()))
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, bad("the %s message has data after the object", header.Type)
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	return request, nil
}

func abbreviate(text string) string {
	if len(text) > 80 {
		text = strings.ToValidUTF8(text[:80], "") + "…"
	}
	return strings.ToValidUTF8(text, "?")
}

func (r *FSList) Validate() error   { return CheckPath(r.Path) }
func (r *FSMkdir) Validate() error  { return needPath(r.Path) }
func (r *FSDelete) Validate() error { return needPath(r.Path) }
func (r *FSRead) Validate() error   { return needPath(r.Path) }

func (r *FSWrite) Validate() error {
	if err := needPath(r.Path); err != nil {
		return err
	}
	if r.BaseVersion == "" {
		return bad("fs.write needs the base_version of the file it replaces")
	}
	return checkContent(r.Content)
}

func (r *FSCreate) Validate() error {
	if err := needPath(r.Path); err != nil {
		return err
	}
	return checkContent(r.Content)
}

func (r *FSRename) Validate() error {
	if err := needPath(r.Path); err != nil {
		return err
	}
	return needPath(r.To)
}

func (r *PTYOpen) Validate() error {
	if err := checkSize(r.Cols, r.Rows); err != nil {
		return err
	}
	if err := CheckPath(r.Cwd); err != nil {
		return err
	}
	switch {
	case len(r.Cmd) > MaxCommandBytes:
		return bad("cmd is longer than %d bytes", MaxCommandBytes)
	case !utf8.ValidString(r.Cmd) || strings.ContainsFunc(r.Cmd, func(c rune) bool { return c < ' ' && c != '\t' || c == 0x7f }):
		return bad("cmd must be one line of plain text")
	}
	return nil
}

func (r *PTYAttach) Validate() error { return needTerm(r.Term) }
func (r *PTYClose) Validate() error  { return needTerm(r.Term) }
func (r *PTYList) Validate() error   { return nil }
func (r *Ping) Validate() error      { return nil }

func (r *PTYResize) Validate() error {
	if err := needTerm(r.Term); err != nil {
		return err
	}
	return checkSize(r.Cols, r.Rows)
}

func needPath(path string) error {
	if path == "" {
		return badPath("this request needs a path inside /work")
	}
	return CheckPath(path)
}

func needTerm(term uint32) error {
	if term == 0 {
		return bad("term must be a terminal ID")
	}
	return nil
}

func checkSize(cols, rows int) error {
	if cols < 1 || cols > MaxCols || rows < 1 || rows > MaxRows {
		return bad("cols must be 1 to %d and rows 1 to %d", MaxCols, MaxRows)
	}
	return nil
}

func checkContent(content string) error {
	if len(content) > MaxFileBytes {
		return Errorf(CodeTooLarge, "the content is larger than %d bytes", MaxFileBytes)
	}
	if strings.IndexByte(content, 0) >= 0 {
		return Errorf(CodeBinary, "the content has a NUL byte; the editor holds text files only")
	}
	return nil
}

// Entry is one name in a folder listing.
type Entry struct {
	Name string `json:"name"`
	// Kind is "file", "dir", "link" (a symbolic link, not followed) or "other".
	Kind string `json:"kind"`
	Size int64  `json:"size,omitempty"`
	// MTime is the modification time in milliseconds since the epoch.
	MTime int64 `json:"mtime,omitempty"`
}

// TermInfo describes an open terminal.
type TermInfo struct {
	Term     uint32 `json:"term"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Cmd      string `json:"cmd,omitempty"`
	Exited   bool   `json:"exited"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

// Message is everything the agent sends in JSON: replies (with the ID of their request), and events (without one). Only the
// fields of the message's type are set.
type Message struct {
	ID   uint64 `json:"id,omitempty"`
	Type string `json:"type"`

	// A reply says whether the request worked; a failed one carries Code and Message.
	OK      *bool  `json:"ok,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`

	// fs.list, fs.read, fs.write, fs.create
	Entries []Entry `json:"entries,omitempty"`
	Content *string `json:"content,omitempty"`
	Version string  `json:"version,omitempty"`
	Size    int64   `json:"size,omitempty"`

	// fs.event: Kind is create, modify, delete, rename or overflow; From is where a renamed file came from.
	Kind  string `json:"kind,omitempty"`
	Path  string `json:"path,omitempty"`
	From  string `json:"from,omitempty"`
	IsDir bool   `json:"is_dir,omitempty"`

	// pty.*
	Term      uint32     `json:"term,omitempty"`
	Terminals []TermInfo `json:"terminals,omitempty"`
	Cols      int        `json:"cols,omitempty"`
	Rows      int        `json:"rows,omitempty"`
	Exited    bool       `json:"exited,omitempty"`
	// pty.exit: ExitCode is the exit status, or 128 plus the signal number when a signal ended the program; Signal names that
	// signal; OOMKilled says the container's memory limit killed a process while the terminal ran.
	ExitCode  *int   `json:"exit_code,omitempty"`
	Signal    string `json:"signal,omitempty"`
	OOMKilled bool   `json:"oom_killed,omitempty"`

	// agent.hello, agent.hb. SlowPaths says the kernel has no openat2, so the agent follows no symbolic link at all.
	Proto         int    `json:"proto,omitempty"`
	AgentVersion  string `json:"agent_version,omitempty"`
	PID1          bool   `json:"pid1,omitempty"`
	UptimeMS      int64  `json:"uptime_ms,omitempty"`
	Seq           uint64 `json:"seq,omitempty"`
	OpenTerminals int    `json:"open_terminals,omitempty"`
	OOMKills      uint64 `json:"oom_kills,omitempty"`
	Watches       int    `json:"watches,omitempty"`
	Work          string `json:"work,omitempty"`
	SlowPaths     bool   `json:"slow_paths,omitempty"`
	Connections   int    `json:"connections,omitempty"`
}

// Failed reports whether the message is a reply that says the request failed.
func (m *Message) Failed() bool { return m.OK != nil && !*m.OK }

// Err turns a failed reply into an *Error, and returns nil for anything else.
func (m *Message) Err() error {
	if !m.Failed() {
		return nil
	}
	return &Error{Code: m.Code, Message: m.Message}
}

// DecodeMessage reads what the agent sent.
func DecodeMessage(payload []byte) (Message, error) {
	var message Message
	if err := json.Unmarshal(payload, &message); err != nil {
		return Message{}, fmt.Errorf("agent message: %w", err)
	}
	return message, nil
}

// Encode writes any message or request as the payload of a JSON frame.
func Encode(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}
