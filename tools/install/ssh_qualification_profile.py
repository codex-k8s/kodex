"""Доставка canonical Go SSH qualification overlay без сети и нового decoder."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import stat
import tempfile

import yaml
from deployment_profile import Rejected, require
from ssh_identity_profile import container, unique_object

ROOT = Path(__file__).resolve().parents[2]
LABEL = 'kodex.dev/ssh-qualification'
MAXIMUM_BYTES = 262144
ENVIRONMENT = {'SECRET_BROKER_SSH_QUALIFICATION_FILE', 'SECRET_BROKER_SSH_QUALIFICATION_SHA256'}
MOUNT = 'ssh-qualifications'


def canonical_projection(raw):
    """Единственный schema decoder принадлежит Go runtime; caller не выбирает команду."""
    require(isinstance(raw, bytes) and 0 < len(raw) <= MAXIMUM_BYTES, 'SSH_QUALIFICATION_INPUT_REJECTED')
    environment = os.environ.copy()
    environment.pop('GOFLAGS', None)
    environment.update(GOWORK='off', GOENV='off', GOTOOLCHAIN='local')
    with tempfile.TemporaryDirectory(prefix='kodex-ssh-qualification-') as temporary:
        directory = Path(temporary)
        source = directory/'profiles.json'
        fd = os.open(source, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(raw)
            stream.flush()
            os.fchmod(stream.fileno(), 0o440)
        result = subprocess.run(['go', 'run', './cmd/ssh-qualification', 'render', '--directory', str(directory),
            '--input', 'profiles.json', '--output', 'bundle.json'], cwd=ROOT/'tools/managed-ssh-helper',
            env=environment, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=120, check=False)
        require(result.returncode == 0 and len(result.stdout) <= 1024, 'SSH_QUALIFICATION_CANONICAL_RENDER_REJECTED')
        output = directory/'bundle.json'
        require(output.is_file() and not output.is_symlink() and output.stat().st_size <= MAXIMUM_BYTES,
                'SSH_QUALIFICATION_BUNDLE_REJECTED')
        encoded = output.read_bytes()
    receipt = json.loads(result.stdout, object_pairs_hook=unique_object)
    require(set(receipt) == {'status', 'artifactSHA256', 'bundleSHA256'} and
            receipt['status'] == 'RENDERED_LOCAL_RECORD' and
            receipt['artifactSHA256'] == hashlib.sha256(raw).hexdigest() and
            receipt['bundleSHA256'] == hashlib.sha256(encoded).hexdigest(), 'SSH_QUALIFICATION_RENDER_BINDING_REJECTED')
    bundle = json.loads(encoded, object_pairs_hook=unique_object)
    require(set(bundle) == {'schema', 'artifactSHA256', 'configMap', 'deploymentPatch', 'networkPolicy'} and
            bundle['schema'] == 'kodex.ssh-qualification-overlay/v1' and
            bundle['artifactSHA256'] == receipt['artifactSHA256'], 'SSH_QUALIFICATION_BUNDLE_REJECTED')
    return bundle


def qualification_objects(resources):
    return [item for item in resources if LABEL in item.get('metadata', {}).get('labels', {})]


def broker_patch(resources):
    pod, broker = container(resources, 'secret-broker')
    return pod, broker, broker.get('env', []), broker.get('volumeMounts', []), pod.get('volumes', [])


def validate_render(resources):
    selected = qualification_objects(resources)
    pod, broker, environment, mounts, volumes = broker_patch(resources)
    settings = [item for item in environment if item.get('name') in ENVIRONMENT]
    mount = [item for item in mounts if item.get('name') == MOUNT]
    volume = [item for item in volumes if item.get('name') == MOUNT]
    deployments = [item for item in resources if item.get('kind') == 'Deployment' and
                   item.get('metadata', {}).get('namespace') == 'kodex-system' and item['metadata']['name'] == 'secret-broker']
    require(len(deployments) == 1, 'SSH_QUALIFICATION_BROKER_REQUIRED')
    annotations = deployments[0]['spec']['template']['metadata'].get('annotations', {})
    digest = annotations.get('kodex.dev/ssh-qualification-sha256')
    if not selected:
        require(not settings and not mount and not volume and digest is None, 'SSH_QUALIFICATION_PARTIAL_PROFILE_REJECTED')
        return
    require(len(selected) == 2, 'SSH_QUALIFICATION_EXACT_OBJECTS_REQUIRED')
    configs = [item for item in selected if item.get('kind') == 'ConfigMap']
    require(len(configs) == 1 and set(configs[0].get('data', {})) == {'profiles.json'}, 'SSH_QUALIFICATION_RECORD_REQUIRED')
    raw = configs[0]['data']['profiles.json'].encode()
    bundle = canonical_projection(raw)
    require(configs[0] == bundle['configMap'] and
            [item for item in selected if item.get('kind') == 'NetworkPolicy'] == [bundle['networkPolicy']],
            'SSH_QUALIFICATION_OBJECT_DRIFT')
    expected = bundle['deploymentPatch']['spec']['template']
    wanted_pod = expected['spec']
    wanted_broker = wanted_pod['containers'][0]
    require(settings == wanted_broker['env'] and mount == wanted_broker['volumeMounts'] and volume == wanted_pod['volumes'] and
            digest == expected['metadata']['annotations']['kodex.dev/ssh-qualification-sha256'], 'SSH_QUALIFICATION_BROKER_DRIFT')


def transform(resources, raw):
    validate_render(resources)
    require(not qualification_objects(resources), 'SSH_QUALIFICATION_ALREADY_CONFIGURED')
    bundle = canonical_projection(raw)
    # Реальный strategic merge consumer сохраняет чужие env, mounts, containers и image.
    with tempfile.TemporaryDirectory(prefix='kodex-ssh-overlay-') as temporary:
        directory = Path(temporary)
        files = {'base.json': {'apiVersion': 'v1', 'kind': 'List', 'items': copy.deepcopy(resources)},
                 'qualification.json': bundle['configMap'], 'policy.json': bundle['networkPolicy'],
                 'patch.json': bundle['deploymentPatch'],
                 'kustomization.yaml': {'apiVersion': 'kustomize.config.k8s.io/v1beta1', 'kind': 'Kustomization',
                     'resources': ['base.json', 'qualification.json', 'policy.json'], 'patches': [{'path': 'patch.json'}]}}
        for name, value in files.items():
            (directory/name).write_text(json.dumps(value))
        result = subprocess.run(['kubectl', '--kubeconfig', '/synthetic/unused', '--context', 'synthetic-offline',
            'kustomize', str(directory)], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=30, check=False)
        require(result.returncode == 0 and len(result.stdout) <= 32 << 20, 'SSH_QUALIFICATION_PATCH_REJECTED')
    rendered = list(filter(None, yaml.safe_load_all(result.stdout)))
    validate_render(rendered)
    return rendered


def main():
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--configuration', type=Path)
    parser.add_argument('--configuration-sha256')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink() and args.render.stat().st_size <= 32 << 20,
            'SSH_QUALIFICATION_RENDER_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(args.render.read_bytes())))
    if args.check:
        require(args.configuration is None and args.configuration_sha256 is None, 'SSH_QUALIFICATION_CHECK_USES_RENDER')
        validate_render(resources)
        return
    require(args.configuration is not None, 'SSH_QUALIFICATION_CONFIGURATION_REQUIRED')
    fd = os.open(args.configuration, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size <= MAXIMUM_BYTES,
                'SSH_QUALIFICATION_CONFIGURATION_REJECTED')
        raw = stream.read(MAXIMUM_BYTES + 1)
    require(0 < len(raw) <= MAXIMUM_BYTES and hashlib.sha256(raw).hexdigest() == args.configuration_sha256,
            'SSH_QUALIFICATION_CONFIGURATION_DIGEST_MISMATCH')
    rendered = transform(resources, raw)
    fd = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(fd, 'w') as stream:
        yaml.safe_dump_all(rendered, stream, sort_keys=False)


if __name__ == '__main__':
    try:
        main()
    except (Rejected, ValueError, OSError, KeyError, TypeError, subprocess.SubprocessError, yaml.YAMLError):
        raise SystemExit('SSH qualification profile rejected') from None
