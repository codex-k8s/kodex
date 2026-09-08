#!/usr/bin/env python3
"""Ограниченный staging entrypoint: только Go modules, без Air/render/apply."""

import argparse
import json
import os
from pathlib import Path
import re
import signal
import sys
import time

sys.dont_write_bytecode = True
from go_cache import Failure, Process, exact_path, prime, require


def main():
    parser = argparse.ArgumentParser(description="Prime exact staging Go module cache")
    parser.add_argument("phase", choices=("plan", "prime"))
    parser.add_argument("--profile", required=True, choices=("staging-hot-reload",))
    parser.add_argument("--source-root", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--cache-root", required=True)
    parser.add_argument("--module", action="append", required=True)
    parser.add_argument("--evidence", required=True)
    parser.add_argument("--timeout-seconds", type=int, default=600)
    parser.add_argument("--lock-timeout-seconds", type=int, default=60)
    parser.add_argument("--confirm")
    args = parser.parse_args()
    require(1 <= args.timeout_seconds <= 1800 and 1 <= args.lock_timeout_seconds <= 600, "TIMEOUT_INVALID")
    require(re.fullmatch(r"[a-f0-9]{40}", args.revision), "REVISION_INVALID")
    require(args.confirm == ("PRIME-STAGING-GO-CACHE" if args.phase == "prime" else None), "CONFIRMATION_INVALID")
    evidence = exact_path(args.evidence, exists=False)
    directory = exact_path(str(evidence.parent))
    info = directory.stat()
    require(directory.is_dir() and info.st_uid == os.getuid() and not info.st_mode & 0o077, "PRIVATE_EVIDENCE_DIRECTORY_REQUIRED")
    require(not evidence.exists(), "EVIDENCE_EXISTS")
    fd = os.open(evidence, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    os.fchmod(fd, 0o600)
    def emit(event):
        data = (json.dumps({"at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), **event}, sort_keys=True) + "\n").encode()
        with os.fdopen(os.dup(fd), "ab") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
    def interrupted(_signum, _frame):
        raise Failure("INTERRUPTED")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        emit({"type": "HEADER", "version": 1, "kind": "STAGING_GO_CACHE_PRIME", "phase": args.phase})
        directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
        result = prime(Path(args.source_root), Path(args.cache_root), args.module, args.revision,
                       process=Process(args.timeout_seconds), clean=True, plan=args.phase == "plan",
                       lock_timeout=args.lock_timeout_seconds, emit=emit)
        print(json.dumps({"status": result["status"], "sourceRevision": args.revision,
                          "modules": args.module, "kubernetesMutations": 0, "imageBuilds": 0}))
    except BaseException as error:
        code = str(error) if isinstance(error, Failure) else "GO_CACHE_PRIME_FAILED"
        emit({"type": "RESULT", "status": "FAIL", "code": code})
        raise
    finally:
        os.close(fd)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        code = str(error) if isinstance(error, Failure) else "GO_CACHE_PRIME_FAILED"
        print(json.dumps({"status": "FAIL", "code": code}), file=sys.stderr)
        sys.exit(1)
