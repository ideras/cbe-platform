# An exam's image

An exam's students and its hidden tests run in a container made from an **image**. There are two ways to say which, and the second is the way to have the tools of one class
without the tools of another (Python and its libraries for a neural-networks class, PostgreSQL and `psql` for a databases class, a C++ toolchain for a compilers class):

1. **A base image of its own** (decision D-CB-1; the way to write a new class's image). The package has `image/Containerfile` and **no `image:`** in `workspace.md`. The Containerfile starts
   `FROM` **any image** (`python:3.12-slim`, `ubuntu:24.04`, `alpine:3.20`, `ghcr.io/your-school/lab-base:2026.1`...) and adds what the class needs. **The server finishes it**: after the author's last
   line it adds the student account, the agent and the rest of the contract, and checks the result. The host needs nothing prepared: no image to build, no entry in a list.
2. **A course image of the server** (`image: cbe/cpp20`; Phase 7). A course image is built by whoever runs the server (`images/README.md`) and approved in `runner.json`; the package may add to it
   with `image/` (a Containerfile that starts `FROM` that course image). Everything below "A course image" is about this way.

The compiler (`examc`) and the server say which one a package is: no `image:` and an `image/Containerfile` is a base of its own; no `image:` and no `image/` is an error.

## A base image of its own

```text
lab-python/
  workspace.md          (no image: line)
  project/
  grading/
  image/
    Containerfile
    data/train.csv      anything the Containerfile copies
```

```dockerfile
# image/Containerfile of a neural-networks class: Python and its libraries, and no C++ compiler.
FROM docker.io/library/python:3.12-slim@sha256:…
RUN pip install --no-cache-dir numpy==2.1.3 torch==2.5.1 --index-url https://download.pytorch.org/whl/cpu
COPY data/ /srv/data/
```

`testdata/workspace/lab-listas-own-base` is a complete example (the C++ course: a toolchain from the Ubuntu archive snapshot, built for real in `deploy/workspace/test-ownbase.sh`).

**Write only what the class needs.** The rules of the Containerfile are the ones of every exam image (`shared/containerfile`: below, in the table): `RUN --network` may be `default` or `none`,
no `VOLUME`, no `ONBUILD`, no variable in a `FROM`, `COPY --from` only from an earlier stage. What is different is the `FROM`: **any image reference**
(`registry/name:tag` or `name@sha256:…`, lower case), or an earlier stage of the file. A `FROM` with no digest is a **warning** (the tag can name another image tomorrow, and a rebuild would not
be the image the exam was tried with); it can be silenced with `image_warnings: off`, and pinned with `podman image inspect --format '{{.Digest}}' python:3.12-slim`.

### What the server adds

After the author's last stage the runner builds **its own layer** on top, which the author cannot reorder or add to (`internal/runner/finalize.go`, `finalize/cbe-finalize.sh`):

- **The student account**: uid 1000, gid 1000, called `student`, home `/home/student`. An account or group that has 1000 in the base (`ubuntu` in the Ubuntu images, `node` in the Node ones) is taken out.
- **No way up**: root's password locked, no `sudo`, and every setuid and setgid bit removed from every file.
- **The container's three places**: `/work` (the student's files are mounted there), `/run/cbe` (the agent's socket) and `/tmp` (a small tmpfs, no exec).
- **The agent** at `/usr/local/bin/cbe-agent` (the file `agent_binary` names in `runner.json`, the one the installer puts in `/opt/cbe-portal/bin`), its hash as a label, `USER 1000:1000`,
  `WORKDIR /work`, `HEALTHCHECK NONE` and the agent as the entrypoint.

It needs a Linux base that has `/bin/sh` and `/etc/passwd` (`FROM scratch` and distroless images cannot be finished; the message says so). The terminals use `/bin/bash` when the image has it
and `/bin/sh` when it does not (Alpine). The agent is a static program: nothing of its own is needed in the base.

### Checking it before the server does

`cbe-portalctl image check lab-python/` (the package folder, its `image/` folder or the Containerfile) does all of the following on **your computer** with its Podman, with the same code the runner uses:
the rules of the Containerfile, your build, the finishing layer, the image's configuration and the agent's self test, and says `OK` or `NOT ACCEPTED: why`. The agent comes from the server's installation
(`/opt/cbe-portal/bin/cbe-agent`) or from `--agent`, and the server's `custom_base` and `base_registries` are applied when `/opt/cbe-portal/etc/runner.json` is there (`--runner-config`). `--keep` leaves the image for
a look (`podman run --rm -it --entrypoint /bin/sh …`). The server does all of it again when the proctor builds the exam's image.

### What is checked, and what is refused

Before the image is kept, the runner starts it **as a student's container is started** (no network, a read-only root, no capabilities, no new privileges, the student user) and runs
`cbe-agent -selftest`, which looks at the container from inside. Its log is in the build's log:

| Check | Fails when |
|---|---|
| `user`, `account` | the agent does not run as 1000:1000, or `/etc/passwd` does not name uid 1000 `student` |
| `shell`, `work`, `tmp` | there is no shell, no `/work` folder in the image, or `/tmp` cannot be written |
| `root-read-only`, `capabilities`, `no-new-privileges`, `network` | the container is not run as a student's is (a fault of the runner's settings, not of the image) |
| `no-sudo`, `no-setuid-files` | a `sudo` or a file with a setuid or setgid bit is in the image |

