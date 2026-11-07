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

Without a CI runner, the same script produces the archives locally. Add `--cross` on a non-Windows host to build the Windows x64 target, then package each platform:

```sh
python3 scripts/ci/build.py --os linux --arch amd64 --output build
python3 scripts/ci/build.py --os windows --arch amd64 --output build --cross
python3 scripts/ci/package.py --os linux --arch amd64 --input build --output build/ci/artifacts
python3 scripts/ci/package.py --os windows --arch amd64 --input build --output build/ci/artifacts
```

To rebuild only the student for Windows from Linux or macOS, run `make student-windows` (`WINDOWS_ARCH=arm64` for ARM64); it writes `build/cbe-portal-student.exe` with the same caveats as `--cross` below.

`--cross` exists only for Windows because the Linux and macOS desktops link their native webview through CGO; it prints a warning, and the resulting binaries are labelled Windows even though they have never run there. `package.py` still verifies the ELF or PE header and machine type of every binary, and both platforms' archives can be staged side by side because the Windows files carry the `.exe` suffix. Validate what you can on the build host before distributing anything: `python3 scripts/ci/smoke.py build/cbe-portal-server` for the server and `python3 scripts/ci/test.py` for the Go suites. Run `python scripts/ci/smoke.py cbe-portal-server.exe` on a real Windows machine before handing those builds to students. Archives built this way are unsigned test builds; the manifest records the commit and the exact Go, Node and npm versions, which may differ from the pinned CI toolchain.

The GitHub Actions workflow builds each component on native x64 and ARM64 hosts. Each successful packaging step uploads a separate archive and `.sha256` file. For each application (`server`, `student`, `proctor`), Linux names are `cbe-portal-<application>-linux-{x64,arm64}.tar.gz` and Windows names are `cbe-portal-<application>-windows-{x64,arm64}.zip`. The macOS server is `cbe-portal-server-macos-universal.tar.gz`; the desktop bundles are `cbe-portal-{student,proctor}-macos-universal.zip`. Verify a checksum from its archive directory with `sha256sum -c <archive>.sha256`, or `shasum -a 256 -c <archive>.sha256` on macOS. The included manifest records the commit and tool versions. Intermediate macOS slices and test logs are separate artifacts.

Artifacts are uploaded before the platform tests finish, so check the complete workflow result before using them. Windows token-file ACL validation and native integration tests must pass before deploying a Windows server. The Linux desktop binaries depend on the Ubuntu 24.04 GTK4 and WebKitGTK 6.0 runtime stack; Windows desktops require WebView2. The universal macOS builds target macOS 12 or later and use CI-only bundle identifiers (`org.cbeportal.ci.student` and `org.cbeportal.ci.proctor`). They are ad-hoc signed and are not Developer ID signed or notarized. These identifiers are not production identifiers; changing them affects macOS permission grants.

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

`make test` and `make vet` cover the root server module plus `shared/clientwire`, `shared/examdoc` and `shared/examimage`. `make desktop-test` builds the desktop applications and runs both desktop Go test suites. The Go race detector requires a supported host toolchain with CGO available.

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

The token must be a regular file and contain at least 32 non-whitespace characters. On Linux and macOS it must have no group or other permission bits. On Windows, its owner and all access-granting ACL entries must be limited to the account running the server, local Administrators and SYSTEM; the server rejects a missing ACL and unsupported ACL entries. Configure the ACL before writing the token and restrict the database and screenshot directories as well: Windows ACLs, not Unix mode bits, control access to those paths. A token file that merely inherits a user-profile ACL is rejected on Windows 11 because the profile grants application-container SIDs; replace the inherited ACL explicitly, for example in PowerShell:

```powershell
New-Item -ItemType File -Path 'C:\ProgramData\cbe-platform\admin-token' -Force | Out-Null
$sid = [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value
icacls 'C:\ProgramData\cbe-platform\admin-token' /inheritance:r /grant:r "*${sid}:F"
[IO.File]::WriteAllText('C:\ProgramData\cbe-platform\admin-token', '<32+ character token>' + [Environment]::NewLine)
```

