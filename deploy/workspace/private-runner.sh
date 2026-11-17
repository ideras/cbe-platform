# Sourced by test-load.sh and test-security.sh: a PRIVATE cbe-runner (its own socket, secret and folders; the same container runtime and quota
# helper as the installed one), so that the installed runner and whatever containers anyone has made with it are not touched and no service is
# restarted. test-grading.sh and test-image.sh do the same by hand.
#
#   private_runner_start WORKDIR      builds the runner into WORKDIR, writes its configuration there and starts it as cbe-runner;
#                                     sets PR_TAG, PR_SOCKET, PR_SECRET and PR_PID
#   private_runner_stop               stops it, removes its folders, and removes the containers it made (whoever's they were) and nothing before it
#
# Needs deploy/workspace/install.sh to have been run. Uses sudo. The variables repo, RUNNER, RUID and BASE must be set by the caller.
runtime() { sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUID" podman "$@"; }

private_runner_start() {
  local work="$1"
  PR_WORK="$work"
  PR_TAG="e2e-$$"
  PR_SOCKET="$BASE/ipc/runner-$PR_TAG.sock"
  PR_SECRET="$BASE/ipc/runner-$PR_TAG.secret"
  PR_BEFORE=$(runtime ps -a --format '{{.Names}}' | sort)
  PR_BEFORE_WORK=$(sudo -n ls "$BASE/work" | sort)
  (cd "$repo" && go build -trimpath -o "$work/cbe-runner" ./cmd/cbe-runner && CGO_ENABLED=0 go build -trimpath -o "$work/cbe-agent" ./cmd/cbe-agent)
  python3 - "$work/runner.json" "$BASE" "$PR_TAG" "$work" <<'PY'
import json, os, sys
out, base, tag, work = sys.argv[1:]
cfg = json.load(open('/etc/cbe-platform/runner.json'))
# agent_binary: the agent the runner puts in the images of exams that bring a base of their own.
cfg.update({'socket': f'{base}/ipc/runner-{tag}.sock', 'secret_file': f'{base}/ipc/runner-{tag}.secret', 'state_root': f'{base}/runner-{tag}',
            'run_root': f'{base}/run-{tag}', 'grade_root': f'{base}/grading-{tag}', 'agent_binary': f'{work}/cbe-agent', 'custom_base': True})
# PR_IMAGES="name=reference,name=reference": more course images this runner approves (an image that `cbe-portalctl image install` made).
for pair in filter(None, os.environ.get('PR_IMAGES', '').split(',')):
    name, reference = pair.split('=', 1)
    cfg.setdefault('images', {})[name] = reference
json.dump(cfg, open(out, 'w'), indent=2)
PY
  head -c 32 /dev/urandom | base64 | tr -d '\n=' > "$work/secret"
  sudo -n install -o "$RUNNER" -g cbe-ipc -m 0640 "$work/secret" "$PR_SECRET"
  sudo -n install -d -o "$RUNNER" -g "$RUNNER" -m 0700 "$BASE/runner-$PR_TAG" "$BASE/run-$PR_TAG" "$BASE/grading-$PR_TAG"
  chmod -R a+rX "$work"; chmod a+rx "$work"
  sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUID" nohup "$work/cbe-runner" -config "$work/runner.json" > "$work/runner.log" 2>&1 &
  for _ in $(seq 1 50); do sudo -n test -S "$PR_SOCKET" && break; sleep 0.2; done
  sudo -n test -S "$PR_SOCKET" || { cat "$work/runner.log" >&2; echo "the private runner did not start" >&2; return 1; }
  PR_PID=$(pgrep -u "$RUNNER" -f "^$work/cbe-runner -config" | head -1)
}

private_runner_stop() {
  [ -n "${PR_TAG:-}" ] || return 0
  sudo -n pkill -u "$RUNNER" -f "^$PR_WORK/cbe-runner -config" || true
  sleep 1
  for name in $(runtime ps -a --format '{{.Names}}' | sort | comm -13 <(echo "$PR_BEFORE") -); do runtime rm -f "$name" >/dev/null 2>&1 || true; done
  # A /work folder the runner did not get to remove (a test that died): it goes too, with what is in it (its project quota stays, harmless).
  for name in $(sudo -n ls "$BASE/work" | sort | comm -13 <(echo "$PR_BEFORE_WORK") -); do sudo -n rm -rf "$BASE/work/$name"; done
  sudo -n rm -rf "$BASE/runner-$PR_TAG" "$BASE/run-$PR_TAG" "$BASE/grading-$PR_TAG" "$PR_SOCKET" "$PR_SECRET" || true
}
