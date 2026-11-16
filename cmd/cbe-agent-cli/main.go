//go:build linux

// Command cbe-agent-cli drives a cbe-agent through its socket without the exam server: it is the test harness of the agent, and a
// way to look at a container by hand.
//
//	cbe-agent-cli [-socket /run/cbe/agent.sock] [-json] [-timeout 10s] COMMAND [ARGS]
//
//	status                  the agent's hello and its next heartbeat
//	ls [PATH]               list a folder
//	cat PATH                print a text file
//	write PATH              replace a file with what comes on standard input (it reads the file's version first)
//	create PATH             make a file with what comes on standard input
//	mkdir PATH              make a folder
//	mv FROM TO              rename
//	rm PATH                 delete a file or folder
//	watch [-for 5s] [-n N]  print file events as JSON lines
//	exec [-cwd DIR] [-cols N] [-rows N] COMMAND...
//	                        run a command in a terminal, print its output, and exit with its exit code
//	shell [-cwd DIR]        an interactive terminal (needs a terminal on standard input)
//	terms                   list the open terminals
//	attach TERM             stream a terminal's output until it ends
//	close TERM              close a terminal
//	raw JSON                send one request as written and print the reply
//
// It exits with 0 on success, 1 when the agent refuses the request or cannot be reached, 2 for a mistake in the command line.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"exam-platform/internal/agentproto"
	"golang.org/x/sys/unix"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type session struct {
	client  *agentproto.Client
	stdout  io.Writer
	stderr  io.Writer
	timeout time.Duration
	asJSON  bool
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("cbe-agent-cli", flag.ContinueOnError)
	flags.SetOutput(stderr)
	socket := flags.String("socket", "/run/cbe/agent.sock", "the agent's socket")
	asJSON := flags.Bool("json", false, "print replies as JSON")
	timeout := flags.Duration("timeout", 10*time.Second, "how long to wait for a reply")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: cbe-agent-cli [-socket PATH] [-json] [-timeout 10s] status|ls|cat|write|create|mkdir|mv|rm|watch|exec|shell|terms|attach|close|raw ...")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	rest := flags.Args()
	if len(rest) == 0 {
		flags.Usage()
		return 2
	}
	client, err := agentproto.Dial(*socket)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer client.Close()
	s := &session{client: client, stdout: stdout, stderr: stderr, timeout: *timeout, asJSON: *asJSON}
	command, operands := rest[0], rest[1:]
	need := func(n int) bool {
		if len(operands) != n {
			fmt.Fprintf(stderr, "%s takes %d argument(s)\n", command, n)
			return false
		}
		return true
	}
	h := func(kind string) agentproto.Header { return agentproto.Header{Type: kind} }
	switch command {
	case "status":
		hello := s.waitFor(agentproto.TypeHello)
		beat := s.waitFor(agentproto.TypeHeartbeat)
		s.print(hello)
		s.print(beat)
		return 0
	case "ls":
		path := ""
		if len(operands) > 1 {
			fmt.Fprintln(stderr, "ls takes at most one argument")
			return 2
		}
		if len(operands) == 1 {
			path = operands[0]
		}
		reply, err := s.do(&agentproto.FSList{Header: h(agentproto.TypeFSList), Path: path})
		if err != nil {
			return s.fail(err)
		}
		if s.asJSON {
			s.print(reply)
			return 0
		}
		for _, entry := range reply.Entries {
			fmt.Fprintf(stdout, "%-5s %9d  %s\n", entry.Kind, entry.Size, entry.Name)
		}
		return 0
	case "cat":
		if !need(1) {
			return 2
		}
		reply, err := s.do(&agentproto.FSRead{Header: h(agentproto.TypeFSRead), Path: operands[0]})
		if err != nil {
			return s.fail(err)
		}
		if s.asJSON {
			s.print(reply)
			return 0
		}
		io.WriteString(stdout, *reply.Content)
		return 0
	case "write":
		if !need(1) {
			return 2
		}
		content, err := io.ReadAll(stdin)
		if err != nil {
			return s.fail(err)
		}
		current, err := s.do(&agentproto.FSRead{Header: h(agentproto.TypeFSRead), Path: operands[0]})
		if err != nil {
			return s.fail(err)
		}
		reply, err := s.do(&agentproto.FSWrite{Header: h(agentproto.TypeFSWrite), Path: operands[0], Content: string(content), BaseVersion: current.Version})
		if err != nil {
			return s.fail(err)
		}
		s.printVersion(reply)
		return 0
	case "create":
		if !need(1) {
			return 2
		}
		content, err := io.ReadAll(stdin)
		if err != nil {
			return s.fail(err)
		}
		reply, err := s.do(&agentproto.FSCreate{Header: h(agentproto.TypeFSCreate), Path: operands[0], Content: string(content)})
		if err != nil {
			return s.fail(err)
		}
		s.printVersion(reply)
		return 0
	case "mkdir", "rm":
		if !need(1) {
			return 2
		}
		var request agentproto.Request = &agentproto.FSMkdir{Header: h(agentproto.TypeFSMkdir), Path: operands[0]}
		if command == "rm" {
			request = &agentproto.FSDelete{Header: h(agentproto.TypeFSDelete), Path: operands[0]}
		}
		if _, err := s.do(request); err != nil {
			return s.fail(err)
		}
		return 0
	case "mv":
		if !need(2) {
			return 2
		}
		if _, err := s.do(&agentproto.FSRename{Header: h(agentproto.TypeFSRename), Path: operands[0], To: operands[1]}); err != nil {
			return s.fail(err)
		}
		return 0
	case "watch":
		return s.watch(operands)
	case "exec":
		return s.exec(operands)
	case "shell":
		return s.shell(operands)
	case "terms":
		reply, err := s.do(&agentproto.PTYList{Header: h(agentproto.TypePTYList)})
		if err != nil {
			return s.fail(err)
		}
		if s.asJSON {
			s.print(reply)
			return 0
		}
		for _, t := range reply.Terminals {
			state := "running"
			if t.Exited {
				state = fmt.Sprintf("exited %d", *t.ExitCode)
			}
			fmt.Fprintf(stdout, "%d  %dx%d  %s  %s\n", t.Term, t.Cols, t.Rows, state, t.Cmd)
		}
		return 0
	case "attach", "close":
		if !need(1) {
			return 2
		}
		term, err := strconv.ParseUint(operands[0], 10, 32)
		if err != nil {
			fmt.Fprintln(stderr, "the terminal must be a number")
			return 2
		}
		if command == "close" {
			if _, err := s.do(&agentproto.PTYClose{Header: h(agentproto.TypePTYClose), Term: uint32(term)}); err != nil {
				return s.fail(err)
			}
			return 0
		}
		if _, err := s.do(&agentproto.PTYAttach{Header: h(agentproto.TypePTYAttach), Term: uint32(term)}); err != nil {
			return s.fail(err)
		}
		return s.stream(uint32(term))
	case "raw":
		if !need(1) {
			return 2
		}
		if !json.Valid([]byte(operands[0])) {
			fmt.Fprintln(stderr, "the argument is not JSON")
			return 2
		}
		reply, err := s.sendRaw(operands[0])
		if err != nil {
			return s.fail(err)
		}
		s.print(reply)
		if reply.Failed() {
			return 1
		}
		return 0
	}
	fmt.Fprintf(stderr, "unknown command %q\n", command)
	return 2
}

