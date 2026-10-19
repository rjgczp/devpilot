#!/usr/bin/env python3
"""Reject audit build drift before running the isolated workload.

Both executables must retain inspectable Go build settings. Ordinary tests and
race tests are independent of this audit-only, pure-Go build contract.
"""
import hashlib
import json
from pathlib import Path
import subprocess
import sys


def check(binary):
    metadata = subprocess.run(
        ["go", "version", "-m", binary], check=True, text=True, capture_output=True
    ).stdout
    settings = {}
    for line in metadata.splitlines():
        fields = line.split()
        if len(fields) == 2 and fields[0] == "build" and "=" in fields[1]:
            key, value = fields[1].split("=", 1)
            if key in settings:
                raise ValueError(f"{binary}: duplicate build setting {key}")
            settings[key] = value
    for key, expected in (("CGO_ENABLED", "0"), ("GOOS", "linux")):
        if settings.get(key) != expected:
            raise ValueError(f"{binary}: requires {key}={expected}")
    return {"binary": binary, "sha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
            "metadata": metadata}


def main():
    if len(sys.argv) != 3:
        raise ValueError("provide both integration.test and pinned Goose")
    print(json.dumps([check(binary) for binary in sys.argv[1:]], indent=2))


if __name__ == "__main__":
    main()