A 32-byte hexadecimal token satisfies both the server and dashboard validators. Both applications trim the trailing newline. Symlinks are rejected.

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
4. Open admission for the exam with **Publish**. Importing an exam alone does not allow students to log in. The publish dialog names the students who can log in (only the exam's class), asks for an optional end time and a confirmation tick (publishing replaces the exam published now; if students are still working in that exam, a second **Override** tick is required, because they cannot log back in to it if their application closes or crashes; the count follows the server while the dialog is open, and a change asks for the override again; **Close admission** asks for the same override in the same case), and then stays open with the exam's login PIN, ready to read out, and a button to the live roster. The PIN is hidden until revealed or copied. No exam list or student view carries a PIN: the dashboard reads it with the administrator credential (`GET /admin/exam/{id}/pin`, never cached, never logged) only when the publish dialog opens.
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

### One computer per student account

An account is open on one computer at a time. The exam PIN is the same for the whole class and account numbers are not secret, so this rule, not the PIN, is what keeps a student from opening a classmate's exam from another computer.

- While the application that logged in is still being heard from, a second login for the same account is refused. The student sees "This account is already open on another computer" and how long to wait. "Still being heard from" means a heartbeat, or the login itself, within the exam's **connection lost** threshold (30 seconds by default; the dashboard's Settings page shows it, and an exam keeps the value it was published with).
- A student who quits the application and opens it again does not wait: a clean close tells the server at once. After a crash or a power cut the student waits for the threshold, or a proctor releases the account.
- Every refused login is kept in the student's history, with the address it came from. On the live roster the row shows **Login refused ×N** with the time and address of the last attempt, counts under *Needs attention*, and offers **Allow new login…**.
- **Allow new login…** asks for a reason and lets the next login in at once, whatever the heartbeat says. That login replaces the session's token: the application that had the account loses it, cannot save, and tells the student to call the instructor. Compare the address in the notice with the computer shown on the row before allowing it. A release is used by one login; the row says "Next login allowed" until the student logs in.
- Logging in again after being let in resumes the same session: the same deadline and the last saved answers.

### Native desktop checks

Run these checks on every supported lab OS and display setup:

- Log in to a practice exam: the login itself takes a test screenshot first and does not start the exam if capture fails, is cancelled or is refused by the operating system (try each case, and revoke the permission after a first success). During the practice exam, trigger the configured focus-loss capture and inspect the resulting screenshots on the dashboard; the initial test image is discarded. Check all attached monitors and the actual display session, including Wayland where applicable.
- On macOS, grant Screen Recording permission to the final student application in **System Settings > Privacy & Security**, then fully quit and reopen it if requested. Retest after relaunch.
- Verify the default sound output can be read and changed. The Linux sound integration uses `pactl` or `amixer`; provide a working command and audio session on student computers. Every heartbeat checks the volume and raises it to the exam's minimum (the student sees a notice); the check also runs every 5 seconds. Only Linux (PulseAudio or PipeWire, through `pactl subscribe`) is told of a change at once — measured about 30 ms until the notification, including the time `pactl` itself takes to run; Windows and macOS rely on the heartbeat and the 5-second check, so a lowered volume can stay low for up to 5 seconds. Lower, mute and switch the output during a practice exam to confirm the notice and the correction.
- Verify focus loss, return to the app, lock handling and proctor unlocks. To test the locks without waiting, set **Lock after away for** and **Lock after no contact for** on the Settings page to a few seconds, then publish the exam again and log in with an account that has not logged in to it: the lock times are fixed in the exam when it is published and in each session when its student logs in. The away lock cannot come before the alarm starts plus one screenshot interval (10 seconds with the defaults), and the no-contact lock has to be longer than the time after which the connection counts as lost (30 seconds with the defaults). The Settings page says these limits beside the fields as you change the values, puts a button on each problem that sets the value it names, and has **Fit the other values** (it keeps the value you changed last, changes only what conflicts with it and lists what it changed, with **Undo**) and **Restore the defaults** (nothing is saved until you save).
- Verify that switching to a text console is noticed (Linux): with the exam open and unlocked, press Ctrl+Alt+F3, wait a few seconds, then Ctrl+Alt+F1 or the console of your session (the one `cat /sys/class/tty/tty0/active` showed when you started the app). The alarm should sound while you are away, the roster should show the student out of the window, and the History should show a "Left the exam window" row that says the session was not the one on screen, with the time away. Whatever the screenshots show meanwhile depends on the desktop (a capture of a session that is not on screen may fail, and the student is then asked to repeat the screenshot test).
- Verify that switching to another user is noticed (Windows and macOS): with the exam open and unlocked, use Fast User Switching to sign in as another user (Windows: Win+L, then pick the other account; macOS: the user menu), wait a few seconds, then switch back. The result should be the same as for the text console: the alarm sounds while you are away, the roster shows the student out of the window, and the History row says the session was not the one on screen. On Windows, run `go test ./internal/sessionwatch -run TestTheRealConsole -v` from `apps/student` on the computer first: it prints the process's session and the console's, and passes when they are the same. A session that is not on the console when the application starts (a Remote Desktop session) is not watched. On macOS, the same test says whether the window server answers. The screens cannot be captured while another user's session has the screen, so expect a "Screenshot capture failed" row in the History (it says the session was not the one on screen when the app knew it) and no images for that absence; when the student is back, the screenshot retest is offered on the lock screen or on the exam page at once, and one click on **Test screenshot capture** restores editing.
- Verify that remote access is noticed: the student application looks for a Remote Desktop session (Windows) and for remote-control or screen-sharing programs from a list, and tells the instructor; it does not sound an alarm or lock the exam, and it tells the student that the instructor has been told (the rules say so before the login). The roster shows "Remote access: ..." on the student's row, which then needs attention, and the History has a "Remote access" row for each change. The list is built into the server (`internal/domain/remote.go`, the program names without ".exe", any case, and a trailing `*` for "starts with") and sent to the application at the login, so changing it takes a server rebuild and restart but no new student application. It is a list of names: a renamed program is not found, a computer watched by hardware or by another computer pointed at its screen is not seen at all, and a program can be there for a reason (a student who opens the Remote Desktop client is shown too). To check it on a computer, run `go test ./internal/remotewatch -run TestLiveWatch -v -live 3m -tools "notepad,mstsc"` from `apps/student` (on Windows, without Go, cross-build the test binary with `GOOS=windows go test -c -o ../../build/remotewatch.test.exe ./internal/remotewatch` and run it with `-test.run TestLiveWatch -test.v -live 3m -tools notepad`): start Notepad, and a line says it was found about 6 seconds later and another says it is gone after you close it; a program that starts and stops within a few seconds is deliberately not reported. For the Remote Desktop session, connect to the Windows computer with a Remote Desktop client (the computer must be a Windows edition that accepts connections, such as Pro): the live test inside the remote session, and the application at the login or in the middle of an exam, report "Remote Desktop" with the name of the computer connected from. The student application needs a server that knows the event: update the server before the students' applications.
- Verify the network lock: with a practice exam open, move the student computer to a Wi-Fi network that cannot reach the server (or turn Wi-Fi off). After the policy's connection-lost time the app shows a warning with the time left before the exam locks. Press **I need to change network** and confirm: the exam locks, the window stops being full screen and loses its place on top of the other windows (so the desktop's panel, taskbar or Dock and the network settings can be reached), and opening the Wi-Fi settings makes no sound. Reconnect to the exam network and return to the exam window. The roster shows the student as locked while "changing network", the History row of the lock says it was asked for by the student (request 1 of 2), when they asked for it and how long before the server heard of it, and carries the screenshots taken meanwhile. Only **Unlock** lifts the lock; when you unlock, the window goes back to full screen, on top, and asks for the focus. A window manager may refuse the focus to a window the student did not click (a Linux Mint laptop did): the window then says so ("This window is not the active one. Click anywhere in it to continue"), and the alarm starts once the focus threshold has passed since the unlock, until the student clicks it. The time the student spent out of the window during the lock does not count towards that threshold.

  The lock for lack of contact is treated the same way. Turn Wi-Fi off and keep working until the server's "lock after no contact" time passes (the Settings page): the exam locks, the window stops being full screen, the History row of the lock says "no contact" and carries the screenshots taken while it lasted, and the roster and the unlock dialog tell you the student could use the computer to reconnect. That one is not limited to two requests, so each one is a blind period that needs you to unlock it: read its screenshots. The lock for an absence (away from the exam window too long) is unchanged: the window stays full screen and nothing is captured because of it. If the application is closed and opened again during either lock, it resumes locked with the window small and takes screenshots again from that moment, under the lock's own record. Check on each system you use (Windows, Linux, macOS): on macOS leaving and re-entering full screen is an animation, and a brief focus flicker right after the unlock must not start the alarm. The student can ask for the lock twice per exam; the application holds the screenshots in memory until the server has recorded the lock, so closing the application during the lock loses them.
