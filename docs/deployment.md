# Build and deployment guide

This guide covers building and distributing the server, student application and proctor dashboard. Commands use a POSIX shell unless a PowerShell example is explicitly provided. Run repository commands from the checkout root, except where a step changes directories.

## Deployment layout

Run one server with persistent local storage on the exam LAN. Install the student executable on student computers and the proctor executable on instructor computers. The desktop executables embed their Svelte frontends; Node.js and Go are build dependencies and are not required on end-user computers.

| Machine | Required artifacts | Runtime requirements |
| --- | --- | --- |
| Server | `cbe-portal-server`, private admin token, persistent database and screenshot directories | Network access from exam computers; writable local storage |
| Student computer | Student executable for its OS and CPU | Graphical desktop, native webview dependencies, screen-capture and sound support |
| Proctor computer | Proctor executable for its OS and CPU, private copy of the server's admin token | Graphical desktop and native webview dependencies |

Use the same source revision for all three applications. Build separate desktop artifacts for each target OS and CPU architecture used in the lab.

### Network configuration

The examples use server address `192.168.10.10` and TCP port `8080`. Replace them with the address and port assigned to your server.

1. Give the server a stable LAN address or DHCP reservation.
2. Place the server and exam computers on the isolated exam network.
3. Allow inbound TCP traffic on the server port from that network. Configure the access point so client isolation does not prevent students from reaching the server.
4. Bind the server to its LAN address or `0.0.0.0:8080`. Binding to `127.0.0.1` permits connections only from the server itself.
5. Configure both desktop applications with `http://192.168.10.10:8080`.

The clients currently require HTTP at the URL root. HTTPS URLs, credentials in URLs, URL subpaths, queries and fragments are rejected. Requests go directly to the server without an HTTP proxy and do not follow redirects. This configuration is intended for an isolated LAN; public internet hosting would require changes to the transport and deployment design.

`0.0.0.0` is a listen address. Give clients the server's actual address. On a student computer, `127.0.0.1` refers to that student computer.

## Prepare the build machine

### Common tools and versions

Install Git and the following project toolchain:

| Tool | Project version or requirement |
| --- | --- |
| Go | **1.26.8 or newer**, as declared by the Go modules |
| Node.js | **22.22.3**, as declared by both frontend packages |
| npm | **11.17.0**, as declared by both frontend packages |
| Wails | **v3.0.0-beta.29**, pinned in the Makefile and desktop Go modules |
| GNU Make | Required for root `make` commands; manual alternatives appear below |
| OpenSSL and curl | Used in this guide to generate credentials and check the API |

Check the installed versions:

```sh
go version
node --version
npm --version
make --version
```

Keep the Go module files and npm lockfiles when preparing a release. The desktop builds use `npm ci` to install their locked dependencies. Do not substitute a newer global Wails version for the pinned build command.

### Linux desktop builds

On a Debian or Ubuntu version that provides GTK4 and WebKitGTK 6.0 development packages:

```sh
sudo apt update
sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev
```

Install the project Go, Node.js and npm versions separately; distribution packages may provide different versions. Keep CGO enabled for the desktop builds. The server build explicitly disables it.

Check the native libraries and the pinned Wails environment:

```sh
pkg-config --modversion gtk4 webkitgtk-6.0
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.29 doctor
```

