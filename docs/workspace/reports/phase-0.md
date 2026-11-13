# Phase 0 report: discovery

**Date:** 2026-11-13. **Branch:** `feat/workspace-phase-0-discovery`. **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** work done; **not closed**. The last acceptance item needs the owner (below).

## What changed

Documentation and a throwaway spike only. No product code, no existing file was modified.

| Path | What |
|------|------|
| `docs/workspace/DISCOVERY.md` | The deliverable: host facts, spike results, findings, repo map, the `examdoc` decision, sizing, baseline, open questions |
| `docs/workspace/DECISIONS.md` | Decisions the plan does not settle (`P0-1` to `P0-16`) |
| `docs/workspace/reports/phase-0.md` | This report |
| `docs/workspace/spike/` | The spike, its own Go module nothing imports: driver, host setup and teardown, quota helper prototype, sizing script, raw results |

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
- [ ] **The owner has confirmed the chosen runtime, and confirmed or changed D6 to D9, D12, D13 and D14.** Pending: the
      recommendations and the eight new questions are in `DISCOVERY.md` section 9. Per standing rule 2, Phase 1 does not
      start until this is done and Appendix D is ticked.

I did not tick Phase 0 in Appendix D of the plan: the plan file is not tracked by git and the box is the owner's to tick.

## Deviations from the plan, and conflicts found (standing rule 1)

None of these was resolved silently; each is in `DISCOVERY.md`.

1. **Real reboot not done**, only simulated (P0-12). The plan asks to "confirm containers survive a host reboot".
2. **Extra checks** beyond the five (agent socket for D7, isolation of neighbours, density, an inode quota, delegation).
3. **"Stored exams need no migration" (plan 5.4) is true only for old rows.** A `kind` column and workspace tables need a numbered
   migration, and the server validates exam documents with its own code and rejects unknown fields.
4. **`examc` takes a file today**; the plan says it detects a package by `workspace.md`, which needs a folder argument.
5. **No WebSocket dependency or support exists** on the server or in the student client, the HTTP server has 10 s and 15 s
   timeouts, and the student interface has no network access of its own (everything is in the Go client, polled every 500 ms).
   Plan section 6's "one WebSocket per student session" needs a decision on who owns it (Q3, Q4).
6. **Three documents the plan names are not in this repository** (`THEME-PROMPT-*`, `TEST-FOCUS.md`, `STUDENT-NOTICE.md`); the
   owner pointed me to the sibling repo (P0-13).
7. **D14 needs refinements:** archive `/work/.` (not `/work`), `0700` session folders, a tar extractor written for a hostile
   filesystem, and `QUOTA` for both `ENOSPC` and `EDQUOT`. D10's runner also needs OOM detection from `memory.events` because the API flag is never set.
8. The plan contains no item marked `[DECIDE]`; I treated the decisions with status "Proposed" as the open ones.

## Left behind and worth knowing

- **Temporary resource backstop on `cbe-runner`'s slice** (`CPUQuota=400%`, `TasksMax=6000`, `MemoryMax=16G`): remove before production.
- **Probe leftovers** from the sizing run: a stopped container `snapprobe` (it exits by itself after 90 s) and the quota folder
  `snap-probe`. A safety check blocked my cleanup commands, so I left them; they are tiny. To remove them:
  `sudo -u cbe-runner env XDG_RUNTIME_DIR=/run/user/1001 podman rm snapprobe` and `sudo cbe-quota-helper remove snap-probe`.
- The course-sized image `localhost/cbe-spike-cpp:1` (343 MB) in `cbe-runner`'s storage, used by the sizing script.
