#!/usr/bin/env python3
"""Validate and archive native Linux/Windows binaries as separate artifacts."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parents[2]
APPS = ("server", "student", "proctor")


def version(*args):
    return subprocess.check_output(args, text=True).strip()


def check_arch(path, target, arch):
    data = path.read_bytes()
    if target == "linux":
        if data[:4] != b"\x7fELF" or data[4] != 2 or data[5] != 1:
            raise ValueError(f"{path}: expected little-endian ELF64")
        machine = struct.unpack_from("<H", data, 18)[0]
        expected = {"amd64": 62, "arm64": 183}[arch]
    elif target == "windows":
        if data[:2] != b"MZ":
            raise ValueError(f"{path}: expected PE executable")
        pe = struct.unpack_from("<I", data, 60)[0]
        if data[pe:pe + 4] != b"PE\0\0":
            raise ValueError(f"{path}: invalid PE header")
        machine = struct.unpack_from("<H", data, pe + 4)[0]
        expected = {"amd64": 0x8664, "arm64": 0xaa64}[arch]
    else:
        raise ValueError(target)
    if machine != expected:
        raise ValueError(f"{path}: machine {machine:#x}, expected {expected:#x}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--os", choices=("linux", "windows"), required=True)
    parser.add_argument("--arch", choices=("amd64", "arm64"), required=True)
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    user_arch = {"amd64": "x64", "arm64": "arm64"}[args.arch]
    suffix = ".exe" if args.os == "windows" else ""
    manifest = {"commit": version("git", "rev-parse", "HEAD"), "os": args.os,
                "architectures": [args.arch], "go": version("go", "version"),
                "node": version("node", "--version"), "npm": version("npm", "--version"),
                "wails": "v3.0.0-beta.29", "signing": "unsigned"}
    notice = ROOT / "apps/proctor/frontend/public/lucide-LICENSE.txt"
    for app in APPS:
        binary = args.input / (f"cbe-portal-{app}" + suffix)
        check_arch(binary, args.os, args.arch)
        if args.os == "linux":
            binary.chmod(0o755)
        name = f"cbe-portal-{app}-{args.os}-{user_arch}"
        manifest_bytes = (json.dumps(manifest, indent=2) + "\n").encode()
        if args.os == "windows":
            archive = args.output / (name + ".zip")
            with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as out:
                out.write(binary, binary.name)
                out.writestr("manifest.json", manifest_bytes)
                if app == "proctor":
                    out.write(notice, notice.name)
            with zipfile.ZipFile(archive) as check:
                assert check.testzip() is None and binary.name in check.namelist()
        else:
            import io
            archive = args.output / (name + ".tar.gz")
            with tarfile.open(archive, "w:gz") as out:
                out.add(binary, arcname=binary.name)
                info = tarfile.TarInfo("manifest.json")
                info.size, info.mode = len(manifest_bytes), 0o644
                out.addfile(info, io.BytesIO(manifest_bytes))
                if app == "proctor":
                    out.add(notice, arcname=notice.name)
            with tarfile.open(archive) as check:
                member = check.getmember(binary.name)
                assert member.isfile() and member.mode & 0o111
        checksum = hashlib.sha256(archive.read_bytes()).hexdigest()
        (args.output / (archive.name + ".sha256")).write_text(f"{checksum}  {archive.name}\n", encoding="utf-8")
        print(f"validated {archive.name}: {checksum}")


if __name__ == "__main__":
    main()
