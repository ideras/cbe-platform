package runner

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// The most a request body may be.
const (
	maxJSONBody = 4 << 10
	maxSeedBody = MaxSeedBytes + MaxSeedFiles*1024 + 1<<20
	// maxGradeBody is a grading request: the submission, the bundle and the framing.
	maxGradeBody = MaxSubmissionBytes + MaxSeedFiles*1024 + maxSeedBody + 1<<20
	// maxBuildBody is an image build's request: the build context and the framing.
	maxBuildBody = maxBuildContextBytes + maxBuildContextFiles*1024 + 1<<20
)

// UpgradeProtocol is what agent.connect upgrades the connection to: from then on it is the agent's own socket.
const UpgradeProtocol = "cbe-agent/1"

// Handler is the runner's API (Appendix E, as built in Phase 3, with container.seed added: decision P3-4):
//
//	GET  /v1/ping
//	POST /v1/containers                            container.create   {"session","exam","image","resources"}
//	PUT  /v1/containers/{session}/seed             container.seed     the starter bundle, a tar
//	POST /v1/containers/{session}/{start|pause|unpause|stop|remove}
//	GET  /v1/containers/{session}                  container.status
//	GET  /v1/containers/{session}/stats            container.stats
//	GET  /v1/exams/{exam}/stats                    container.stats of an exam
//	GET  /v1/containers/{session}/archive?path=    container.archive  a tar stream of /work/. or a path under /work
//	GET  /v1/containers/{session}/agent            agent.connect      upgrades to a raw connection to the agent
//	GET  /v1/images?name=                          image.inspect
//	POST /v1/grading                               grading.run        multipart: request (JSON), submission (a tar), grading (a tar); answers the run's result
//	POST /v1/images/build                          image.build        multipart: request (JSON), context (a tar); answers with a stream (below)
//
// Every request carries the shared secret as a bearer token. Anything else is 404 or 405; bodies are strict JSON, and a field that
// is not in the request is refused, so there is no way to ask for a mount, a capability or a network.
func Handler(svc *Service, secret string, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, status int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(value)
	}
	// answer is the status and the words an error is answered with, and logs the ones that are the runner's own.
	answer := func(operation string, err error) (int, string) {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrBadRequest):
			status = http.StatusBadRequest
		case errors.Is(err, ErrNotApproved):
			status = http.StatusForbidden
		case errors.Is(err, ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrConflict):
			status = http.StatusConflict
		case errors.Is(err, ErrFrozen):
			status = http.StatusLocked
		case errors.Is(err, ErrCapacity):
			status = http.StatusServiceUnavailable
		}
		message := err.Error()
		if status == http.StatusInternalServerError {
			logger.Error("runner operation failed", "operation", operation, "error", err)
			message = "the operation failed; see the runner's log"
		}
		return status, message
	}
	fail := func(w http.ResponseWriter, operation string, err error) {
		status, message := answer(operation, err)
		write(w, status, map[string]string{"error": message})
	}
	mux.HandleFunc("GET /v1/ping", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, map[string]string{"runner": "cbe-runner"})
	})
	mux.HandleFunc("POST /v1/containers", func(w http.ResponseWriter, r *http.Request) {
		var req CreateRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil || decoder.More() {
			fail(w, "container.create", badRequest("the request is not the JSON container.create takes (session, exam, image, resources)"))
			return
		}
		status, err := svc.Create(r.Context(), req)
		if err != nil {
			fail(w, "container.create", err)
			return
		}
		write(w, http.StatusCreated, status)
	})
	mux.HandleFunc("PUT /v1/containers/{session}/seed", func(w http.ResponseWriter, r *http.Request) {
		status, err := svc.Seed(r.Context(), r.PathValue("session"), http.MaxBytesReader(w, r.Body, maxSeedBody))
		if err != nil {
			fail(w, "container.seed", err)
			return
		}
		write(w, http.StatusOK, status)
	})
	for action, do := range map[string]func(context.Context, string) (Status, error){"start": svc.Start, "pause": svc.Pause, "unpause": svc.Unpause, "stop": svc.Stop} {
		mux.HandleFunc("POST /v1/containers/{session}/"+action, func(w http.ResponseWriter, r *http.Request) {
			status, err := do(r.Context(), r.PathValue("session"))
			if err != nil {
				fail(w, "container."+action, err)
				return
			}
			write(w, http.StatusOK, status)
		})
	}
	mux.HandleFunc("POST /v1/containers/{session}/remove", func(w http.ResponseWriter, r *http.Request) {
		if err := svc.Remove(r.Context(), r.PathValue("session")); err != nil {
			fail(w, "container.remove", err)
			return
		}
		write(w, http.StatusOK, map[string]bool{"removed": true})
	})
	mux.HandleFunc("GET /v1/containers/{session}", func(w http.ResponseWriter, r *http.Request) {
		status, err := svc.Status(r.Context(), r.PathValue("session"))
		if err != nil {
			fail(w, "container.status", err)
			return
		}
		write(w, http.StatusOK, status)
	})
	mux.HandleFunc("GET /v1/containers/{session}/stats", func(w http.ResponseWriter, r *http.Request) {
		stats, err := svc.Stats(r.Context(), r.PathValue("session"))
		if err != nil {
			fail(w, "container.stats", err)
			return
		}
		write(w, http.StatusOK, stats)
	})
	mux.HandleFunc("GET /v1/exams/{exam}/stats", func(w http.ResponseWriter, r *http.Request) {
		stats, err := svc.ExamStats(r.Context(), r.PathValue("exam"))
		if err != nil {
			fail(w, "container.stats", err)
			return
		}
		if stats == nil {
			stats = []Stats{}
		}
		write(w, http.StatusOK, map[string][]Stats{"containers": stats})
	})
	mux.HandleFunc("GET /v1/containers/{session}/archive", func(w http.ResponseWriter, r *http.Request) {
		stream, err := svc.Archive(r.Context(), r.PathValue("session"), r.URL.Query().Get("path"))
		if err != nil {
			fail(w, "container.archive", err)
			return
		}
		defer stream.Close()
		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, stream)
	})
	mux.HandleFunc("GET /v1/containers/{session}/agent", func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), UpgradeProtocol) {
			fail(w, "agent.connect", badRequest("agent.connect upgrades the connection: send Upgrade: %s", UpgradeProtocol))
			return
		}
		agent, err := svc.ConnectAgent(r.Context(), r.PathValue("session"))
		if err != nil {
			fail(w, "agent.connect", err)
			return
		}
		relay(w, agent, logger)
	})
	mux.HandleFunc("GET /v1/images", func(w http.ResponseWriter, r *http.Request) {
		status, err := svc.ImageInspect(r.Context(), r.URL.Query().Get("name"))
		if err != nil {
			fail(w, "image.inspect", err)
			return
		}
		write(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /v1/grading", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxGradeBody)
		reader, err := r.MultipartReader()
		if err != nil {
			fail(w, "grading.run", badRequest("grading.run takes multipart/form-data with the parts request, submission and grading"))
			return
		}
		// The request comes first, so that it is checked before the two archives are read.
		first, err := reader.NextPart()
		if err != nil || first.FormName() != "request" {
			fail(w, "grading.run", badRequest("the first part of grading.run is request"))
			return
		}
		var req GradeRequest
		decoder := json.NewDecoder(io.LimitReader(first, maxJSONBody))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil || decoder.More() {
			fail(w, "grading.run", badRequest("the request is not the JSON grading.run takes (grading, exam, image, timeout_seconds, resources)"))
			return
		}
		// The two archives are read in turn, each when the service asks for it, and in the order the plan names them.
		next := func(name string) func() (io.Reader, error) {
			return func() (io.Reader, error) {
				part, err := reader.NextPart()
				if err != nil || part.FormName() != name {
					return nil, badRequest("the next part of grading.run is %s", name)
				}
				return part, nil
			}
		}
		result, err := svc.Grade(r.Context(), req, &lazyPart{open: next("submission")}, &drained{part: &lazyPart{open: next("grading")}, body: r.Body})
		if err != nil {
			fail(w, "grading.run", err)
			return
		}
		write(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/images/build", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBuildBody)
		reader, err := r.MultipartReader()
		if err != nil {
			fail(w, "image.build", badRequest("image.build takes multipart/form-data with the parts request and context"))
			return
		}
		first, err := reader.NextPart()
		if err != nil || first.FormName() != "request" {
			fail(w, "image.build", badRequest("the first part of image.build is request"))
			return
		}
		var req BuildRequest
		decoder := json.NewDecoder(io.LimitReader(first, maxJSONBody))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil || decoder.More() {
			fail(w, "image.build", badRequest("the request is not the JSON image.build takes (exam, image, rebuild, timeout_seconds)"))
			return
		}
		part, err := reader.NextPart()
		if err != nil || part.FormName() != "context" {
			fail(w, "image.build", badRequest("the second part of image.build is context"))
			return
		}
		// What the build prints goes to the client as it is printed (below), and the result is the last message.
		stream := newStreamWriter(w)
		defer stream.close()
		result, err := svc.BuildImageLive(r.Context(), req, &drained{part: part, body: r.Body}, stream)
		if err != nil {
			if stream.started() {
				// The answer has begun, with its status: the failure is the last message of the stream.
				status, message := answer("image.build", err)
				stream.fail(status, message)
				return
			}
			fail(w, "image.build", err)
			return
		}
		stream.finish(result)
	})
	return authenticate(secret, mux, logger)
}

// drained is a part of a request that, when it has been read to its end, reads the rest of the request's body to its end (the closing boundary, which is
// not wanted). Only a body that has been read to its end is watched by the server for a client that goes away, which is what cancels a build or a grading
// run: without it, one run in eight of a cancelled build went on to its end (found in CI, then reproduced: 11 of 80 runs of the test with -race).
type drained struct {
	part io.Reader
	body io.Reader
	done bool
}

func (d *drained) Read(p []byte) (int, error) {
	n, err := d.part.Read(p)
	if errors.Is(err, io.EOF) && !d.done {
		d.done = true
		_, _ = io.Copy(io.Discard, d.body)
	}
	return n, err
}

// lazyPart reads a part of a multipart request when it is first read, so that the parts are taken in order and none is read before it is
// wanted.
type lazyPart struct {
	open func() (io.Reader, error)
	r    io.Reader
	err  error
}

func (l *lazyPart) Read(p []byte) (int, error) {
	if l.r == nil && l.err == nil {
		l.r, l.err = l.open()
	}
	if l.err != nil {
		return 0, l.err
	}
	return l.r.Read(p)
}

// authenticate lets through only requests that carry the shared secret. The comparison takes the same time whatever the secret
// they send.
func authenticate(secret string, next http.Handler, logger *slog.Logger) http.Handler {
	expected := sha256.Sum256([]byte(secret))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, provided, found := strings.Cut(r.Header.Get("Authorization"), " ")
		actual := sha256.Sum256([]byte(provided))
		if secret == "" || len(r.Header.Values("Authorization")) != 1 || !found || !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			logger.Warn("runner request refused: bad credential", "path", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// relay takes over the connection and copies bytes between it and the agent, until either end closes. It does not read them: the
// exam server validates what the agent says, and the agent what the exam server says.
func relay(w http.ResponseWriter, agent net.Conn, logger *slog.Logger) {
	defer agent.Close()
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		logger.Error("agent.connect: the connection cannot be taken over", "error", err)
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Time{})
	if _, err := rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: " + UpgradeProtocol + "\r\n\r\n"); err != nil {
		return
	}
	if err := rw.Flush(); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() {
		// What the exam server already sent after its request is in the buffer.
		copyBuffered(agent, rw.Reader, conn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(conn, agent)
		done <- struct{}{}
	}()
	<-done
}

func copyBuffered(dst net.Conn, buffered *bufio.Reader, src net.Conn) {
	if n := buffered.Buffered(); n > 0 {
		chunk, _ := buffered.Peek(n)
		if _, err := dst.Write(chunk); err != nil {
			return
		}
		_, _ = buffered.Discard(n)
	}
	_, _ = io.Copy(dst, src)
}

// Serve answers the API on listener until ctx ends.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
