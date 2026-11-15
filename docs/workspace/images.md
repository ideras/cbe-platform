# An exam's own image

Phase 7. A workspace exam runs in a container made from a **course image** (`cbe/cpp20`): one image per course, built and approved by whoever runs the server
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
them, and the build log repeats them.

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
`deploy/workspace/test-image.sh` (`deployment.md`).
