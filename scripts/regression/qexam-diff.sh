#!/usr/bin/env bash
# Phase 8 regression (standing rule 7): the same scripted question-exam session against the exam server of an earlier commit and the one of this tree,
# the two event logs (and everything else the proctor's routes say about it) compared. The diff must be empty, or explained: see docs/workspace/reports/phase-8.md.
#
#   scripts/regression/qexam-diff.sh [BASE_REF]       BASE_REF is the build to compare with (default: the commit this branch left main at)
#
# Needs only Go and git. The earlier build is made in a temporary worktree and removed after.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
cd "$repo"
base="${1:-$(git merge-base HEAD main)}"
work=$(mktemp -d)
trap 'git worktree remove --force "$work/base" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT
echo "== the earlier build: $(git log -1 --format='%h %s' "$base")"
git worktree add --detach "$work/base" "$base" >/dev/null
(cd "$work/base" && CGO_ENABLED=0 go build -trimpath -o "$work/server-base" ./cmd/server)
echo "== this tree: $(git log -1 --format='%h %s' HEAD)$(git diff --quiet HEAD -- . ':!docs' || echo ' (with uncommitted changes)')"
CGO_ENABLED=0 go build -trimpath -o "$work/server-new" ./cmd/server
go build -o "$work/qexam" ./scripts/regression
"$work/qexam" -server-bin "$work/server-base" > "$work/base.json"
"$work/qexam" -server-bin "$work/server-new" > "$work/new.json"
# The driver is run twice on one build to show what is noise: it must be empty, or the comparison means nothing.
"$work/qexam" -server-bin "$work/server-new" > "$work/new2.json"
if ! diff -q "$work/new.json" "$work/new2.json" >/dev/null; then echo "the driver is not deterministic on one build:"; diff "$work/new.json" "$work/new2.json" | head -20; exit 2; fi
steps=$(grep -c '"step":' "$work/new.json")
echo "== $steps steps, $(wc -l < "$work/new.json") lines of record"
if diff -u "$work/base.json" "$work/new.json" > "$work/diff.txt"; then
  echo "== the two event logs are identical"
else
  echo "== the logs differ:"; cat "$work/diff.txt"
  [ -n "${QEXAM_KEEP:-}" ] && cp "$work/base.json" "$QEXAM_KEEP.base.json" && cp "$work/new.json" "$QEXAM_KEEP.new.json"
  exit 1
fi
[ -n "${QEXAM_KEEP:-}" ] && cp "$work/new.json" "$QEXAM_KEEP.json"
exit 0
