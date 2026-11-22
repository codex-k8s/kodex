"""Локальный single-context export через явно выбранный kubectl без сетевого клиента."""

import base64
import json
import os
from pathlib import Path
import selectors
import signal
import stat
import subprocess
import tempfile
import time
from urllib.parse import urlsplit

MAXIMUM_INPUT = 8 << 20
MAXIMUM_MATERIAL = 1 << 20
MAXIMUM_DIAGNOSTIC = 64 << 10
EXPORT_SECONDS = 15
ENVIRONMENT = {"PATH": "/usr/bin:/bin", "LC_ALL": "C", "KUBECTL_KUBERC": "false", "KUBERC": "off"}


class ExportRejected(ValueError):
    """Отказ без private bytes, source paths либо исходной диагностики kubectl."""


def require(condition):
    """Закрывает невалидную локальную форму до публикации private output."""
    if not condition:
        raise ExportRejected()


def text(value, maximum=512):
    """Принимает bounded UTF-8 имена без control characters и ambient defaults."""
    return type(value) is str and 0 < len(value.encode("utf-8")) <= maximum and value.isprintable()


def fields(value, allowed, required=()):
    """Ограничивает только известный kubeconfig shape; unknown SDK features не теряются молча."""
    return type(value) is dict and not set(value) - set(allowed) and set(required) <= set(value)


def embedded(value):
    """Проверяет canonical nonempty base64; cryptographic target proof остаётся в broker."""
    if type(value) is not str or not value or len(value) > MAXIMUM_MATERIAL:
        return False
    try:
        return base64.b64encode(base64.b64decode(value, validate=True)).decode("ascii") == value
    except ValueError:
        return False


def validate_selected(config, context_name, flattened):
    """Сохраняет ровно выбранные context/cluster/user и static embedded-compatible auth.

    Не вызывает API, plugins или auth providers. Namespace из context не выдаёт grant.
    Проверка current target/CA/identity/RBAC выполняется отдельно настоящим broker.
    """
    require(fields(config, ("apiVersion", "kind", "preferences", "current-context", "clusters", "contexts", "users"), ("apiVersion", "kind", "current-context", "clusters", "contexts", "users")))
    require(config["apiVersion"] == "v1" and config["kind"] == "Config" and config["current-context"] == context_name and config.get("preferences", {}) == {})
    for key in ("clusters", "contexts", "users"):
        require(type(config[key]) is list and len(config[key]) == 1)
    cluster, context, user = config["clusters"][0], config["contexts"][0], config["users"][0]
    require(fields(cluster, ("name", "cluster"), ("name", "cluster")) and text(cluster["name"]))
    require(fields(context, ("name", "context"), ("name", "context")) and context["name"] == context_name)
    require(fields(user, ("name", "user"), ("name", "user")) and text(user["name"]))
    require(fields(context["context"], ("cluster", "user", "namespace"), ("cluster", "user")) and context["context"]["cluster"] == cluster["name"] and context["context"]["user"] == user["name"])
    require("namespace" not in context["context"] or context["context"]["namespace"] == "" or text(context["context"]["namespace"], 253))
    require(fields(cluster["cluster"], ("server", "tls-server-name", "certificate-authority", "certificate-authority-data"), ("server",)))
    raw_server = cluster["cluster"]["server"]
    require(text(raw_server, 2048) and not any(char.isspace() for char in raw_server))
    server = urlsplit(raw_server)
    require(server.scheme == "https" and server.hostname and server.username is None and server.password is None and not server.path and not server.query and not server.fragment and "?" not in server.geturl() and "#" not in server.geturl() and 1 <= (server.port or 443) <= 65535)
    require("tls-server-name" not in cluster["cluster"] or text(cluster["cluster"]["tls-server-name"], 253))
    auth = user["user"]
    require(fields(auth, ("token", "client-certificate", "client-certificate-data", "client-key", "client-key-data")))
    for record, path_key, data_key in ((cluster["cluster"], "certificate-authority", "certificate-authority-data"), (auth, "client-certificate", "client-certificate-data"), (auth, "client-key", "client-key-data")):
        path, data = record.get(path_key), record.get(data_key)
        require(not (path and data))
        require(path is None or text(path, 4096))
        require(data is None or embedded(data))
        if flattened:
            require(path is None)
    require(bool(cluster["cluster"].get("certificate-authority")) or bool(cluster["cluster"].get("certificate-authority-data")))
    if "token" in auth:
        token = auth["token"]
        require(type(token) is str and 0 < len(token) <= 16 << 10 and all(0x21 <= ord(char) <= 0x7e for char in token) and set(auth) == {"token"})
    else:
        require(bool(auth.get("client-certificate")) or bool(auth.get("client-certificate-data")))
        require(bool(auth.get("client-key")) or bool(auth.get("client-key-data")))
    return cluster["cluster"], auth


def read_public_reference(path):
    """Читает CA/client certificate одним descriptor; public-readable не означает mutable input."""
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC), "rb") as source:
        before = os.fstat(source.fileno())
        require(stat.S_ISREG(before.st_mode) and 0 < before.st_size <= MAXIMUM_MATERIAL and not before.st_mode & 0o022)
        raw = source.read(MAXIMUM_MATERIAL + 1)
        after = os.fstat(source.fileno())
        names = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns", "st_mode", "st_uid", "st_nlink")
        require(len(raw) == before.st_size and all(getattr(before, key) == getattr(after, key) for key in names))
        return raw


