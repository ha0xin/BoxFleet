#!/usr/bin/env python3
"""Fetch pinned reference files; verify bytes without changing app dependencies."""

from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import shutil
import time
from urllib.request import Request, urlopen


ROOT = Path(__file__).resolve().parents[1]
MANIFEST = ROOT / "docs/references/telemetry-prior-art-manifest.json"
DEST = ROOT / "refs/telemetry-prior-art"


def fetch_file(item):
    project, record = item
    target = DEST / project["repository"] / record["path"]
    if target.exists() and hashlib.sha256(target.read_bytes()).hexdigest() == record["sha256"]:
        return
    url = f'https://raw.githubusercontent.com/{project["repository"]}/{project["commit"]}/{record["path"]}'
    for attempt in range(3):
        try:
            request = Request(url, headers={"User-Agent": "BoxFleet-prior-art-reference-fetch"})
            with urlopen(request, timeout=20) as response:
                data = response.read()
            if hashlib.sha256(data).hexdigest() != record["sha256"]:
                raise ValueError(f"SHA-256 mismatch: {record['url']}")
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
            return
        except (OSError, ValueError):
            if attempt == 2:
                raise
            time.sleep(0.5 * (attempt + 1))


def main():
    DEST.mkdir(parents=True, exist_ok=True)
    # Go ignores Git exclusions: a nested module keeps third-party reference
    # packages out of the application module's `go test ./...` and `go mod tidy`.
    (DEST / "go.mod").write_text("module boxfleet-prior-art-references\n\ngo 1.26.3\n")
    manifest = json.loads(MANIFEST.read_text())
    items = [(project, record) for project in manifest["projects"] for record in project["files"]]
    with ThreadPoolExecutor(max_workers=4) as executor:
        list(executor.map(fetch_file, items))
    poc = DEST / "poc"
    poc.mkdir(parents=True, exist_ok=True)
    for filename in ("go.mod", "queue_test.go"):
        shutil.copyfile(ROOT / "docs/references/telemetry-queue-poc" / filename, poc / filename)
    implementation = poc / "internal/diskqueue/diskqueue.go"
    implementation.parent.mkdir(parents=True, exist_ok=True)
    # Only copied into the ignored reference experiment, never an app package.
    if implementation.is_symlink():
        implementation.unlink()
    shutil.copyfile(DEST / "nsqio/go-diskqueue/diskqueue.go", implementation)
    print(f"Verified {len(items)} files from {len(manifest['projects'])} pinned repositories.")
    print("Run: go -C refs/telemetry-prior-art/poc test -v ./...")


if __name__ == "__main__":
    main()
