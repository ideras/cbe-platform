#!/usr/bin/env python3
"""Run Go tests and vet for all modules with a built-server integration fixture."""
import argparse
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
MODULES = (".", "shared/calc", "shared/clientwire", "shared/examdoc", "shared/examimage", "apps/student", "apps/proctor")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server", type=Path, required=True)
    parser.add_argument("--race", action="store_true")
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    server = args.server.resolve()
    if not server.is_file():
        raise SystemExit(f"missing integration server: {server}")
    args.report.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env["EXAM_SERVER_BINARY"] = str(server)
    failures = []
    for module in MODULES:
        for command in (["go", "vet", "./..."], ["go", "test", "-timeout", "20m", *(["-race"] if args.race else []), "./..."]):
            name = ("root" if module == "." else module.replace("/", "-")) + "-" + command[1]
            print(f"+ {module}: {' '.join(command)}", flush=True)
            with (args.report / (name + ".log")).open("w", encoding="utf-8") as log:
                process = subprocess.Popen(command, cwd=ROOT / module, env=env, stdout=subprocess.PIPE,
                                           stderr=subprocess.STDOUT, text=True, errors="replace")
                for line in process.stdout:
                    print(line, end="", flush=True)
                    log.write(line)
                    log.flush()
                code = process.wait()
            if code:
                failures.append(name)
    if failures:
        print("Failed: " + ", ".join(failures), file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
