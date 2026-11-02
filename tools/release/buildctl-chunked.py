#!/usr/bin/env python3
"""Опциональный BUILDCTL_PATH: проверенный OCI archive → bounded publisher → исходная metadata."""
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import selectors
import shutil
import stat
import subprocess
import sys
import tempfile
import time


class Rejected(ValueError):
    pass


def require(value):
    if not value:
        raise Rejected("Chunked OCI adapter rejected")


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result)
        result[key] = value
    return result


def private_json(path, maximum=65536):
    require(path.is_absolute())
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and info.st_nlink == 1
                and stat.S_IMODE(info.st_mode) in (0o400, 0o600) and info.st_size <= maximum)
        raw = stream.read(maximum + 1)
    require(len(raw) <= maximum)
    return json.loads(raw, object_pairs_hook=unique_object), raw


def secure_generated_metadata(path):
    # Buildctl задаёт mode явно; ограничиваем только собственный новый файл.
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and info.st_nlink == 1)
        os.fchmod(stream.fileno(), 0o600)


def executable(value):
    require(isinstance(value, dict) and set(value) == {"path", "sha256"})
    path = Path(value["path"])
    require(path.is_absolute() and not path.is_symlink() and re.fullmatch(r"[a-f0-9]{64}", value["sha256"]))
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid in (0, os.getuid()) and info.st_nlink == 1
            and not info.st_mode & 0o022 and info.st_size <= 128 << 20 and os.access(path, os.X_OK))
    with path.open("rb") as stream:
        require(hashlib.file_digest(stream, "sha256").hexdigest() == value["sha256"])
    return str(path)


def configuration():
    name = os.environ.get("KODEX_RELEASE_CHUNKED_CONFIG")
    require(name is not None)
    value, _ = private_json(Path(name))
    require(set(value) == {"schema_version", "buildctl", "publisher", "chunk_bytes", "max_archive_bytes", "timeout_seconds", "ca_file"}
            and value["schema_version"] == 1)
    require(type(value["chunk_bytes"]) is int and 65536 <= value["chunk_bytes"] <= 16 << 20)
    require(type(value["max_archive_bytes"]) is int and 1 << 20 <= value["max_archive_bytes"] <= 8 << 30)
    require(type(value["timeout_seconds"]) is int and 5 <= value["timeout_seconds"] <= 600)
    require(isinstance(value["ca_file"], str) and (not value["ca_file"] or Path(value["ca_file"]).is_absolute()))
    value["buildctl"] = executable(value["buildctl"])
    value["publisher"] = executable(value["publisher"])
    return value


def arguments(args):
    require(len(args) >= 3 and args[0] == "--addr" and args[1].startswith("unix:///") and args[2] == "build")
    require(len(args) <= 200 and all(len(item) <= 8192 for item in args))
    require((len(args) - 3) % 2 == 0)
    allowed = {"--frontend", "--local", "--opt", "--output", "--metadata-file"}
    require(all(args[index] in allowed and not args[index + 1].startswith("--")
                for index in range(3, len(args), 2)))
    require(not any(item == flag or item.startswith(flag + "=") for item in args for flag in ("--secret", "--ssh", "--allow")))
    require(args.count("--frontend") == 1 and args[args.index("--frontend") + 1] == "dockerfile.v0")
    require(args.count("--output") == 1 and args.count("--metadata-file") == 1)
    output_index = args.index("--output")
    metadata_index = args.index("--metadata-file")
    require(output_index + 1 < len(args) and metadata_index + 1 < len(args))
    fields = {}
    for item in args[output_index + 1].split(","):
        key, separator, value = item.partition("=")
        require(separator and key not in fields)
        fields[key] = value
    require(set(fields) == {"type", "name", "push"} and fields["type"] == "image" and fields["push"] == "true")
    require(re.fullmatch(r"[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9._/-]*:[a-zA-Z0-9._-]+", fields["name"])
            and "//" not in fields["name"] and "@" not in fields["name"])
    metadata = Path(args[metadata_index + 1])
    require(metadata.is_absolute() and not metadata.exists() and not metadata.is_symlink())
    parent = metadata.parent
    require(parent.is_dir() and not parent.is_symlink() and parent.stat().st_uid == os.getuid()
            and not parent.stat().st_mode & 0o022)
    return fields["name"], metadata, output_index, metadata_index


