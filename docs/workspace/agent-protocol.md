# The agent protocol

What `cbe-agent` and everything that talks to it say on the agent's Unix socket. It is the "same message set" of section 6 of the
workspace plan, as built in Phase 2. The Go side is `internal/agentproto` (frames, messages, path rules, a small client); the agent
is `internal/agent` and `cmd/cbe-agent`; `cmd/cbe-agent-cli` is the harness that drives it without the exam server.

**Trust.** The agent runs as the student, so the student can kill it, delete its socket and listen in its place. Everything that
comes **from** an agent is student-controlled input; the relay (Phase 3) validates it like any other. The other direction is the
agent's job: **no request reaches a file outside `/work`.**

## Frames

A frame is a 4-byte big-endian length, then that many bytes: one byte of kind, then the payload.

| Kind | Byte | Payload |
|------|------|---------|
| JSON | `1` | one JSON object: a request, a reply or an event |
| Binary | `2` | a 4-byte big-endian terminal ID, then raw terminal bytes (keystrokes to the agent, output from it) |

At most 8 MiB per frame (a 1 MiB file with every character escaped still fits) and 1 MiB of terminal data per binary frame. A frame that
is empty, too long, of an unknown kind, or a binary frame without a terminal ID is malformed, and **the agent closes the connection**:
after a malformed frame the stream cannot be trusted.

## Requests and replies

A request is a JSON object with a `type`, an optional `id` and the fields of its type. **Unknown types, unknown fields, fields of the
wrong type, trailing bytes and any value the rules refuse are errors.** The reply has the same `id` and `type`, `"ok": true` or
`"ok": false` with a `code` and a `message`. A request with no `id` still gets a reply, with no `id`.

File requests of one connection run **in the order they were sent**. Terminal requests are answered at once.

### Paths