and before that it looks at the image's own configuration: the user, the entrypoint and the working folder are the runner's, and **the image declares no volume**. A base that declares one
(the `postgres`, `mysql` and `redis` images do) is **refused**: a container made from it would get a writable folder outside the student's, with no quota. For a databases class start from a plain
`ubuntu` and install the server from its packages (`apt-get install postgresql`), and start it from the exam's Run command or by hand: **the agent starts nothing but terminals**.

### What the administrator decides (`runner.json`)

- `custom_base` (default `true`): `false` turns bases of an exam's own off; such an exam's build fails with the failure *policy*, and only course images can be built on.
- `base_registries` (default none: **any registry**): `["docker.io", "ghcr.io"]` restricts the `FROM`s of every exam to those registries. A build with another is refused with the failure *policy* and the list.
- `agent_binary`: the static agent the runner puts in every image it finishes. The installer sets it. An image is named by the exam and a hash of (its files, this agent, the finishing),
  so **a new agent makes new images** (a build that finds the same hash builds nothing).

Everything else is as for any exam image: built from the proctor's **Image** button before the exam is published, cached, one at a time, with the runner's `build_max_seconds` and
`build_memory_mb`, with the network of the server while it builds and **none** in the containers made from the result. Images are kept after an exam is over (`podman rmi` removes them),
and **a base is pulled into the runner's storage** (a Python image with its libraries is several GB: the runner's home, or `CONTAINER_STORAGE_DIR`, needs the room).

---

# A course image

A workspace exam may run in a container made from a **course image** (`cbe/cpp20`): one image per course, built and approved by whoever runs the server
(`images/README.md`). When an exam needs something the course image does not have (a library, a tool, a configuration file), its package can hold
`image/`: a `Containerfile` that starts from the course image, and the files it copies. The server builds it **before the exam is published**, as an action
of the proctor, checks that it is still the course image's contract, and gives the exam's students and its hidden tests that image and no other exam's.

## Writing it

```text
lab-image/
  workspace.md
  project/
  grading/
  image/
    Containerfile        the file the build reads (this name, not Dockerfile)
    lib/greet.cpp        anything the Containerfile copies
    lib/greet.hpp
```

```dockerfile
# Two stages, both from the course image: build a library in the first, install it in the second.
FROM cbe/cpp20 AS build
USER root
COPY lib/ /src/
RUN g++ -std=c++20 -O2 -c /src/greet.cpp -o /src/greet.o && ar rcs /src/libgreet.a /src/greet.o

FROM cbe/cpp20
USER root
COPY --from=build /src/libgreet.a /usr/local/lib/libgreet.a
COPY lib/greet.hpp /usr/local/include/greet.hpp
USER 1000:1000
```

The rules (the compiler, the server and the runner apply the same code, `shared/containerfile`):

