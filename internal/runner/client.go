package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is the exam server's side: it asks the runner for the Appendix E operations over the runner's socket, with the shared secret.
type Client struct {
	socket string
	secret string
	http   *http.Client
}

// APIError is an error the runner answered with.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("runner: %s (%d)", e.Message, e.Status) }

// Is lets errors.Is(err, ErrNotFound) and the like work across the API.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrConflict:
		return e.Status == http.StatusConflict
	case ErrBadRequest:
		return e.Status == http.StatusBadRequest
	case ErrNotApproved:
		return e.Status == http.StatusForbidden
	case ErrFrozen:
		return e.Status == http.StatusLocked
	}
	return false
}

// NewClient makes a client of the runner listening on socket.
func NewClient(socket, secret string) *Client {
	return &Client{socket: socket, secret: secret, http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socket)
		}, DisableKeepAlives: true}}}
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, "http://runner"+path, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.secret)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 {
		defer response.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		return nil, &APIError{Status: response.StatusCode, Message: failure.Error}
	}
	return response, nil
}

func (c *Client) json(ctx context.Context, method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		payload, _ := json.Marshal(in)
		body = bytes.NewReader(payload)
	}
	response, err := c.request(ctx, method, path, body, "application/json")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out)
}

// Ping checks that the runner answers and that the secret is right.
func (c *Client) Ping(ctx context.Context) error {
	return c.json(ctx, http.MethodGet, "/v1/ping", nil, nil)
}

// Create is container.create.
func (c *Client) Create(ctx context.Context, req CreateRequest) (Status, error) {
	var status Status
	err := c.json(ctx, http.MethodPost, "/v1/containers", req, &status)
	return status, err
}

// Seed is container.seed: the starter bundle, a tar archive.
func (c *Client) Seed(ctx context.Context, session string, bundle io.Reader) (Status, error) {
	var status Status
	response, err := c.request(ctx, http.MethodPut, "/v1/containers/"+session+"/seed", bundle, "application/x-tar")
	if err != nil {
		return status, err
	}
	defer response.Body.Close()
	return status, json.NewDecoder(response.Body).Decode(&status)
}

func (c *Client) act(ctx context.Context, session, action string) (Status, error) {
	var status Status
	err := c.json(ctx, http.MethodPost, "/v1/containers/"+session+"/"+action, nil, &status)
	return status, err
}

// Start, Pause, Unpause and Stop are the container operations of those names.
func (c *Client) Start(ctx context.Context, session string) (Status, error) {
	return c.act(ctx, session, "start")
}
func (c *Client) Pause(ctx context.Context, session string) (Status, error) {
	return c.act(ctx, session, "pause")
}
func (c *Client) Unpause(ctx context.Context, session string) (Status, error) {
	return c.act(ctx, session, "unpause")
}
func (c *Client) Stop(ctx context.Context, session string) (Status, error) {
	return c.act(ctx, session, "stop")
}

// Remove is container.remove.
func (c *Client) Remove(ctx context.Context, session string) error {
	return c.json(ctx, http.MethodPost, "/v1/containers/"+session+"/remove", nil, nil)
}

// Status is container.status; a session with no container has the state "missing".
func (c *Client) Status(ctx context.Context, session string) (Status, error) {
	var status Status
	err := c.json(ctx, http.MethodGet, "/v1/containers/"+session, nil, &status)
	return status, err
}

// Stats is container.stats of one session.
func (c *Client) Stats(ctx context.Context, session string) (Stats, error) {
	var stats Stats
	err := c.json(ctx, http.MethodGet, "/v1/containers/"+session+"/stats", nil, &stats)
	return stats, err
}

// ExamStats is container.stats of every running container of an exam.
func (c *Client) ExamStats(ctx context.Context, exam string) ([]Stats, error) {
	var out struct {
		Containers []Stats `json:"containers"`
	}
	err := c.json(ctx, http.MethodGet, "/v1/exams/"+exam+"/stats", nil, &out)
	return out.Containers, err
}

// Archive is container.archive: a tar stream of a path under /work ("/work/." for all of it). The caller closes it.
func (c *Client) Archive(ctx context.Context, session, path string) (io.ReadCloser, error) {
	response, err := c.request(ctx, http.MethodGet, "/v1/containers/"+session+"/archive?path="+url.QueryEscape(path), nil, "")
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

// ImageInspect is image.inspect.
func (c *Client) ImageInspect(ctx context.Context, name string) (ImageStatus, error) {
	var status ImageStatus
	err := c.json(ctx, http.MethodGet, "/v1/images?name="+url.QueryEscape(name), nil, &status)
	return status, err
}

// ConnectAgent is agent.connect: it returns the connection to the agent's socket, once the runner has switched protocols.
func (c *Client) ConnectAgent(ctx context.Context, session string) (net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", c.socket)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (net.Conn, error) {
		conn.Close()
		return nil, err
	}
	request := "GET /v1/containers/" + session + "/agent HTTP/1.1\r\nHost: runner\r\nAuthorization: Bearer " + c.secret + "\r\nConnection: Upgrade\r\nUpgrade: " + UpgradeProtocol + "\r\n\r\n"
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	}
	if _, err := io.WriteString(conn, request); err != nil {
		return fail(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		return fail(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		return fail(&APIError{Status: response.StatusCode, Message: strings.TrimSpace(failure.Error)})
	}
	_ = conn.SetDeadline(time.Time{})
	if reader.Buffered() > 0 {
		return &bufferedConn{Conn: conn, reader: reader}, nil
	}
	return conn, nil
}

// bufferedConn gives back what was read ahead of the switch.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.reader.Read(p) }
