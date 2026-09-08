"""Общая подготовка host Go cache; политика вызова принадлежит двум adapters."""

import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import tempfile
import time


MODULES = (
    "services/internal/control-plane", "services/internal/secret-broker",
    "services/internal/stt-tts-service", "services/internal/email-bridge",
    "services/internal/internal-rpc-authority", "services/internal/runtime-controller",
    "services/external/control-api-gateway", "services/external/egress-gateway",
    "services/external/integration-gateway", "services/jobs/automation-scheduler",
    "services/jobs/artifact-retention", "services/jobs/session-archive",
    "services/external/interaction-gateway",
)
REPOSITORY = "github.com/codex-k8s/kodex"
ORIGINS = ("https://github.com/codex-k8s/kodex.git", "git@github.com:codex-k8s/kodex.git")
GO_VERSION = "go1.26.6"


class Failure(Exception):
    pass


def require(value, code):
    if not value:
        raise Failure(code)


def digest(value):
    return hashlib.sha256(value).hexdigest()


def exact_path(value, *, exists=True):
    path = Path(value)
    require(path.is_absolute() and str(path) == value and path != Path("/"), "PATH_INVALID")
    require(path.resolve() == path and (not exists or path.exists()), "PATH_UNSAFE")
    return path


class Process:
    def __init__(self, timeout):
        self.deadline = time.monotonic() + timeout

    def run(self, argv, *, cwd=None, env=None):
        remaining = self.deadline - time.monotonic()
        require(remaining > 0, "DEADLINE_EXCEEDED")
        # Ни stdout, ни stderr Go/git не попадают в диагностику: они могут включать URL.
        with tempfile.TemporaryFile() as output, tempfile.TemporaryFile() as errors:
            child = subprocess.Popen(argv, cwd=cwd, env=env, stdout=output,
                                     stderr=errors, start_new_session=True)
            try:
                code = child.wait(timeout=remaining)
            except BaseException as error:
                with contextlib.suppress(ProcessLookupError):
                    os.killpg(child.pid, signal.SIGKILL)
                child.wait()
                if isinstance(error, subprocess.TimeoutExpired):
                    raise Failure("DEADLINE_EXCEEDED") from None
                raise
            require(code == 0, "COMMAND_FAILED")
            require(output.tell() <= 16 << 20, "COMMAND_OUTPUT_TOO_LARGE")
            output.seek(0)
            return output.read()


def source_state(source, process, clean):
    git = lambda *args: process.run(["git", "-C", str(source), *args]).strip()
    require(git("rev-parse", "--show-toplevel").decode() == str(source), "SOURCE_ROOT_INVALID")
    require(git("remote", "get-url", "origin").decode() in ORIGINS, "FOREIGN_REPOSITORY")
    head = git("rev-parse", "HEAD").decode()
    require(re.fullmatch(r"[a-f0-9]{40}", head), "REVISION_INVALID")
    status = git("status", "--porcelain", "--untracked-files=all")
    if clean:
        require(not status, "SOURCE_DIRTY")
        require(not any((source / name).exists() for name in (".env", ".kodex-env", ".kodex-remote-env")), "PRIVATE_SOURCE_FORBIDDEN")
    # Dirty local render сохраняет прежний контракт; staging adapter такого режима не имеет.
    return {"revision": head, "statusSHA256": digest(status),
            "diffSHA256": digest(git("diff", "--no-ext-diff", "--binary", "HEAD", "--"))}


def safe_tree(root):
    require(not root.is_symlink(), "CACHE_ENTRY_UNSAFE")
    if not root.exists():
        return []
    require(root.is_dir() and not root.is_symlink(), "CACHE_ENTRY_UNSAFE")
    paths = [root]
    for directory, names, files in os.walk(root, followlinks=False):
        paths.extend(Path(directory) / name for name in names + files)
    for path in paths:
        info = path.lstat()
        require(info.st_uid == os.getuid() and not stat.S_ISLNK(info.st_mode) and
                (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)) and
                (stat.S_ISDIR(info.st_mode) or info.st_nlink == 1), "CACHE_ENTRY_UNSAFE")
    return paths


def check_host_cache(cache):
    build = exact_path(str(cache / "go-build-v2"), exists=False)
    if build.exists():
        require(build.is_dir() and build.stat().st_uid == os.getuid(), "HOST_PRIME_CACHE_UNSAFE")
    host = exact_path(str(build / "host-prime"), exists=False)
    safe_tree(host)
    return build, host


def permissions(roots, writable):
    for root in roots:
        paths = safe_tree(root)
        for path in paths:
            mode = path.stat().st_mode
            target = (0o755 if writable else 0o555) if path.is_dir() else (
                (0o755 if writable else 0o555) if mode & 0o111 else (0o644 if writable else 0o444))
            path.chmod(target)


