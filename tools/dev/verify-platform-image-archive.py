"""Ограниченная проверка preserved OCI закрытых platform images репозитория."""

import hashlib, json, os, re, shutil, stat, sys, tarfile
from pathlib import Path

def require(condition):
    if not condition:
        raise ValueError()

def decode(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result)
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=unique)

try:
    stable = sys.argv[1:2] == ['--stable']
    retained = sys.argv[1:2] == ['--retained']
    receipt = None
    if stable:
        _, receipt_path, archive_path, output = sys.argv[1:]
        fd = os.open(receipt_path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, 'rb') as source:
            info = os.fstat(source.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and info.st_nlink == 1
                    and stat.S_IMODE(info.st_mode) == 0o600 and 0 < info.st_size <= 16384)
            receipt = decode(source.read())
        require(set(receipt) == {'schema', 'component', 'sourceSHA', 'contextSHA256', 'imageDigest',
                'archiveSHA256', 'dockerfile', 'target', 'admission', 'publication', 'nodePull'})
        component, expected = receipt['component'], receipt['imageDigest']
        require(component in ('control-plane', 'secret-broker', 'email-bridge', 'runtime-controller',
                'integration-gateway', 'integration-synthetic', 'control-api-gateway', 'staff-control-center',
                'egress-gateway', 'automation-scheduler', 'artifact-retention', 'session-archive', 'registry-pull-authorizer',
                'image-admission-controller'))
        require(receipt['schema'] == 'kodex.stable-local-build/v1'
                and re.fullmatch(r'[a-f0-9]{40}', receipt['sourceSHA']) and receipt['sourceSHA'] != '0' * 40
                and all(re.fullmatch(r'[a-f0-9]{64}', receipt[key]) and receipt[key] != '0' * 64
                        for key in ('contextSHA256', 'archiveSHA256'))
                and all(receipt[key] == 'NOT_RUN' for key in ('admission', 'publication', 'nodePull')))
        registry = decode((Path(__file__).resolve().parents[1] / 'release/images.json').read_bytes())
        recipes = [image for image in registry['images'] if image['component'] == component]
        require(len(recipes) == 1 and receipt['dockerfile'] == recipes[0]['dockerfile']
                and receipt['target'] == recipes[0].get('target', ''))
        fd = os.open(archive_path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, 'rb') as source:
            before = os.fstat(source.fileno())
            require(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and before.st_nlink == 1
                    and stat.S_IMODE(before.st_mode) == 0o600 and 0 < before.st_size <= 8 * 1024**3)
            archive_digest = hashlib.sha256()
            with open(output, 'xb') as copy:
                os.chmod(output, 0o600)
                while block := source.read(1024**2):
                    archive_digest.update(block)
                    copy.write(block)
                copy.flush()
                os.fsync(copy.fileno())
            after = os.fstat(source.fileno())
            require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns)
                    == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns)
                    and archive_digest.hexdigest() == receipt['archiveSHA256'])
    elif retained:
        _, component, archive_path, expected, output = sys.argv[1:]
        require(component in ('image-admission', 'image-admission-tools'))
        match = re.fullmatch(re.escape(component) + r'-([a-f0-9]{64})\.oci\.tar', os.path.basename(archive_path))
        require(match is not None)
        retained_tag = 'local-' + match.group(1)
        parent = os.lstat(os.path.dirname(archive_path))
        require(stat.S_ISDIR(parent.st_mode) and parent.st_uid == os.geteuid() and stat.S_IMODE(parent.st_mode) == 0o700)
        fd = os.open(archive_path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, 'rb') as source:
            before = os.fstat(source.fileno())
            require(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and before.st_nlink == 1
                    and not before.st_mode & 0o002 and 0 < before.st_size <= 8 * 1024**3)
            with open(output, 'xb') as copy:
                os.chmod(output, 0o600)
                shutil.copyfileobj(source, copy, 1024**2)
                copy.flush()
                os.fsync(copy.fileno())
            after = os.fstat(source.fileno())
            require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns)
                    == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns))
    else:
        component, cache, expected, output = sys.argv[1:]
        require(component in ('session-archive', 'role-image-builder'))
    require(re.fullmatch(r'sha256:[a-f0-9]{64}', expected))
    if not stable and not retained:
        cache_info = os.lstat(cache)
        require(stat.S_ISDIR(cache_info.st_mode) and cache_info.st_uid == os.geteuid()
                and stat.S_IMODE(cache_info.st_mode) == 0o700)
    selected = None
    for name in ([] if stable or retained else sorted(os.listdir(cache))):
        if not re.fullmatch(re.escape(component) + r'-[a-f0-9]{64}\.oci\.tar', name):
            continue
        path = os.path.join(cache, name)
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, 'rb') as source:
            before = os.fstat(source.fileno())
            require(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid()
                    and 0 < before.st_size <= 8 * 1024**3)
            with tarfile.open(fileobj=source, mode='r:') as tar:
                index = None
                for number, member in enumerate(tar):
                    require(number < 1024)
                    if member.name == 'index.json':
                        require(member.isreg() and member.size <= 4 * 1024**2)
                        index = decode(tar.extractfile(member).read())
                        break
                require(index is not None)
            if len(index.get('manifests', [])) != 1 or index['manifests'][0].get('digest') != expected:
                continue
            source.seek(0)
            with open(output, 'xb') as copy:
                os.chmod(output, 0o600)
                shutil.copyfileobj(source, copy, 1024**2)
            after = os.fstat(source.fileno())
            require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns)
                    == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns))
            selected = name
            break
    require(stable or retained or selected is not None)
    with tarfile.open(output, mode='r:') as tar:
        members = {}
        for member in tar:
            name = member.name.rstrip('/')
            require(name not in members and len(members) < 1024)
            if member.isdir():
                require(name in ('blobs', 'blobs/sha256'))
            else:
                require(member.isreg() and (name in ('index.json', 'oci-layout')
                        or re.fullmatch(r'blobs/sha256/[a-f0-9]{64}', name)))
            members[name] = member
            if name.startswith('blobs/sha256/'):
                digest = hashlib.sha256()
                with tar.extractfile(member) as stream:
                    while block := stream.read(1024**2):
                        digest.update(block)
                require(digest.hexdigest() == name.rsplit('/', 1)[1])
        def document(name):
            require(name in members and members[name].isreg() and members[name].size <= 4 * 1024**2)
            return decode(tar.extractfile(members[name]).read())
        def descriptor(value, media_types):
            require(isinstance(value, dict) and value.get('mediaType') in media_types
                    and re.fullmatch(r'sha256:[a-f0-9]{64}', value.get('digest', ''))
                    and type(value.get('size')) is int and value['size'] > 0)
            if stable or retained:
                require('urls' not in value and 'data' not in value)
            name = 'blobs/sha256/' + value['digest'][7:]
            require(name in members and members[name].size == value['size'])
            return name
        require(document('oci-layout') == {'imageLayoutVersion': '1.0.0'})
        index = document('index.json')
        require(index.get('schemaVersion') == 2 and len(index.get('manifests', [])) == 1)
        desc = index['manifests'][0]
        require(desc['digest'] == expected)
        annotation = desc.get('annotations', {})
        tag = receipt['sourceSHA'] if stable else retained_tag if retained else 'local-' + selected[len(component + '-'):-len('.oci.tar')]
        require(annotation.get('org.opencontainers.image.ref.name') == tag
                and annotation.get('io.containerd.image.name') == 'registry.local.kodex/kodex/' + component + ':' + tag)
        manifest = document(descriptor(desc, {'application/vnd.oci.image.manifest.v1+json'}))
        require(manifest.get('schemaVersion') == 2)
        config = document(descriptor(manifest['config'], {'application/vnd.oci.image.config.v1+json'}))
        entrypoint = component + '-entrypoint' if component == 'staff-control-center' else component
        require(config.get('os') == 'linux' and config.get('architecture') == 'amd64')
        if retained:
            require(config.get('config', {}).get('User') == '10001:10001'
                    and config['config'].get('Entrypoint') is None and config['config'].get('Cmd') == ['/bin/sh']
                    and re.fullmatch(r'[a-f0-9]{40}', config['config'].get('Labels', {}).get('org.opencontainers.image.revision', '')))
        else:
            require(config.get('config', {}).get('Entrypoint') == ['/usr/local/bin/' + entrypoint])
        if stable:
            require(config.get('config', {}).get('User') == ('101:101' if component == 'staff-control-center' else '10001:10001'))
            if component in ('staff-control-center', 'image-admission-controller'):
                require(config['config'].get('Labels', {}).get('org.opencontainers.image.revision') == receipt['sourceSHA'])
        layers = manifest['layers']
        require(isinstance(layers, list) and 0 < len(layers) <= 128)
        for layer in layers:
            descriptor(layer, {'application/vnd.oci.image.layer.v1.tar', 'application/vnd.oci.image.layer.v1.tar+gzip'})
        if stable or retained:
            reachable = {'blobs/sha256/' + desc['digest'][7:], 'blobs/sha256/' + manifest['config']['digest'][7:]}
            reachable.update('blobs/sha256/' + layer['digest'][7:] for layer in layers)
            require(reachable == {name for name in members if name.startswith('blobs/sha256/')})
        # tar parser останавливается на zero header: скрытый второй archive
        # не должен попасть в regctl при тех же проверенных descriptors.
        with open(output, 'rb') as remaining:
            remaining.seek(tar.offset)
            while block := remaining.read(1024**2):
                require(not any(block))
    os.chmod(output, 0o400)
except (OSError, ValueError, KeyError, TypeError, AttributeError, tarfile.TarError):
    print('Platform image OCI verification failed', file=sys.stderr)
    sys.exit(1)
