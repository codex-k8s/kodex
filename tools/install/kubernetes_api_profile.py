"""Точный API endpoint route для всех зарегистрированных клиентов переносимого профиля."""
import argparse
import copy
import ipaddress
import os
from pathlib import Path
import yaml
from deployment_profile import require, Rejected

ANCHORS = (
    'kodex-image-admission-controller-exact-paths', 'runtime-controller-exact-paths',
    'session-archive-exact-paths', 'internal-rpc-authority-publisher-exact-paths',
    'internal-rpc-authority-restore-controller-exact-paths',
    'internal-rpc-authority-restore-jobs-exact-paths',
    'internal-rpc-authority-restore-pitr-telemetry',
)
REPAIR = ('control-plane-exact-runtime-paths', 'secret-broker-exact-runtime-paths')
PUBLISHER = 'internal-rpc-authority-publisher-exact-paths'
REPAIRED_POLICIES = REPAIR + (PUBLISHER,)
SELECTORS = {
    REPAIR[0]: {'matchLabels': {'app.kubernetes.io/name': 'control-plane', 'app.kubernetes.io/component': 'internal-service'}},
    REPAIR[1]: {'matchLabels': {'app.kubernetes.io/name': 'secret-broker', 'app.kubernetes.io/component': 'secret-broker'}},
    PUBLISHER: {'matchLabels': {'app.kubernetes.io/name': 'internal-rpc-authority-publisher'}},
}


def host_cidr(value):
    require(isinstance(value, str), 'EXACT_API_HOST_CIDR_REQUIRED')
    network = ipaddress.ip_network(value, strict=True)
    require(value == str(network) and network.prefixlen == network.max_prefixlen and not network.network_address.is_unspecified
            and not network.network_address.is_multicast, 'EXACT_API_HOST_CIDR_REQUIRED')
    return network


def endpoint_rule(value):
    require(isinstance(value, dict) and set(value) == {'to', 'ports'} and
            isinstance(value['to'], list) and 1 <= len(value['to']) <= 16 and
            isinstance(value['ports'], list) and 1 <= len(value['ports']) <= 8, 'BOUNDED_API_ENDPOINT_RULE_REQUIRED')
    hosts = []
    for peer in value['to']:
        require(isinstance(peer, dict) and set(peer) == {'ipBlock'} and
                isinstance(peer['ipBlock'], dict) and set(peer['ipBlock']) == {'cidr'}, 'EXACT_API_ENDPOINT_PEER_REQUIRED')
        hosts.append(host_cidr(peer['ipBlock']['cidr']))
    ports = []
    for port in value['ports']:
        require(isinstance(port, dict) and set(port) == {'protocol', 'port'} and
                port['protocol'] == 'TCP' and type(port['port']) is int and 1 <= port['port'] <= 65535,
                'EXACT_API_ENDPOINT_PORT_REQUIRED')
        ports.append(port['port'])
    require(len(hosts) == len(set(hosts)) and len(ports) == len(set(ports)), 'DUPLICATE_API_ENDPOINT_REJECTED')
    return hosts


def transform(resources):
    result = copy.deepcopy(resources)
    policies = {}
    for name in ANCHORS + REPAIR:
        selected = [r for r in result if r.get('kind') == 'NetworkPolicy' and r['metadata']['name'] == name]
        require(len(selected) == 1 and selected[0].get('apiVersion') == 'networking.k8s.io/v1' and
                selected[0]['metadata'].get('namespace') == 'kodex-system', 'EXACT_API_CLIENT_POLICY_REQUIRED')
        policies[name] = selected[0]
    require(all(policies[name]['spec']['podSelector'] == selector for name, selector in SELECTORS.items()),
            'EXACT_API_REPAIR_SELECTOR_REQUIRED')
    # Семь прежних клиентов уже несут один согласованный endpoint rule последним.
    # Новый маршрут берётся только из закреплённого render, без discovery/новых inputs.
    rules = [policies[name]['spec']['egress'][-1] for name in ANCHORS]
    require(all(rule == rules[0] for rule in rules), 'API_ENDPOINT_ANCHOR_DRIFT')
    hosts = endpoint_rule(rules[0])
    service_rules = []
    for rule in policies[ANCHORS[0]]['spec']['egress'][:-1]:
        if rule.get('ports') != [{'protocol': 'TCP', 'port': 443}] or len(rule.get('to', [])) != 1:
            continue
        try:
            endpoint_rule(rule)
        except (Rejected, ValueError, TypeError, KeyError):
            continue
        if all(rule in policies[name]['spec']['egress'][:-1] for name in ANCHORS if name != PUBLISHER):
            service_rules.append(rule)
    require(len(service_rules) == 1, 'EXACT_API_SERVICE_ANCHOR_REQUIRED')
    service = host_cidr(service_rules[0]['to'][0]['ipBlock']['cidr'])
    require(service not in hosts, 'API_SERVICE_ENDPOINT_COLLISION')
    # Publisher исторически несёт default Service IP. Меняется только его
    # единственный host-CIDR/TCP443 маршрут, не PostgreSQL/authority/telemetry.
    publisher_rules = policies[PUBLISHER]['spec']['egress']
    candidates = [rule for rule in publisher_rules[:-1]
                  if rule.get('ports') == [{'protocol': 'TCP', 'port': 443}]
                  and len(rule.get('to', [])) == 1 and 'ipBlock' in rule['to'][0]]
    require(len(candidates) == 1, 'EXACT_PUBLISHER_API_SERVICE_RULE_REQUIRED')
    endpoint_rule(candidates[0])
    publisher_rules[publisher_rules.index(candidates[0])] = copy.deepcopy(service_rules[0])
    for name in REPAIR:
        policy = policies[name]
        require(service_rules[0] in policy['spec']['egress'], 'API_CLIENT_SERVICE_RULE_DRIFT')
        if rules[0] not in policy['spec']['egress']:
            policy['spec']['egress'].append(copy.deepcopy(rules[0]))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes();require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(raw)));require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    desired = transform(resources)
    descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'w') as stream:yaml.safe_dump_all(desired, stream, sort_keys=False)


if __name__ == '__main__':
    try:main()
    except (Rejected, OSError, ValueError, KeyError, TypeError, IndexError, yaml.YAMLError):
        raise SystemExit('Kubernetes API endpoint profile rejected') from None
