"""Удаляет только явно выбранный завершённый worktree после проверки оператора.

Оператор подтверждает происхождение и завершение потребителей самостоятельно.
Матрица: оператор → preflight → --apply → сохранённый ref → non-force remove.
Без --apply проверка не меняет Git или файлы. После удаления восстановление:
git worktree add --detach <точный прежний путь> <сохранённый SHA>.
Ref refs/kodex/cleanup-preserved/<SHA> сохраняется и после восстановления.
"""

import argparse
import json
import os
from pathlib import Path
import re
import stat
import subprocess


class Failure(Exception):
    pass


def require(condition, code):
    if not condition:
        raise Failure(code)


def git(repository, *arguments, missing=False):
    environment = {
        "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
        "LC_ALL": "C", "GIT_CONFIG_NOSYSTEM": "1",
        "GIT_CONFIG_GLOBAL": os.devnull, "GIT_OPTIONAL_LOCKS": "0",
        "GIT_TERMINAL_PROMPT": "0",
    }
    try:
        result = subprocess.run(
            ["git", "-C", str(repository), *arguments], env=environment,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        raise Failure("GIT_COMMAND_FAILED") from None
    if missing and result.returncode == 1:
        return None
    require(result.returncode == 0, "GIT_COMMAND_FAILED")
    return result.stdout.rstrip(b"\n")


def owned(path, kind):
    try:
        metadata = path.lstat()
    except OSError:
        raise Failure("PATH_UNAVAILABLE") from None
    require(metadata.st_uid == os.getuid(), "FOREIGN_OWNER")
    require(kind(metadata.st_mode), "PATH_TYPE_INVALID")
    return metadata


def exact_path(value):
    path = Path(value)
    require(path.is_absolute() and str(path) == value, "PATH_NOT_EXACT")
    require(path.resolve() == path, "SYMLINK_PATH_FORBIDDEN")
    return path


def credential_name(name):
    name = name.lower()
    return (
        name in {".env", ".kodex-env", ".kodex-remote-env", "auth.json",
                 "credentials", "credentials.json", "kubeconfig",
                 "id_rsa", "id_ed25519", "cookies", "cookie.jar"}
        or name.startswith((".env.", "agent-secrets.", "credentials."))
        or name.endswith((".env", ".pem", ".key"))
    )


def inventory(target):
    inodes = set()
    pending = [target]
    while pending:
        path = pending.pop()
        require(not credential_name(path.name), "CREDENTIAL_PATH_FORBIDDEN")
        metadata = owned(path, lambda mode: stat.S_ISREG(mode) or stat.S_ISDIR(mode))
        inodes.add((metadata.st_dev, metadata.st_ino))
        if stat.S_ISDIR(metadata.st_mode):
            try:
                pending.extend(path.iterdir())
            except OSError:
                raise Failure("PATH_UNAVAILABLE") from None
    return len(inodes)


def registered(repository, target, head):
    entries = git(repository, "worktree", "list", "--porcelain", "-z").split(b"\0\0")
    expected = os.fsencode("worktree " + str(target))
    matches = [entry.split(b"\0") for entry in entries
               if entry.split(b"\0")[0] == expected]
    require(len(matches) == 1, "WORKTREE_NOT_REGISTERED")
    require(b"HEAD " + head.encode() in matches[0], "HEAD_MISMATCH")
    require(not any(field.startswith((b"locked", b"prunable")) for field in matches[0]),
            "WORKTREE_UNAVAILABLE")


def preflight(repository, target_value, head, permitted_root):
    require(re.fullmatch(r"[0-9a-f]{40}", head) is not None, "HEAD_NOT_EXACT")
    repository = exact_path(str(repository))
    target = exact_path(target_value)
    root = exact_path(str(permitted_root))
    require(target != root and target.is_relative_to(root), "TARGET_OUTSIDE_TRUSTED_ROOT")
    require(not repository.is_relative_to(target), "CURRENT_REPOSITORY_PROTECTED")
    owned(repository, stat.S_ISDIR)
    owned(target, stat.S_ISDIR)
    require(git(repository, "rev-parse", "--show-toplevel") == os.fsencode(repository),
            "CURRENT_REPOSITORY_NOT_ROOT")
    common = exact_path(os.fsdecode(git(repository, "rev-parse", "--path-format=absolute", "--git-common-dir")))
    owned(common, stat.S_ISDIR)
    owned(target / ".git", stat.S_ISREG)
    inode_count = inventory(target)
    registered(repository, target, head)
    require(git(target, "rev-parse", "--show-toplevel") == os.fsencode(target),
            "WORKTREE_ROOT_MISMATCH")
    require(git(target, "rev-parse", "--path-format=absolute", "--git-common-dir") == os.fsencode(common),
            "COMMON_GIT_MISMATCH")
    administrative = exact_path(os.fsdecode(git(target, "rev-parse", "--absolute-git-dir")))
    require(administrative.is_relative_to(common / "worktrees"), "WORKTREE_ADMIN_INVALID")
    owned(administrative, stat.S_ISDIR)
    require(git(target, "rev-parse", "HEAD").decode() == head, "HEAD_MISMATCH")
    require(not git(target, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching"),
            "WORKTREE_NOT_CLEAN")
    return repository, target, inode_count


def _cleanup(repository, target_value, head, *, apply=False, permitted_root=Path("/tmp")):
    repository, target, inode_count = preflight(repository, target_value, head, permitted_root)
    ref = "refs/kodex/cleanup-preserved/" + head
    require(git(repository, "symbolic-ref", "--quiet", ref, missing=True) is None,
            "PRESERVED_REF_NOT_DIRECT")
    preserved = git(repository, "rev-parse", "--verify", "--quiet", ref, missing=True)
    require(preserved is None or preserved.decode() == head, "PRESERVED_REF_MISMATCH")
    if apply:
        if preserved is None:
            git(repository, "update-ref", ref, head, "0" * 40)
        require(git(repository, "show-ref", "--verify", "--hash", ref).decode() == head,
                "PRESERVED_REF_MISMATCH")
        # Повторная проверка сразу перед non-force удалением.
        preflight(repository, target_value, head, permitted_root)
        git(repository, "worktree", "remove", "--", str(target))
        require(not target.exists() and not target.is_symlink(), "WORKTREE_STILL_PRESENT")
        entries = git(repository, "worktree", "list", "--porcelain", "-z").split(b"\0")
        require(os.fsencode("worktree " + str(target)) not in entries, "WORKTREE_STILL_REGISTERED")
        require(git(repository, "show-ref", "--verify", "--hash", ref).decode() == head,
                "PRESERVED_REF_MISMATCH")
    return {"status": "PASS", "path": str(target), "head": head,
            "refStatus": "PRESERVED" if apply or preserved else "ABSENT",
            "inodeCount": inode_count}


def main():
    parser = argparse.ArgumentParser(description="Check or remove an operator-selected completed worktree")
    parser.add_argument("--worktree", required=True)
    parser.add_argument("--expected-head", required=True)
    parser.add_argument("--apply", action="store_true")
    options = parser.parse_args()
    try:
        # Доверенный production root не настраивается оператором через CLI.
        result = _cleanup(Path.cwd(), options.worktree, options.expected_head,
                          apply=options.apply, permitted_root=Path("/tmp"))
    except (Failure, OSError, ValueError):
        # Пути и строки внешних ошибок не выводятся: они могут содержать секреты.
        print(json.dumps({"status": "FAIL"}))
        return 1
    print(json.dumps(result))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
