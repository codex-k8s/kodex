#!/usr/bin/env python3
"""Согласованные пути source→promotion→node pull; без смены admission boundary."""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import re
import yaml
import provider_sandbox_profile
ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'deploy/k8s/base/image-supply-chain/release-artifact-materializer.sh'

def require(value):
    if not value:
        raise ValueError('Promoted registry contract rejected')

def one(resources, kind, name):
    found = [v for v in resources if v.get('kind') == kind and v.get('metadata', {}).get('name') == name]
    require(len(found) == 1 and found[0]['metadata'].get('namespace') == 'kodex-system')
    return found[0]

def bind(resources, lock):
    previous_runtime_profile = copy.deepcopy(resources)
    policy = one(resources, 'ConfigMap', 'kodex-image-admission-policy')
    data = policy['data']
    host = data['pullRegistryHost']
    require(isinstance(host, str) and re.fullmatch('[a-z0-9][a-z0-9.-]*[a-z0-9]', host))
    require(data['promotionRepository'] == 'kodex-image-registry-promotion.kodex-system.svc.cluster.local:5003/kodex/roles')
    base = 'kodex-image-registry.kodex-system.svc.cluster.local:5000/'
    require(data['trustedRoleBaseRepository'].startswith(base))
    destination = data['trustedRoleBaseRepository'].removeprefix(base)
    require(destination in ['kodex/agent-runner', 'kodex/agent-runner-opencode'])
    component = destination.split('/')[-1]
    engine = lock.get('role_base_runtime_engine', 'codex')
    require(engine in ('codex', 'opencode') and (component == 'agent-runner-opencode') == (engine == 'opencode'))
    if component == 'agent-runner-opencode':
        require(lock.get('role_base_runtime_engine') == 'opencode')
    agents = [v for v in lock['images'] if v['component'] == component]
    controls = [v for v in lock['images'] if v['component'] == 'control-plane']
    require(len(agents) == 1 and len(controls) == 1)
    agent = agents[0]
    control = controls[0]
    require(all((isinstance(v['digest'], str) and re.fullmatch('sha256:[a-f0-9]{64}', v['digest']) and (v['digest'] != 'sha256:' + '0' * 64) for v in [agent, control])))
    require(agent['digest'] == data['trustedRoleBaseDigest'])
    data['promotedPullRepository'] = host + '/kodex/roles'
    data['nodeReadbackImage'] = host + '/' + destination + '@' + agent['digest']
    job = one(resources, 'Job', 'release-artifact-materializer')
    container = next((v for v in job['spec']['template']['spec']['containers'] if v['name'] == 'materializer'))
    env = container['env']
    source = [v for v in env if v['name'] == 'AGENT_RUNNER_SOURCE_REF']
    require(source == [{'name': 'AGENT_RUNNER_SOURCE_REF', 'value': lock['registry']['push'] + '/' + lock['registry']['repository_prefix'] + '/' + component + '@' + agent['digest']}])
    wanted = {'name': 'AGENT_RUNNER_DESTINATION_REPOSITORY', 'value': destination}
    existing = [v for v in env if v['name'] == wanted['name']]
    require(not existing or existing == [wanted])
    if not existing:
        env.append(wanted)
    script = one(resources, 'ConfigMap', 'kodex-release-artifact-materializer')
    payload = SCRIPT.read_text()
    script['data']['release-artifact-materializer.sh'] = payload
    script['metadata'].setdefault('annotations', {})['kodex.dev/materializer-script-sha256'] = hashlib.sha256(payload.encode()).hexdigest()
    registry = one(resources, 'Deployment', 'kodex-image-registry-pull')
    authorizer = [v for v in registry['spec']['template']['spec']['containers'] if v['name'] == 'pull-authorizer']
    require(len(authorizer) == 1)
    from component_image_profile import select_runtime, RUNTIME_ENV
    if engine == 'opencode':
        select_runtime(authorizer[0], engine)
    else:
        existing = [v for v in authorizer[0].get('env', []) if v.get('name') == RUNTIME_ENV]
        require(not existing or existing == [{'name': RUNTIME_ENV, 'value': 'codex'}])
    guard = next((v for v in registry['spec']['template']['spec']['containers'] if v['name'] == 'certificate-guard'))
    readback = [v for v in guard['env'] if v['name'] == 'READBACK_IMAGE']
    require(len(readback) == 1)
    readback[0]['value'] = host + '/kodex/control-plane@' + control['digest']
    canonical = {k: v for k, v in data.items() if k not in ['orchestrationRevision', 'policySHA256']}
    data['policySHA256'] = hashlib.sha256((json.dumps(canonical, sort_keys=True, separators=(',', ':'), ensure_ascii=False) + '\n').encode()).hexdigest()
    parameters = one(resources, 'ImageAdmissionPolicyParameters', 'kodex-image-admission-policy')
    require(parameters['apiVersion'] == 'supplychain.kodex.dev/v1alpha1')
    parameters['spec'] = copy.deepcopy(data)
    resources[:] = provider_sandbox_profile.rebase(resources, previous_runtime_profile)
    return resources

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--lock', type=Path, required=True)
    args = parser.parse_args()
    require(args.render.is_file() and (not args.render.is_symlink()) and args.lock.is_file() and (not args.lock.is_symlink()))
    raw = args.render.read_bytes()
    require(len(raw) <= 16 << 20)
    lock = args.lock.read_bytes()
    require(len(lock) <= 1 << 20)
    resources = [v for v in yaml.safe_load_all(raw) if v]
    require(len(resources) <= 10000)
    bind(resources, json.loads(lock))
    args.render.write_text(yaml.safe_dump_all(resources, sort_keys=False))
if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, StopIteration):
        raise SystemExit('Promoted registry contract rejected') from None
