#!/usr/bin/env python3
"""Start a built server with synthetic private data and check loopback health."""
import argparse
import os
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

if os.name == "nt":
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
    from windows_private import restrict_private


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix="cbe-ci-") as temporary:
        directory = Path(temporary)
        token = directory / "token"
        token.write_text(secrets.token_urlsafe(48), encoding="utf-8")
        token.chmod(0o600)
        if os.name == "nt":
            # Windows ignores the chmod; replace the inherited ACL so the server accepts the token on every image.
            restrict_private(token)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        command = [str(binary), "-bind", f"127.0.0.1:{port}", "-database", str(directory / "db"),
                   "-screenshots", str(directory / "images"), "-admin-token-file", str(token)]
        with tempfile.TemporaryFile(mode="w+t", encoding="utf-8") as log:
            process = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT)
            try:
                deadline = time.monotonic() + 30
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                while True:
                    if process.poll() is not None:
                        raise RuntimeError("server exited before health check")
                    try:
                        with opener.open(f"http://127.0.0.1:{port}/healthz", timeout=1) as response:
                            if response.status == 200:
                                print("server health check passed")
                                break
                    except OSError:
                        pass
                    if time.monotonic() >= deadline:
                        raise RuntimeError("server health check timed out")
                    time.sleep(0.1)
            except Exception:
                log.seek(0)
                print(log.read())
                raise
            finally:
                if process.poll() is None:
                    process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    main()
