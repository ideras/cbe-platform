# Deploying workspace exams

How the pieces of a workspace exam are set up on the container host, which is also the exam server's computer. The plan is
`.agents/WORKSPACE-PLAN.md`; the reasons for each choice are in `DECISIONS.md` (P3-1 to P3-21).

## What runs, and as whom

```
 student's application ──HTTPS/WebSocket──▶  cbe-portal-server     (account cbe-server)
                                              │  the exam server: login, relay, snapshots, supervisor
                                              │  HTTP + secret over  /data/cbe-platform/ipc/runner.sock
                                              ▼
                                            cbe-runner              (account cbe-runner, a user service)
                                              │  the only holder of the container runtime's socket
                                              │  ├── rootless Podman  (/run/user/<uid>/podman/podman.sock)
                                              │  ├── cbe-quota-helperd  (root; /run/cbe-quota/helper.sock; serves only cbe-runner)
                                              │  └── each container's agent  (/data/cbe-platform/run/<session>/agent.sock)
                                              ▼
                                            one container per student: cbe-agent as PID 1, a terminal, /work
```

| Process | Account | Can do | Cannot do |
|---|---|---|---|
| `cbe-portal-server` | `cbe-server` (no login) | Talk to the runner's socket; write its own folder. | Reach the runtime's socket, the runner's folders, the students' folders. |
| `cbe-runner` | `cbe-runner` (lingering user service) | Use rootless Podman; make the agents' folders; ask the quota helper. | Anything as root. |
| `cbe-quota-helperd` | root (system service, sandboxed) | Make and remove `/data/cbe-platform/work/<session>` with an XFS project quota. | Serve anyone but `cbe-runner`. |
| the student's program | uid 1000 in a user namespace | Its own `/work`. | The network, the host, other containers, a larger disk or process count than its limits. |

## Folders

| Folder | Owner and mode | What is in it |
|---|---|---|
| `/data/cbe-platform/work` | root; the XFS volume (`prjquota`) | One quota folder per session, mode 0700, owned by the container's mapped student. Made by the quota helper. |
| `/data/cbe-platform/ipc` | `cbe-runner:cbe-ipc`, 2710 | `runner.sock` and `runner.secret`. A member of `cbe-ipc` reaches them by name and cannot list the folder. |
| `/data/cbe-platform/run` | `cbe-runner`, 0700 | One folder per session (mode 0777 inside the private parent) for the agent's socket. |
| `/data/cbe-platform/runner` | `cbe-runner`, 0700 | What the runner knows about each container, as a small file per session. |
| `/data/cbe-platform/server` | `cbe-server`, 0700 | The server's database, screenshots, admin token and `workspace/` (bundles, blobs, snapshots). |
| `/etc/cbe-platform/runner.json` | root, 0644 | The runner's settings (below). Kept if it already exists. |

## Installing

On the container host, after `docs/workspace/spike/setup-host.sh` (the `cbe-runner` user, rootless Podman, the XFS volume):

```sh
go build -o bin/cbe-runner ./cmd/cbe-runner
go build -o bin/cbe-quota-helper ./cmd/cbe-quota-helper
sudo deploy/workspace/install.sh bin       # accounts, folders, both services; safe to run again
PODMAN='sudo -u cbe-runner env XDG_RUNTIME_DIR=/run/user/1001 podman' images/build.sh cpp20   # the course image into the runner's storage
```

