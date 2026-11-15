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
| `-workspace-grade-parallel` | 2 | How many grading runs the server has going at once (1 to 32). The runner bounds them again (`grade_parallel`); the lower rules. |
| `-workspace-grade-timeout` | 5m | How long a grading run may take for an exam that sets no `grade_timeout` (10s to 30m). |

## The runner's settings (`/etc/cbe-platform/runner.json`)

| Key | Example | Meaning |
|---|---|---|
| `socket`, `socket_group`, `secret_file` | `/data/cbe-platform/ipc/runner.sock`, `cbe-ipc`, `…/runner.secret` | Where the API listens, who may connect, and the shared secret (made on first start). |
| `runtime_socket` | `unix:///run/user/1001/podman/podman.sock` | The container runtime. Only the runner opens it. |
| `quota_socket`, `work_root`, `run_root`, `state_root` | see the file | The quota helper and the three folders above. |
| `images` | `{"cbe/cpp20": "localhost/cbe/cpp20:latest"}` | **The approved images**: the name an exam uses, and the runtime's reference. An exam that names any other image is refused at import and again at `container.create`. |
| `max`, `default` | cpu 2, memory 2048, disk 500; 1, 1024, 100 | The largest limits an exam may ask for, and what it gets for a limit it leaves out. |
| `pids`, `inodes`, `tmp_mb`, `stop_seconds` | 256, 20000, 64, 5 | Fixed for every container. |
| `grade_root` | `/data/cbe-platform/grading` | Where a grading run's copy of the hidden bundle is for as long as the run lasts. Private to the runner (`install.sh` makes it, mode 0700). |
| `build_max_seconds`, `build_memory_mb` | 900, 2048 | **Phase 7** (`images.md`): the longest an image build may last (a request may ask for less, from 10 s), and the memory the steps of a build may use together (enforced: a step over it is killed). One build runs at a time. |
| `grade_parallel`, `grade_max_seconds`, `grade_output_bytes` | 2, 1800, 262144 | How many gradings run at once (1 to 32), the longest a run may be asked to last, and the most of each of a run's two output streams that is kept. |

**Upgrading a host that has Phase 6 (to Phase 7):** only `cbe-runner` changes (the quota helper, the agent and the unit file do not). Replace `/usr/local/bin/cbe-runner`, restart the runner's unit; `runner.json` needs nothing (the two `build_*` keys have defaults). The new server binary adds a migration (023) on its first start. Then **build the image of every exam that has one** from the proctor application before publishing it.

**Upgrading a host that has Phases 3 to 5:** re-run `install.sh` (it makes the grading folder and leaves the existing `runner.json` alone: the new keys have these defaults) with the new `cbe-runner` binary, and restart the runner's unit (`systemctl --user restart cbe-runner` as `cbe-runner`): its containers keep running. Then give the exam server the new binary.

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
| `POST /v1/grading` (`multipart/form-data`: `request`, `submission`, `grading`) | `grading.run` (Phase 6, `grading.md`) | 200 the run's result, whatever way it ended; 400 a request or archive that is not what it takes, or a bundle with no runnable `grade`; 403 image not approved |
| `POST /v1/images/build` (`multipart/form-data`: `request`, `context`) | `image.build` (Phase 7, `images.md`): the exam's build context (a tar with a `Containerfile`) is built on the approved course image | 200 **with a stream** (`application/x-ndjson`, one JSON object a line): `{"log": "…"}` for what the build printed (a few times a second, so the exam server can show it while the build goes), then `{"result": {…}}` as the last line, whatever way the build ended (`ok`, `failure`, `message`, the log, the base's digest, the tag, `cached`); a failure of the runner's own after the stream began is `{"error": "…", "status": 500}`; **400** a request or context the runner cannot take (a bad exam ID, no `Containerfile`, a link out of the context, over 22 MiB) and **403** image not approved, as an HTTP status before the stream begins. Closing the connection ends the build. |
| `GET /v1/images?name=cbe-exam/<exam>:<build>` | `image.inspect` of an exam's built image: `approved` when the image was built for that exam and that hash, `present` when the runtime has it | 200 |
| `container.create`, `grading.run` | take an optional `build` (16 hex) beside `image` | with it the runner uses the exam's built image and refuses another exam's (403), or one that is not there (404) |

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
| `deploy/workspace/test-server.sh --manual [address]` | Not a test: a throwaway exam server for a person to try the applications against (the focus-loss script). It listens on `127.0.0.1:8090` unless given an address (`0.0.0.0:8090`, or the host's address on a virtual machine's network), has its own database, a class of three students (accounts `0001` to `0003`) and the example package published (PIN and admin token are printed), and runs until Ctrl+C, when it removes what the students made. It never touches the server on `:8080`. |

| `deploy/workspace/test-grading.sh` | Phase 6 (`grading.md`): grading with nothing replaced. It starts a **private** runner (its own socket, secret and folders; the same runtime and quota helper), so the installed runner and anyone's containers are not touched and no service is restarted. Grades the example package's three students in real containers, holds misbehaving grade programs (no network, a read-only `/grading` and root, a run that does not end, runs out of memory or floods) and runs forty at once, two at a time. About two minutes; reports whatever it finds left (nothing, so far). |

| `deploy/workspace/test-image.sh` | Phase 7 (`images.md`): an exam's own image with nothing replaced, against a **private** runner as `test-grading.sh` does. Real image builds from the course image and real containers: publishing refused until built; the log records the course image's digest; cache hit and rebuild; the exam's student compiles against a library the course image does not have, with no network, and is graded there; another exam has no such library and cannot be given the image; a `FROM` that is not the exam's image, and a changed user, entrypoint, command, working folder or agent, all fail with their reasons; a build is stopped at its time and at its memory limit and builds run one at a time; reports what it left (nothing). `CBE_E2E_APT=1` also builds a library from the Ubuntu snapshot over HTTPS (needs the network; about six minutes in all instead of two). It removes the images it built. |

All of them use `sudo` without a password.