| Rule | Why |
|---|---|
| **Every `FROM` names the exam's own `image:`** (`cbe/cpp20`, or `cbe/cpp20@sha256:…`) **or an earlier stage of the file.** No tag, no variable, no other image. | The build starts from an approved image, and a second stage cannot pull an unpinned image off the internet. |
| `COPY --from` and `RUN --mount …,from=` may name **only an earlier stage.** | The same reason. |
| `RUN --network` may be `default` or `none`, never `host`; no `RUN --security`; no `VOLUME`; no `ONBUILD`. | `host` would let a build reach the services of the machine; a volume would be writable in a container whose files are meant to be read-only. |
| `USER`, `ENTRYPOINT` and `CMD` **are allowed in the file**, and judged by what the image ends up as (below). | To install a package a build needs `USER root`; it must say `USER 1000:1000` again at the end. |
| The name `ca.crt` is the server's, not yours. | See "HTTPS" below. |

**Warnings** (never a failure): a package install with no version (`apt-get install` without `name=version`, `apk add`, `pip install` without `==`, `npm install` without
`@version`, `gem install`), and an `ADD` from a URL with no `--checksum`. An `apt` install is not warned about when the file points apt at a dated archive snapshot
(`snapshot.ubuntu.com`), which fixes the versions the way `images/cpp20` does. `build/examc` prints them when the package is compiled, the import preview shows
them, and the build log repeats them. **They can be switched off for an exam** with `image_warnings: off` in the front matter of `workspace.md` (`on` is the default and the same as
not saying it): they are then not printed, not shown in the import preview, not in the build log and not kept with the build. The problems (the rules of the table above) are never switched off.

**HTTPS.** The course image has no certificates, so a step that reaches an HTTPS archive has none to trust. The build lends the server's certificate bundle as
`ca.crt` in the context; mount it for the one step that needs it, as `images/cpp20/Containerfile` does, and it stays out of the image:

```dockerfile
RUN --mount=type=bind,source=ca.crt,target=/run/host-ca.crt,ro \
    set -eux; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/20261105T000000Z\nSuites: noble noble-updates noble-security\nComponents: main universe\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n' > /etc/apt/sources.list.d/snapshot.sources; \
    apt_options='-o Acquire::https::CaInfo=/run/host-ca.crt -o Acquire::Check-Valid-Until=false'; \
    apt-get ${apt_options} update; \
    apt-get ${apt_options} install -y --no-install-recommends nlohmann-json3-dev; \
    apt-get clean; rm -rf /var/lib/apt/lists/* /etc/apt/sources.list.d/snapshot.sources
```

(That exact step was built for real against the Ubuntu snapshot: 105 seconds on a slow link, most of it the package index.)

## Building it

In the proctor application, an exam with an image shows **Image not built** and an **Image** button. The dialog has **Build the image**; the build goes on in the background (seconds
when the Containerfile only copies files and compiles a little, minutes when it installs packages), and the dialog follows it, **with the log as it grows**: the runner streams what the build prints a few times a second, the server keeps it and hands the dialog only what is new
each second, and the dialog shows it with **where the build is** (*Step 4 of 9: RUN apt-get install …*, a bar), **how long ago it last printed anything** (a step that downloads can be quiet for a
while) and a *Follow the end of the log* box (scroll up to read and it stops following). The log starts with the course image's name and **digest** (it is what the build was made on) and ends with
what verification said. While the build goes the log kept for the view stops at 2 MiB (and says so); when it ends, the runner's own log (its start and its end) is the one that stays. The log so far
is written to the database every ten seconds, so a server that stops during a build leaves it on the build marked *interrupted*.

- **The base is pinned.** A `FROM cbe/cpp20` is built on the digest of the course image the server has *now*, and the log says so (`pinned by this build`). A `FROM cbe/cpp20@sha256:…` is
  compared with that digest: if it is another, the build fails and says both, so a file written against one version of the course image does not silently build on another.
