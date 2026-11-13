# Phase 0 report: discovery

**Date:** 2026-11-13. **Branch:** `feat/workspace-phase-0-discovery`. **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** **closed on 2026-11-13.** The owner confirmed the runtime and the decisions the same day, and the real reboot of the host
passed (`spike/results/reboot-real-run-1.log`).

## What changed

Documentation, a throwaway spike and four copied documents. No product code, no existing file was modified.

| Path | What |
|------|------|
| `docs/workspace/DISCOVERY.md` | The deliverable: host facts, spike results, findings, repo map, the `examdoc` decision, sizing, baseline, open questions |
| `docs/workspace/DECISIONS.md` | Decisions the plan does not settle (`P0-1` to `P0-16`) |
| `docs/workspace/reports/phase-0.md` | This report |
| `docs/workspace/spike/` | The spike, its own Go module nothing imports: driver, host setup and teardown, quota helper prototype, sizing script, `reboot-check.sh` for the real reboot, raw results |
| `docs/THEME-PROMPT-proctor-student.md`, `docs/THEME-PROMPT-v2-amendment.md`, `docs/tools/TEST-FOCUS.md`, `docs/tools/STUDENT-NOTICE.md` | Copied byte for byte from the sibling repository at the owner's request (`DECISIONS.md` P0-26). New files, not changes to existing ones |

**Changes outside the repository, on this machine** (all reversible with `spike/teardown-host.sh [--purge]`):
Podman 4.9.3 and its dependencies installed (15 packages, nothing removed or upgraded); the system user `cbe-runner` with
lingering; two systemd drop-ins (`user@1001.service.d/delegate.conf`, `user-1001.slice.d/spike-backstop.conf`); a 16 GiB XFS
image at `/data/cbe-platform/work.img`, mounted with project quotas, with a line in `/etc/fstab` (the original is saved as
`/etc/fstab.bak-cbe`); `/usr/local/sbin/cbe-quota-helper`. The owner's rootful Docker and its containers were not touched.

## Tests run and results

- **The five spike checks** (limits, disk quota, pause, stats, archive), plus logout, the agent socket, neighbour
  isolation, controller delegation and the API: **all pass** (final run, `spike/results/run-2.log`). The first run had four
  failures, explained in `DISCOVERY.md` section 3 and kept in `run-1.log`.
- **The real reboot (P0-12, P0-27), run by the owner at 14:06:** **pass**, all seven lines (`spike/results/reboot-real-run-1.log`). The boot
  ID changed; the XFS volume mounted by itself with project quotas; `user@1001.service` was up with no login session for `cbe-runner`;
  the container with `RestartPolicy=always` came back and its start count went from 1 to 2 with `/work` intact. This closes the one
  Phase 0 task that was still open, and checks the fstab line and the boot order (P0-4).
- **Sizing:** one compile, 8 and 40 at once, autosave latency, idle density, snapshot cost (`DISCOVERY.md` section 7).
- **Baseline of the existing code** (`spike/results/baseline-summary.log`): `scripts/ci/test.py --race` over 7 modules **pass**; student unit
  45/45; proctor unit 74/74; student Playwright 131 passed; proctor Playwright 294 passed.
- `go vet` of the spike module: clean. No tests were written for the spike; it is not product code.

## Acceptance (plan, Phase 0)

- [x] `DISCOVERY.md` committed, with the spike results, the chosen runtime and the sizing estimate.
- [x] The spike program is not merged into the product code; it is under `docs/workspace/spike/` as a separate module.
- [x] Baseline tests recorded.
- [x] **The owner has confirmed the chosen runtime, and confirmed or changed D6 to D9, D12, D13 and D14.** Done on 2026-11-13
      (`DECISIONS.md`, `P0-17` to `P0-21`): rootless Podman; D6, D7, D9, D12, D13 and D14 as the plan states them; D8 with the
      interval made configurable.

I ticked Phase 0 in Appendix D of the plan, and moved D6 to D14 to "Resolved (owner, 2026-11-13)" (the owner confirmed the moby client, D14 with its corrections and D8's placement in a second message, `P0-28`), after the owner's message of that
day. The plan file is not tracked by git (`.agents` is in `.git/info/exclude`), so that change is on disk only.

## Where things stand

- **Phase 1 has not started.** The owner asked it to wait until the real reboot had been tested ("Don't start Phase 1, allow me to
  restart and test"). The test has passed; Phase 1 starts when the owner says so.
- **Q2 and Q5 were answered later the same day** (`DECISIONS.md`, P0-29 and P0-30): keep the host setup; the `student` user is uid and gid
  1000. The uid check found no conflict on the host, but `ubuntu:24.04` already has a user `ubuntu` at 1000:1000, so the Phase 2 image must rename
  it to `student`, not add a second user.