A path is relative to `/work`, with `/` between names. `""` is `/work` itself (only `fs.list` and `pty.open`'s `cwd` accept it). **Refused
(`BAD_PATH`):** absolute paths, empty names (`a//b`, a trailing `/`), `.` and `..`, NUL bytes, backslashes, control characters, invalid
UTF-8, a name over 255 bytes, a path over 4,096 bytes, and any name that starts with `.cbe-` (kept for the agent). The server applies the
same rule before a message reaches the agent, and the agent applies it again.

On top of that the agent never builds a path string for the kernel. It holds a descriptor for `/work` and resolves every path with
`openat2` and `RESOLVE_BENEATH`, which the kernel refuses to take above it: a symbolic link a student makes **inside** `/work` is
followed, one that leads outside, an absolute one, and a loop are `BAD_PATH`. Where the kernel has no `openat2` (or a sandbox forbids
it) the agent follows no symbolic link at all, and says `slow_paths` in its hello.

### File requests

| Request | Fields | Reply |
|---------|--------|-------|
| `fs.list` | `path` | `entries`: `name`, `kind` (`file`, `dir`, `link`, `other`), `size`, `mtime` (ms). Sorted by name. Names the protocol cannot carry are left out. At most 5,000 names (`LIMIT`). |
| `fs.read` | `path` | `content`, `version`, `size`. Text files of at most 1 MiB: bigger is `TOO_LARGE`, not UTF-8 or with a NUL is `BINARY`. A symbolic link is followed. |
| `fs.write` | `path`, `content`, `base_version` | `version`. Replaces an existing regular file if `base_version` is its version, else `CONFLICT` with the current `version` in the reply. The content goes to a temporary file and is renamed over the file: a reader never sees half a write, a full disk leaves the old content, permissions are kept. A symbolic link is not written through (`BAD_PATH`). |
| `fs.create` | `path`, `content?` | `version`. A new regular file; `EXISTS` if there is anything at the path, `NOT_FOUND` if the folder is missing. |
| `fs.mkdir` | `path` | One folder in an existing one. |
| `fs.rename` | `path`, `to` | Never over something (`EXISTS`); a folder cannot move into itself (`BAD_PATH`); a symbolic link moves itself. |
| `fs.delete` | `path` | A file, a link, or a folder with everything in it. It never follows a link. Folders more than 128 levels deep are `LIMIT`. |

**Versions** are `sha256:` and the hex SHA-256 of the file's bytes.

### Terminal requests

| Request | Fields | Reply |
|---------|--------|-------|
| `pty.open` | `cols`, `rows`, `cmd?`, `cwd?` | `term`, `cols`, `rows`. Without `cmd` a login shell; with it, `sh -c cmd` (the Run and Check buttons), and the terminal ends with the command. At most **4** open terminals (`LIMIT`). The connection is attached after the reply, so the reply comes before the output. |
| `pty.attach` | `term` | `term`, `cols`, `rows`, `exited`, `exit_code`. Then the output the terminal still holds (the last 64 KiB), then live output. |
| `pty.list` | | `terminals`: `term`, `cols`, `rows`, `cmd`, `exited`, `exit_code`. |
| `pty.resize` | `term`, `cols`, `rows` | |
| `pty.close` | `term` | The terminal's process group gets SIGHUP and, after 2 seconds, SIGKILL. |
| keystrokes | binary frame to `term` | No reply. Dropped, with an `agent.error` event (`RATE`), if the program is not reading. |

Terminals belong to the agent, not to a connection: they go on when a connection ends, and a new connection lists and attaches them.
Output goes only to connections attached to the terminal, and **a connection that does not keep up holds the program back** (the
reading of the terminal waits until the connection has taken what it was given): a runaway program cannot make the agent's memory grow.

The programs run in `/work` (or `cwd`) with an environment of the agent's own: `PATH`, `HOME` (`/tmp/home`, made at start), `USER`,
`LOGNAME`, `SHELL`, `TERM=xterm-256color`, `LANG=C.UTF-8`, `TMPDIR`, `PWD`. Nothing from the agent's own environment is passed on.

### Other requests

`agent.ping` answers `ok` with `uptime_ms`.

## Events (no `id`)

| `type` | When | Fields |
|--------|------|--------|
| `agent.hello` | when a connection is made | `proto` (1), `agent_version`, `pid1`, `uptime_ms`, `work`, `slow_paths`, `connections`, `watches`, `open_terminals`, `oom_kills` |
| `agent.hb` | every 5 s (configurable) on every connection | the same, plus a rising `seq` |
| `fs.event` | a file or folder changed under `/work` | `kind`: `create`, `modify`, `delete`, `rename` (with `from`) or `overflow`; `path`; `is_dir` |
| `pty.exit` | a terminal's program ended | `term`, `exit_code` (128 + the signal when a signal ended it), `signal`, `oom_killed` |
| `agent.error` | a binary frame could not be used | `term`, `code`, `message` |

**`fs.event` is built from inotify** and keeps its promises small: repeated writes to one file are one `modify`; at most 2,000 events wait
for the next flush (every 100 ms), beyond which they become one `overflow`; a kernel queue that overflowed, and a tree with more than 4,096
folders, are `overflow` too. **After an `overflow` the client lists the folders again.** A write by the agent itself shows as a `modify` of
the file; its temporary file is never mentioned. A folder that appears is watched at once, and what it already holds is announced as created.

**`oom_killed`** is read from the container's own `/sys/fs/cgroup/memory.events`: the `oom_kill` counter before and after the terminal's
program (the runtime's own flag is never set by Podman, P0-8). **A paused container freezes the agent too**, so its heartbeat stops; the
runner must not read that as a lost agent (Phase 3).

## Errors

`NOT_FOUND`, `EXISTS`, `CONFLICT`, `TOO_LARGE`, `BINARY`, `QUOTA`, `LIMIT`, `BAD_PATH`, `UNAVAILABLE`, `RATE` are the plan's. The protocol adds
`BAD_REQUEST` (a malformed message), `DENIED` (the file's permissions refuse it) and `INTERNAL`. A full disk is `QUOTA` whether the kernel says
`ENOSPC` (an XFS project quota) or `EDQUOT` (P0-7).

## The connection

The socket is `/run/cbe/agent.sock`, a folder the runner mounts from the host, with mode `0666`: what guards it is the folder, which only
the runner and the container can reach. The agent keeps at most 8 connections and closes the extra ones at once. It answers `SIGTERM`
by closing its connections and terminals, removing its socket and exiting (as PID 1 it would otherwise ignore the signal and `podman stop`
would wait for the kill).

## Trying it

```bash
build/cbe-agent -socket /tmp/agent.sock -work /tmp/work -home /tmp/home &
cbe-agent-cli -socket /tmp/agent.sock ls
cbe-agent-cli -socket /tmp/agent.sock exec make          # output streams; the exit code is make's
cbe-agent-cli -socket /tmp/agent.sock shell              # a real terminal
cbe-agent-cli -socket /tmp/agent.sock raw '{"type":"fs.read","path":"../etc/passwd"}'
```
