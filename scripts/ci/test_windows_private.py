"""Exercise private and exposed Windows ACLs with the Python input reader."""
from pathlib import Path
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from create_exam import private_file


def main():
    with tempfile.TemporaryDirectory(prefix="cbe-acl-test-") as directory:
        path = Path(directory) / "input"
        path.write_bytes(b"synthetic")
        if private_file(path) != b"synthetic":
            raise AssertionError("private input did not round trip")
        subprocess.run(["icacls", str(path), "/grant:r", "*S-1-1-0:(R)"], check=True)
        try:
            private_file(path)
        except ValueError:
            return
        raise AssertionError("input readable by Everyone was accepted")


if __name__ == "__main__":
    main()
