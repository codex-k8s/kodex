#!/usr/bin/env python3
"""Trusted local render adapter; не является staging release entrypoint."""

import json
import signal
import sys

sys.dont_write_bytecode = True
from go_cache import Failure, MODULES, Process, prime, require, source_state, exact_path


def main():
    require(len(sys.argv) == 4 and sys.argv[3] in ("web-only", "web-with-mattermost"), "RENDER_ARGUMENTS_INVALID")
    process = Process(1800)
    def interrupted(_signum, _frame):
        raise Failure("INTERRUPTED")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    source = exact_path(sys.argv[1])
    revision = source_state(source, process, clean=False)["revision"]
    cache = exact_path(sys.argv[2], exists=False)
    cache.mkdir(mode=0o755, parents=True, exist_ok=True)
    modules = list(MODULES if sys.argv[3] == "web-with-mattermost" else MODULES[:-1])
    result = prime(source, cache, modules, revision, process=process, clean=False, air=True)
    print(json.dumps({"airSHA256": result["airSHA256"]}))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        code = str(error) if isinstance(error, Failure) else "GO_CACHE_PRIME_FAILED"
        print(f"Go render cache prime failed: {code}", file=sys.stderr)
        sys.exit(1)
