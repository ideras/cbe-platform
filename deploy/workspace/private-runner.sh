# Sourced by the tests of this folder: a PRIVATE cbe-runner (its own socket, secret and folders; the same container runtime and quota helper as the
# installed one), so that the installed runner and whatever containers anyone has made with it are not touched and no service is restarted.
#
#   private_runner_start WORKDIR      builds the runner into WORKDIR, writes its settings there (the installed runner's, with a socket, a secret and
#                                     folders of its own) and starts it as the runner's account; sets PR_TAG, PR_SOCKET, PR_SECRET and PR_PID
#   private_runner_stop               stops it, removes its folders, and removes the containers it made (whoever's they were) and nothing before it
#
#   PR_IMAGES="name=reference,..."    more course images it approves (an image that `cbe-portalctl image install` made into a scratch installation)
#   PR_EXTRA='{"key": value}'         settings of its own on top of the installed ones (grade_parallel, build_memory_mb...)
#
# host.sh must be sourced first. Uses sudo.

private_runner_start() {
  local work="$1"
  PR_WORK="$work"
  PR_TAG="e2e-$$"
  PR_SOCKET="$IPC_DIR/runner-$PR_TAG.sock"
  PR_SECRET="$IPC_DIR/runner-$PR_TAG.secret"
  PR_BEFORE=$(runtime ps -a --format '{{.Names}}' | sort)
  PR_BEFORE_WORK=$(sudo -n ls "$WORK_ROOT" | sort)
  (cd "$repo" && go build -trimpath -o "$work/cbe-runner" ./cmd/cbe-runner && CGO_ENABLED=0 go build -trimpath -o "$work/cbe-agent" ./cmd/cbe-agent)
  python3 - "$SETTINGS" "$work/runner.json" "$DATA_DIR" "$IPC_DIR" "$PR_TAG" "$work" <<'PY'
import json, os, sys
settings, out, data, ipc, tag, work = sys.argv[1:]
cfg = json.load(open(settings))
# agent_binary: the agent the runner puts in the images of exams that bring a base of their own: this tree's.
cfg.update({'socket': f'{ipc}/runner-{tag}.sock', 'secret_file': f'{ipc}/runner-{tag}.secret', 'state_root': f'{data}/runner-{tag}',
            'run_root': f'{data}/run-{tag}', 'grade_root': f'{data}/grading-{tag}', 'agent_binary': f'{work}/cbe-agent', 'custom_base': True})
for pair in filter(None, os.environ.get('PR_IMAGES', '').split(',')):
    name, reference = pair.split('=', 1)
    cfg.setdefault('images', {})[name] = reference
cfg.update(json.loads(os.environ.get('PR_EXTRA') or '{}'))
json.dump(cfg, open(out, 'w'), indent=2)
PY
  head -c 32 /dev/urandom | base64 | tr -d '\n=' > "$work/secret"
  sudo -n install -o "$RUNNER" -g "$IPC_GROUP" -m 0640 "$work/secret" "$PR_SECRET"
  sudo -n install -d -o "$RUNNER" -g "$RUNNER" -m 0700 "$DATA_DIR/runner-$PR_TAG" "$DATA_DIR/run-$PR_TAG" "$DATA_DIR/grading-$PR_TAG"
  chmod -R a+rX "$work"; chmod a+rx "$work"
  sudo -n -u "$RUNNER" env HOME="$RUNNER_HOME" XDG_RUNTIME_DIR="/run/user/$RUID" nohup "$work/cbe-runner" -config "$work/runner.json" > "$work/runner.log" 2>&1 &
  for _ in $(seq 1 50); do sudo -n test -S "$PR_SOCKET" && break; sleep 0.2; done
  sudo -n test -S "$PR_SOCKET" || { cat "$work/runner.log" >&2; echo "the private runner did not start" >&2; return 1; }
  PR_PID=$(pgrep -u "$RUNNER" -f "^$work/cbe-runner -config" | head -1)
}

# What the private runner left (a test that died) goes, and only that: the installed runner serves the same runtime and the same volume, so a
# container of a session or a grading it has a note of is never removed, and a /work folder is removed only when the private runner has a note of
# its session or its grading.
private_runner_stop() {
  [ -n "${PR_TAG:-}" ] || return 0
  sudo -n pkill -u "$RUNNER" -f "^$PR_WORK/cbe-runner -config" || true
  sleep 1
  local installed mine name session grading
  installed=$(setting state_root)
  mine="$DATA_DIR/runner-$PR_TAG"
  for name in $(runtime ps -a --format '{{.Names}}' | sort | comm -13 <(echo "$PR_BEFORE") -); do
    session=$(runtime inspect --format '{{index .Config.Labels "cbe.session"}}' "$name" 2>/dev/null || true)
    grading=$(runtime inspect --format '{{index .Config.Labels "cbe.grading"}}' "$name" 2>/dev/null || true)
    if { [ -n "$session" ] && sudo -n test -e "$installed/$session.json"; } || { [ -n "$grading" ] && sudo -n test -e "$installed/grading/$grading.json"; }; then
      continue
    fi
    runtime rm -f "$name" >/dev/null 2>&1 || true
  done
  for name in $(sudo -n ls "$WORK_ROOT" | sort | comm -13 <(echo "$PR_BEFORE_WORK") -); do
    if sudo -n test -e "$mine/$name.json" || { [ "${name#grade-}" != "$name" ] && sudo -n test -e "$mine/grading/${name#grade-}.json"; }; then
      sudo -n rm -rf "${WORK_ROOT:?}/$name"
    fi
  done
  sudo -n rm -rf "$mine" "$DATA_DIR/run-$PR_TAG" "$DATA_DIR/grading-$PR_TAG" "$PR_SOCKET" "$PR_SECRET" || true
  PR_TAG=
}