def filtered_progress(data):
    text = data.decode("utf-8", errors="replace")
    text = re.sub(r"(https?://[^\s?]+)\?[^\s]+", r"\1?[REDACTED_QUERY]", text)
    text = re.sub(r"(?i)(Authorization:\s*(?:Bearer|Basic)\s+)\S+", r"\1[REDACTED]", text)
    return text


def build(command, environment, timeout, archive_limit):
    def limits():
        resource.setrlimit(resource.RLIMIT_FSIZE, (archive_limit, archive_limit))
    process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                               env=environment, preexec_fn=limits)
    deadline = time.monotonic() + timeout
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ)
    pending = b""
    total = 0
    try:
        while selector.get_map():
            require(time.monotonic() < deadline)
            for key, _ in selector.select(min(1, max(0, deadline - time.monotonic()))):
                data = os.read(key.fileobj.fileno(), 16384)
                if not data:
                    selector.unregister(key.fileobj)
                    continue
                total += len(data)
                pending += data
                require(total <= 32 << 20 and len(pending) <= 1 << 20)
                lines = pending.split(b"\n")
                pending = lines.pop()
                for line in lines:
                    print(filtered_progress(line), file=sys.stderr, flush=True)
        if pending:
            print(filtered_progress(pending), file=sys.stderr, flush=True)
        require(process.wait(timeout=max(1, deadline - time.monotonic())) == 0)
    finally:
        selector.close()
        if process.poll() is None:
            process.kill()
            process.wait(timeout=10)
        process.stdout.close()


def main():
    os.umask(0o077)
    config = configuration()
    args = sys.argv[1:]
    destination, target, output_index, metadata_index = arguments(args)
    docker_directory = os.environ.get("DOCKER_CONFIG")
    require(docker_directory is not None)
    auth = Path(docker_directory) / "config.json"
    private_json(auth, 1 << 20)
    environment = {key: value for key, value in os.environ.items() if key in (
        "PATH", "HOME", "TMPDIR", "DOCKER_CONFIG", "SSL_CERT_FILE", "SSL_CERT_DIR",
        "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy")}
    root = Path(tempfile.mkdtemp(prefix=".chunked-oci-", dir=target.parent))
    stage = "build"
    digest = None
    try:
        archive = root / "image.tar"
        metadata = root / "metadata.json"
        changed = list(args)
        changed[output_index + 1] = "type=oci,dest=" + str(archive) + ",name=" + destination + ",oci-mediatypes=true,attestation-inline=true"
        changed[metadata_index + 1] = str(metadata)
        build([config["buildctl"], *changed], environment, config["timeout_seconds"], config["max_archive_bytes"])
        stage = "metadata"
        archive_info = archive.lstat()
        require(stat.S_ISREG(archive_info.st_mode) and archive_info.st_nlink == 1
                and archive_info.st_uid == os.getuid() and archive_info.st_size <= config["max_archive_bytes"])
        secure_generated_metadata(metadata)
        value, raw = private_json(metadata, 1 << 20)
        digest = value.get("containerimage.digest")
        require(isinstance(digest, str) and re.fullmatch(r"sha256:[a-f0-9]{64}", digest))
        command = [config["publisher"], "--archive", str(archive), "--docker-config", str(auth),
                   "--destination", destination, "--expected-digest", digest, "--chunk-bytes", str(config["chunk_bytes"])]
        if config["ca_file"]:
            command += ["--ca-file", config["ca_file"]]
        stage = "publication"
        published = subprocess.run(command, capture_output=True, env=environment, timeout=config["timeout_seconds"])
        safe_errors = {b"Publisher input rejected", b"Publisher credential rejected",
                       b"Publisher destination digest conflict", b"Publisher unavailable; reconcile destination digest"}
        if published.returncode and published.stderr.strip() in safe_errors:
            print(published.stderr.strip().decode("ascii"), file=sys.stderr)
        require(published.returncode == 0 and len(published.stdout) <= 65536)
        stage = "readback"
        proof = json.loads(published.stdout, object_pairs_hook=unique_object)
        require(proof.get("digest") == digest)
        require(metadata.read_bytes() == raw)
        os.link(metadata, target, follow_symlinks=False)
        print("Chunked OCI publication verified: " + digest)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        receipt = {"state": "RECONCILIATION_REQUIRED", "stage": stage,
                   "destination": destination, "expected_digest": digest}
        with (root / "failure.json").open("x") as stream:
            json.dump(receipt, stream)
        print("Private reconciliation evidence retained: " + str(root), file=sys.stderr)
        raise
    else:
        shutil.rmtree(root)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        raise SystemExit("Chunked OCI publication rejected; reconcile destination digest before retry") from None
