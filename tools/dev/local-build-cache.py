"""Отбор и явная очистка старого private build-cache, не образов или volumes.

audit не меняет Docker. prune требует ID и fingerprint из audit и повторяет
проверку перед каждым эффектом. default — общий performance cache: его очистка
может замедлить будущие сборки других задач, exclusive ownership не утверждается.
Описание BuildKit никогда не выводится. Timeout не разрешает слепой повтор.
"""

import argparse
import hashlib
import json
import os
import pwd
import re
import selectors
import signal
import subprocess
import time


BUILDERS = ("kodex-local-dev", "default")
MAX_OUTPUT = 10 << 20
MAX_RECORDS = 10000
MAX_TARGETS = 128
COMMAND_SECONDS = 25
TOTAL_SECONDS = 120
ID_PATTERN = re.compile(r"[a-z0-9]{20,40}\Z")
HASH_PATTERN = re.compile(r"[0-9a-f]{64}\Z")
SIZE_PATTERN = re.compile(r"[0-9]+(?:\.[0-9]+)?(?:B|kB|KB|MB|GB|TB|KiB|MiB|GiB|TiB)\Z")


class Failure(Exception):
    pass


def require(condition, code):
    if not condition:
        raise Failure(code)


def docker_environment():
    return {"PATH": "/usr/bin:/bin", "HOME": pwd.getpwuid(os.getuid()).pw_dir,
            "LC_ALL": "C", "LANG": "C"}


def docker(builder, operation, target=None, deadline=None):
    require(builder in BUILDERS, "BUILDER_REJECTED")
    require(operation in ("du", "prune"), "OPERATION_REJECTED")
    command = ["/usr/bin/docker", "--host", "unix:///var/run/docker.sock",
               "buildx", operation, "--builder", builder]
    if operation == "du":
        require(target is None, "TARGET_REJECTED")
        # DU boolean filters проверяют presence, а until не гарантирует отбор.
        command += ["--format", "{{json .}}"]
    else:
        require(isinstance(target, str) and ID_PATTERN.fullmatch(target), "TARGET_REJECTED")
        # Buildx переводит id в regex, поэтому закрепляем обе границы.
        # BuildKit private — presence field с пустым значением, не boolean.
        command += ["--force", "--filter", "id=^" + target + "$",
                    "--filter", "until=24h", "--filter", 'private=""']
    end = min(time.monotonic() + COMMAND_SECONDS, deadline or float("inf"))
    require(time.monotonic() < end, "BUDGET_EXHAUSTED")
    try:
        process = subprocess.Popen(command, env=docker_environment(), stdin=subprocess.DEVNULL,
                                   stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                   start_new_session=True)
    except OSError:
        raise Failure("DOCKER_COMMAND_FAILED") from None
    chunks, size = [], 0
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            while selector.get_map():
                remaining = end - time.monotonic()
                require(remaining > 0, "DOCKER_COMMAND_TIMEOUT")
                for key, _ in selector.select(min(remaining, 0.1)):
                    chunk = os.read(key.fileobj.fileno(), 65536)
                    if not chunk:
                        selector.unregister(key.fileobj)
                        continue
                    size += len(chunk)
                    require(size <= MAX_OUTPUT, "DOCKER_OUTPUT_LIMIT")
                    chunks.append(chunk)
            try:
                code = process.wait(timeout=max(0.001, end - time.monotonic()))
            except subprocess.TimeoutExpired:
                raise Failure("DOCKER_COMMAND_TIMEOUT") from None
        require(code == 0, "DOCKER_COMMAND_FAILED")
        return b"".join(chunks)
    except OSError:
        raise Failure("DOCKER_COMMAND_FAILED") from None
    finally:
        if process.poll() is None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait(timeout=2)
        process.stdout.close()


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "DU_RESPONSE_INVALID")
        result[key] = value
    return result


