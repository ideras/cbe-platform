# Course images

> **A class does not need a course image any more.** An exam can bring a base image of its own (`image/Containerfile` that starts `FROM` any image, and no `image:` in `workspace.md`), and the server finishes
> and checks it: `docs/workspace/images.md`, "A base image of its own". This folder is how the server's own **course images** (`cbe/cpp20`) are made, for exams that name one with `image:`; the C++ course as
> a package of its own is `testdata/workspace/lab-listas-own-base`.

A workspace exam runs in a container made from a **course image**: one image per course (decision D2), such as `cbe/cpp20` or
`cbe/python-nn`. The image holds the course's tools and the agent (`cmd/cbe-agent`), which is the container's first process. An exam
names its image in `workspace.md` (`image: cbe/cpp20`); the server only starts images on its approved list.

`cbe-portalctl` is the one program that builds, checks and installs these images (decision D-CB-9): a folder here holds only the
Containerfile, and the agent is added by the runner's last layer, as it is to every image.

| Image | Folder | What is in it |
|-------|--------|---------------|
| `cbe/cpp20` | [`cpp20/`](cpp20/Containerfile) | g++ (C++20), make, cmake, gdb, flex, bison, binutils, nano, less, procps |

## The contract every course image keeps

The runner, the quota helper and the agent depend on these. The runner's last layer makes 1, 2 and 4 true, `cbe-portalctl image check` checks
them all (the agent's self test), and `cbe-portalctl image test` tries them in a workspace the runner makes.

1. **The user is `student`, uid 1000, gid 1000** (decision P0-30). The quota helper creates each student's `/work` folder owned by the host
   user that container uid 1000 maps to, so an image with another uid makes `/work` read-only to the student. Ubuntu's base image already has
   a user `ubuntu` at 1000:1000: **rename it** (`usermod -l student -d /home/student -m ubuntu; groupmod -n student ubuntu`); a second user
   would get 1001 and break the contract. Images on another base check the same thing.
2. **The agent is the entrypoint**, `/usr/local/bin/cbe-agent`, and the image's `USER` is `1000:1000`. Nothing runs as root, not even to start.
   The Containerfile does not copy the agent: the runner's last layer adds it, with `USER`, `WORKDIR` and `ENTRYPOINT`, after the file's last
   line (`internal/runner/finalize.go`), and `image check` refuses a `COPY cbe-agent`.
3. **Three paths are the container's interface:** `/work` (the student's files; the runner mounts a quota-limited folder over it, owned by uid
   1000), `/run/cbe` (the agent's socket; the runner mounts a folder from the host over it) and `/tmp` (a small tmpfs, **no exec**). Everything
   else is read-only at run time. A program that needs to write elsewhere (a home folder, a cache) must be pointed into `/tmp` or `/work`; the
   agent already makes `/tmp/home` and uses it as `HOME`.
4. **No way up:** no `sudo`, no password for `root` or `student`, no setuid or setgid file (the image build strips them), no package manager
   the student could use, because the container has no network and no root.
5. **Reproducible:** the base image is pinned by digest and every package comes from a dated snapshot of the Ubuntu archive, so a rebuild next
   year has the same toolchain (decision D11). `cbe-portalctl image check images/<course> --reproducible` builds twice from nothing and
   compares the two images.
6. **Labelled** with the hash and version of the agent inside it (`io.cbe.agent.sha256` by the runner's last layer, `io.cbe.agent.version` by
   `cbe-portalctl`), the name it is installed as (`io.cbe.course`, by `image install`) and the snapshot date (by the Containerfile).

The agent needs from the kernel what any container has: `/dev/ptmx` and a `/dev/pts` (for the terminals), inotify, and `openat2` (the default
seccomp profile of Podman allows it). Without `openat2` it still works, and follows no symbolic link.

## Adding a course image

1. **Copy `cpp20/`** to `images/<course>/` (the name is the course: `python-nn`). The folder holds the `Containerfile` and anything it
   `COPY`s (`ca.crt`, the host's certificates, is lent to the build by `cbe-portalctl`, as the server lends it).
2. **Change what is course-specific:** the packages in step 1 of the Containerfile and the labels. Keep the `FROM` pinned by digest (find it
   with `podman pull ubuntu:24.04 && podman image inspect --format '{{.Digest}}' ubuntu:24.04`) and keep the dated snapshot. For tools that do
   not come from the archive (pip packages, models), download them in the build with the version and checksum written in the file.
3. **Keep step 2 as it is:** the user, the lock on root, the removal of setuid files. Do not copy the agent, and do not set `USER`, `WORKDIR` or
   `ENTRYPOINT`: the runner's last layer does.
4. **Write its checks**, `images/<course>/checks`: the commands that show the course's tools work (cpp20's compile and run C++20, build with make
   and cmake, run gdb, flex and bison). A check starts with a line `# check: what it checks`; its lines run in a terminal in `/work` with
   `sh -e`, and it passes when they end with status 0.
5. **Check it** on any computer with Podman: `cbe-portalctl image check images/<course>`, and once `--reproducible`.
6. **Install it** on the server, which builds it in the runner's storage, approves it and tries it: `sudo cbe-portalctl image install
   cbe/<course> images/<course> --restart --test` (`docs/deployment-linux.md`, "An image for a whole class").

## Building and checking

```bash
cbe-portalctl image check images/cpp20                   # build it, finish it, check it; nothing is kept
cbe-portalctl image check images/cpp20 --reproducible    # build it twice from nothing; say whether the two images are identical
sudo cbe-portalctl image install cbe/cpp20 images/cpp20  # on the server: build it in the runner's storage and approve it as cbe/cpp20
sudo cbe-portalctl image test cbe/cpp20 images/cpp20     # on the server: try it in a student's workspace, with its checks
```

The agent put in the image is the one named with `--agent`; else the installed server's (`agent_binary` in its `runner.json`); else the
`cbe-agent` next to `cbe-portalctl`, so `make agent portalctl` and `build/cbe-portalctl image check images/cpp20` use the agent of this tree.
It is checked before anything is built (a static Linux program for this machine, and `cbe-agent` itself). Every file and the image itself are
stamped with `SOURCE_DATE_EPOCH` (default 2000-01-01). The base image has no CA certificates, so for the one step that downloads packages over
HTTPS the build lends the host's bundle as a bind mount that leaves nothing in the image.

`image test` asks the runner for a workspace made from the installed image, as the exam server does for a student, and drives its agent
through the runner: the agent (PID 1, uid 1000, its heartbeat), the student (no network, a read-only root, no capabilities, no sudo, no
setuid file), files, terminals, the course's own `checks`, the limits (a full `/work` is `QUOTA`, the memory limit kills the program and not
the workspace, a fork storm stops at the process limit) and a stop. The workspace is removed at the end, whatever happened.

## How the runner starts an image (Phase 3, and what `image test` gets)

```text
podman run -d --name cbe-<exam>-<session> \
  --network none --read-only --tmpfs /tmp:rw,size=64m,mode=1777 \
  --cap-drop all --security-opt no-new-privileges \
  --pids-limit 256 --memory 1g --memory-swap 1g --cpus 1 \
  -v <quota-limited folder>:/work -v <socket folder>:/run/cbe  cbe/cpp20:latest
```

The limits come from the exam's `resources`, within the server's maximums. The agent is PID 1 and reaps every process whose parent has gone.
`podman stop` ends it at once: it answers SIGTERM by closing its terminals and removing its socket.

## Updating an image

Change the Containerfile (a new snapshot date to take newer packages, a new digest to take a newer base), check it with `--reproducible`,
and install it again on the server (`image install ... --replace --test`). An exam that is running keeps the container it has; a new exam gets the
new image.
