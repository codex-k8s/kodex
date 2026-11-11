#!/usr/bin/env python3
"""Связывает пустые начальные проекции с итоговой policy штатными Go producers."""
import argparse
import copy
import json
from pathlib import Path
import subprocess
import tempfile

import yaml

ROOT = Path(__file__).resolve().parents[2]


def one(items):
    if len(items) != 1:
        raise ValueError("exact bootstrap consumer required")
    return items[0]


def bind(resources):
    result = copy.deepcopy(resources)
    gateway = one([r for r in result if r['kind'] == 'Deployment' and r['metadata']['name'] == 'egress-gateway'])
    template = gateway['spec']['template']
    container = one([c for c in template['spec']['containers'] if c['name'] == 'egress-gateway'])
    def expected(name):
        return one([e for e in container['env'] if e['name'] == name])['value']
    base = one([r for r in result if r['kind'] == 'ConfigMap' and r['metadata']['name'].startswith('egress-gateway-policy-')])
    # Только пустое начальное поколение; live owner-проекции не перезаписываются.
    for kind, generation in (('mail', 'configurationRevision'), ('integration', 'generation')):
        value = one([r for r in result if r['kind'] == 'ConfigMap' and kind + '-policy.json' in r.get('data', {})])
        policy = json.loads(value['data'][kind + '-policy.json'])
        if policy['destinations'] != [] or policy[generation] != 1:
            raise ValueError("empty bootstrap projection required")
    mailbox = one([r for r in result if r['kind'] == 'Secret' and r['metadata']['name'] == 'email-bridge-mailbox-projection'])
    mailbox_raw = mailbox['stringData']['mailboxes.json']
    if json.loads(mailbox_raw)['mailboxes'] != []:
        raise ValueError("empty mailbox bootstrap required")
    with tempfile.TemporaryDirectory(prefix='kodex-egress-bootstrap-') as directory:
        root = Path(directory)
        source = root / 'policy.json'
        source.write_text(base['data']['policy.json'])
        mailbox_file = root / 'mailboxes.json'
        mailbox_file.write_text(mailbox_raw)
        for kind in ('mail', 'integration'):
            output = root / kind
            args = ['go', 'run', './cmd/' + kind + '-projection', '-policy', str(source),
                    '-revision', expected('EGRESS_GATEWAY_EXPECTED_POLICY_REVISION'),
                    '-digest', expected('EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST'), '-output', str(output)]
            if kind == 'mail':
                args += ['-configuration', str(mailbox_file)]
            process = subprocess.run(args, cwd=ROOT / 'services/external/egress-gateway', capture_output=True, timeout=120)
            if process.returncode != 0:
                raise ValueError("bootstrap projection producer failed")
            cm = json.loads((output / (kind + '-configmap.json')).read_bytes())
            np = json.loads((output / (kind + '-networkpolicy.json')).read_bytes())
            patch = json.loads((output / (kind + '-deployment-patch.json')).read_bytes())['spec']['template']
            for new in (cm, np):
                new['metadata'].setdefault('labels', {})['app.kubernetes.io/part-of'] = 'kodex'
            result = [r for r in result if not (
                r['kind'] == 'ConfigMap' and kind + '-policy.json' in r.get('data', {}) or
                r['kind'] == 'NetworkPolicy' and r['metadata']['name'] == np['metadata']['name'])] + [cm, np]
            template['metadata'].setdefault('annotations', {}).update(patch['metadata']['annotations'])
            env = patch['spec']['containers'][0]['env'][0]
            one([e for e in container['env'] if e['name'] == env['name']]).update(env)
            volume = patch['spec']['volumes'][0]
            one([v for v in template['spec']['volumes'] if v['name'] == volume['name']]).update(volume)
            if kind == 'mail':
                runtime = one([r for r in result if r['kind'] == 'ConfigMap' and r['metadata']['name'] == 'email-bridge-runtime'])
                runtime['data']['EMAIL_BRIDGE_EGRESS_POLICY_DIGEST'] = env['value']
                for workload in result:
                    if workload['kind'] == 'Deployment' and workload['metadata']['name'] in ('email-bridge', 'egress-gateway'):
                        annotations = workload['spec']['template']['metadata'].setdefault('annotations', {})
                        annotations.update(patch['metadata']['annotations'])
                        annotations['kodex.dev/mail-policy-digest'] = env['value']
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    args = parser.parse_args()
    if args.render.is_symlink() or not args.render.is_file() or args.render.stat().st_size > 16 << 20:
        raise ValueError("regular bounded render required")
    values = list(filter(None, yaml.safe_load_all(args.render.read_bytes())))
    args.render.write_text(yaml.safe_dump_all(bind(values), sort_keys=False))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError, subprocess.TimeoutExpired, yaml.YAMLError):
        raise SystemExit('Egress bootstrap projection rejected') from None
