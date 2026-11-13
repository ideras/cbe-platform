# Phase 0 report: discovery

**Date:** 2026-11-13. **Branch:** `feat/workspace-phase-0-discovery`. **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** **closed on 2026-11-13**, with one task still open that the plan lists but its acceptance does not: the real reboot of the host
(see "Still open"). The owner confirmed the runtime and the decisions the same day.

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

- **The five spike checks** (limits, disk quota, pause, stats, archive), plus logout, a simulated reboot, the agent socket, neighbour
  isolation, controller delegation and the API: **all pass** (final run, `spike/results/run-2.log`). The first run had four
  failures, explained in `DISCOVERY.md` section 3 and kept in `run-1.log`.
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

## Still open

- **The real reboot (P0-12, P0-27).** The plan's Phase 0 task asks to confirm containers survive a host reboot. The owner agreed and
  asked to be the one who reboots. `spike/reboot-check.sh prep` has been run; after the reboot, `spike/reboot-check.sh verify` gives
  the answer. It is not an acceptance criterion, so it did not hold Phase 0 open, but it is the last check on the fstab line and the
  boot order (P0-4).
- **Phase 1 is on hold at the owner's request** until the real reboot has been tested ("Don't start Phase 1, allow me to restart and test").
- **Q2 and Q5 were not answered.** Defaults are in force until the owner says otherwise (`DECISIONS.md`, "Not answered yet").