def environment(home, cache):
    # Только публичный proxy/sumdb; private env, netrc, git credentials и GOFLAGS не наследуются.
    return {"PATH": os.environ.get("PATH", "/usr/local/go/bin:/usr/bin:/bin"),
            "HOME": str(home), "GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local",
            "GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org",
            "GOMODCACHE": str(cache / "go-mod-v2"), "GOCACHE": str(cache / "go-build-v2/host-prime"),
            "GOPATH": str(home / "gopath"), "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null",
            "GIT_TERMINAL_PROMPT": "0", "CGO_ENABLED": "0"}


def manifests(source, modules, process, env):
    """Копируется только module metadata; Go не может записать go.sum в source."""
    pending, content = list(modules), {}
    while pending:
        module = pending.pop()
        if f"{module}/go.mod" in content:
            continue
        directory = exact_path(str(source / module))
        require(directory.is_relative_to(source), "REPLACEMENT_OUTSIDE_SOURCE")
        for name in ("go.mod", "go.sum"):
            path = directory / name
            if name == "go.mod" or path.exists():
                require(path.is_file() and not path.is_symlink() and path.stat().st_size <= 2 << 20, "MODULE_FILE_UNSAFE")
                content[f"{module}/{name}"] = path.read_bytes()
        data = json.loads(process.run(["go", "mod", "edit", "-json", str(directory / "go.mod")], env=env))
        require(data.get("Module", {}).get("Path") == f"{REPOSITORY}/{module}" and data.get("Go") == GO_VERSION[2:], "MODULE_IDENTITY_INVALID")
        for replacement in data.get("Replace") or []:
            target = replacement["New"]
            if target.get("Version"):
                continue
            value = target["Path"]
            require(value.startswith("../") and not Path(value).is_absolute(), "LOCAL_REPLACEMENT_INVALID")
            absolute = (directory / value).resolve()
            require(absolute.is_relative_to(source / "libs/go"), "REPLACEMENT_OUTSIDE_SOURCE")
            relative = absolute.relative_to(source).as_posix()
            require(re.fullmatch(r"libs/go/[a-z0-9-]+", relative), "LOCAL_REPLACEMENT_INVALID")
            require((directory / value).absolute().resolve() == absolute, "LOCAL_REPLACEMENT_INVALID")
            # resolve не должен скрыть symlink на одном из исходных path components.
            exact_path(str(source / relative))
            pending.append(relative)
        require(len(content) <= 256, "MODULE_GRAPH_TOO_LARGE")
    return content


def json_stream(raw):
    decoder, values, position = json.JSONDecoder(), [], 0
    text = raw.decode()
    while position < len(text):
        if text[position].isspace():
            position += 1
            continue
        item, position = decoder.raw_decode(text, position)
        values.append(item)
        require(len(values) <= 4096, "DEPENDENCY_GRAPH_TOO_LARGE")
    return values


def dependency_metadata(raw, cache):
    result = []
    for item in json_stream(raw):
        require(not item.get("Error") and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._~+/-]*", item.get("Path", "")) and
                re.fullmatch(r"v[0-9][A-Za-z0-9._+~-]*", item.get("Version", "")), "DEPENDENCY_IDENTITY_INVALID")
        for field in ("Sum", "GoModSum"):
            require(re.fullmatch(r"h1:[A-Za-z0-9+/]{43}=", item.get(field, "")), "DEPENDENCY_SUM_MISSING")
        for field in ("GoMod", "Zip", "Dir", "Info"):
            path = exact_path(item.get(field, ""))
            require(path.is_relative_to(cache / "go-mod-v2"), "DEPENDENCY_CACHE_PATH_INVALID")
        result.append({key: item[key] for key in ("Path", "Version", "Sum", "GoModSum")})
    return sorted(result, key=lambda value: (value["Path"], value["Version"]))


