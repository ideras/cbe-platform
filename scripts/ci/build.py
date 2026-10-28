#!/usr/bin/env python3
"""Build the three applications on a native host; outputs are intermediate binaries."""

import argparse
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
WAILS = "github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.29"


def run(args, *, cwd=ROOT, env=None, capture=False):
    executable = shutil.which(args[0])
    if not executable:
        raise RuntimeError(f"missing command: {args[0]}")
    print("+", " ".join(args), flush=True)
    return subprocess.run([executable, *args[1:]], cwd=cwd, env=env, check=True,
                          text=True, stdout=subprocess.PIPE if capture else None).stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--os", required=True, choices=["linux", "windows", "darwin"])
    parser.add_argument("--arch", required=True, choices=["amd64", "arm64"])
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    go_env = run(["go", "env", "GOHOSTOS", "GOHOSTARCH", "GOOS", "GOARCH"], capture=True).split()
    node_arch = run(["node", "-p", "process.arch"], capture=True).strip()
    runner_arch = os.environ.get("RUNNER_ARCH", "(local)")
    print(f"RUNNER_ARCH={runner_arch}; Go={go_env}; Node={node_arch}", flush=True)
    expected_node = {"amd64": "x64", "arm64": "arm64"}[args.arch]
    if go_env != [args.os, args.arch, args.os, args.arch] or node_arch != expected_node:
        raise RuntimeError("Go host/target or Node architecture does not match requested native target")
    if runner_arch != "(local)" and runner_arch.upper() != {"amd64": "X64", "arm64": "ARM64"}[args.arch]:
        raise RuntimeError("runner architecture does not match requested target")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    suffix = ".exe" if args.os == "windows" else ""
    env = os.environ.copy()
    env["CGO_ENABLED"] = "0"
    run(["go", "build", "-trimpath", "-o", str(output / ("cbe-portal-server" + suffix)), "./cmd/server"], env=env)
    env["CGO_ENABLED"] = "1" if args.os != "windows" else "0"
    for app in ("student", "proctor"):
        cwd = ROOT / "apps" / app
        run(["go", "run", WAILS, "generate", "bindings", "-b", "-f", "-tags bindings", "./cmd/" + app], cwd=cwd, env=env)
        run(["npm", "--prefix", "frontend", "ci"], cwd=cwd, env=env)
        run(["npm", "--prefix", "frontend", "run", "build"], cwd=cwd, env=env)
        run(["go", "build", "-trimpath", "-tags", "production", "-o", str(output / ("cbe-portal-" + app + suffix)), "./cmd/" + app], cwd=cwd, env=env)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.CalledProcessError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
