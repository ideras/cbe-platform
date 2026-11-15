#!/usr/bin/env bash
# Phase 8: fuzz every decoder the student or the agent can reach, for $1 (default 5m) each, two at a time.
#
#   deploy/workspace/fuzz.sh [duration]       logs to the standard output; a failing input is written under testdata/fuzz/ of the package
#
# The targets (what each stands for is in the comment above it):
#   internal/agentproto   FuzzDecodeRequest   the agent's decoder of requests (also the exam server's, for what the student sends)
#                         FuzzReadFrame       the frames of the agent's socket
#                         FuzzCheckPath       the rule for a path under /work
#   internal/agent        FuzzFilePaths       every file operation, on paths and content a fuzzer makes up, with links that lead outside /work
#   internal/httpapi      FuzzStudentMessages the WebSocket the student's application opens, through the real relay and the real agent
#                         FuzzHostileAgent    an agent that says whatever the fuzzer invents, through the real relay
#   apps/student          FuzzWorkspaceServerMessage, FuzzWorkspacePageRequest   the student application's two readers
set -u
cd "$(dirname "$0")/../.."
duration="${1:-5m}"
parallel="${CBE_FUZZ_PARALLEL:-3}"
status=0
run() { # directory, package, target
  local dir="$1" pkg="$2" target="$3"
  echo "== $target ($pkg) $duration  start $(date +%T)"
  (cd "$dir" && go test "$pkg" -run '^$' -fuzz "^${target}\$" -fuzztime "$duration" -parallel "$parallel" 2>&1 | tail -4) || status=1
  echo "== $target done $(date +%T)"
}
{
  run . ./internal/agentproto FuzzDecodeRequest
  run . ./internal/agentproto FuzzReadFrame
  run . ./internal/agentproto FuzzCheckPath
  run apps/student ./internal/client FuzzWorkspaceServerMessage
} > >(sed 's/^/[lane 1] /') 2>&1 &
lane1=$!
{
  run . ./internal/httpapi FuzzStudentMessages
  run . ./internal/httpapi FuzzHostileAgent
  run . ./internal/agent FuzzFilePaths
  run apps/student ./internal/client FuzzWorkspacePageRequest
} > >(sed 's/^/[lane 2] /') 2>&1 &
lane2=$!
wait "$lane1" "$lane2"
# A failure leaves its input under testdata/fuzz.
if find . -path ./node_modules -prune -o -path '*/testdata/fuzz/Fuzz*' -newer "$0" -type f -print 2>/dev/null | grep -q .; then
  echo "A fuzz target failed: see the files above under testdata/fuzz/"; status=1
fi
exit "$status"
