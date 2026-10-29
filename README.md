# CBE Portal

CBE Portal is a computer-based examination platform for supervised exams on an isolated local network. Instructors manage classes and exams through a proctor dashboard, and students take exams in a dedicated desktop application. A central server controls admission, exam timing, answer storage and submission.

## Applications

| Component | Purpose | Source |
| --- | --- | --- |
| Server | HTTP API, class rosters, exam admission, sessions, autosaved answers, submissions and monitoring records | `cmd/server`, `internal` |
| Student app | Student login, exam exercises, autosaving, submission, exam-enabled calculators, focus monitoring and screen capture | `apps/student` |
| Proctor dashboard | Class and roster management, Markdown exam import, admission, session monitoring, locks, submission review and downloads | `apps/proctor` |

The student app and proctor dashboard are native desktop applications. Each embeds its frontend and communicates with the server through Go services exposed by Wails.

## Development stack

| Area | Technologies |
| --- | --- |
| Backend and desktop services | Go; modules declare Go **1.26.8** |
| HTTP server | Go standard library `net/http` |
| Desktop framework | Wails **v3.0.0-beta.29** |
| Frontends | Svelte **5.57.2**, JavaScript, Vite **8.3.4** |
| Frontend toolchain | Node.js **22.22.3**, npm **11.17.0**, committed npm lockfiles |
| Student code editor | CodeMirror 6 |
| Database | SQLite through the pure-Go `modernc.org/sqlite` driver |
| Exam documents | Markdown compiled by the shared `examdoc` module |
| Tests | Go tests and race detector, Node.js test runner, Playwright |

## Database and files

The server uses a local SQLite database; no separate database service is required. It stores classes, students, exams, sessions, answer versions, submissions, settings and monitoring metadata. Structured exam data uses SQLite JSONB. The bundled driver supplies SQLite, and the server checks JSONB support at startup.

By default, runtime files are kept in:

```text
data/
├── exams.db       # SQLite database
├── screenshots/   # Captured images; metadata is stored in SQLite
└── admin-token    # Private credential for the proctor dashboard
```

The database and screenshot directories are created at startup. The admin token file must be created beforehand. SQL migrations are embedded from `internal/storage/migrations` and applied automatically. Before upgrading an existing database with pending migrations, the server creates a private database backup beside it.

## Repository structure

```text
cmd/server/          Server entry point
internal/            Server configuration, HTTP handlers, domain, services and storage
apps/student/        Student Go module, native application and Svelte frontend
apps/proctor/        Proctor Go module, native application and Svelte frontend
shared/calc/         Calculator engine
shared/clientwire/   Shared desktop HTTP transport and wire contracts
shared/examdoc/      Markdown exam compiler and examc command
shared/examimage/    Checks and writes the pictures embedded in exams
shared/exam/         Shared exam rendering helpers
shared/theme/        Shared styles and Svelte components
docs/                Deployment documentation
Makefile             Build and verification commands
```

The applications and shared Go packages have separate modules. Keep the full repository checkout together: local `replace` directives resolve the shared modules through relative paths.

## Clone the repository

```sh
git clone https://github.com/ideras/cbe-platform.git
cd cbe-platform
```

For SSH access, use `git@github.com:ideras/cbe-platform.git` instead.

## Build requirements

- Git and GNU Make for the root build commands.
- Go **1.26.8 or newer**.
- Node.js **22.22.3** and npm **11.17.0** for the desktop frontends.
- Native Wails dependencies on each desktop build host: a C compiler, GTK4 and WebKitGTK 6.0 development libraries on Linux; Xcode Command Line Tools on macOS; and WebView2 on Windows.

Build desktop applications on their target operating system and architecture. The Makefile invokes the pinned Wails CLI through `go run`, so a global Wails installation is optional. Initial builds require internet access to download Go modules and npm dependencies.

See the [deployment guide](docs/deployment.md#prepare-the-build-machine) for platform setup and commands that do not require Make.

## Build the three components

The native build helper `python3 scripts/ci/build.py --os linux --arch amd64 --output build` checks the Go and Node architectures, builds the server, generates bindings and builds both desktop applications. Use `darwin` or `windows` and `amd64` or `arm64` for the native host. Its outputs are intermediate executables. The GitHub Actions workflow builds on six native targets and prepares separate archives; see [the deployment guide](docs/deployment.md) for names, runtime dependencies and verification status.

Run these commands from the repository root.

### Server

```sh
make server
```

Output: `build/cbe-portal-server`. The server builds with CGO disabled and does not need the desktop toolchain.

### Student app

```sh
make student
```

Output: `build/cbe-portal-student`.

### Proctor dashboard

```sh
make proctor
```

Output: `build/cbe-portal-proctor`.

Each desktop build generates the Wails bindings, installs frontend dependencies with `npm ci`, builds the frontend, and compiles a native executable with the assets embedded. `make desktop` builds both desktop applications; `make server student proctor` builds all three components.

These output names follow the current Taskfiles. For Windows builds with `.exe` names and macOS application distribution, follow the [deployment guide](docs/deployment.md).

## Run locally

The following commands use a POSIX shell on Linux or macOS. Create the development credential once, from the repository root:

```sh
install -d -m 700 data
umask 077
openssl rand -hex 32 > data/admin-token
chmod 600 data/admin-token
```

Keep this file private and reuse it on subsequent starts. Start the server:

```sh
./build/cbe-portal-server \
  --bind 127.0.0.1:8080 \
  --database data/exams.db \
  --screenshots data/screenshots \
  --admin-token-file data/admin-token
```

In another terminal, check the API:

```sh
curl --fail http://127.0.0.1:8080/healthz
```

Launch the dashboard from a desktop session:

```sh
./build/cbe-portal-proctor \
  --server http://127.0.0.1:8080 \
  --admin-token-file data/admin-token
```

Use the dashboard to add a class from a roster CSV, import a Markdown exam, set its student PIN, and open admission. Then launch the student app:

```sh
./build/cbe-portal-student --server http://127.0.0.1:8080
```

Students sign in with their roster account number and the exam PIN. The admin credential belongs only on the server and proctor machines.

For separate computers, bind the server to its LAN interface and give the applications the server's reachable LAN address. The current clients accept an `http://host:port` base address and are intended for an isolated exam network. See the [deployment guide](docs/deployment.md) for network setup, persistent storage and service installation.

## Verify changes

```sh
make test
make vet
```

These targets cover the server, `shared/clientwire`, `shared/examdoc` and `shared/examimage`. Desktop modules and the calculator module have separate checks:

```sh
make desktop-test
go -C shared/calc test -race ./...
```

Frontend unit tests can be run with:

```sh
npm --prefix apps/student/frontend test
npm --prefix apps/proctor/frontend test
```

The [deployment guide](docs/deployment.md#verify-a-release) also describes UI and server integration tests.
