"""Закрытая подготовка credential files; входы никогда не печатаются."""
import argparse
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import stat
import tempfile


def require(value):
    if not value:
        raise ValueError("credential preparation rejected")


def read(root, name):
    source = (root / name).resolve(strict=True)
    require(source.is_relative_to(root.resolve()) and source.is_file())
    mode = stat.S_IMODE(source.stat().st_mode)
    require(mode in (0o400, 0o440, 0o600))
    with source.open("rb") as stream:
        value = stream.read(65537)
    require(0 < len(value) <= 65536)
    return value.decode("utf-8", errors="strict")


def closed_object(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value)
        value[key] = item
    return value


def storage_config(raw):
    value = json.loads(raw, object_pairs_hook=closed_object)
    require(isinstance(value, dict) and set(value) == {"identities"})
    identities = value["identities"]
    require(isinstance(identities, list) and 1 <= len(identities) <= 16)
    names, keys = set(), set()
    for identity in identities:
        require(isinstance(identity, dict) and set(identity) == {"name", "credentials", "actions"})
        name = identity["name"]
        require(isinstance(name, str) and re.fullmatch(r"[a-z][a-z0-9-]{2,63}", name)
                and name != "anonymous" and name not in names)
        names.add(name)
        actions = identity["actions"]
        require(isinstance(actions, list) and 1 <= len(actions) <= 64 and
                all(isinstance(action, str) and re.fullmatch(
                    r"(Admin|Read|Write|List|Tagging)(:[a-z0-9][a-z0-9.-]{2,62})?", action)
                    for action in actions))
        credentials = identity["credentials"]
        require(isinstance(credentials, list) and 1 <= len(credentials) <= 2)
        for credential in credentials:
            require(isinstance(credential, dict) and set(credential) == {"accessKey", "secretKey"})
            access, secret = credential["accessKey"], credential["secretKey"]
            require(isinstance(access, str) and re.fullmatch(r"[A-Za-z0-9_-]{16,128}", access)
                    and access not in keys)
            require(isinstance(secret, str) and 32 <= len(secret.encode()) <= 256
                    and not any(char in secret for char in ("\n", "\r", "\0")))
            keys.add(access)
    return json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n"


def admin_config(user, password):
    require(re.fullmatch(r"[a-z][a-z0-9-]{2,63}", user))
    require(32 <= len(password.encode()) <= 256 and
            not any(char in password for char in ("\n", "\r", "\0")))
    return ("[admin]\nuser = " + json.dumps(user, ensure_ascii=False) +
            "\npassword = " + json.dumps(password, ensure_ascii=False) +
            '\n[https.admin]\ncert = "/tls/tls.crt"\nkey = "/tls/tls.key"\n')


def proxy_config(peers):
    require(len(peers) <= 16 and len(set(peers)) == len(peers))
    for peer in peers:
        network = ipaddress.ip_network(peer, strict=True)
        address = network.network_address
        require(network.prefixlen == network.max_prefixlen and not address.is_loopback
                and not address.is_multicast and not address.is_unspecified
                and not getattr(address, "ipv4_mapped", None))
    return "[s3.trusted_proxies]\nwhite_list = " + json.dumps(",".join(peers)) + "\n"


def registry_config(raw, writer, reader):
    require(writer != reader and all(re.fullmatch(r"[a-z][a-z0-9-]{2,63}", user)
                                    for user in (writer, reader)))
    entries = {}
    for line in raw.splitlines():
        user, separator, digest = line.partition(":")
        require(separator and user not in entries and user in (writer, reader))
        require(re.fullmatch(r"\$2[aby]\$1[0-6]\$[./A-Za-z0-9]{53}", digest))
        entries[user] = digest
    require(set(entries) == {writer, reader} and len(set(entries.values())) == 2)
    return "".join(user + ":" + entries[user] + "\n" for user in sorted(entries))


def write_once(path, value):
    data = value.encode()
    if path.exists():
        require(not path.is_symlink() and stat.S_IMODE(path.stat().st_mode) == 0o400
                and secrets.compare_digest(path.read_bytes(), data))
        return
    fd, temporary = tempfile.mkstemp(prefix=".prepared-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(temporary, 0o400)
        os.link(temporary, path, follow_symlinks=False)
    finally:
        os.unlink(temporary)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("storage", "registry"))
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--ui", action="store_true")
    parser.add_argument("--writer")
    parser.add_argument("--reader")
    parser.add_argument("--trusted-proxy", action="append", default=[])
    args = parser.parse_args()
    os.umask(0o077)
    require(not args.input.is_symlink() and not args.output.is_symlink())
    args.output.mkdir(mode=0o700, parents=True, exist_ok=True)
    if args.mode == "storage":
        data = storage_config(read(args.input, "s3.json"))
        ui = admin_config(read(args.input, "ui-user"), read(args.input, "ui-password")) if args.ui else None
        security = proxy_config(args.trusted_proxy) + (ui or "")
        write_once(args.output / "s3.json", data)
        write_once(args.output / "security.toml", security)
        print("Storage credential files prepared")
    else:
        data = registry_config(read(args.input, "htpasswd"), args.writer, args.reader)
        write_once(args.output / "htpasswd", data)
        print("Registry credential file prepared")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError, UnicodeError):
        raise SystemExit("Portable credential preparation rejected") from None
