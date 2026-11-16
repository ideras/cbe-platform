# Course images

A workspace exam runs in a container made from a **course image**: one image per course (decision D2), such as `cbe/cpp20` or
`cbe/python-nn`. The image holds the course's tools and the agent (`cmd/cbe-agent`), which is the container's first process. An exam
names its image in `workspace.md` (`image: cbe/cpp20`); the server only starts images on its approved list.

| Image | Folder | What is in it |
|-------|--------|---------------|
| `cbe/cpp20` | [`cpp20/`](cpp20/Containerfile) | g++ (C++20), make, cmake, gdb, flex, bison, binutils, nano, less, procps |

## The contract every course image keeps

The runner, the quota helper and the agent depend on these. `images/test.sh` checks them.

1. **The user is `student`, uid 1000, gid 1000** (decision P0-30). The quota helper creates each student's `/work` folder owned by the host
   user that container uid 1000 maps to, so an image with another uid makes `/work` read-only to the student. Ubuntu's base image already has
   a user `ubuntu` at 1000:1000: **rename it** (`usermod -l student -d /home/student -m ubuntu; groupmod -n student ubuntu`); a second user
   would get 1001 and break the contract. Images on another base check the same thing.
2. **The agent is the entrypoint**, `/usr/local/bin/cbe-agent`, and the image's `USER` is `1000:1000`. Nothing runs as root, not even to start.
3. **Three paths are the container's interface:** `/work` (the student's files; the runner mounts a quota-limited folder over it, owned by uid
   1000), `/run/cbe` (the agent's socket; the runner mounts a folder from the host over it) and `/tmp` (a small tmpfs, **no exec**). Everything
   else is read-only at run time. A program that needs to write elsewhere (a home folder, a cache) must be pointed into `/tmp` or `/work`; the
   agent already makes `/tmp/home` and uses it as `HOME`.
4. **No way up:** no `sudo`, no password for `root` or `student`, no setuid or setgid file (the image build strips them), no package manager
   the student could use, because the container has no network and no root.
5. **Reproducible:** the base image is pinned by digest and every package comes from a dated snapshot of the Ubuntu archive, so a rebuild next
   year has the same toolchain (decision D11). `images/build.sh COURSE --check` builds twice from nothing and compares the two images.
6. **Labelled** with the hash and version of the agent inside it (`io.cbe.agent.sha256`, `io.cbe.agent.version`) and the snapshot date.

The agent needs from the kernel what any container has: `/dev/ptmx` and a `/dev/pts` (for the terminals), inotify, and `openat2` (the default
seccomp profile of Podman allows it). Without `openat2` it still works, and follows no symbolic link.

## Adding a course image

1. **Copy `cpp20/`** to `images/<course>/` (the name is the course: `python-nn`). The folder holds the `Containerfile` and anything it
   `COPY`s (the agent and `ca.crt` are added by `build.sh`).
2. **Change what is course-specific:** the packages in step 1 of the Containerfile and the labels. Keep the `FROM` pinned by digest (find it
   with `podman pull ubuntu:24.04 && podman image inspect --format '{{.Digest}}' ubuntu:24.04`) and keep the dated snapshot. For tools that do
   not come from the archive (pip packages, models), download them in the build with the version and checksum written in the file.
3. **Keep steps 2 and the end of the Containerfile as they are:** the user, the lock on root, the removal of setuid files, `COPY cbe-agent`,
   `USER 1000:1000`, `WORKDIR /work`, `ENTRYPOINT`.
4. **Build it:** `images/build.sh <course>` (on the container host: `PODMAN='sudo -u cbe-runner env XDG_RUNTIME_DIR=/run/user/1001 podman'
   images/build.sh <course>`, because the images belong to the `cbe-runner` user).
5. **Check it:** `images/build.sh <course> --check` (reproducible), then `images/test.sh <course>` (the contract, with the hardening the runner uses).
   `test.sh` also runs the `cpp20` checks of the toolchain; for a new course, add what its exams need to the "terminals" section.
6. **Approve it** on the server (Phase 3): the runner starts only the images on its list.

## Building and checking

```bash
images/build.sh cpp20              # build cbe/cpp20:latest, with the agent built from this tree
images/build.sh cpp20 --check      # build it twice from nothing; say whether the two images are identical
images/test.sh cpp20               # start it as the runner will, and test it from outside, through the agent's socket
```

`build.sh` builds the agent here (static, no cgo, `-trimpath`, no build ID) and hands the file to the build, so the image holds exactly the
binary the tests ran. It stamps every file and the image itself with `SOURCE_DATE_EPOCH` (default 2000-01-01). The base image has no CA
certificates, so for the one step that downloads packages over HTTPS the build lends the host's bundle as a bind mount that leaves nothing
in the image.

## How the runner starts an image (Phase 3, and what `test.sh` already does)

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

Change the Containerfile (a new snapshot date to take newer packages, a new digest to take a newer base), rebuild, run `--check` and
`test.sh`, and approve the new build on the server. An exam that is running keeps the container it has; a new exam gets the new image.
