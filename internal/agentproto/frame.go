// Package agentproto is the language of the in-container agent (cmd/cbe-agent): the frames on its socket, the messages in them,
// the rules for the paths they carry, and a small client. The agent, its test harness and, later, the runner and the exam server's
// relay all speak it, so none of them has a private copy of the rules.
//
// A frame on the wire is a 4-byte big-endian length, then that many bytes: one byte for the kind, then the payload.
//
//	KindJSON    the payload is one JSON object (a request, a reply or an event)
//	KindBinary  the payload is a 4-byte big-endian terminal ID, then raw terminal bytes (input to the terminal, or its output)
//
// Section 6 of the workspace plan lists the messages; docs/workspace/agent-protocol.md describes them as built.
package agentproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Frame kinds.
const (
	KindJSON   byte = 1
	KindBinary byte = 2
)

const (
	// MaxFrame is the most bytes a frame may hold after its length prefix. A JSON message with a 1 MiB file in it, every control
	// character written as a six-byte escape, still fits.
	MaxFrame = 8 << 20
	// MaxTerminalData is the most terminal bytes one binary frame may carry.
	MaxTerminalData = 1 << 20
)

// Frame is one frame. For KindJSON, Data is the JSON text; for KindBinary, Term and Data are the terminal and its bytes.
type Frame struct {
	Kind byte
	Term uint32
	Data []byte
}

// ErrFrame says a frame is malformed. The stream cannot be trusted after one: the reader closes the connection.
var ErrFrame = errors.New("malformed frame")

// WriteFrame writes frame in one Write call, so concurrent writers that take turns never interleave bytes of two frames.
func WriteFrame(w io.Writer, frame Frame) error {
	var size int
	switch frame.Kind {
	case KindJSON:
		size = 1 + len(frame.Data)
	case KindBinary:
		size = 1 + 4 + len(frame.Data)
		if len(frame.Data) > MaxTerminalData {
			return fmt.Errorf("%w: terminal data of %d bytes is over the %d byte limit", ErrFrame, len(frame.Data), MaxTerminalData)
		}
	default:
		return fmt.Errorf("%w: unknown kind %d", ErrFrame, frame.Kind)
	}
	if size > MaxFrame {
		return fmt.Errorf("%w: %d bytes is over the %d byte limit", ErrFrame, size, MaxFrame)
	}
	buffer := make([]byte, 4, 4+size)
	binary.BigEndian.PutUint32(buffer, uint32(size))
	buffer = append(buffer, frame.Kind)
	if frame.Kind == KindBinary {
		buffer = binary.BigEndian.AppendUint32(buffer, frame.Term)
	}
	buffer = append(buffer, frame.Data...)
	_, err := w.Write(buffer)
	return err
}

// ReadFrame reads one frame. A length over MaxFrame is refused before any memory is set aside for it. An error that wraps ErrFrame
// means the bytes were not a frame; any other error is the reader's own (io.EOF at a frame boundary is a clean end).
func ReadFrame(r io.Reader) (Frame, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return Frame{}, err
	}
	size := binary.BigEndian.Uint32(prefix[:])
	switch {
	case size == 0:
		return Frame{}, fmt.Errorf("%w: empty frame", ErrFrame)
	case size > MaxFrame:
		return Frame{}, fmt.Errorf("%w: %d bytes is over the %d byte limit", ErrFrame, size, MaxFrame)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	switch body[0] {
	case KindJSON:
		return Frame{Kind: KindJSON, Data: body[1:]}, nil
	case KindBinary:
		if len(body) < 5 {
			return Frame{}, fmt.Errorf("%w: a terminal frame has no terminal ID", ErrFrame)
		}
		if len(body)-5 > MaxTerminalData {
			return Frame{}, fmt.Errorf("%w: terminal data of %d bytes is over the %d byte limit", ErrFrame, len(body)-5, MaxTerminalData)
		}
		return Frame{Kind: KindBinary, Term: binary.BigEndian.Uint32(body[1:5]), Data: body[5:]}, nil
	}
	return Frame{}, fmt.Errorf("%w: unknown kind %d", ErrFrame, body[0])
}
