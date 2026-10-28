#!/usr/bin/env python3
"""Import an exam into a class without exposing credentials in command-line arguments."""

import argparse
import json
import os
from pathlib import Path
import stat
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None


def private_file(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError("Input files must be regular, private files (chmod 600).")
    with path.open("rb") as source:
        opened = os.fstat(source.fileno())
        if not stat.S_ISREG(opened.st_mode):
            raise ValueError("Input files must be regular, private files.")
        if os.name == "nt":
            from windows_private import check_private
            check_private(source)
        elif opened.st_mode & 0o077:
            raise ValueError("Input files must be regular, private files (chmod 600).")
        return source.read()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server", default="http://127.0.0.1:8080")
    parser.add_argument("--admin-token-file", type=Path, default=Path("data/admin-token"))
    parser.add_argument("--exam-file", type=Path, required=True)
    parser.add_argument("--class-id", required=True, help="the class the exam is imported into, where it stays")
    args = parser.parse_args()
    try:
        if str(uuid.UUID(args.class_id)) != args.class_id:
            raise ValueError
    except ValueError:
        parser.error("class id must be a lowercase UUID")
    url = urllib.parse.urlsplit(args.server)
    if url.scheme not in ("http", "https") or not url.hostname or url.username or url.password or url.query or url.fragment or url.path not in ("", "/"):
        parser.error("server must be an HTTP(S) origin without credentials, path, or query")
    try:
        token = private_file(args.admin_token_file).decode("utf-8").strip()
        if len(token) < 32 or any(char.isspace() for char in token):
            raise ValueError("Invalid admin token file.")
        body = private_file(args.exam_file)
        request = urllib.request.Request(
            args.server.rstrip("/") + "/admin/class/" + args.class_id + "/exam", data=body, method="POST",
            headers={"Content-Type": "application/json", "Authorization": "Bearer " + token},
        )
        # Do not forward bearer credentials to redirects or environment proxies.
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        with opener.open(request, timeout=10) as response:
            if response.status != 201:
                raise ValueError("Unexpected exam-creation response.")
            result = json.loads(response.read(16384))
            exam_id = str(uuid.UUID(result["exam_id"]))
        print(exam_id)
        return 0
    except urllib.error.HTTPError as error:
        print("Exam creation rejected (HTTP %d)." % error.code, file=sys.stderr)
    except (urllib.error.URLError, TimeoutError):
        print("Network failure; outcome may be unknown. Check the server before retrying.", file=sys.stderr)
    except (OSError, ValueError, KeyError, TypeError):
        print("Invalid input file or server response; credentials and payload were not logged.", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
