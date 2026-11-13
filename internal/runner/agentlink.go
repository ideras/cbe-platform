package runner

import (
	"context"
	"net"
	"sync"
	"time"

	"exam-platform/internal/agentproto"
)

// agentLink is the runner's own connection to the agent of one container. It reads what the agent says unprompted, its hello and its
// heartbeats, so that container.status can tell the exam server whether the agent is alive. It reconnects when the connection goes,
// and does nothing while the container is paused (a paused container freezes the agent too, and its heartbeat stops).
type agentLink struct {
	socket string
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	status AgentStatus
}

func startAgentLink(socket string) *agentLink {
	ctx, cancel := context.WithCancel(context.Background())
	l := &agentLink{socket: socket, cancel: cancel, done: make(chan struct{})}
	go l.run(ctx)
	return l
}

func (l *agentLink) stop() {
	l.cancel()
	<-l.done
}

func (l *agentLink) current() AgentStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.status
}

func (l *agentLink) set(change func(*AgentStatus)) {
	l.mu.Lock()
	change(&l.status)
	l.mu.Unlock()
}

func (l *agentLink) run(ctx context.Context) {
	defer close(l.done)
	for ctx.Err() == nil {
		l.connect(ctx)
		l.set(func(s *AgentStatus) { s.Connected = false })
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (l *agentLink) connect(ctx context.Context) {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", l.socket)
	if err != nil {
		return
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	l.set(func(s *AgentStatus) { s.Connected = true })
	for {
		frame, err := agentproto.ReadFrame(conn)
		if err != nil {
			return
		}
		if frame.Kind != agentproto.KindJSON {
			continue
		}
		message, err := agentproto.DecodeMessage(frame.Data)
		if err != nil || (message.Type != agentproto.TypeHello && message.Type != agentproto.TypeHeartbeat) {
			continue
		}
		l.set(func(s *AgentStatus) {
			s.LastSeen = time.Now().UTC()
			s.Version, s.UptimeMS, s.OpenTerminals, s.OOMKills, s.SlowPaths = message.AgentVersion, message.UptimeMS, message.OpenTerminals, message.OOMKills, message.SlowPaths
		})
	}
}