func (s *session) do(request agentproto.Request) (agentproto.Message, error) {
	return s.client.Do(request, s.timeout)
}

func (s *session) fail(err error) int {
	fmt.Fprintln(s.stderr, err)
	return 1
}

func (s *session) print(m agentproto.Message) {
	payload, _ := agentproto.Encode(m)
	fmt.Fprintln(s.stdout, string(payload))
}

func (s *session) printVersion(m agentproto.Message) {
	if s.asJSON {
		s.print(m)
	} else if m.Version != "" {
		fmt.Fprintln(s.stdout, m.Version)
	}
}

func (s *session) waitFor(kind string) agentproto.Message {
	deadline := time.After(s.timeout)
	for {
		select {
		case m, open := <-s.client.Events:
			if !open {
				return agentproto.Message{}
			}
			if m.Type == kind {
				return m
			}
		case <-deadline:
			return agentproto.Message{Type: kind, Message: "no such message arrived"}
		}
	}
}

// sendRaw sends one request as the user wrote it, with an ID of 1, and waits for the message with that ID.
func (s *session) sendRaw(text string) (agentproto.Message, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		return agentproto.Message{}, err
	}
	fields["id"] = json.RawMessage("1")
	payload, _ := json.Marshal(fields)
	if err := s.client.SendRaw(payload); err != nil {
		return agentproto.Message{}, err
	}
	deadline := time.After(s.timeout)
	for {
		select {
		case m, open := <-s.client.Events:
			if !open {
				return agentproto.Message{}, agentproto.ErrClosed
			}
			if m.ID == 1 {
				return m, nil
			}
		case <-deadline:
			return agentproto.Message{}, errors.New("no reply")
		}
	}
}