def old_enough(value):
    if not isinstance(value, str):
        return False
    match = re.fullmatch(r"([0-9]+) (hours?|days?) ago", value)
    if not match:
        return False
    count = int(match[1])
    return count >= (2 if match[2].startswith("day") else 25)


def records(builder, raw):
    require(builder in BUILDERS, "BUILDER_REJECTED")
    require(isinstance(raw, bytes) and len(raw) <= MAX_OUTPUT, "DOCKER_OUTPUT_LIMIT")
    result = {}
    try:
        lines = raw.decode("utf-8").splitlines()
        require(len(lines) <= MAX_RECORDS, "DU_RECORD_LIMIT")
        for line in lines:
            require(bool(line.strip()), "DU_RESPONSE_INVALID")
            record = json.loads(line, object_pairs_hook=unique_object)
            require(isinstance(record, dict), "DU_RESPONSE_INVALID")
            identifier = record.get("ID")
            require(isinstance(identifier, str) and ID_PATTERN.fullmatch(identifier), "DU_RESPONSE_INVALID")
            require(identifier not in result, "DU_RESPONSE_INVALID")
            for field in ("Reclaimable", "Shared", "Mutable"):
                require(type(record.get(field)) is bool, "DU_RESPONSE_INVALID")
            size = record.get("Size")
            require(isinstance(size, str) and SIZE_PATTERN.fullmatch(size), "DU_RESPONSE_INVALID")
            for field in ("CreatedAt", "LastUsedAt", "Description"):
                require(isinstance(record.get(field), str) and len(record[field]) <= 65536,
                        "DU_RESPONSE_INVALID")
            parents = record.get("Parents")
            parents = [] if parents is None else parents
            require(isinstance(parents, list) and len(parents) <= MAX_RECORDS and
                    all(isinstance(parent, str) and ID_PATTERN.fullmatch(parent) for parent in parents),
                    "DU_RESPONSE_INVALID")
            # age повторно проверяется; относительный LastUsedAt не включён в
            # fingerprint: его текст меняется без изменения самой cache record.
            binding = {field: record[field] for field in
                       ("ID", "Size", "Mutable", "Shared", "Reclaimable", "CreatedAt")}
            binding["builder"] = builder
            binding["parents"] = sorted(parents)
            cache_type = record.get("Type", "regular")
            require(isinstance(cache_type, str) and len(cache_type) <= 128, "DU_RESPONSE_INVALID")
            binding["type"] = cache_type
            binding["descriptionDigest"] = hashlib.sha256(record["Description"].encode()).hexdigest()
            fingerprint = hashlib.sha256(json.dumps(binding, sort_keys=True,
                                                    separators=(",", ":")).encode()).hexdigest()
            result[identifier] = {"id": identifier, "size": size,
                                  "mutable": record["Mutable"], "fingerprint": fingerprint,
                                  "parents": parents, "cacheType": cache_type,
                                  "eligible": record["Reclaimable"] and not record["Shared"]
                                  and cache_type in ("regular", "source.local", "source.git.checkout", "exec.cachemount")
                                  and old_enough(record["LastUsedAt"])}
    except (UnicodeError, ValueError, TypeError, RecursionError):
        raise Failure("DU_RESPONSE_INVALID") from None
    # DU помечает предков unused descendants как reclaimable, но точный prune
    # не удалит их, пока остаются дочерние cache refs. Отбираем только листья.
    referenced = {parent for item in result.values() for parent in item["parents"]}
    for identifier, item in result.items():
        item["eligible"] = item["eligible"] and identifier not in referenced
    return result


def inspect(builder, deadline):
    return records(builder, docker(builder, "du", deadline=deadline))


def free_bytes():
    data = os.statvfs("/")
    return data.f_bavail * data.f_frsize


