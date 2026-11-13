package agentproto

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Output is a piece of a terminal's output.
type Output struct {
	Term uint32
	Data []byte
}

// Client is one connection to an agent. It sends requests and matches the replies to them, and hands out what the agent sends on
// its own: events and terminal output. It is what the harness and the tests use; the exam server's relay will wrap the same frames.
type Client struct {
	conn    net.Conn
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan Message
	closed  bool
	err     error

	// Events carries every message that is not a reply: fs.event, pty.exit, agent.hello, agent.hb. It is closed when the
	// connection ends. A reader that falls behind loses the oldest events, never the replies.
	Events chan Message
	// Terminal carries terminal output. It is closed when the connection ends. Whoever opens a terminal must read it: when its
	// buffer is full the client stops reading the connection, which is how a slow reader slows the agent down.
	Terminal chan Output
}

// Dial connects to the agent's socket.
func Dial(socket string) (*Client, error) {
	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return NewClient(conn), nil
}

// NewClient runs a client over an open connection.
func NewClient(conn net.Conn) *Client {
	c := &Client{conn: conn, pending: map[uint64]chan Message{}, Events: make(chan Message, 1024), Terminal: make(chan Output, 4096)}
	go c.read()
	return c
}

// ErrClosed is returned by calls on a connection that has ended.
var ErrClosed = errors.New("the connection to the agent is closed")

func (c *Client) read() {
	defer func() {
		c.mu.Lock()
		c.closed = true
		for id, reply := range c.pending {
			close(reply)
			delete(c.pending, id)
		}
		c.mu.Unlock()
		close(c.Events)
		close(c.Terminal)
	}()
	for {
		frame, err := ReadFrame(c.conn)
		if err != nil {
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			return
		}
		if frame.Kind == KindBinary {
			c.Terminal <- Output{Term: frame.Term, Data: frame.Data}
			continue
		}
		message, err := DecodeMessage(frame.Data)
		if err != nil {
			continue
		}
		if message.ID != 0 && message.OK != nil {
			c.mu.Lock()
			reply := c.pending[message.ID]
			delete(c.pending, message.ID)
			c.mu.Unlock()
			if reply != nil {
				reply <- message
				continue
			}
			// A reply to a request this client did not make (one sent raw) is an event like any other.
		}
		select {
		case c.Events <- message:
		default:
			select {
			case <-c.Events:
			default:
			}
			select {
			case c.Events <- message:
			default:
			}
		}
	}
}

// Do sends a request and waits for its reply. A reply that says the request failed is returned as the *Error it carries, with the
// reply itself.
func (c *Client) Do(request Request, timeout time.Duration) (Message, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Message{}, ErrClosed
	}
	c.nextID++
	id := c.nextID
	request.Head().ID = id
	reply := make(chan Message, 1)
	c.pending[id] = reply
	c.mu.Unlock()
	if err := c.SendJSON(request); err != nil {
		return Message{}, err
	}
	select {
	case message, open := <-reply:
		if !open {
			return Message{}, ErrClosed
		}
		return message, message.Err()
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return Message{}, fmt.Errorf("no reply to %s within %v", request.Head().Type, timeout)
	}
}

// SendJSON sends a request without waiting for a reply.
func (c *Client) SendJSON(request any) error {
	payload, err := Encode(request)
	if err != nil {
		return err
	}
	return c.send(Frame{Kind: KindJSON, Data: payload})
}

// SendRaw sends JSON text exactly as given, which lets a test send what the encoder never would.
func (c *Client) SendRaw(payload []byte) error { return c.send(Frame{Kind: KindJSON, Data: payload}) }

// Input sends keystrokes to a terminal.
func (c *Client) Input(term uint32, data []byte) error {
	return c.send(Frame{Kind: KindBinary, Term: term, Data: data})
}

func (c *Client) send(frame Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WriteFrame(c.conn, frame)
}

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }
