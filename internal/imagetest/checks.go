package imagetest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"exam-platform/internal/agentproto"
	"exam-platform/internal/runner"
)

// ChecksFile is the file of a course's own checks, in the image's folder next to its Containerfile.
const ChecksFile = "checks"

// Check is one of a course's own checks: a script run in a terminal of the workspace, in /work, with sh -c and `set -e`. It passes when it ends
// with status 0. Line is where it starts in its file.
type Check struct {
	Name, Script string
	Line         int
}

// ParseChecks reads a checks file. A check starts with a line `# check: what it checks`, and its script is the lines that follow, up to the next
// check; the lines before the first check are the file's own comments.
func ParseChecks(text string) ([]Check, error) {
	var checks []Check
	for i, line := range strings.Split(text, "\n") {
		if name, found := strings.CutPrefix(strings.TrimSpace(line), "# check:"); found {
			if name = strings.TrimSpace(name); name == "" {
				return nil, fmt.Errorf("line %d: `# check:` is followed by what the check checks", i+1)
			}
			checks = append(checks, Check{Name: name, Line: i + 1})
			continue
		}
		if len(checks) > 0 {
			checks[len(checks)-1].Script += line + "\n"
		}
	}
	if len(checks) == 0 {
		return nil, errors.New("there is no check in it: a check starts with a line `# check: what it checks`, and its commands follow")
	}
	for i := range checks {
		checks[i].Script = strings.TrimRight(checks[i].Script, "\n") + "\n"
		commands := false
		for _, line := range strings.Split(checks[i].Script, "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				commands = true
			}
		}
		if !commands {
			return nil, fmt.Errorf("line %d: the check %q has no command", checks[i].Line, checks[i].Name)
		}
	}
	return checks, nil
}

// ReadChecks reads the checks file of an image's folder. found is false when the folder has none.
func ReadChecks(folder string) (checks []Check, found bool, err error) {
	path := filepath.Join(folder, ChecksFile)
	text, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	checks, err = ParseChecks(string(text))
	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", path, err)
	}
	return checks, true, nil
}

// groupOf is a group of checks by name, for what the options say (the course's checks, the runner's folder).
func groupOf(name string, o Options) group {
	switch name {
	case groupAgent:
		return group{"the agent, through the runner", []check{
			{"it answers through the runner, in protocol 1", checkHello},
			{"it is the first process of the workspace (PID 1)", checkPID1},
			{"it runs as the student, uid 1000", checkAgentUser},
			{"the kernel lets it open files without following links out of /work (openat2)", checkOpenat2},
			{"its heartbeat arrives", checkHeartbeat},
		}}
	case groupStudent:
		return group{"the student", []check{
			{"the user is student, 1000:1000", checkStudent},
			{"/work is the student's, with the starter in it", checkWork},
			{"the root file system is read-only", checkReadOnly},
			{"there is no network", checkNoNetwork},
			{"there is no way to gain privileges: no capabilities, no new privileges, no sudo", checkNoPrivileges},
			{"no file is setuid or setgid", checkNoSetuid},
		}}
	case groupFiles:
		return group{"files, through the agent", []check{
			{"create, read, write, move and delete", checkFiles},
			{"a file made in a terminal is the student's", checkTerminalFile},
			{"a link made in a terminal leads nowhere outside /work", checkLinks},
		}}
	case groupTerminal:
		return group{"terminals", []check{
			{"a command's exit status comes back", checkExitStatus},
			{"a terminal has the size it was opened with", checkSize},
			{"a file made in a terminal is an event", checkEvent},
			{"orphaned processes are reaped (no zombies)", checkZombies},
		}}
	case groupCourse:
		g := group{title: fmt.Sprintf("the course's own checks (%s)", ChecksFile)}
		for _, c := range o.Checks {
			g.checks = append(g.checks, check{c.Name, courseCheck(c)})
		}
		return g
	case groupLimits:
		return group{"limits", []check{
			{"a full /work is QUOTA, for a new file and for a write, and frees up again", checkQuota},
			{"the memory limit kills the program, not the workspace, and the agent says so", checkMemory},
			{"a fork storm stops at the process limit, and the agent still answers", checkForkStorm},
		}}
	case groupStop:
		g := group{"stopping", []check{{"a stop ends the agent at once (SIGTERM), with status 0", checkStop}}}
		if o.RunRoot != "" {
			g.checks = append(g.checks, check{"the agent's socket is gone after the stop", checkSocketGone})
		}
		return g
	}
	return group{}
}

// ----- the agent

func (t *tester) hello() (agentproto.Message, error) {
	hello, found := t.await(0, 10*time.Second, func(m agentproto.Message) bool { return m.Type == agentproto.TypeHello })
	if !found {
		return hello, errors.New("no agent.hello came through the runner's connection")
	}
	return hello, nil
}

func checkHello(t *tester) error {
	hello, err := t.hello()
	if err != nil {
		return err
	}
	if hello.Proto != agentproto.ProtocolVersion {
		return fmt.Errorf("the agent speaks protocol %d, and the runner %d", hello.Proto, agentproto.ProtocolVersion)
	}
	return nil
}

func checkPID1(t *tester) error {
	hello, err := t.hello()
	if err != nil {
		return err
	}
	if !hello.PID1 {
		return errors.New("the agent says it is not PID 1: the image's entrypoint must be the agent")
	}
	printed, err := t.sh(`tr '\0' ' ' < /proc/1/cmdline`)
	if err != nil {
		return err
	}
	if !strings.Contains(printed, runner.AgentPath) {
		return expect(printed, "the agent, "+runner.AgentPath)
	}
	return nil
}

func checkAgentUser(t *tester) error {
	printed, err := t.sh(`grep '^Uid:' /proc/1/status`)
	if err != nil {
		return err
	}
	if fields := strings.Fields(printed); len(fields) != 5 || fields[1] != "1000" || fields[2] != "1000" || fields[3] != "1000" || fields[4] != "1000" {
		return expect(printed, "Uid: 1000 1000 1000 1000")
	}
	return nil
}

func checkOpenat2(t *tester) error {
	hello, err := t.hello()
	if err != nil {
		return err
	}
	if hello.SlowPaths {
		return errors.New("the agent has no openat2 (slow paths): it follows no link at all, and the student's links do not work")
	}
	return nil
}

func checkHeartbeat(t *tester) error {
	if _, found := t.await(0, 15*time.Second, func(m agentproto.Message) bool { return m.Type == agentproto.TypeHeartbeat }); !found {
		return errors.New("no agent.hb came within 15 seconds")
	}
	return nil
}

// ----- the student

func checkStudent(t *tester) error {
	printed, err := t.sh(`id -un; id -u; id -g`)
	if err != nil {
		return err
	}
	if strings.Join(strings.Fields(printed), " ") != "student 1000 1000" {
		return expect(printed, "student, 1000 and 1000")
	}
	return nil
}

func checkWork(t *tester) error {
	printed, err := t.sh(`pwd; stat -c %u:%g /work; cat README.txt`)
	if err != nil {
		return err
	}
	if lines := strings.Split(strings.TrimSpace(printed), "\n"); len(lines) < 3 || lines[0] != runner.WorkPath || lines[1] != "1000:1000" || !strings.Contains(lines[2], "image test") {
		return expect(printed, "/work, 1000:1000, and the starter's README.txt")
	}
	return nil
}

func checkReadOnly(t *tester) error {
	result, err := t.run(`touch /usr/.cbe-image-test`, 120, 30, time.Minute)
	if err != nil {
		return err
	}
	if result.code == 0 {
		return errors.New("a file was made in /usr")
	}
	return nil
}

func checkNoNetwork(t *tester) error {
	printed, err := t.sh(`ls /sys/class/net`)
	if err != nil {
		return err
	}
	if strings.Join(strings.Fields(printed), " ") != "lo" {
		return expect(printed, "lo alone")
	}
	return nil
}

func checkNoPrivileges(t *tester) error {
	printed, err := t.sh(`grep -E '^(CapEff|NoNewPrivs):' /proc/self/status; command -v sudo || echo no-sudo`)
	if err != nil {
		return err
	}
	text := strings.Join(strings.Fields(printed), " ")
	if !strings.Contains(text, "CapEff: 0000000000000000") || !strings.Contains(text, "NoNewPrivs: 1") || !strings.HasSuffix(text, "no-sudo") {
		return expect(printed, "CapEff: 0000000000000000, NoNewPrivs: 1 and no sudo")
	}
	return nil
}

func checkNoSetuid(t *tester) error {
	printed, err := t.sh(`find / -xdev -type f \( -perm -4000 -o -perm -2000 \) 2>/dev/null | head -5; echo end`)
	if err != nil {
		return err
	}
	if strings.TrimSpace(printed) != "end" {
		return expect(printed, "no file")
	}
	return nil
}

// ----- files

func checkFiles(t *tester) error {
	steps := []struct {
		what    string
		request agentproto.Request
	}{
		{"fs.mkdir src", &agentproto.FSMkdir{Header: header(agentproto.TypeFSMkdir), Path: "src"}},
		{"fs.create src/a.c", &agentproto.FSCreate{Header: header(agentproto.TypeFSCreate), Path: "src/a.c", Content: "int a;\n"}},
	}
	for _, s := range steps {
		if _, err := t.do(s.request); err != nil {
			return fmt.Errorf("%s: %w", s.what, err)
		}
	}
	read, err := t.do(&agentproto.FSRead{Header: header(agentproto.TypeFSRead), Path: "src/a.c"})
	if err != nil || read.Content == nil || *read.Content != "int a;\n" {
		return fmt.Errorf("fs.read src/a.c: %v", err)
	}
	if _, err := t.do(&agentproto.FSWrite{Header: header(agentproto.TypeFSWrite), Path: "src/a.c", Content: "int b;\n", BaseVersion: read.Version}); err != nil {
		return fmt.Errorf("fs.write src/a.c: %w", err)
	}
	if read, err := t.do(&agentproto.FSRead{Header: header(agentproto.TypeFSRead), Path: "src/a.c"}); err != nil || read.Content == nil || *read.Content != "int b;\n" {
		return fmt.Errorf("fs.read after the write: %v", err)
	}
	if _, err := t.do(&agentproto.FSRename{Header: header(agentproto.TypeFSRename), Path: "src/a.c", To: "src/b.c"}); err != nil {
		return fmt.Errorf("fs.rename: %w", err)
	}
	list, err := t.do(&agentproto.FSList{Header: header(agentproto.TypeFSList), Path: "src"})
	if err != nil || len(list.Entries) != 1 || list.Entries[0].Name != "b.c" {
		return fmt.Errorf("fs.list src after the move: %+v, %v", list.Entries, err)
	}
	if _, err := t.do(&agentproto.FSDelete{Header: header(agentproto.TypeFSDelete), Path: "src"}); err != nil {
		return fmt.Errorf("fs.delete src: %w", err)
	}
	if _, err := t.do(&agentproto.FSList{Header: header(agentproto.TypeFSList), Path: "src"}); agentproto.CodeOf(err) != agentproto.CodeNotFound {
		return fmt.Errorf("src is still there after fs.delete: %v", err)
	}
	return nil
}

func checkTerminalFile(t *tester) error {
	printed, err := t.sh(`echo made > in-terminal.txt; stat -c %u:%g in-terminal.txt`)
	if err != nil {
		return err
	}
	if strings.TrimSpace(printed) != "1000:1000" {
		return expect(printed, "1000:1000")
	}
	return nil
}

func checkLinks(t *tester) error {
	if _, err := t.sh(`ln -s /etc to-etc; ln -s ../../../../etc/passwd up; ln -s loop loop`); err != nil {
		return err
	}
	if _, err := t.do(&agentproto.FSRead{Header: header(agentproto.TypeFSRead), Path: "to-etc/passwd"}); agentproto.CodeOf(err) != agentproto.CodeBadPath {
		return fmt.Errorf("fs.read to-etc/passwd was not BAD_PATH: %v", err)
	}
	for _, path := range []string{"up", "loop"} {
		if _, err := t.do(&agentproto.FSRead{Header: header(agentproto.TypeFSRead), Path: path}); err == nil {
			return fmt.Errorf("fs.read %s read a file through the link", path)
		}
	}
	if _, err := t.do(&agentproto.FSCreate{Header: header(agentproto.TypeFSCreate), Path: "to-etc/planted", Content: "x"}); err == nil {
		return errors.New("fs.create to-etc/planted made a file")
	}
	if _, err := t.sh(`test ! -e /etc/planted`); err != nil {
		return errors.New("/etc/planted is there")
	}
	return nil
}

// ----- terminals

func checkExitStatus(t *tester) error {
	result, err := t.run(`exit 3`, 120, 30, time.Minute)
	if err != nil {
		return err
	}
	if result.code != 3 {
		return fmt.Errorf("`exit 3` ended with status %d", result.code)
	}
	return nil
}

func checkSize(t *tester) error {
	result, err := t.run(`stty size`, 132, 50, time.Minute)
	if err != nil {
		return err
	}
	if strings.TrimSpace(result.output) != "50 132" {
		return expect(result.output, "50 132")
	}
	return nil
}

func checkEvent(t *tester) error {
	from := len(t.seen)
	if _, err := t.sh(`touch from-terminal.txt`); err != nil {
		return err
	}
	if _, found := t.await(from, 10*time.Second, func(m agentproto.Message) bool {
		return m.Type == agentproto.TypeFSEvent && m.Path == "from-terminal.txt"
	}); !found {
		return errors.New("no fs.event for from-terminal.txt came within 10 seconds")
	}
	return nil
}

func checkZombies(t *tester) error {
	if _, err := t.sh(`sleep 0.3 & sleep 0.1 & exit 0`); err != nil {
		return err
	}
	sleep(t.ctx, 1500*time.Millisecond)
	printed, err := t.sh(`for f in /proc/[0-9]*/status; do grep -q '^State:[[:space:]]*Z' "$f" 2>/dev/null && echo "zombie $f"; done; echo end`)
	if err != nil {
		return err
	}
	if strings.Contains(printed, "zombie") {
		return expect(printed, "no zombie")
	}
	return nil
}

// ----- the course's own checks

// courseScript is where a course's check is put in /work to be run: a terminal's command is one line, and a check is a script.
const courseScript = ".image-test-check.sh"

func courseCheck(c Check) func(*tester) error {
	return func(t *tester) error {
		_, _ = t.do(&agentproto.FSDelete{Header: header(agentproto.TypeFSDelete), Path: courseScript})
		if _, err := t.do(&agentproto.FSCreate{Header: header(agentproto.TypeFSCreate), Path: courseScript, Content: c.Script}); err != nil {
			return fmt.Errorf("the check could not be put in /work: %w", err)
		}
		defer t.do(&agentproto.FSDelete{Header: header(agentproto.TypeFSDelete), Path: courseScript})
		result, err := t.run("sh -e "+courseScript, 120, 30, 3*time.Minute)
		if err != nil {
			return fmt.Errorf("%s, line %d: %w", ChecksFile, c.Line, err)
		}
		if result.code != 0 {
			return fmt.Errorf("%s, line %d: status %d; it printed:\n%s", ChecksFile, c.Line, result.code, tail(result.output, 10))
		}
		return nil
	}
}

// ----- limits

func checkQuota(t *tester) error {
	if _, err := t.do(&agentproto.FSCreate{Header: header(agentproto.TypeFSCreate), Path: "kept.txt", Content: "kept\n"}); err != nil {
		return fmt.Errorf("fs.create kept.txt: %w", err)
	}
	result, err := t.run(`dd if=/dev/zero of=big bs=1M count=100 2>&1; echo done`, 120, 30, 2*time.Minute)
	if err != nil {
		return err
	}
	if !strings.Contains(result.output, "No space left") {
		return fmt.Errorf("writing 100 MB to /work did not fill it (%d MB): %s", Resources.DiskMB, tail(result.output, 4))
	}
	if _, err := t.do(&agentproto.FSCreate{Header: header(agentproto.TypeFSCreate), Path: "small.txt", Content: "x"}); agentproto.CodeOf(err) != agentproto.CodeQuota {
		return fmt.Errorf("fs.create on a full /work was not QUOTA: %v", err)
	}
	read, err := t.do(&agentproto.FSRead{Header: header(agentproto.TypeFSRead), Path: "kept.txt"})
	if err != nil {
		return fmt.Errorf("fs.read kept.txt: %w", err)
	}
	if _, err := t.do(&agentproto.FSWrite{Header: header(agentproto.TypeFSWrite), Path: "kept.txt", Content: "changed\n", BaseVersion: read.Version}); agentproto.CodeOf(err) != agentproto.CodeQuota {
		return fmt.Errorf("fs.write on a full /work was not QUOTA: %v", err)
	}
	if _, err := t.do(&agentproto.FSDelete{Header: header(agentproto.TypeFSDelete), Path: "big"}); err != nil {
		return fmt.Errorf("fs.delete big: %w", err)
	}
	if _, err := t.do(&agentproto.FSCreate{Header: header(agentproto.TypeFSCreate), Path: "small.txt", Content: "x"}); err != nil {
		return fmt.Errorf("fs.create after the room was freed: %w", err)
	}
	if read, err := t.do(&agentproto.FSRead{Header: header(agentproto.TypeFSRead), Path: "kept.txt"}); err != nil || read.Content == nil || *read.Content != "kept\n" {
		return fmt.Errorf("kept.txt changed when /work was full: %v", err)
	}
	return nil
}

func checkMemory(t *tester) error {
	// The shell keeps what the command prints in its memory: 400 MB, over the workspace's 256 MB.
	result, err := t.run(`x=$(head -c 400000000 /dev/zero | tr '\0' a); echo ${#x}`, 120, 30, 2*time.Minute)
	if err != nil {
		return err
	}
	if result.code != 137 || !result.oomKilled {
		return fmt.Errorf("the program ended with status %d (out of memory: %v), and should have been killed by the memory limit (137)", result.code, result.oomKilled)
	}
	if _, found := t.await(len(t.seen), 15*time.Second, func(m agentproto.Message) bool {
		return m.Type == agentproto.TypeHeartbeat && m.OOMKills > 0
	}); !found {
		return errors.New("no heartbeat counted the kill within 15 seconds")
	}
	if _, err := t.do(&agentproto.Ping{Header: header(agentproto.TypePing)}); err != nil {
		return fmt.Errorf("the agent does not answer after the kill: %w", err)
	}
	return nil
}

func checkForkStorm(t *tester) error {
	reply, err := t.do(&agentproto.PTYOpen{Header: header(agentproto.TypePTYOpen), Cols: 120, Rows: 30,
		Cmd: `i=0; while [ $i -lt 400 ]; do sleep 3 & i=$((i+1)); done 2>/dev/null; wait`})
	if err != nil {
		return fmt.Errorf("pty.open: %w", err)
	}
	sleep(t.ctx, time.Second)
	if _, err := t.do(&agentproto.Ping{Header: header(agentproto.TypePing)}); err != nil {
		return fmt.Errorf("the agent does not answer during the storm: %w", err)
	}
	_, found := t.await(len(t.seen), time.Minute, func(m agentproto.Message) bool { return m.Type == agentproto.TypePTYExit && m.Term == reply.Term })
	_, _ = t.do(&agentproto.PTYClose{Header: header(agentproto.TypePTYClose), Term: reply.Term})
	if !found {
		return errors.New("the storm did not end within a minute")
	}
	printed, err := t.sh(`echo alive`)
	if err != nil {
		return fmt.Errorf("after the storm: %w", err)
	}
	if strings.TrimSpace(printed) != "alive" {
		return expect(printed, "alive")
	}
	return nil
}

// ----- stopping

func checkStop(t *tester) error {
	started := time.Now()
	if _, err := t.o.Runner.Stop(t.ctx, t.session); err != nil {
		return fmt.Errorf("container.stop: %w", err)
	}
	took := time.Since(started)
	status, err := t.o.Runner.Status(t.ctx, t.session)
	if err != nil {
		return fmt.Errorf("container.status: %w", err)
	}
	switch {
	case status.State != runner.StateExited || status.ExitCode == nil:
		return fmt.Errorf("after the stop the workspace is %s", status.State)
	case *status.ExitCode != 0:
		return fmt.Errorf("the agent ended with status %d (in %s): it did not end by itself on SIGTERM, and was killed", *status.ExitCode, took.Round(100*time.Millisecond))
	case took > 4*time.Second:
		return fmt.Errorf("the stop took %s: the agent did not end at once on SIGTERM", took.Round(100*time.Millisecond))
	}
	return nil
}

func checkSocketGone(t *tester) error {
	if !socketGone(t.o.RunRoot, t.session) {
		return fmt.Errorf("%s is still there", filepath.Join(t.o.RunRoot, t.session, "agent.sock"))
	}
	return nil
}
