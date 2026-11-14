//go:build linux

package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/sys/unix"
)

// peerListener accepts only connections made by one user: the kernel says who is on the other end of a Unix socket (SO_PEERCRED),
// which no one can forge. The socket's own permissions are the first guard; this is the second.
type peerListener struct {
	net.Listener
	uid int
}

func (l peerListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if ok := l.allowed(conn); ok {
			return conn, nil
		}
		conn.Close()
	}
}

func (l peerListener) allowed(conn net.Conn) bool {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return false
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) { cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil || credErr != nil {
		return false
	}
	return int(cred.Uid) == l.uid
}

type createRequest struct {
	Session string `json:"session"`
	SizeMB  int    `json:"size_mb"`
	Inodes  int    `json:"inodes,omitempty"`
}

// Serve answers the helper's API on listener until ctx ends. Only connections from user uid are served.
//
//	POST   /v1/folders           {"session","size_mb","inodes"}   make a folder (or return the one there is)
//	DELETE /v1/folders/{session}                                   delete it, with what is in it, and free its quota
//	GET    /v1/folders                                             the sessions that have a folder
func Serve(ctx context.Context, listener net.Listener, h *Helper, uid int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/folders", func(w http.ResponseWriter, r *http.Request) {
		var request createRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || decoder.More() {
			reply(w, http.StatusBadRequest, map[string]string{"error": "the request is not the JSON the helper takes"})
			return
		}
		folder, err := h.Create(request.Session, request.SizeMB, request.Inodes)
		if err != nil {
			reply(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		reply(w, http.StatusCreated, folder)
	})
	mux.HandleFunc("DELETE /v1/folders/{session}", func(w http.ResponseWriter, r *http.Request) {
		if err := h.Remove(r.PathValue("session")); err != nil {
			reply(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/folders", func(w http.ResponseWriter, r *http.Request) {
		sessions, err := h.Sessions()
		if err != nil {
			reply(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		reply(w, http.StatusOK, map[string][]string{"sessions": sessions})
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 60 * time.Second, MaxHeaderBytes: 8 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err := server.Serve(peerListener{Listener: listener, uid: uid})
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// Client is the runner's side: it asks the helper for folders over the socket.
type Client struct {
	Socket string
	http   *http.Client
}

// NewClient makes a client of the helper listening on socket.
func NewClient(socket string) *Client {
	return &Client{Socket: socket, http: &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socket)
		}, DisableKeepAlives: true}}}
}

func (c *Client) do(method, path string, body any, into any) error {
	var reader io.Reader
	if body != nil {
		payload, _ := json.Marshal(body)
		reader = bytesReader(payload)
	}
	request, err := http.NewRequest(method, "http://quota"+path, reader)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		return fmt.Errorf("quota helper: %s (%d)", failure.Error, response.StatusCode)
	}
	if into != nil {
		return json.Unmarshal(data, into)
	}
	return nil
}

// Create asks for the folder of a session.
func (c *Client) Create(session string, sizeMB, inodes int) (Folder, error) {
	var folder Folder
	err := c.do(http.MethodPost, "/v1/folders", createRequest{Session: session, SizeMB: sizeMB, Inodes: inodes}, &folder)
	return folder, err
}

// Remove asks for the folder of a session to be deleted.
func (c *Client) Remove(session string) error {
	return c.do(http.MethodDelete, "/v1/folders/"+session, nil, nil)
}

// Sessions lists the sessions that have a folder.
func (c *Client) Sessions() ([]string, error) {
	var out struct {
		Sessions []string `json:"sessions"`
	}
	err := c.do(http.MethodGet, "/v1/folders", nil, &out)
	return out.Sessions, err
}
