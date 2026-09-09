#!/usr/bin/env python3
"""Read-only проверка exact OCI base; файловая система образа не извлекается."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tarfile
import tempfile

TARGET = 'usr/local/bin/kodex-agent-runner'
INPUTS = ('services/jobs/agent-runner', 'libs/go')
OCI = 'application/vnd.oci.image.'
LIMIT = 8 * 1024**3
JSON_LIMIT = 4 * 1024**2
MEMBER_LIMIT = 500000


class Failure(Exception):
    pass


def require(value, code):
    if not value:
        raise Failure(code)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def stream_hash(stream, limit=LIMIT, sink=None):
    digest, count = hashlib.sha256(), 0
    while True:
        block = stream.read(1024 * 1024)
        if not block:
            break
        count += len(block)
        require(count <= limit, 'SIZE_LIMIT_EXCEEDED')
        digest.update(block)
        if sink is not None:
            sink.write(block)
    return digest.hexdigest(), count


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, 'JSON_DUPLICATE_KEY')
        value[key] = item
    return value


def decode(data):
    require(len(data) <= JSON_LIMIT, 'JSON_SIZE_EXCEEDED')
    try:
        value = json.loads(data, object_pairs_hook=unique_object)
    except (ValueError, UnicodeError):
        raise Failure('JSON_INVALID') from None
    require(isinstance(value, dict), 'JSON_OBJECT_REQUIRED')
    return value


def fingerprint(s):
    return (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns, s.st_ctime_ns)


def regular_open(path):
    require(Path(path).is_absolute() and Path(path).resolve() == Path(path), 'PATH_INVALID')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    info = os.fstat(fd)
    if not (stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and 0 < info.st_size <= LIMIT):
        os.close(fd)
        raise Failure('REGULAR_INPUT_REQUIRED')
    return os.fdopen(fd, 'rb'), info


def command(args, cwd):
    result = subprocess.run(args, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=120,
                            env={k: v for k, v in os.environ.items() if k not in ('TAR_OPTIONS', 'GIT_INDEX_FILE', 'GIT_WORK_TREE', 'GIT_DIR')})
    require(result.returncode == 0, 'SOURCE_COMMAND_FAILED')
    return result.stdout


def source_input(source, revision):
    source = Path(source)
    require(source.is_absolute() and source.resolve() == source and re.fullmatch('[a-f0-9]{40}', revision), 'SOURCE_INVALID')
    require(command(['git', 'rev-parse', '--show-toplevel'], source).decode().strip() == str(source), 'SOURCE_ROOT_INVALID')
    require(command(['git', 'rev-parse', 'HEAD'], source).decode().strip() == revision, 'SOURCE_REVISION_MISMATCH')
    require(not command(['git', 'status', '--porcelain=v1', '--untracked-files=all'], source), 'SOURCE_DIRTY')
    require(not command(['git', 'ls-files', '--others', '--ignored', '--exclude-standard', '--', *INPUTS], source), 'SOURCE_IGNORED_INPUT')
    # Совпадает с прежним builder: GNU tar, mtime/owner/group/order, без Git archive.
    with tempfile.TemporaryFile() as error:
        process = subprocess.Popen(['tar', '--sort=name', "--mtime=UTC 1970-01-01", '--owner=0', '--group=0', '--numeric-owner',
                                    '-C', str(source), '-cf', '-', *INPUTS], stdout=subprocess.PIPE, stderr=error,
                                   env={k: v for k, v in os.environ.items() if k != 'TAR_OPTIONS'})
        try:
            digest, _ = stream_hash(process.stdout)
            require(process.wait(timeout=120) == 0, 'SOURCE_TAR_FAILED')
        finally:
            process.stdout.close()
            if process.poll() is None:
                process.kill()
                process.wait()
    require(not command(['git', 'status', '--porcelain=v1', '--untracked-files=all'], source), 'SOURCE_CHANGED')
    require(command(['git', 'rev-parse', 'HEAD'], source).decode().strip() == revision, 'SOURCE_CHANGED')
    return digest


def member_path(name):
    require(isinstance(name, str) and '\\' not in name and '\x00' not in name and not name.startswith('/'), 'TAR_PATH_INVALID')
    if name.startswith('./'):
        name = name[2:]
    name = name.rstrip('/')
    if name in ('', '.'):
        return ''
    require(all(p not in ('', '.', '..') for p in name.split('/')), 'TAR_PATH_INVALID')
    return name


def tar_members(archive):
    seen = set()
    for member in archive:
        require(len(seen) < MEMBER_LIMIT and not member.sparse, 'TAR_ENTRY_INVALID')
        name = member_path(member.name)
        require(name not in seen, 'TAR_DUPLICATE_PATH')
        seen.add(name)
        require(0 <= member.size <= LIMIT and (name or member.isdir()) and (member.isreg() or member.size == 0), 'TAR_ENTRY_INVALID')
        yield name, member


def validate_tar_tail(archive):
    # tarfile прекращает чтение на первом zero header; скрытый второй tar запрещён.
    archive.fileobj.seek(archive.offset)
    total = 0
    while True:
        block = archive.fileobj.read(1024 * 1024)
        if not block:
            break
        total += len(block)
        require(not any(block) and total <= LIMIT, 'TAR_TRAILING_DATA')
    require(total >= 1024 and total % 512 == 0, 'TAR_TERMINATOR_INVALID')


def ancestors(path):
    parts = path.split('/')
    return ['/'.join(parts[:i]) for i in range(1, len(parts))]


def remove(tree, path, children_only=False):
    for key in list(tree):
        if (key == path and not children_only) or key.startswith(path + '/') or (not path and children_only):
            del tree[key]


def overlay_layer(tree, layer):
    entries, whiteouts = {}, []
    with tarfile.open(fileobj=layer, mode='r:') as tar:
        for name, member in tar_members(tar):
            if not name:
                continue
            parent, _, base = name.rpartition('/')
            if base.startswith('.wh.'):
                require(member.isreg() and member.size == 0, 'WHITEOUT_INVALID')
                if base == '.wh..wh..opq':
                    whiteouts.append((parent, True))
                else:
                    removed = base[4:]
                    require(removed not in ('', '.', '..') and not removed.startswith('.wh.'), 'WHITEOUT_INVALID')
                    whiteouts.append(((parent + '/' if parent else '') + removed, False))
                continue
            kind = 'dir' if member.isdir() else 'file' if member.isreg() else 'link' if member.issym() or member.islnk() else 'special'
            digest = None
            if name == TARGET and kind == 'file':
                with tar.extractfile(member) as content:
                    digest, size = stream_hash(content)
                require(size == member.size and size > 0, 'RUNNER_CONTENT_INVALID')
            entries[name] = (kind, member.mode, digest)
        validate_tar_tail(tar)
    # Whiteouts затрагивают только нижние слои независимо от порядка tar entries.
    for path, opaque in whiteouts:
        remove(tree, path, opaque)
    for name in sorted(entries, key=lambda p: (p.count('/'), p)):
        for parent in ancestors(name):
            require(parent not in entries or entries[parent][0] == 'dir', 'LAYER_PARENT_AMBIGUOUS')
            require(parent not in tree or tree[parent][0] == 'dir', 'LAYER_PARENT_NOT_DIRECTORY')
            tree.setdefault(parent, ('dir', 0o755, None))
        item = entries[name]
        if name in tree and tree[name][0] == 'dir' and item[0] != 'dir':
            remove(tree, name)
        tree[name] = item
    # Посторонние distro links не следуют на host; target/его ancestry — только regular/dir.
    for parent in ancestors(TARGET):
        require(parent not in tree or tree[parent][0] == 'dir', 'RUNNER_PARENT_UNSAFE')
    require(TARGET not in tree or tree[TARGET][0] == 'file', 'RUNNER_LINK_OR_SPECIAL')


def descriptor(value, media):
    require(isinstance(value, dict) and value.get('mediaType') in media and re.fullmatch(r'sha256:[a-f0-9]{64}', value.get('digest', ''))
            and type(value.get('size')) is int and 0 < value['size'] <= LIMIT and not value.get('urls') and 'data' not in value, 'DESCRIPTOR_INVALID')
    return 'blobs/sha256/' + value['digest'][7:]


def verify_archive(archive_path, expected, input_digest, repository):
    require(Path(archive_path).name == f'agent-runner-{input_digest}.oci.tar', 'ARCHIVE_INPUT_BINDING_MISMATCH')
    archive, before = regular_open(archive_path)
    with archive:
        archive_hash, _ = stream_hash(archive)
        archive.seek(0)
        with tarfile.open(fileobj=archive, mode='r:') as tar:
            members = {}
            for name, member in tar_members(tar):
                if member.isdir():
                    require(name in ('', 'blobs', 'blobs/sha256'), 'OCI_DIRECTORY_INVALID')
                    continue
                require(member.isreg() and (name in ('index.json', 'oci-layout') or re.fullmatch('blobs/sha256/[a-f0-9]{64}', name)), 'OCI_ENTRY_INVALID')
                members[name] = member
            validate_tar_tail(tar)
            def raw(name, limit=JSON_LIMIT):
                require(name in members and members[name].size <= limit, 'OCI_BLOB_MISSING_OR_LARGE')
                with tar.extractfile(members[name]) as stream:
                    return stream.read(limit + 1)
            require(decode(raw('oci-layout')) == {'imageLayoutVersion': '1.0.0'}, 'OCI_LAYOUT_INVALID')
            # Проверка всех blobs, включая недостижимые; filename не заменяет hash bytes.
            for name, member in members.items():
                if name.startswith('blobs/'):
                    with tar.extractfile(member) as stream:
                        digest, size = stream_hash(stream)
                    require(digest == name.split('/')[-1] and size == member.size, 'OCI_BLOB_DIGEST_MISMATCH')
            index_bytes = raw('index.json')
            index = decode(index_bytes)
            require(index.get('schemaVersion') == 2 and index.get('mediaType', OCI + 'index.v1+json') == OCI + 'index.v1+json'
                    and isinstance(index.get('manifests'), list) and len(index['manifests']) == 1, 'OCI_INDEX_INVALID')
            def blob(desc, types):
                name = descriptor(desc, types)
                require(name in members and members[name].size == desc['size'], 'DESCRIPTOR_SIZE_MISMATCH')
                return name
            manifest_desc = index['manifests'][0]
            expected_annotations = {'io.containerd.image.name': repository + ':local-' + input_digest,
                                    'org.opencontainers.image.ref.name': 'local-' + input_digest}
            annotations = manifest_desc.get('annotations', {})
            require(isinstance(annotations, dict) and all(annotations.get(k) == v for k, v in expected_annotations.items()), 'SOURCE_TAG_BINDING_MISMATCH')
            index_annotations = index.get('annotations', {})
            require(isinstance(index_annotations, dict) and all(k not in index_annotations or index_annotations[k] == v for k, v in expected_annotations.items()), 'SOURCE_TAG_BINDING_MISMATCH')
            manifest_name = blob(manifest_desc, {OCI + 'manifest.v1+json'})
            require(manifest_desc['digest'] == expected, 'MANIFEST_MISMATCH')
            manifest = decode(raw(manifest_name))
            require(manifest.get('schemaVersion') == 2 and manifest.get('mediaType') == OCI + 'manifest.v1+json', 'MANIFEST_INVALID')
            config = decode(raw(blob(manifest.get('config'), {OCI + 'config.v1+json'})))
            layers = manifest.get('layers')
            require(config.get('os') == 'linux' and config.get('architecture') == 'amd64' and isinstance(layers, list) and 0 < len(layers) <= 128, 'IMAGE_PLATFORM_INVALID')
            rootfs = config.get('rootfs', {})
            require(rootfs.get('type') == 'layers' and isinstance(rootfs.get('diff_ids'), list) and len(rootfs['diff_ids']) == len(layers), 'DIFF_IDS_INVALID')
            tree, proof = {}, []
            for number, desc in enumerate(layers):
                name = blob(desc, {OCI + 'layer.v1.tar', OCI + 'layer.v1.tar+gzip'})
                with tar.extractfile(members[name]) as compressed, tempfile.TemporaryFile() as expanded:
                    stream = gzip.GzipFile(fileobj=compressed) if desc['mediaType'].endswith('+gzip') else compressed
                    digest, size = stream_hash(stream, sink=expanded)
                    require(rootfs['diff_ids'][number] == 'sha256:' + digest, 'LAYER_DIFF_ID_MISMATCH')
                    expanded.seek(0)
                    overlay_layer(tree, expanded)
                proof.append({'digest': desc['digest'], 'size': desc['size'], 'diffID': 'sha256:' + digest, 'uncompressedSize': size})
            require(TARGET in tree and tree[TARGET][0] == 'file' and tree[TARGET][1] & 0o111 and not tree[TARGET][1] & 0o6000, 'RUNNER_EXECUTABLE_REQUIRED')
            require(fingerprint(os.fstat(archive.fileno())) == fingerprint(before) == fingerprint(os.stat(archive_path, follow_symlinks=False)), 'ARCHIVE_CHANGED')
            return {'binarySHA256': tree[TARGET][2], 'archiveSHA256': archive_hash, 'manifestSHA256': expected[7:],
                    'configSHA256': manifest['config']['digest'][7:], 'indexSHA256': sha(index_bytes),
                    'sourceImageTag': expected_annotations['io.containerd.image.name'], 'layers': proof}


def private_parent(path):
    path = Path(path)
    require(path.is_absolute() and path.parent.resolve() == path.parent and path.name not in ('', '.', '..'), 'OUTPUT_PATH_INVALID')
    fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    info = os.fstat(fd)
    if not (stat.S_IMODE(info.st_mode) == 0o700 and info.st_uid == os.geteuid()):
        os.close(fd)
        raise Failure('PRIVATE_OUTPUT_DIRECTORY_REQUIRED')
    return fd


def publish(path, value, check_existing=False):
    data = (json.dumps(value, sort_keys=True, indent=2) + '\n').encode()
    parent = private_parent(path)
    temporary = '.runner-provenance-' + os.urandom(16).hex()
    try:
        if check_existing:
            fd = os.open(Path(path).name, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=parent)
            with os.fdopen(fd, 'rb') as file:
                info = os.fstat(file.fileno())
                require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o600 and info.st_uid == os.geteuid(), 'PRIVATE_OUTPUT_INVALID')
                require(file.read(JSON_LIMIT + 1) == data, 'EXISTING_PROVENANCE_MISMATCH')
            return sha(data)
        fd = os.open(temporary, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600, dir_fd=parent)
        with os.fdopen(fd, 'wb') as file:
            file.write(data)
            file.flush()
            os.fsync(file.fileno())
        os.link(temporary, Path(path).name, src_dir_fd=parent, dst_dir_fd=parent, follow_symlinks=False)
        os.unlink(temporary, dir_fd=parent)
        os.fsync(parent)
        require(fingerprint(os.stat(Path(path).parent))[:2] == fingerprint(os.fstat(parent))[:2], 'OUTPUT_DIRECTORY_CHANGED')
        return sha(data)
    finally:
        try:
            os.unlink(temporary, dir_fd=parent)
        except FileNotFoundError:
            pass
        os.close(parent)


def main(argv):
    parser = argparse.ArgumentParser(description='Verify a preserved OCI runner archive without image execution or extraction.', allow_abbrev=False)
    parser.add_argument('phase', choices=('input', 'verify', 'check'))
    parser.add_argument('--source-root', required=True)
    parser.add_argument('--revision', required=True)
    parser.add_argument('--archive')
    parser.add_argument('--expected-manifest')
    parser.add_argument('--expected-input-digest')
    parser.add_argument('--repository')
    parser.add_argument('--output')
    args = parser.parse_args(argv)
    # Не допускаем неоднозначного повторения security-significant flags.
    flags = [v.split('=', 1)[0] for v in argv if v.startswith('--')]
    require(len(flags) == len(set(flags)), 'ARGUMENT_DUPLICATE')
    digest = source_input(args.source_root, args.revision)
    if args.phase == 'input':
        require(not any((args.archive, args.expected_manifest, args.expected_input_digest, args.repository, args.output)), 'ARGUMENT_INVALID')
        print(digest)
        return
    require(args.expected_input_digest == digest, 'SOURCE_INPUT_MISMATCH')
    require(args.archive and args.output and re.fullmatch('sha256:[a-f0-9]{64}', args.expected_manifest or '')
            and re.fullmatch('[a-z0-9][a-z0-9./:_-]*', args.repository or ''), 'ARGUMENT_INVALID')
    proof = verify_archive(args.archive, args.expected_manifest, digest, args.repository)
    require(source_input(args.source_root, args.revision) == digest, 'SOURCE_CHANGED')
    value = {'version': 1, 'kind': 'RUNNER_BINARY_PROVENANCE', 'sourceRevision': args.revision,
             'sourceInputSHA256': digest, 'baseImage': args.repository + '@' + args.expected_manifest,
             'binaryPath': '/' + TARGET, **proof}
    output_digest = publish(args.output, value, args.phase == 'check')
    print(json.dumps({'status': 'PASS', 'provenanceSHA256': output_digest, 'binarySHA256': proof['binarySHA256']}))


if __name__ == '__main__':
    try:
        main(sys.argv[1:])
    except (Failure, OSError, ValueError, TypeError, KeyError, AttributeError, tarfile.TarError, EOFError, subprocess.SubprocessError) as error:
        print('Runner provenance failed: ' + (str(error) if isinstance(error, Failure) else 'VERIFICATION_FAILED'), file=sys.stderr)
        sys.exit(1)