func (s *session) watch(args []string) int {
	flags := flag.NewFlagSet("watch", flag.ContinueOnError)
	flags.SetOutput(s.stderr)
	duration := flags.Duration("for", 5*time.Second, "how long to watch")
	count := flags.Int("n", 0, "stop after this many events (0: no limit)")
	if flags.Parse(args) != nil {
		return 2
	}
	deadline := time.After(*duration)
	seen := 0
	for {
		select {
		case m, open := <-s.client.Events:
			if !open {
				return 1
			}
			if m.Type != agentproto.TypeFSEvent {
				continue
			}
			s.print(m)
			if seen++; *count > 0 && seen >= *count {
				return 0
			}
		case <-deadline:
			return 0
		}
	}
}

func (s *session) exec(args []string) int {
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags.SetOutput(s.stderr)
	cwd := flags.String("cwd", "", "the folder to run in, from /work")
	cols := flags.Int("cols", 80, "terminal width")
	rows := flags.Int("rows", 24, "terminal height")
	if flags.Parse(args) != nil {
		return 2
	}
	if flags.NArg() == 0 {
		fmt.Fprintln(s.stderr, "exec needs a command")
		return 2
	}
	reply, err := s.do(&agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: *cols, Rows: *rows, Cmd: strings.Join(flags.Args(), " "), Cwd: *cwd})
	if err != nil {
		return s.fail(err)
	}
	code := s.stream(reply.Term)
	// A terminal that has ended stays open, as a tab does, and counts toward the limit of four until it is closed.
	_, _ = s.do(&agentproto.PTYClose{Header: agentproto.Header{Type: agentproto.TypePTYClose}, Term: reply.Term})
	return code
}

// stream prints a terminal's output until its program ends, and returns the program's exit code.
func (s *session) stream(term uint32) int {
	deadline := time.After(10 * time.Minute)
	events := s.client.Events
	for {
		select {
		case out, open := <-s.client.Terminal:
			if !open {
				return 1
			}
			if out.Term == term {
				s.stdout.Write(out.Data)
			}
		case m, open := <-events:
			if !open {
				return 1
			}
			if m.Type != agentproto.TypePTYExit || m.Term != term {
				continue
			}
			// The last of the output was sent before the exit; take what is waiting.
			for {
				select {
				case out := <-s.client.Terminal:
					if out.Term == term {
						s.stdout.Write(out.Data)
					}
					continue
				default:
				}
				break
			}
			if m.OOMKilled {
				fmt.Fprintln(s.stderr, "cbe-agent-cli: the container's memory limit killed a process")
			}
			if m.ExitCode == nil {
				return 1
			}
			return *m.ExitCode & 0xff
		case <-deadline:
			fmt.Fprintln(s.stderr, "timed out")
			return 1
		}
	}
}

// shell connects the real terminal to a terminal of the agent.
func (s *session) shell(args []string) int {
	flags := flag.NewFlagSet("shell", flag.ContinueOnError)
	flags.SetOutput(s.stderr)
	cwd := flags.String("cwd", "", "the folder to start in, from /work")
	if flags.Parse(args) != nil {
		return 2
	}
	fd := int(os.Stdin.Fd())
	size, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		fmt.Fprintln(s.stderr, "shell needs a terminal on standard input")
		return 2
	}
	reply, err := s.do(&agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: int(size.Col), Rows: int(size.Row), Cwd: *cwd})
	if err != nil {
		return s.fail(err)
	}
	saved, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return s.fail(err)
	}
	raw := *saved
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return s.fail(err)
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, saved)
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buffer)
			if n > 0 {
				_ = s.client.Input(reply.Term, buffer[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		for range winch {
			if size, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ); err == nil {
				_, _ = s.do(&agentproto.PTYResize{Header: agentproto.Header{Type: agentproto.TypePTYResize}, Term: reply.Term, Cols: int(size.Col), Rows: int(size.Row)})
			}
		}
	}()
	return s.stream(reply.Term)
}
