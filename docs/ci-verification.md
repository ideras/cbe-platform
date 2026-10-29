# CI verification status

The workflow is green. [Run 40770853562](https://github.com/ideras/cbe-platform/actions/runs/40770853562) on commit
`03a6bc4` finished with all nine jobs successful: the six native matrix entries (`linux x64` on `ubuntu-24.04`,
`linux arm64` on `ubuntu-24.04-arm`, `windows x64` on `windows-2025`, `windows arm64` on `windows-11-arm`,
`darwin x64` on `macos-15-intel`, `darwin arm64` on `macos-15`), plus `macOS universal packaging` and the two
`universal-smoke` jobs that exercise the merged server on each macOS architecture.

## Artifact evidence

The run produced all fifteen final archives, each with a `.sha256` beside it, plus the intermediate slices and
per-module test logs:

| Application | Windows x64 | Windows ARM64 | Linux x64 | Linux ARM64 | macOS universal |
| --- | --- | --- | --- | --- | --- |
| Server | `cbe-portal-server-windows-x64.zip` | `cbe-portal-server-windows-arm64.zip` | `cbe-portal-server-linux-x64.tar.gz` | `cbe-portal-server-linux-arm64.tar.gz` | `cbe-portal-server-macos-universal.tar.gz` |
| Student | `cbe-portal-student-windows-x64.zip` | `cbe-portal-student-windows-arm64.zip` | `cbe-portal-student-linux-x64.tar.gz` | `cbe-portal-student-linux-arm64.tar.gz` | `cbe-portal-student-macos-universal.zip` |
| Proctor | `cbe-portal-proctor-windows-x64.zip` | `cbe-portal-proctor-windows-arm64.zip` | `cbe-portal-proctor-linux-x64.tar.gz` | `cbe-portal-proctor-linux-arm64.tar.gz` | `cbe-portal-proctor-macos-universal.zip` |

`macos-slices-x64` and `macos-slices-arm64` are the thin intermediates the universal job merges, and
`test-logs-<os>-<arch>` holds one log per module and command. Every archive was validated before upload: ELF or PE header and
machine type, archive contents, executable modes, and SHA-256 checksums. The macOS executables were checked with
`lipo -verify_arch x86_64 arm64` after merging, the bundles with `plutil -lint`, and the ad-hoc signatures with
`codesign --verify`. The universal server passed its smoke test on both `macos-15-intel` and `macos-15`.

Certificates, notarization and release publication are out of scope: the macOS bundles are ad-hoc signed, use the
CI-only bundle identifiers `org.cbeportal.ci.student` and `org.cbeportal.ci.proctor`, and are not Developer ID
signed. Those identifiers are placeholders, not production identifiers; changing them affects macOS permission
grants. The workflow needs no repository secrets.

## Test coverage

- `go vet` and `go test` in all seven modules; the race detector everywhere except Windows ARM64, which has no
  supported race detector. `scripts/ci/test.py` selects the flags per target because the root Makefile always
  passes `-race`.
- Desktop integration: `EXAM_SERVER_BINARY` points at that job's freshly built server, so the student and proctor
  suites drive a real server over loopback, including the synthetic manual-exam bootstrap and the regression
  output under the runner's temporary directory.
- Server smoke test on every platform with synthetic data and a generated credential: `/healthz` over loopback.
- Windows private-input policy: a restricted temporary directory, plus checks that a private file is accepted and a
  file readable by `Everyone` is refused.
- Frontend unit suites per platform; both Playwright suites with Chromium on Linux x64 (73 student and 125 proctor
  tests).
- Archive validation as described above, so a missing file, an unexpected architecture or a lost executable mode
  fails the job instead of producing a green result.

## What the earlier failing runs established

The pipeline was green only after four rounds, and each cause is worth keeping:

- [40724847477](https://github.com/ideras/cbe-platform/actions/runs/40724847477) and
  [40734674992](https://github.com/ideras/cbe-platform/actions/runs/40734674992) were early hosted rounds. Both macOS
  jobs failed the thin-binary `lipo` check because the input filename followed `-verify_arch`; Windows failed server
  smoke, storage and exam-document tests on drive-path URLs and checkout line endings; in the earlier of the two,
  macOS ARM64 was cancelled before a runner was assigned, so universal packaging had no slice to download. Later
  rounds passed Linux and macOS, leaving the Windows and ARM64 failures below.
- [40757761938](https://github.com/ideras/cbe-platform/actions/runs/40757761938) fixed the ARM64 hangs: the proctor
  dashboard's disconnect test parked an HTTP handler on a channel send nobody drained after the test body finished.
  The poll loop's post-connection requests filled the buffer, the parked handler kept the connection active, and
  the cleanup's `server.Close` waited out the 20-minute timeout on the slower ARM64 runners. The handler now
  counts requests without ever blocking.
- The same round addressed Windows ARM64 rejecting token files whose DACL only inherited the surrounding
  directory: Windows 11 profiles grant application-container SIDs, which the strict credential policy refuses.
  Every fixture and helper that creates a private input now sets an explicit private ACL.
- [40770853562](https://github.com/ideras/cbe-platform/actions/runs/40770853562) fixed the last Windows x64 failure:
  `scripts/manual_exam.py` wrote `roster.csv` with a raw `os.open` instead of `private_write`, so the one file the
  input policy also reads never received the private ACL. The Windows ARM64 path additionally verifies its own
  restricted file with the same rule the server applies and falls back to a deterministic single-ACE protected
  DACL when the inherited template survives `icacls`. Both checkers now name the offending SID, owner or ACE in the
  rejection, which is what made each remaining failure identifiable from the message alone.

## Unresolved and manual

The Windows server checks the token file's ACL instead of Unix mode bits; Linux and macOS keep the existing
private-file protection. Real audio device mutation, interactive screen-recording consent, macOS permission
dialogs and multi-monitor capture remain manual checks on real desktops and are deliberately not forced in CI.
Signing, notarization, release publication and automatic updates are separate follow-up work.

Building without CI is also supported and documented in `docs/deployment.md`: `scripts/ci/build.py --cross`
produces a Windows target from a non-Windows host. Binaries built that way are cross-compiled, not natively
executed, so run `scripts/ci/smoke.py` on the target platform before distributing them. Archives built outside CI
record the local toolchain in `manifest.json`, which may differ from the pinned Go 1.26.8 and npm 11.17.0.
