#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 3 ]]; then echo "usage: $0 x64-slices arm64-slices output" >&2; exit 2; fi
x64=$1 arm64=$2 out=$3
mkdir -p "$out" build/ci/macos-universal
stage=build/ci/macos-universal
for app in server student proctor; do
  name="cbe-portal-$app"
  chmod 755 "$x64/$name" "$arm64/$name"
  lipo "$x64/$name" -verify_arch x86_64
  lipo "$arm64/$name" -verify_arch arm64
  lipo -create "$x64/$name" "$arm64/$name" -output "$stage/$name"
  chmod 755 "$stage/$name"
  lipo "$stage/$name" -verify_arch x86_64 arm64
done
python3 - "$stage/manifest.json" <<'PY'
import json, os, subprocess, sys
v = lambda *c: subprocess.check_output(c, text=True).strip()
with open(sys.argv[1], "w", encoding="utf-8") as f:
    json.dump({"commit": v("git", "rev-parse", "HEAD"), "os": "macos", "architectures": ["x86_64", "arm64"],
               "go": v("go", "version"), "node": v("node", "--version"), "npm": v("npm", "--version"),
               "wails": "v3.0.0-beta.29", "minimum_macos": os.environ["MACOSX_DEPLOYMENT_TARGET"],
               "signing": "ad-hoc; not notarized"}, f, indent=2)
    f.write("\n")
PY
codesign --force --sign - "$stage/cbe-portal-server"
codesign --verify --strict "$stage/cbe-portal-server"
tar -C "$stage" -czf "$out/cbe-portal-server-macos-universal.tar.gz" cbe-portal-server manifest.json
tar -tzf "$out/cbe-portal-server-macos-universal.tar.gz"
for app in student proctor; do
  binary="cbe-portal-$app"
  bundle="$stage/CBE Portal $app.app"
  mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources"
  cp "$stage/$binary" "$bundle/Contents/MacOS/$binary"
  cp "$stage/manifest.json" "$bundle/Contents/Resources/manifest.json"
  if [[ "$app" == proctor ]]; then cp apps/proctor/frontend/public/lucide-LICENSE.txt "$bundle/Contents/Resources/"; fi
  python3 - "$bundle/Contents/Info.plist" "$binary" "$app" <<'PY'
import os, plistlib, sys
path, binary, app = sys.argv[1:]
plist = {"CFBundleExecutable": binary, "CFBundleIdentifier": "org.cbeportal.ci." + app,
         "CFBundleName": "CBE Portal " + app.title(), "CFBundlePackageType": "APPL",
         "CFBundleShortVersionString": "0.1.0", "CFBundleVersion": "0.1.0",
         "LSMinimumSystemVersion": os.environ["MACOSX_DEPLOYMENT_TARGET"],
         "NSHighResolutionCapable": True,
         "NSScreenCaptureUsageDescription": "Screen capture is used for exam monitoring when enabled."}
with open(path, "wb") as f: plistlib.dump(plist, f)
PY
  plutil -lint "$bundle/Contents/Info.plist"
  test -x "$bundle/Contents/MacOS/$binary"
  lipo "$bundle/Contents/MacOS/$binary" -verify_arch x86_64 arm64
  codesign --force --deep --sign - "$bundle"
  codesign --verify --deep --strict --verbose=2 "$bundle"
  ditto -c -k --sequesterRsrc --keepParent "$bundle" "$out/$binary-macos-universal.zip"
  verify_dir=$(mktemp -d)
  ditto -x -k "$out/$binary-macos-universal.zip" "$verify_dir"
  unpacked="$verify_dir/$(basename "$bundle")"
  test -x "$unpacked/Contents/MacOS/$binary"
  plutil -lint "$unpacked/Contents/Info.plist"
  lipo "$unpacked/Contents/MacOS/$binary" -verify_arch x86_64 arm64
  codesign --verify --deep --strict "$unpacked"
done
for archive in "$out"/*.zip "$out"/*.tar.gz; do
  (cd "$out" && shasum -a 256 "$(basename "$archive")" > "$(basename "$archive").sha256")
done