def execute(mode, builder, targets=(), fingerprints=()):
    require(builder in BUILDERS, "BUILDER_REJECTED")
    require(mode in ("audit", "prune"), "OPERATION_REJECTED")
    require(len(targets) <= MAX_TARGETS and len(targets) == len(set(targets)), "TARGET_REJECTED")
    require(all(isinstance(item, str) and ID_PATTERN.fullmatch(item) for item in targets), "TARGET_REJECTED")
    require(len(fingerprints) == len(targets) and all(HASH_PATTERN.fullmatch(item) for item in fingerprints),
            "FINGERPRINT_REQUIRED")
    require(bool(targets) if mode == "prune" else not targets, "TARGET_REJECTED")
    deadline = time.monotonic() + TOTAL_SECONDS
    initial = inspect(builder, deadline)
    if mode == "audit":
        return {"status": "PASS", "mode": mode, "builder": builder, "minimumAgeHours": 24,
                "records": [item for _, item in sorted(initial.items()) if item["eligible"]],
                "excludedCount": sum(not item["eligible"] for item in initial.values())}
    # Весь выбранный список проверяется до первого эффекта.
    for target, fingerprint in zip(targets, fingerprints):
        require(target in initial and initial[target]["eligible"], "TARGET_NOT_ELIGIBLE")
        require(initial[target]["fingerprint"] == fingerprint, "TARGET_CHANGED")
    before = free_bytes()
    outcomes = []
    for target, fingerprint in zip(targets, fingerprints):
        error = None
        try:
            fresh = inspect(builder, deadline)
            require(target in fresh and fresh[target]["eligible"], "TARGET_NOT_ELIGIBLE")
            require(fresh[target]["fingerprint"] == fingerprint, "TARGET_CHANGED")
        except Failure as failure:
            outcomes.append({"id": target, "effect": "NOT_ATTEMPTED", "error": str(failure)})
            break
        try:
            docker(builder, "prune", target, deadline)
        except Failure as failure:
            error = str(failure)
        # Независимый bounded readback даже после исчерпания общего бюджета.
        try:
            surviving = inspect(builder, time.monotonic() + COMMAND_SECONDS)
            effect = "ABSENT_AFTER" if target not in surviving else "PRESENT_AFTER"
        except Failure:
            effect = "UNKNOWN"
        item = {"id": target, "effect": effect, "sizeBefore": initial[target]["size"]}
        if error:
            item["error"] = error
        outcomes.append(item)
        if error or effect != "ABSENT_AFTER":
            break
    done = len(outcomes)
    outcomes.extend({"id": target, "effect": "NOT_ATTEMPTED"} for target in targets[done:])
    complete = all(item["effect"] == "ABSENT_AFTER" and "error" not in item for item in outcomes)
    try:
        after = free_bytes()
    except OSError:
        after = None
        complete = False
    return {"status": "PASS" if complete else "PARTIAL", "mode": mode, "builder": builder,
            "outcomes": outcomes, "freeBytesBefore": before, "freeBytesAfter": after,
            "freeBytesDelta": after - before if after is not None else None,
            "spaceMeasurement": "HOST_ROOT_CONCURRENT_NOT_ATTRIBUTED"}


class SafeParser(argparse.ArgumentParser):
    def error(self, message):
        print(json.dumps({"status": "FAIL", "error": "ARGUMENTS_INVALID"}))
        raise SystemExit(2)


def main(arguments=None):
    parser = SafeParser(description="Audit or explicitly prune old private build cache only")
    parser.add_argument("mode", choices=("audit", "prune"))
    parser.add_argument("--builder", choices=BUILDERS, required=True)
    parser.add_argument("--targetid", action="append", default=[])
    parser.add_argument("--expected-fingerprint", action="append", default=[])
    options = parser.parse_args(arguments)
    try:
        result = execute(options.mode, options.builder, options.targetid, options.expected_fingerprint)
    except Failure as failure:
        result = {"status": "FAIL", "error": str(failure)}
    except (OSError, ValueError, TypeError):
        result = {"status": "FAIL", "error": "LOCAL_CHECK_FAILED"}
    print(json.dumps(result, sort_keys=True))
    return 0 if result["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
