#!/usr/bin/env python3
"""Prepare and launch an isolated, synthetic manual exam (never real exam data)."""

import argparse
import csv
import io
import json
import os
from pathlib import Path
import secrets
import stat
import subprocess
import sys
import time
import urllib.parse
import urllib.request
import uuid

from create_exam import NoRedirect, private_file

ROOT = Path(__file__).resolve().parent.parent
DEMO_CLASS = "Demo class"


def server_binary():
    override = os.environ.get("EXAM_SERVER_BINARY")
    return Path(override).absolute() if override else ROOT / "build" / ("cbe-portal-server.exe" if os.name == "nt" else "cbe-portal-server")


def private_write(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8", newline="") as output:
        output.write(data)
    if os.name == "nt":
        # Windows ignores the 0600 mode; replace the inherited ACL so the strict private-input policy holds.
        from windows_private import restrict_private
        restrict_private(path)


def server_args(directory, profile):
    args = [str(server_binary()), "-bind", profile["listen"],
            "-database", str(directory / "exam.db"), "-screenshots", str(directory / "screenshots"),
            "-admin-token-file", str(directory / "admin-token")]
    return args


def initialize(directory, port, duration, grace, volume=None, calculator=True):
    if not (1 <= port <= 65535 and 1 <= duration <= 86400 and 1 <= grace <= 3600):
        raise ValueError("Invalid port, exam duration or grace.")
    if volume is not None and not 0 <= volume <= 100:
        raise ValueError("Invalid exam volume (0-100).")
    if not server_binary().is_file():
        raise ValueError("Build the server first: make server.")
    directory.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    directory.mkdir(mode=0o700)  # Never overwrite or reuse an exam silently.
    token = secrets.token_urlsafe(48)
    private_write(directory / "admin-token", token + "\n")
    rows = [["canvas_id", "account_number", "name", "email", "sis_user_id"]]
    for index, name in enumerate(["Ada Demo", "Bruno Demo", "Carla Demo", "Diego Demo", "Elena Demo"], 1):
        rows.append([str(index), "%04d" % index, name, "demo%d@example.invalid" % index, "DEMO-%d" % index])
    # Every private input goes through private_write, which also replaces the inherited Windows ACL with an explicit
    # private one; a raw os.open here would skip that and the file would be rejected by the input policy.
    roster = io.StringIO()
    csv.writer(roster).writerows(rows)
    private_write(directory / "roster.csv", roster.getvalue())
    # The content is what shared/examdoc compiles from Markdown: HTML with a data-slot placeholder for each answer field.
    exam = {"title": "Manual synthetic exam — no grading", "pin": "demo-only", "duration_seconds": duration,
            "grace_seconds": grace, "content": {
                "instructions": "<p>Synthetic text only. Literal text must stay literal: &lt;script&gt;alert('not HTML')&lt;/script&gt;</p>",
                "exercises": [
                    {"id": "1", "title": "Ejercicio 1 — Transacciones", "points": 10, "items": [
                        {"id": "1a", "title": "a. Conteo", "points": 4,
                         "body": "<table><thead><tr><th>Propiedad</th><th align=\"center\">Letra</th></tr></thead><tbody>"
                                 "<tr><td>Atomicidad</td><td align=\"center\"><span class=\"slot\" data-slot=\"1a.a\"></span></td></tr>"
                                 "<tr><td>Durabilidad</td><td align=\"center\"><span class=\"slot\" data-slot=\"1a.d\"></span></td></tr></tbody></table>",
                         "slots": [{"id": "1a.a", "kind": "blank"}, {"id": "1a.d", "kind": "blank"}]},
                        {"id": "1b", "title": "b. ¿Qué significa durabilidad? 🧪", "points": 6,
                         "body": "<div class=\"slot\" data-slot=\"1b.answer\"></div>", "slots": [{"id": "1b.answer", "kind": "text", "lines": 6}]}]},
                    {"id": "2", "title": "Ejercicio 2 — Código", "points": 10,
                     "body": "<p>Explain an atomic transaction in code comments.</p><div class=\"slot\" data-slot=\"2.code\"></div>",
                     "slots": [{"id": "2.code", "kind": "code", "language": "c", "starter": "/* atomic */\n"}]}]}}
    # The demo exam enables the calculator (both implemented modes, student-selectable angle), so a plain
    # `make demo-setup` is a ready calculator test target; --no-calculator turns it off.
    if calculator:
        exam["tools"] = {"version": 1, "calculator": {"enabled": True, "modes": ["scientific", "programmer"], "angle_mode": "user"}}
    if volume is not None:  # omitted: the server applies its default
        exam["min_volume_percent"] = volume
    private_write(directory / "exam.json", json.dumps(exam, ensure_ascii=False, indent=2) + "\n")
    profile = {"listen": "127.0.0.1:%d" % port, "server": "http://127.0.0.1:%d" % port,
               "duration_seconds": duration, "grace_seconds": grace, "calculator": calculator}
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    private_write(directory / "bootstrap.log", "")
    with (directory / "bootstrap.log").open("ab") as log:
        process = subprocess.Popen(server_args(directory, profile), stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 15
            while True:
                if process.poll() is not None:
                    raise ValueError("Bootstrap server exited; inspect the private bootstrap.log. Use a new demo directory.")
                try:
                    with opener.open(profile["server"] + "/healthz", timeout=0.3) as response:
                        if response.status == 200:
                            break
                except OSError:
                    pass
                if time.monotonic() >= deadline:
                    raise ValueError("Bootstrap timed out; inspect bootstrap.log. Use a new directory.")
                time.sleep(0.05)
            # The five demo students become a class, and the exam is imported into it: it belongs to that class from then on.
            request = urllib.request.Request(profile["server"] + "/admin/classes?name=" + urllib.parse.quote(DEMO_CLASS), data=private_file(directory / "roster.csv"), method="POST",
                                             headers={"Content-Type": "text/csv", "Authorization": "Bearer " + token})
            with opener.open(request, timeout=10) as response:
                if response.status != 201:
                    raise ValueError("Class creation unconfirmed; do not retry in the same directory.")
                profile["class_id"] = str(uuid.UUID(json.loads(response.read(65536))["class"]["class_id"]))
            request = urllib.request.Request(profile["server"] + "/admin/class/" + profile["class_id"] + "/exam", data=private_file(directory / "exam.json"), method="POST",
                                             headers={"Content-Type": "application/json", "Authorization": "Bearer " + token})
            with opener.open(request, timeout=10) as response:
                if response.status != 201:
                    raise ValueError("Exam creation unconfirmed; do not retry in the same directory.")
                result = json.loads(response.read(16384))
                profile["exam_id"] = str(uuid.UUID(result["exam_id"]))
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
    private_write(directory / "profile.json", json.dumps(profile, indent=2) + "\n")
    return profile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["init", "server", "student", "proctor", "info"])
    parser.add_argument("--directory", type=Path, default=ROOT / "data" / "manual-demo")
    parser.add_argument("--port", type=int, default=8787)
    parser.add_argument("--duration", type=int, default=600)
    parser.add_argument("--grace", type=int, default=30)
    parser.add_argument("--volume", type=int, default=None, help="minimum student system volume in percent for the exam (0 leaves it alone)")
    parser.add_argument("--no-calculator", action="store_true", help="prepare the synthetic exam without the calculator")
    args = parser.parse_args()
    directory = args.directory.absolute()
    try:
        if args.command == "init":
            profile = initialize(directory, args.port, args.duration, args.grace, args.volume, calculator=not args.no_calculator)
        else:
            profile = json.loads(private_file(directory / "profile.json"))
        if args.command in ("init", "info"):
            print("Synthetic exam ready in %s" % directory)
            print("Server: %s | Exam: %s" % (profile["server"], profile["exam_id"]))
            print("Fake accounts: 0001–0005 | Fake PIN: demo-only | Duration: %ds + %ds grace" % (profile["duration_seconds"], profile["grace_seconds"]))
            if profile.get("calculator", True):
                print("The exam has the calculator: scientific and programmer, angle selectable. Open it next to the exam title; every evaluation is recorded.")
            else:
                print("The exam has no calculator (pass --no-calculator to make that permanent in a new directory).")
            print("Students can log in once the proctor dashboard publishes the exam (Classes, open \"%s\", Exams tab, Publish)." % DEMO_CLASS)
            print("Separate terminals: make demo-server ; make demo-proctor ; make demo-student")
            print("Custom directory: pass --directory PATH to each script command. See docs/manual-end-to-end.md.")
            return 0
        if args.command == "server":
            command = server_args(directory, profile)
        else:
            binary = {"student": "cbe-portal-student", "proctor": "cbe-portal-proctor"}[args.command]
            if os.name == "nt":
                binary += ".exe"
            command = [str(ROOT / "build" / binary), "-server", profile["server"]]
            if args.command == "proctor":
                command += ["-exam-id", profile["exam_id"], "-admin-token-file", str(directory / "admin-token")]
        os.execv(command[0], command)
    except (OSError, ValueError, KeyError, TypeError) as failure:
        # The failure itself never carries credentials (they travel only in request headers), so report it
        # instead of hiding the phase that failed; otherwise hosted failures are undiagnosable.
        print("Demo setup/launch failed: %s: %s" % (type(failure).__name__, failure), file=sys.stderr)
        log = directory / "bootstrap.log"
        if args.command == "init" and log.is_file():
            print("--- bootstrap.log ---", file=sys.stderr)
            print(log.read_text(encoding="utf-8", errors="replace")[-4000:], file=sys.stderr)
        print("Build binaries first, use a fresh private directory for init, and inspect its bootstrap.log. No credentials were logged.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
