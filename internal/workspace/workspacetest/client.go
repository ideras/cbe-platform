//go:build linux

package workspacetest

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Client is a student's application as far as the WebSocket goes: it sends requests, keeps what the server says (text messages) and
// the terminals' output (binary frames: four bytes of terminal number, then the bytes).
type Client struct {
	t      *testing.T
	conn   *websocket.Conn
	mu     sync.Mutex
	texts  []map[string]any
	output map[uint32]*bytes.Buffer
	next   uint64
	// Closed is closed when the connection ends.
	Closed chan struct{}
	// Err is why it ended.
	Err error
}

// Dial opens the WebSocket of the server at baseURL (http://host:port) with a student's token.
func Dial(t *testing.T, baseURL, token string) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(baseURL, "http")+"/workspace/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.SetReadLimit(8 << 20)
	c := &Client{t: t, conn: conn, output: map[uint32]*bytes.Buffer{}, Closed: make(chan struct{})}
	t.Cleanup(func() { conn.CloseNow() })
	go c.read()
	return c
}

// Close drops the connection as a student's closing the application would.
func (c *Client) Close() { c.conn.CloseNow() }

func (c *Client) read() {
	defer close(c.Closed)
	for {
		kind, data, err := c.conn.Read(context.Background())
		if err != nil {
			c.mu.Lock()
			c.Err = err
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		if kind == websocket.MessageBinary && len(data) >= 4 {
			term := binary.BigEndian.Uint32(data[:4])
			if c.output[term] == nil {
				c.output[term] = &bytes.Buffer{}
			}
			c.output[term].Write(data[4:])
		} else if kind == websocket.MessageText {
			var message map[string]any
			if json.Unmarshal(data, &message) == nil {
				c.texts = append(c.texts, message)
			}
		}
		c.mu.Unlock()
	}
}

// Send writes a request.
func (c *Client) Send(message map[string]any) {
	c.t.Helper()
	payload, _ := json.Marshal(message)
	c.SendRaw(string(payload))
}

// SendRaw writes a text frame as it is.
func (c *Client) SendRaw(payload string) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		c.t.Fatalf("send %s: %v", payload, err)
	}
}

// Keys writes keystrokes to a terminal.
func (c *Client) Keys(term uint32, data string) {
	c.t.Helper()
	payload := binary.BigEndian.AppendUint32(nil, term)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageBinary, append(payload, data...)); err != nil {
		c.t.Fatalf("keys: %v", err)
	}
}

// Wait returns the first message, of those not taken yet, that matches; it takes it.
func (c *Client) Wait(what string, match func(map[string]any) bool) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for i, message := range c.texts {
			if match(message) {
				c.texts = append(c.texts[:i], c.texts[i+1:]...)
				c.mu.Unlock()
				return message
			}
		}
		c.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	c.t.Fatalf("timed out waiting for %s", what)
	return nil
}

// Call sends a request and returns its reply.
func (c *Client) Call(message map[string]any) map[string]any {
	c.t.Helper()
	c.mu.Lock()
	c.next++
	id := c.next
	c.mu.Unlock()
	message["id"] = id
	c.Send(message)
	return c.Wait("the reply to "+message["type"].(string), func(m map[string]any) bool { return Num(m["id"]) == id && m["ok"] != nil })
}

// OfType matches the messages of a type.
func (c *Client) OfType(kind string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["type"] == kind }
}

// Has says whether an untaken message has this code (the server's refusals carry one).
func (c *Client) Has(code string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.texts {
		if m["code"] == code {
			return true
		}
	}
	return false
}

// OutputOf is everything a terminal has printed since the connection began.
func (c *Client) OutputOf(term uint32) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if b := c.output[term]; b != nil {
		return b.String()
	}
	return ""
}

// WaitOutput waits for a terminal to have printed a text.
func (c *Client) WaitOutput(term uint32, want string) string {
	c.t.Helper()
	Eventually(c.t, 30*time.Second, "output "+want, func() bool { return strings.Contains(c.OutputOf(term), want) })
	return c.OutputOf(term)
}

// WaitExit waits for a terminal's program to end, and returns its exit code.
func (c *Client) WaitExit(term uint32) float64 {
	c.t.Helper()
	m := c.Wait("pty.exit", func(m map[string]any) bool { return m["type"] == "pty.exit" && uint32(Num(m["term"])) == term })
	code, _ := m["exit_code"].(float64)
	return code
}

// Eventually polls until ok is true.
func Eventually(t *testing.T, within time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Num reads a JSON number as an unsigned integer (0 if it is not one).
func Num(v any) uint64 {
	if f, ok := v.(float64); ok {
		return uint64(f)
	}
	return 0
}

// OK is whether a reply says ok.
func OK(m map[string]any) bool { v, _ := m["ok"].(bool); return v }

// Code is a reply's error code.
func Code(m map[string]any) string { s, _ := m["code"].(string); return s }
