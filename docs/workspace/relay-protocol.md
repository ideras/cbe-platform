# The student's WebSocket (what Phase 4's IDE speaks)

`GET /workspace/ws` on the exam server, upgraded to a WebSocket, with the session token as for every other student route
(`Authorization: Bearer <token>`). The student's application (the Go client, P0-24) owns the connection. `GET /workspace` gives the
exam's text and the workspace's state first (below). Built in Phase 3; `internal/workspace/relay.go` is the source.

The relay is a gate, not a pipe: it decodes every message from the student with the agent's own strict decoder
(`internal/agentproto`, described in `agent-protocol.md`), adds its own rules, and writes to the agent only what it decoded. What the
agent says is decoded and written again. Neither side gets the other's bytes.

## Messages

Text frames are one JSON object each; binary frames are terminal bytes: **4 bytes of terminal number (big endian), then the bytes**,
in both directions (keystrokes to the program, output from it). A request has `id` and `type`; its reply repeats both and has `ok`,
and `code` and `message` when `ok` is false. Messages with no `id` are events.

### From the student

| Type | Fields | Notes |
|---|---|---|
| `fs.list`, `fs.read`, `fs.write`, `fs.create`, `fs.mkdir`, `fs.rename`, `fs.delete` | as in `agent-protocol.md` | Paths are from `/work` down, no `..`. `fs.write` needs the `base_version` of what was read, or the reply is `CONFLICT` with the current version. |
| `pty.open` | `cols`, `rows`, optionally `cwd` | A shell. `DENIED` when the exam has no terminal. **A `cmd` is `DENIED`**: programs are started with `run.start`. The reply has `term`. |
| `pty.attach`, `pty.list`, `pty.resize`, `pty.close` | `term`, `cols`, `rows` | A new connection lists the student's terminals and attaches them: the last 64 KiB of output comes first. At most 4 terminals; a finished one makes room for a new one (P3-9). |
| `run.start` | `which` (`run` or `check`), optionally `cols`, `rows` | Runs the command the exam names, in a terminal, and replies with `term`. `NOT_FOUND` if the exam has no such command. The student never sends the command. |
| `todo.set` | `item` (a TODO `id` of the exam), `done` | Self-reported progress. `NOT_FOUND` for an `id` the exam does not have. |
| `submit` | none | Takes a snapshot of `/work` now; the reply has `snapshot`. `UNAVAILABLE` (try again) if it could not be taken. |
| `agent.ping` | none | Answered by the agent: `ok`. |

Anything else (an unknown type, an unknown field, a message that is not an object, one of the agent's own messages such as
`agent.hello`) is a reply or a `ws.error` with `BAD_REQUEST`, and the connection goes on.

### From the server

| Type | When | Fields |
|---|---|---|
| `ws.status` | on connecting, when the state changes, and every 5 s | `state` (`preparing`, `running`, `restarting`, `frozen`, `ended`, `failed`), `last_snapshot_at`, `deadline_at`, `seconds_left`, `agent_restarts`, `terminal` (whether the exam has one), `failure` |
| `ws.agent_restarted` | the container was started again after the agent was lost | none; a `ws.status` follows. The terminals are gone and the files are not: list the terminals again. |
| replies | to each request | as above |
| `fs.event` | files changed in `/work` (by the student's programs too) | `kind` (`create`, `modify`, `delete`, `rename`, `overflow`), `path`, `from` |
| `pty.exit` | a program ended | `term`, `exit_code`, `signal`, `oom_killed` |
| `agent.error` | a terminal problem not tied to a request | `term`, `code` (`RATE` when keystrokes were dropped), `message` |
| `ws.error` | a message that could not even be read as a request | `code`, `message` |

When the connection closes, the close reason says why: `the exam is over` (the deadline and grace have passed; the workspace is
frozen), `replaced by a newer connection of the same student` (one connection a student), or `the connection stopped answering`.

### Codes

The agent's (`agent-protocol.md`): `BAD_PATH`, `NOT_FOUND`, `EXISTS`, `CONFLICT`, `QUOTA`, `TOO_LARGE`, `BINARY`, `LIMIT`, `RATE`,
`UNAVAILABLE`, `BAD_REQUEST`, `DENIED`, `INTERNAL`. `UNAVAILABLE` also means the workspace is starting or restarting: the relay
waits up to 8 s for the agent before saying so, and the application should try again.

## Limits of one connection (P3-12)

| What | Limit |
|---|---|
| Requests | 100 a second, burst 200; 4 MiB of them a second, burst 8 MiB |
| Keystrokes | 256 KiB a second, burst 512 KiB; over it they are dropped and the student gets `RATE` |
| Terminal output | 2 MiB a second, burst 1 MiB; over it the output is held back, not dropped |
| One message | 2 MiB |
| Pings | the server pings every 20 s and closes a connection that does not answer within 10 s |

## `GET /workspace`

What the application needs before and beside the socket (never the run and check commands, the image, the limits, the PIN or
anything of the grading bundle):

```json
{ "kind": "workspace", "exam_id": "…", "session_id": "…", "title": "…",
  "terminal": true, "can_run": true, "can_check": true,
  "content": { "instructions": "<p>…</p>", "todos": [{ "id": "t1", "text_html": "…", "points": 10 }] },
  "done": { "t1": true },
  "status": { "state": "running", "deadline_at": "…", "seconds_left": 6000, "agent_restarts": 0, "terminal": true } }
```

`404` when the session is not a workspace session. When the exam is over the same answer says so (`status.state` is `ended`, or `frozen` once the final snapshot is taken); the socket closes with `the exam is over`.