The exam server is not started by `install.sh`: it may already be running somewhere else (the owner's own, on `:8080`). To run it as `cbe-server`:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=<release>" -o bin/cbe-portal-server ./cmd/server
sudo install -m 0755 bin/cbe-portal-server /usr/local/bin/cbe-portal-server
sudo -u cbe-server sh -c 'head -c 40 /dev/urandom | base64 -w0 | tr -d "=+/" > /data/cbe-platform/server/admin-token; chmod 600 /data/cbe-platform/server/admin-token'
sudo install -m 0644 deploy/workspace/cbe-server.service /etc/systemd/system/    # edit -bind first
sudo systemctl daemon-reload && sudo systemctl enable --now cbe-server
```

The unit runs the server with `NoNewPrivileges`, a read-only system, only its own folder writable, the IPC folder read-only and no capabilities; it was checked against the real runner. **The server's flags for workspace exams** (they are all off unless `-runner-socket` is given):

| Flag | Default | Meaning |
|---|---|---|
| `-runner-socket`, `-runner-secret-file` | none | The runner's socket and the file with the shared secret. Both or neither. |
| `-workspace-dir` | `data/workspace` | Starter and grading bundles, snapshot blobs and manifests. |
| `-workspace-retention` | 48h | How long a frozen workspace's container is kept after its exam; its snapshots stay. |
| `-workspace-snapshot-every` | 60s | The periodic snapshot interval for exams that set none (10s at least). |
| `-workspace-max-cpu`, `-workspace-max-memory-mb`, `-workspace-max-disk-mb` | 2, 2048, 500 | The most an exam may ask for each student. The runner has its own maximums too. |

## The runner's settings (`/etc/cbe-platform/runner.json`)

| Key | Example | Meaning |
|---|---|---|
| `socket`, `socket_group`, `secret_file` | `/data/cbe-platform/ipc/runner.sock`, `cbe-ipc`, `…/runner.secret` | Where the API listens, who may connect, and the shared secret (made on first start). |
| `runtime_socket` | `unix:///run/user/1001/podman/podman.sock` | The container runtime. Only the runner opens it. |
| `quota_socket`, `work_root`, `run_root`, `state_root` | see the file | The quota helper and the three folders above. |
| `images` | `{"cbe/cpp20": "localhost/cbe/cpp20:latest"}` | **The approved images**: the name an exam uses, and the runtime's reference. An exam that names any other image is refused at import and again at `container.create`. |
| `max`, `default` | cpu 2, memory 2048, disk 500; 1, 1024, 100 | The largest limits an exam may ask for, and what it gets for a limit it leaves out. |
| `pids`, `inodes`, `tmp_mb`, `stop_seconds` | 256, 20000, 64, 5 | Fixed for every container. |

## The runner's API

Each request carries `Authorization: Bearer <secret>`. Bodies are strict JSON; a field that is not listed is a 400.

| Request | Operation | Answers |
|---|---|---|
| `GET /v1/ping` | is the runner there | 200 |
| `POST /v1/containers` `{session, exam, image, resources}` | `container.create` | 200 status; 400 bad request or a limit over the maximum; 403 image not approved; 409 the session has a container for another exam or image |
| `PUT /v1/containers/{session}/seed` (a tar) | `container.seed` (P3-4) | 200; 409 when seeded already or started |
| `POST /v1/containers/{session}/{start,pause,unpause,stop,remove}` | the lifecycle | 200; 404; 409 |
| `GET /v1/containers/{session}` | `container.status` (state, agent heartbeat, limits) | 200; a session with nothing is `state: missing` |
| `GET /v1/containers/{session}/stats`, `GET /v1/exams/{exam}/stats` | `container.stats` | 200; 404 |
| `GET /v1/containers/{session}/archive?path=/work/.` | `container.archive` (a tar stream; only paths under `/work`) | 200; 400; 404 |
| `GET /v1/containers/{session}/agent` with `Upgrade: cbe-agent/1` | `agent.connect` (the connection becomes the agent's socket) | 101; 423 when the container is paused; 409 when it is not running |
| `GET /v1/images?name=` | `image.inspect` | 200 `{approved, present}` |
| `POST /v1/grading`, `POST /v1/images/build` | Phases 6 and 7 | 501 |

Everything else is 404 or 405. A 401 means the secret was wrong.

## Restarts

- **The exam server restarts:** its sessions are in its database, the containers keep running. At the next login, connection or supervisor round it finds each workspace `ready` and asks the runner for it; nothing is created or restarted.
- **The runner restarts:** the containers are not its children (`KillMode=process`) and keep running. It reads its files, asks the runtime what exists and connects to each running agent again. The server's connections to the agents drop and the relay reconnects.
- **The machine reboots:** `cbe-runner` has lingering, so its service and Podman's socket start without a login. The containers do not survive a boot, but they stay in Podman's storage as stopped containers and `/work` stays on the volume. Once the exam server is running again (its unit starts it after the quota helper), its supervisor finds each session's container ended and brings it back like an agent that ended: it snapshots the files (`agent_loss`), starts the container on the same `/work` and counts the loss. The end-to-end test shows this by restarting the runtime's user manager, which takes every container down as a boot does (phase C); **a real reboot during an exam is still to be rehearsed in Phase 8.** One difference is known: a container that was *paused* (a frozen workspace) when that restart happened is left in a state Podman cannot remove until it refreshes; a real boot clears Podman's runtime state, which is what refreshes it, but that has not been seen yet.
- **A student's agent ends (they typed `kill -TERM 1`):** the container exits, the supervisor snapshots and starts it again on the same `/work` within a few seconds.

## Looking at it

```sh
journalctl -u cbe-quota-helper                                   # the helper
sudo -u cbe-runner env XDG_RUNTIME_DIR=/run/user/1001 journalctl --user -u cbe-runner     # the runner
sudo -u cbe-runner env XDG_RUNTIME_DIR=/run/user/1001 podman ps  # the containers (names cbe-<exam>-<session>)
```

A session's workspace as the proctor would see it: `GET /admin/workspace/session/{id}` (state, restarts of the agent, the snapshots) and `GET /admin/workspace/snapshot/{id}/tar` for the files of one snapshot, with the admin token.

## The tests that need this host

| Script | Does |
|---|---|
| `images/test.sh cpp20` | 31 checks of the image and its agent, from outside. |
| `deploy/workspace/test-runner.sh` | The runner and the quota helper against Podman, as `cbe-server`; restarts the runner and checks it finds its containers. |
| `deploy/workspace/test-server.sh` | The server's binary as `cbe-server` against all of it: login, files, terminals, Run and Check, abuse, an agent that ends, a restarted server and a restarted runner, the deadline. About four minutes; removes everything it made. |

All three use `sudo` without a password.
