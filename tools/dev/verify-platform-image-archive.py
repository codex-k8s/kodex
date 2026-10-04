"""Ограниченная проверка preserved OCI закрытых platform images репозитория."""

import hashlib, json, os, re, shutil, stat, sys, tarfile

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
    component, cache, expected, output = sys.argv[1:]
    require(component in ('session-archive', 'role-image-builder'))
    require(re.fullmatch(r'sha256:[a-f0-9]{64}', expected))
    cache_info = os.lstat(cache)
    require(stat.S_ISDIR(cache_info.st_mode) and cache_info.st_uid == os.geteuid()
            and stat.S_IMODE(cache_info.st_mode) == 0o700)
    selected = None
    for name in sorted(os.listdir(cache)):
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
    require(selected is not None)
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
            name = 'blobs/sha256/' + value['digest'][7:]
            require(name in members and members[name].size == value['size'])
            return name
        require(document('oci-layout') == {'imageLayoutVersion': '1.0.0'})
        index = document('index.json')
        require(index.get('schemaVersion') == 2 and len(index.get('manifests', [])) == 1)
        desc = index['manifests'][0]
        require(desc['digest'] == expected)
        annotation = desc.get('annotations', {})
        tag = 'local-' + selected[len(component + '-'):-len('.oci.tar')]
        require(annotation.get('org.opencontainers.image.ref.name') == tag
                and annotation.get('io.containerd.image.name') == 'registry.local.kodex/kodex/' + component + ':' + tag)
        manifest = document(descriptor(desc, {'application/vnd.oci.image.manifest.v1+json'}))
        require(manifest.get('schemaVersion') == 2)
        config = document(descriptor(manifest['config'], {'application/vnd.oci.image.config.v1+json'}))
        require(config.get('os') == 'linux' and config.get('architecture') == 'amd64'
                and config.get('config', {}).get('Entrypoint') == ['/usr/local/bin/' + component])
        layers = manifest['layers']
        require(isinstance(layers, list) and 0 < len(layers) <= 128)
        for layer in layers:
            descriptor(layer, {'application/vnd.oci.image.layer.v1.tar', 'application/vnd.oci.image.layer.v1.tar+gzip'})
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