def stage_file(path, raw):
    """Создаёт новый owned0600 snapshot без overwrite либо усыновления чужого файла."""
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC
    descriptor = os.open(path, flags, 0o600)
    original = os.fstat(descriptor)
    try:
        with os.fdopen(descriptor, "wb") as output:
            os.fchmod(output.fileno(), 0o600)
            output.write(raw)
            output.flush()
            os.fsync(output.fileno())
            current = os.stat(path, follow_symlinks=False)
            require((current.st_dev, current.st_ino) == (original.st_dev, original.st_ino) and current.st_nlink == 1 and current.st_uid == os.getuid() and stat.S_IMODE(current.st_mode) == 0o600 and current.st_size == len(raw))
    except BaseException:
        try:
            current = os.stat(path, follow_symlinks=False)
            if (current.st_dev, current.st_ino) == (original.st_dev, original.st_ino):
                os.unlink(path)
        except FileNotFoundError:
            pass
        raise


def run_view(binary, source, context_name, flatten, deadline):
    """Запускает только config view с закрытыми flags, bounded pipes, kill своей сессии при отказе и join её leader."""
    command = [str(binary), "--kubeconfig=" + str(source), "--context=" + context_name, "config", "view", "--merge=false", "--minify", "--raw", "--output=json"]
    if flatten:
        command.append("--flatten")
    process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=ENVIRONMENT, start_new_session=True)
    buffers = {process.stdout: bytearray(), process.stderr: bytearray()}
    try:
        with selectors.DefaultSelector() as selector:
            for stream in buffers:
                os.set_blocking(stream.fileno(), False)
                selector.register(stream, selectors.EVENT_READ)
            while selector.get_map():
                remaining = deadline - time.monotonic()
                require(remaining > 0)
                for key, _ in selector.select(min(remaining, 0.1)):
                    chunk = os.read(key.fileobj.fileno(), 65536)
                    if not chunk:
                        selector.unregister(key.fileobj)
                        continue
                    buffer = buffers[key.fileobj]
                    buffer.extend(chunk)
                    require(len(buffer) <= (MAXIMUM_MATERIAL if key.fileobj is process.stdout else MAXIMUM_DIAGNOSTIC))
        try:
            code = process.wait(timeout=max(0.001, deadline - time.monotonic()))
        except subprocess.TimeoutExpired:
            raise ExportRejected() from None
        require(code == 0 and not buffers[process.stderr] and time.monotonic() < deadline)
        return bytes(buffers[process.stdout])
    finally:
        # Не reaping-им leader перед kill: его PID ещё принадлежит только этой сессии.
        if process.returncode is None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                raise ExportRejected() from None
        for stream, buffer in buffers.items():
            stream.close()
            buffer.clear()


def export_identity(args, read_private, decode_json):
    """Преобразует один явный owner-local context в новый private embedded файл без stdout material."""
    require(text(args.context) and (args.target_context is None or text(args.target_context, 256)))
    binary = Path(args.kubectl)
    require(binary.is_absolute())
    binary = binary.resolve(strict=True)
    executable = binary.stat()
    require(stat.S_ISREG(executable.st_mode) and executable.st_uid in (0, os.getuid()) and not executable.st_mode & 0o022 and os.access(binary, os.X_OK))
    original = Path(args.kubeconfig).absolute()
    source = read_private(original, MAXIMUM_INPUT)
    deadline = time.monotonic() + EXPORT_SECONDS
    with tempfile.TemporaryDirectory(prefix="kodex-kube-export-") as directory:
        root = Path(directory)
        stage_file(root / "source", source)
        config = decode_json(run_view(binary, root / "source", args.context, False, deadline).decode("utf-8"))
        cluster, auth = validate_selected(config, args.context, False)
        for record, path_key, data_key, private in ((cluster, "certificate-authority", "certificate-authority-data", False), (auth, "client-certificate", "client-certificate-data", False), (auth, "client-key", "client-key-data", True)):
            if path_key in record:
                path = Path(record[path_key])
                if not path.is_absolute():
                    path = original.parent / path
                raw = read_private(path, MAXIMUM_MATERIAL) if private else read_public_reference(path)
                record[data_key] = base64.b64encode(raw).decode("ascii")
                del record[path_key]
                require(time.monotonic() < deadline)
        selected = json.dumps(config, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        require(len(selected) <= MAXIMUM_MATERIAL)
        stage_file(root / "selected", selected)
        output = decode_json(run_view(binary, root / "selected", args.context, True, deadline).decode("utf-8"))
        validate_selected(output, args.context, True)
        if args.target_context is not None:
            output["current-context"] = args.target_context
            output["contexts"][0]["name"] = args.target_context
            validate_selected(output, args.target_context, True)
        raw = (json.dumps(output, ensure_ascii=False, separators=(",", ":")) + "\n").encode("utf-8")
        require(len(raw) <= MAXIMUM_MATERIAL and time.monotonic() < deadline)
        stage_file(Path(args.output), raw)