- Open one account on two computers: the second login is refused with the time to wait, the first keeps saving, the roster shows the refused login, and **Allow new login…** lets the second computer in and cuts the first off. Quit the application cleanly and open it again: it logs in at once.
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
| A student is told the account is already open on another computer | Another computer, or a copy of the application that was not closed, has the account open. Close it, or wait for the time shown, then log in again; after a crash the wait is the connection lost threshold. If the student has no other computer open, check the address on the roster's **Login refused** notice, and use **Allow new login…** once it is the student's own computer. |
| An application says its session is no longer valid and answers are not being saved | The account was opened on another computer after a release or a long silence. Call the instructor: the work is saved up to the last acknowledged save, and the student continues on the computer that took over. |
| Exam import fails | Review the dashboard validation messages and check the Markdown with `examc`. The compiled request must fit the server's JSON limit. |
| Go cannot resolve a shared module | Build from the full repository checkout and preserve the relative module layout. |
| Desktop build cannot embed `frontend/dist` | Run the full desktop build so bindings and frontend assets exist before native compilation. |
| Native GTK/WebKit dependency error | Run the pinned Wails `doctor`; install the build or runtime packages appropriate to that machine. |
| Frontend opens without working native actions | Launch the compiled desktop executable. A Vite page or static `dist` directory does not supply the Wails services. |
| Login reports that screen capture failed, or editing is blocked by a screenshot retest | Check OS permissions, display support and the final installed executable; notify the instructor and log in again (or retest, during the exam). |
| Sound checks fail on Linux | Verify `pactl` or `amixer` works in the student's graphical/audio session. |
| Dashboard shows no classes on first start | Add a class and roster through the dashboard, then import and admit an exam. |