For other Linux distributions, use the equivalent native packages. The [Wails installation documentation](https://v3.wails.io/getting-started/installation/) describes the platform dependencies. This repository's default Linux build uses GTK4 and WebKitGTK 6.0.

### macOS desktop builds

Install Xcode Command Line Tools:

```sh
xcode-select --install
```

Build on a Mac with the required CPU architecture, then run the pinned Wails `doctor` command above. For distribution, package the native executables as application bundles and test screen-recording permission using the final student bundle, as described below.

### Windows desktop builds

Install the Go, Node.js and npm versions above, Git, and the Microsoft Edge WebView2 Runtime. Use native Windows builds for Windows computers. The manual PowerShell build instructions below avoid the Makefile and its POSIX `mkdir -p` commands.

Run the pinned Wails environment check in PowerShell:

```powershell
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.29 doctor
```

Follow any additional platform requirements it reports. End-user Windows computers need WebView2 as well.

## Obtain the source

```sh
git clone https://github.com/ideras/cbe-platform.git
cd cbe-platform
```

For a release build, check out the chosen published tag or commit before building. Keep the entire checkout: the desktop and shared Go modules use relative `replace` directives. Copying only `apps/student` or `apps/proctor` is insufficient.

## Build the applications

For a native intermediate build, `python3 scripts/ci/build.py --os linux --arch amd64 --output build` runs the server and desktop build sequence after checking the host and tool architectures. Substitute the host OS (`linux`, `darwin`, or `windows`) and Go architecture (`amd64` or `arm64`). Run it from a full checkout with the dependencies above installed; on Windows use `python` if `python3` is unavailable. To run the Go checks against that server, use `python3 scripts/ci/test.py --server build/cbe-portal-server --race --report build/ci/reports` (omit `--race` on Windows ARM64 and use the `.exe` server name on Windows).

The GitHub Actions workflow builds each component on native x64 and ARM64 hosts. Each successful packaging step uploads a separate archive and `.sha256` file. For each application (`server`, `student`, `proctor`), Linux names are `cbe-portal-<application>-linux-{x64,arm64}.tar.gz` and Windows names are `cbe-portal-<application>-windows-{x64,arm64}.zip`. The macOS server is `cbe-portal-server-macos-universal.tar.gz`; the desktop bundles are `cbe-portal-{student,proctor}-macos-universal.zip`. Verify a checksum from its archive directory with `sha256sum -c <archive>.sha256`, or `shasum -a 256 -c <archive>.sha256` on macOS. The included manifest records the commit and tool versions. Intermediate macOS slices and test logs are separate artifacts.

Artifacts are uploaded before the platform tests finish, so check the complete workflow result before using them. The initial Windows server has an unresolved credential-file permission check: Windows mode bits do not represent file ACLs, and startup and integration checks can fail. Do not deploy the Windows server until a Windows credential policy and native tests have been verified. The Linux desktop binaries depend on the Ubuntu 24.04 GTK4 and WebKitGTK 6.0 runtime stack; Windows desktops require WebView2. The universal macOS builds target macOS 12 or later and use CI-only bundle identifiers (`org.cbeportal.ci.student` and `org.cbeportal.ci.proctor`). They are ad-hoc signed and are not Developer ID signed or notarized. These identifiers are not production identifiers; changing them affects macOS permission grants.

CI does not verify system audio changes, interactive screen-recording permissions or multi-monitor capture; perform those checks manually on the target desktop. No signing or deployment secrets are used by the workflow.

### Server

```sh
make server
```

This runs the equivalent of:

```sh
mkdir -p build
CGO_ENABLED=0 go build -trimpath -o build/cbe-portal-server ./cmd/server
```

The result is a native executable containing the server and embedded SQL migrations. It does not require a separate SQLite installation, Go runtime or Node.js runtime on the server host.

The server can also be built for another OS and architecture because it does not require CGO. For example, build a Linux AMD64 server from a POSIX build host with:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -o build/cbe-portal-server-linux-amd64 ./cmd/server
```

Use the resulting filename when copying that artifact to the server.

### Student application

```sh
make student
```

The student Taskfile performs these steps from `apps/student`:

```sh
cd apps/student
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.29 generate bindings -b -f '-tags bindings' ./cmd/student
npm --prefix frontend ci
npm --prefix frontend run build
mkdir -p ../../build
go build -tags production -o ../../build/cbe-portal-student ./cmd/student
cd ../..
```

### Proctor dashboard

```sh
make proctor
```

The proctor Taskfile performs these steps from `apps/proctor`:

```sh
cd apps/proctor
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.29 generate bindings -b -f '-tags bindings' ./cmd/proctor
npm --prefix frontend ci
npm --prefix frontend run build
mkdir -p ../../build
go build -tags production -o ../../build/cbe-portal-proctor ./cmd/proctor
cd ../..
```

The binding generation step uses the `bindings` build tag so it can run before `frontend/dist` exists. Build the frontend before compiling the final desktop executable, because the executable embeds that directory.

`npm run build` alone produces frontend assets. Distribute the final native executable. The Wails bridge requires the native application; uploading `frontend/dist` to a static web host does not deploy a working student app or dashboard.

To build all three components on a native Linux or macOS host:

```sh
make server student proctor
```

Expected files:

```text
build/
├── cbe-portal-server
├── cbe-portal-student
└── cbe-portal-proctor
```

### Manual Windows desktop build

Run the following in PowerShell from the repository root. These commands explicitly give the desktop artifacts `.exe` filenames. Use the Linux server installation described below for the exam server.

```powershell
$env:GO111MODULE = "on"
New-Item -ItemType Directory -Force build | Out-Null

Push-Location apps/student
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.29 generate bindings -b -f '-tags bindings' ./cmd/student
npm --prefix frontend ci
npm --prefix frontend run build
go build -tags production -o ../../build/cbe-portal-student.exe ./cmd/student
Pop-Location

Push-Location apps/proctor
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.29 generate bindings -b -f '-tags bindings' ./cmd/proctor
npm --prefix frontend ci
npm --prefix frontend run build
go build -tags production -o ../../build/cbe-portal-proctor.exe ./cmd/proctor
Pop-Location
```

Check each command succeeds before proceeding. The current Taskfiles produce raw executables; installer generation, signing and automatic updates are not configured in this repository.

## Verify a release

Run these checks from the repository root:

```sh
make test
make vet
make desktop-test
go -C shared/calc test -race ./...
npm --prefix apps/student/frontend test
npm --prefix apps/proctor/frontend test
```

`make test` and `make vet` cover the root server module plus `shared/clientwire` and `shared/examdoc`. `make desktop-test` builds the desktop applications and runs both desktop Go test suites. The Go race detector requires a supported host toolchain with CGO available.

For explicit client checks against the built server:

```sh
make student-client-test
make proctor-client-test
```

These targets set `EXAM_SERVER_BINARY` to the server built in this checkout. The current proctor target excludes the named test specified in the Makefile; it is not a substitute for the broader desktop suite.

For browser UI checks, install the Playwright browser and required host libraries, then run the UI targets:

```sh
cd apps/student/frontend
npx playwright install --with-deps chromium
cd ../../proctor/frontend
npx playwright install --with-deps chromium
cd ../../..
make student-ui-test
make proctor-ui-test
```

UI tests use simulated native APIs. Validate the actual desktop applications on the lab computers as well, especially screenshot permission, focus monitoring, audio and the final submission flow.

## Deploy the server

### Persistent files and credentials

Choose permanent locations for the database, screenshots and admin credential. A Linux layout used below is:

```text
/opt/cbe-platform/bin/cbe-portal-server
/etc/cbe-platform/admin-token
/var/lib/cbe-platform/exams.db
/var/lib/cbe-platform/screenshots/
```

The database contains student records, exam PINs, answers and monitoring data. Keep the database, screenshots, credentials and backups private. Store SQLite on a local persistent filesystem and run one server process for a database.

The server creates the database and screenshot directories if needed and applies migrations at startup. It reads its settings from command-line flags; an `.env` file is not loaded. It does **not** generate the admin token.

### First installation on Linux with systemd

The following example targets a Debian/Ubuntu-style Linux host. Run installation commands on the server host with an account that can use `sudo`. If building elsewhere, transfer the server artifact to that host first and adjust the source path in the `install` command.

Create a dedicated service account and directories on the first installation:

```sh
sudo useradd --system --home-dir /var/lib/cbe-platform --shell /usr/sbin/nologin cbe-platform
sudo install -d -m 755 /opt/cbe-platform/bin
sudo install -d -m 750 -o root -g cbe-platform /etc/cbe-platform
sudo install -d -m 700 -o cbe-platform -g cbe-platform /var/lib/cbe-platform
sudo install -m 755 build/cbe-portal-server /opt/cbe-platform/bin/cbe-portal-server
```

Generate the credential **once**. These commands initialize a new token file; do not rerun them against an existing installation unless intentionally rotating the credential:

```sh
sudo install -m 600 -o cbe-platform -g cbe-platform /dev/null /etc/cbe-platform/admin-token
openssl rand -hex 32 | sudo tee /etc/cbe-platform/admin-token > /dev/null
```

The token must be a regular file, contain at least 32 non-whitespace characters and have no group or other permission bits on the server. A 32-byte hexadecimal token satisfies both the server and dashboard validators. Both applications trim the trailing newline. Symlinks are rejected.

Save the following as `/etc/systemd/system/cbe-platform.service`, replacing the listen address if needed:

```ini
[Unit]
Description=CBE Portal exam server
After=network.target

[Service]
Type=simple
User=cbe-platform
Group=cbe-platform
WorkingDirectory=/var/lib/cbe-platform
ExecStart=/opt/cbe-platform/bin/cbe-portal-server --bind 0.0.0.0:8080 --database /var/lib/cbe-platform/exams.db --screenshots /var/lib/cbe-platform/screenshots --admin-token-file /etc/cbe-platform/admin-token
Restart=on-failure
RestartSec=3
TimeoutStopSec=30
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/var/lib/cbe-platform

[Install]
WantedBy=multi-user.target
```

Load and start the service:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now cbe-platform
sudo systemctl status cbe-platform
curl --fail http://127.0.0.1:8080/healthz
```

The health response contains `"status":"ok"` and the server's UTC time. A new database starts with no classes, so the initial log asking you to add a class is expected.

From a separate exam computer, verify the LAN address:

```sh
curl --fail http://192.168.10.10:8080/healthz
```

Review logs with:

```sh
sudo journalctl -u cbe-platform -f
```

The server writes structured JSON logs to standard error. It handles SIGINT and SIGTERM for graceful shutdown.

### Run the server in a terminal

For an instructor-operated server without a service manager, prepare private runtime paths once:

```sh
install -d -m 700 data
umask 077
openssl rand -hex 32 > data/admin-token
chmod 600 data/admin-token
```

On subsequent starts, reuse the files and run:

```sh
./build/cbe-portal-server \
  --bind 0.0.0.0:8080 \
  --database data/exams.db \
  --screenshots data/screenshots \
  --admin-token-file data/admin-token
```

Relative paths are resolved from the current working directory. Start from the same directory each time or supply absolute paths. Keep the terminal and computer running for the exam. Stop with Ctrl+C after sessions have finished.

### Server flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--bind` | `:8080` | HTTP listen address; the default listens on available interfaces |
| `--database` | `data/exams.db` | SQLite database path |
| `--screenshots` | `data/screenshots` | Private image directory |
| `--admin-token-file` | `data/admin-token` | Existing private proctor credential file |
| `--worker-interval` | `1s` | Session lifecycle worker cadence |
| `--max-json-bytes` | `1048576` | Maximum exam/answer JSON request size |
| `--max-answer-bytes` | `65536` | Maximum UTF-8 bytes per answer |
| `--max-answers-bytes` | `524288` | Maximum total UTF-8 answer bytes per snapshot |

The limits must satisfy `per-answer <= total-answer <= JSON request size`. Use `cbe-portal-server --help` to inspect the flags for a built version. Exam policies and timing settings are managed through the dashboard and exam documents.

## Deploy the proctor dashboard

1. Copy the native proctor executable to the instructor computer. On Linux, install the GTK4 and WebKitGTK 6.0 runtime libraries matching the build. On Windows, ensure WebView2 is available. On macOS, use an application bundle for distribution.
2. Transfer a private copy of the **same token file used by the server** through a secure channel. A separately generated token will not authenticate. Keep it on proctor computers only.
3. Place the token in a private directory and restrict its access. On Linux or macOS, for example:

   ```sh
   install -d -m 700 "$HOME/.config/cbe-portal-proctor"
   chmod 600 "$HOME/.config/cbe-portal-proctor/admin-token"
   ```

   The token must already have been copied to that filename before `chmod`. On Windows, use the file's Security settings to limit access to the instructor account; the dashboard skips the Unix permission-bit check on Windows.

4. Start the executable from the instructor's desktop session:

   ```sh
   ./cbe-portal-proctor \
     --server http://192.168.10.10:8080 \
     --admin-token-file "$HOME/.config/cbe-portal-proctor/admin-token"
   ```

   On Windows, use the `.exe` filename and a Windows token path. Paths with spaces must be quoted.

The dashboard can also be opened without flags. In **Server connection…**, enter the server address, choose the credential file, and connect. It remembers the accepted server address and token file path in `cbe-portal-proctor/config.json` under the user's OS configuration directory. It reads the credential from that file on future starts.

Optional flags:

| Flag | Purpose |
| --- | --- |
| `--server` | Explicit startup server address; otherwise use the remembered address or `http://127.0.0.1:8080` |
| `--admin-token-file` | Private credential file |
| `--exam-id` | Exam UUID to monitor after connecting; this does not change admission |
| `--config` | Override the dashboard's remembered-settings file path |

### Initialize classes and exams

1. Add a class by choosing its roster CSV in the dashboard and reviewing the import preview.
2. Open the class and choose a Markdown exam file. Review the compiled exam and any validation messages.
3. Choose the student PIN and navigation behavior, then import the exam.
4. Open admission for the exam. Importing an exam alone does not allow students to log in.
5. Monitor students and verify incoming saves, connectivity, focus events and submissions.

An optional compiler is available for checking exam files before importing:

```sh
make examc
./build/examc path/to/exam.md
```

`shared/examdoc/testdata/calculator.md` is a small example of the Markdown format. Use test data when validating a new installation.

## Deploy the student application

1. Copy the native student executable to each student computer. Install its native webview dependencies as described for the dashboard.
2. Keep the executable in a stable location and make it executable on Linux or macOS:

   ```sh
   chmod 755 cbe-portal-student
   ./cbe-portal-student --server http://192.168.10.10:8080
   ```

3. Confirm the login screen shows the correct server. The address can also be entered on the login screen.
4. Complete the screen-capture check and permissions flow, accept the exam rules, and sign in with a roster account number and the published exam's PIN.
5. Confirm that edits are acknowledged as saved by the server and that the dashboard sees the student's session.

The student application needs no admin credential. Distribute only the student application and its launch instructions to students.

Student startup flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--server` | `http://127.0.0.1:8080` | Initial server address |
| `--autosave-debounce` | `2s` | Delay after edits before a full answer snapshot is saved |
| `--autosave-interval` | `20s` | Periodic saving during continuous editing |
| `--status-interval` | `3s` | Server status polling interval |

### Native desktop checks

Run these checks on every supported lab OS and display setup:

- Complete the built-in screenshot test. During a practice exam, trigger the configured focus-loss capture and inspect the resulting screenshots on the dashboard; the initial test image is discarded. Check all attached monitors and the actual display session, including Wayland where applicable.
- On macOS, grant Screen Recording permission to the final student application in **System Settings > Privacy & Security**, then fully quit and reopen it if requested. Retest after relaunch.
- Verify the default sound output can be read and changed. The Linux sound integration uses `pactl` or `amixer`; provide a working command and audio session on student computers.
- Verify focus loss, return to the app, lock handling and proctor unlocks.
- Submit a practice exam and confirm the final answers can be reviewed and downloaded.

Fullscreen and always-on-top behavior are deterrents and can be bypassed. Evaluate the native monitoring behavior on the actual exam computers before relying on it operationally.

### macOS application bundles

The local build produces bare executables. For managed distribution on macOS, place each executable in a separate `.app` bundle with its own stable identifier. The CI universal bundles use CI-only identifiers and ad-hoc signatures as described above. For example, after building natively on a Mac:

```sh
mkdir -p "release/CBE Portal Student.app/Contents/MacOS"
cp build/cbe-portal-student "release/CBE Portal Student.app/Contents/MacOS/cbe-portal-student"
chmod 755 "release/CBE Portal Student.app/Contents/MacOS/cbe-portal-student"
```

Create `release/CBE Portal Student.app/Contents/Info.plist` with at least:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>CBE Portal Student</string>
  <key>CFBundleDisplayName</key><string>CBE Portal Student</string>
  <key>CFBundleExecutable</key><string>cbe-portal-student</string>
  <key>CFBundleIdentifier</key><string>org.example.cbe-portal-student</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
```

Replace the example bundle identifier and version values with your organization's release values. Repeat for the proctor bundle using `CBE Portal Proctor`, executable `cbe-portal-proctor`, and its own identifier. Assets are already embedded in the executables.

Validate each plist with `plutil -lint`, then apply your organization's signing and notarization process before distribution. See the [Wails macOS packaging guide](https://v3.wails.io/guides/build/macos/) for platform packaging requirements. Creating the bundle alone does not sign or notarize it. Install the final bundles in `/Applications` and test the student's screen-recording grant, denial and relaunch behavior there.

## Backups, upgrades and rollback

Back up the database, screenshot directory and admin token as one installation. Protect backups with the same access restrictions as the originals. Keep exam source files and release artifacts separately as well.

For a consistent offline backup on the Linux service installation:

```sh
sudo systemctl stop cbe-platform
sudo install -d -m 700 /var/backups/cbe-platform
sudo tar -czf /var/backups/cbe-platform/pre-upgrade.tar.gz \
  -C / var/lib/cbe-platform etc/cbe-platform
sudo chmod 600 /var/backups/cbe-platform/pre-upgrade.tar.gz
sudo systemctl start cbe-platform
```

Use a unique archive name for each backup instead of overwriting an earlier copy. Take backups between exams. Stopping the server does not pause exam deadlines; time continues to pass while it is down. Do not copy only the live `.db` file while the server is running, because SQLite may also have pending data in its WAL file.

For an upgrade:

1. Finish active sessions and take a consistent backup. Keep the previous executables.
2. Build and verify all applications from the chosen source revision.
3. Stop the server, replace its executable, and keep the existing database, screenshots and token paths.
4. Restart the server and review its logs. Pending database migrations run automatically; before changing an existing schema, the server creates an `exams.db.backup-…-before-…` file beside the database. Startup fails if that required backup cannot be created.
5. Check `/healthz`, connect the matching dashboard, and run a practice student session before reopening admission.

The automatic migration backup contains the database only. It does not replace a backup of screenshots and credentials. Keep it until the release has been validated.

For rollback, stop the server and preserve the failed installation before restoring the previous release and a matching complete backup. Restore SQLite into a clean data location so WAL/SHM files from the newer database are not reused. Restore the database and screenshots together: startup reconciles missing image files and orphaned images. A rollback to an earlier backup also discards work recorded after that backup, so review what must be retained before restoring.

To rotate the admin token, replace it with a new private token, restart the server, and update the copy on every proctor machine. Student applications do not receive this file.

## Troubleshooting

| Symptom | Checks |
| --- | --- |
| Server exits before becoming ready | Inspect the JSON error log, listen address, token permissions, token contents and writable data directories. |
| Local health check succeeds but another computer cannot connect | Check the LAN listen address, firewall, server IP and access-point isolation. |
| Dashboard cannot authenticate | Confirm the credential matches the server file, is a regular file, and is private. Reconnect after token rotation. |
| Students cannot sign in | Check class membership, account number, exam PIN, exam schedule and that the intended exam is admitting students. |
| Exam import fails | Review the dashboard validation messages and check the Markdown with `examc`. The compiled request must fit the server's JSON limit. |
| Go cannot resolve a shared module | Build from the full repository checkout and preserve the relative module layout. |
| Desktop build cannot embed `frontend/dist` | Run the full desktop build so bindings and frontend assets exist before native compilation. |
| Native GTK/WebKit dependency error | Run the pinned Wails `doctor`; install the build or runtime packages appropriate to that machine. |
| Frontend opens without working native actions | Launch the compiled desktop executable. A Vite page or static `dist` directory does not supply the Wails services. |
| Screenshot test fails or editing is blocked | Check OS permissions, display support and the final installed executable; notify the instructor and retest. |
| Sound checks fail on Linux | Verify `pactl` or `amixer` works in the student's graphical/audio session. |
| Dashboard shows no classes on first start | Add a class and roster through the dashboard, then import and admit an exam. |