- **Cached.** The image is named by the exam and a hash of (the base's digest, the files of `image/`). Building again the same files on the same base builds nothing and says so
  (**the log of the build that made the image is kept with it**); **Build again from scratch** builds without the cache and replaces the name.
- **One at a time**, with the runner's limits: `build_max_seconds` (default 900) and `build_memory_mb` (default 2048, **enforced**: a step that goes over is killed, exit 137).
- **Network.** The build has the network of the server (decision D11). The containers made from the result never do.

### What a built image must still be

Before the image is kept, it is checked (decision P7-8). The checks say what failed:

| Check | Fails when |
|---|---|
| It is **made from the course image** | its first layers are not the course image's (a `FROM` of something else got through) |
| **The user, the entrypoint, the command and the working folder** are the course image's | the build ends as `root` (no `USER 1000:1000` at the end), or sets another `ENTRYPOINT`, `CMD` or `WORKDIR` |
| **The agent's label** is the course image's | the build changes `io.cbe.agent.sha256` |
| **The agent file is the course image's agent**: `/usr/local/bin/cbe-agent`, hashed in a container with no network, is the file that label names | a `COPY` replaces it (the entrypoint would still say `cbe-agent`) |

An image that fails is not kept, and no exam can use it. A setuid file that a build adds is not looked for: the student's container has no capabilities and no new privileges, which
makes it harmless, but `images/README.md` says the course image has none.

## Publishing

**An exam with its own image cannot be published until it has a verified build of its current files, that the runner still has.** The Publish button is disabled with the reason, and the
server refuses with a 409 that says it too (the proctor's application is not the only way to ask). A **failed rebuild does not take away a good image** that was built before it;
closing admission is never refused.

## What uses the image

`container.create` and `grading.run` are given the **build** next to the course image's name. The runner uses `localhost/cbe-exam/<exam>:<build>`, and **refuses an image whose
`io.cbe.exam` label is not the request's exam**. So the library is in that exam only (the real test shows a student of another exam, in the same class, who does not have it, and the
runner refusing to make a container of another exam from the image). The hidden tests run in the exam's image too, since they may need the library. Students already working keep the container they have;
a student who logs in after a rebuild gets the new image.

## For the person who runs the server

- **Runner settings** (`/etc/cbe-platform/runner.json`): `build_max_seconds`, `build_memory_mb`. API: `POST /v1/images/build` (`deployment.md`). The built images are in `cbe-runner`'s account:
  `sudo -u cbe-runner env XDG_RUNTIME_DIR=/run/user/1001 podman images` shows them as `localhost/cbe-exam/<exam>:<hash>`.
- **They are not removed** when an exam is deleted or built again (decision P7-12). Each is the course image plus a layer or two (a few MB to tens of MB), but they add up over a year:
  `podman rmi localhost/cbe-exam/<exam>:<hash>`, once the exam is over. `podman image prune` takes the layers nothing uses any more.
- **A build that was cancelled** (it took too long, or the runner stopped) leaves the builder's working container in this runtime (Podman 4.9); the runner removes it by name after the
  cancellation and when it starts (decision P7-13).
- **Nothing is built when a student logs in.** A student of an exam whose image was removed from the runtime gets a workspace that fails with that reason; the proctor builds the image again.

## Checked how

`shared/containerfile` (the rules: table-driven), `internal/runner/image_test.go` (the runner with a fake runtime), `internal/httpapi/workspace_phase7_test.go` (the server's flow, through
its routes and the real database), the proctor's Playwright specs (`tests/ui/workspace-image.spec.js`) and, **with real builds, real containers and a real network**,
`deploy/workspace/test-image.sh` (`deployment.md`). For a base of its own: `internal/runner/ownbase_test.go` (the two builds, the policy and the verification, against a fake runtime),
`internal/agent/selftest_test.go`, `shared/examdoc` and `internal/httpapi/workspace_ownbase_test.go` (the whole flow through the routes) and, **with real builds, containers and network**,
`deploy/workspace/test-ownbase.sh` (Alpine, Ubuntu, Python with a pip library, a Redis base that is refused for its volume, and the C++ example package built from the archive snapshot). The validator:
`internal/imagecheck` (against a fake runtime) and `deploy/workspace/test-imagecheck.sh` (the real command, run as `cbe-runner`).