def atomic_file(path, content, mode):
    fd, temporary = tempfile.mkstemp(prefix=".prime-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(content)
            stream.flush()
            os.fchmod(stream.fileno(), mode)
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(temporary)


def prime(source, cache, modules, revision, *, process, clean=True, air=False,
          plan=False, lock_timeout=60, emit=lambda event: None):
    source, cache = exact_path(str(source)), exact_path(str(cache))
    require(cache.is_dir() and cache.stat().st_uid == os.getuid(), "CACHE_ROOT_OWNER_REQUIRED")
    require(not cache.is_relative_to(source) and not source.is_relative_to(cache), "CACHE_SOURCE_OVERLAP")
    require(modules and len(modules) <= len(MODULES) and len(set(modules)) == len(modules) and
            all(module in MODULES for module in modules), "MODULE_ALLOWLIST_REJECTED")
    before = source_state(source, process, clean)
    require(before["revision"] == revision, "SOURCE_REVISION_MISMATCH")
    roots = [cache / "go-mod-v2", cache / "go-sumdb"] + ([cache / "go-tools"] if air else [])
    for root in roots:
        safe_tree(root)
    check_host_cache(cache)
    # Эта временная копия не содержит source, .env или Git credentials.
    with tempfile.TemporaryDirectory(prefix="kodex-go-manifests-") as temporary:
        mirror = Path(temporary)
        env = environment(mirror, cache)
        require(process.run(["go", "env", "GOVERSION"], env=env).strip().decode() == GO_VERSION, "GO_TOOLCHAIN_MISMATCH")
        content = manifests(source, modules, process, env)
        for relative, value in content.items():
            target = mirror / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(value)
        metadata = {"source": before, "modules": modules, "manifests": {key: digest(value) for key, value in sorted(content.items())}, "goVersion": GO_VERSION}
        emit({"type": "PLAN", **metadata})
        if plan:
            require(source_state(source, process, clean) == before and all((source / path).read_bytes() == data for path, data in content.items()), "SOURCE_DRIFT")
            result = {"status": "PLANNED", **metadata}
            emit({"type": "RESULT", **result})
            return result
        lock = cache / ".go-prime.lock"
        fd = os.open(lock, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            info = os.fstat(fd)
            require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and info.st_nlink == 1 and not info.st_mode & 0o077, "CACHE_LOCK_UNSAFE")
            until = min(process.deadline, time.monotonic() + lock_timeout)
            while True:
                try:
                    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    require(time.monotonic() < until, "CACHE_LOCK_TIMEOUT")
                    time.sleep(0.05)
            require(source_state(source, process, clean) == before, "SOURCE_DRIFT")
            emit({"type": "INTENT", "operation": "PRIME_GO_CACHE", **metadata})
            dependencies, air_digest = {}, None
            try:
                for root in roots:
                    root.mkdir(mode=0o755, exist_ok=True)
                # Повтор под общим lock до mkdir/chmod и использования GOCACHE.
                build, host = check_host_cache(cache)
                build.mkdir(mode=0o755, exist_ok=True)
                host.mkdir(mode=0o700, exist_ok=True)
                check_host_cache(cache)
                host.chmod(0o700)
                permissions(roots, True)
                for path in (cache / "go-mod-v2/cache/download/sumdb/sum.golang.org", cache / "go-sumdb/sum.golang.org"):
                    path.mkdir(parents=True, exist_ok=True)
                # Go хранит latest sumdb checkpoint отдельно от download cache.
                # Приватный GOPATH связывает его с тем же read-only mount будущих Pods.
                (mirror / "gopath/pkg").mkdir(parents=True)
                (mirror / "gopath/pkg/sumdb").symlink_to(cache / "go-sumdb")
                for module in modules:
                    raw = process.run(["go", "-C", str(mirror / module), "mod", "download", "-json"], env=env)
                    dependencies[module] = dependency_metadata(raw, cache)
                    process.run(["go", "-C", str(mirror / module), "mod", "verify"], env=env)
                if air:
                    lock_data = json.loads((source / "tools/dev/components.lock.json").read_bytes())["tools"]["air"]
                    require(lock_data["module"] == "github.com/air-verse/air" and re.fullmatch(r"v\d+\.\d+\.\d+", lock_data["version"]), "AIR_LOCK_INVALID")
                    platform = process.run(["go", "env", "GOOS", "GOARCH"], env=env).decode().split()
                    require(len(platform) == 2 and all(re.fullmatch(r"[a-z0-9]+", p) for p in platform), "GO_PLATFORM_INVALID")
                    contract = f'{lock_data["module"]}@{lock_data["version"]}|CGO_ENABLED=0|{"/".join(platform)}'
                    binary, receipt = cache / "go-tools/air", cache / "go-tools/air.contract"
                    if not binary.is_file() or not os.access(binary, os.X_OK) or not receipt.is_file() or receipt.read_text().strip() != contract:
                        install = mirror / "bin"
                        install.mkdir()
                        process.run(["go", "install", f'{lock_data["module"]}@{lock_data["version"]}'], env={**env, "GOBIN": str(install)}, cwd=mirror)
                        require((install / "air").is_file(), "AIR_BINARY_MISSING")
                        atomic_file(binary, (install / "air").read_bytes(), 0o755)
                        atomic_file(receipt, (contract + "\n").encode(), 0o644)
                    air_digest = digest(binary.read_bytes())
                require(all((mirror / path).read_bytes() == data for path, data in content.items()), "GO_MANIFEST_REWRITE_REQUIRED")
                require(all(not (mirror / path.replace("go.mod", "go.sum")).exists()
                            for path in content if path.endswith("/go.mod") and
                            path.replace("go.mod", "go.sum") not in content), "GO_MANIFEST_REWRITE_REQUIRED")
                require(source_state(source, process, clean) == before and all((source / path).read_bytes() == data for path, data in content.items()), "SOURCE_DRIFT")
            finally:
                permissions(roots, False)
            result = {"status": "PASS", **metadata, "dependencies": dependencies,
                      "moduleCacheVerified": True, "sealed": True, "airSHA256": air_digest}
            emit({"type": "RESULT", **result})
            return result
        finally:
            os.close(fd)
