#!/usr/bin/env python3
"""Проверяет двусторонний effective NetworkPolicy путь на итоговых profiles."""
import copy
from pathlib import Path
import subprocess

import yaml

ROOT = Path(__file__).resolve().parents[2]


def matches(selector, labels):
    assert set(selector) <= {"matchLabels", "matchExpressions"}, "unsupported selector"
    if any(labels.get(key) != value for key, value in selector.get("matchLabels", {}).items()):
        return False
    for expression in selector.get("matchExpressions", []):
        key, operator = expression["key"], expression["operator"]
        values = expression.get("values", [])
        assert operator in {"In", "NotIn", "Exists", "DoesNotExist"}, "unsupported selector operator"
        if operator == "In" and labels.get(key) not in values:
            return False
        if operator == "NotIn" and labels.get(key) in values:
            return False
        if operator == "Exists" and key not in labels:
            return False
        if operator == "DoesNotExist" and key in labels:
            return False
    return True


def permits(objects, destination, source, direction, port, protocol="TCP"):
    # direction определяет выбранный Pod; peers проверяются на противоположном конце.
    selected, other = (destination, source) if direction == "Ingress" else (source, destination)
    policies = [item for item in objects if item["kind"] == "NetworkPolicy"
                and item["metadata"].get("namespace") == selected[0]
                and matches(item["spec"]["podSelector"], selected[1])
                and direction in item["spec"].get("policyTypes", ["Ingress"] + (["Egress"] if "egress" in item["spec"] else []))]
    if not policies:
        return True
    for policy in policies:
        for rule in policy["spec"].get(direction.lower(), []):
            ports = rule.get("ports", [])
            assert all(isinstance(item.get("port", port), int) for item in ports), "named port requires explicit resolver"
            if ports and not any(item.get("protocol", "TCP") == protocol
                                 and item.get("port", port) <= port <= item.get("endPort", item.get("port", port)) for item in ports):
                continue
            peers = rule.get("from" if direction == "Ingress" else "to", [])
            if not peers:
                return True
            for peer in peers:
                if not peer:
                    return True
                assert "ipBlock" not in peer, "IP peer requires independently pinned Pod IP"
                assert set(peer) <= {"namespaceSelector", "podSelector"}, "unsupported peer"
                if "namespaceSelector" in peer:
                    namespace_labels = next((item["metadata"].get("labels", {}) for item in objects
                                             if item["kind"] == "Namespace" and item["metadata"]["name"] == other[0]), {})
                    namespace_labels = {**namespace_labels, "kubernetes.io/metadata.name": other[0]}
                    if not matches(peer["namespaceSelector"], namespace_labels):
                        continue
                elif other[0] != policy["metadata"]["namespace"]:
                    continue
                if matches(peer.get("podSelector", {}), other[1]):
                    return True
    return False


def pod(objects, name):
    found = [item for item in objects if item["kind"] == "Deployment" and item["metadata"]["name"] == name]
    assert len(found) == 1, "missing or duplicated workload"
    return found[0]["metadata"]["namespace"], found[0]["spec"]["template"]["metadata"]["labels"]


def verify(objects):
    broker, gateway = pod(objects, "secret-broker"), pod(objects, "egress-gateway")
    assert permits(objects, gateway, broker, "Ingress", 8080), "broker ingress blocked"
    assert permits(objects, gateway, broker, "Egress", 8080), "broker egress blocked"
    cases = [(broker, 8081, "TCP"), (broker, 8082, "TCP"), (broker, 8080, "UDP"),
             (("foreign-namespace", broker[1]), 8080, "TCP")]
    for key in ("app.kubernetes.io/name", "app.kubernetes.io/component"):
        cases.append(((broker[0], {**broker[1], key: "foreign-workload"}), 8080, "TCP"))
    for source, port, protocol in cases:
        assert not permits(objects, gateway, source, "Ingress", port, protocol), "unrelated ingress admitted"


def main():
    for profile in ("web-only", "web-with-mattermost"):
        result = subprocess.run(["kubectl", "kustomize", str(ROOT / "deploy/k8s/profiles" / profile)],
                                capture_output=True, check=True, timeout=60)
        objects = [item for item in yaml.safe_load_all(result.stdout) if item]
        verify(objects)
        for mutation in ("missing-peer", "namespace", "component", "extra-port", "empty-peer", "additive-policy"):
            changed = copy.deepcopy(objects)
            policy = next(item for item in changed if item["kind"] == "NetworkPolicy"
                          and item["metadata"]["name"] == "egress-gateway-exact-runtime-paths")
            rule = next(rule for rule in policy["spec"]["ingress"]
                        if any(peer.get("podSelector", {}).get("matchLabels", {}).get("app.kubernetes.io/name") == "secret-broker"
                               for peer in rule.get("from", [])))
            peer = next(peer for peer in rule["from"] if peer.get("podSelector", {}).get("matchLabels", {}).get("app.kubernetes.io/name") == "secret-broker")
            if mutation == "missing-peer":
                rule["from"].remove(peer)
            elif mutation == "namespace":
                peer["namespaceSelector"] = {}
            elif mutation == "component":
                del peer["podSelector"]["matchLabels"]["app.kubernetes.io/component"]
            elif mutation == "extra-port":
                rule["ports"].append({"protocol": "TCP", "port": 8081})
            elif mutation == "empty-peer":
                rule["from"].append({})
            else:
                extra = copy.deepcopy(policy)
                extra["metadata"]["name"] = "synthetic-unrestricted-ingress"
                extra["spec"]["ingress"] = [{}]
                changed.append(extra)
            try:
                verify(changed)
            except AssertionError:
                pass
            else:
                raise AssertionError("policy regression was not detected")
    print("egress broker policy: both profiles and negative mutations passed")


if __name__ == "__main__":
    main()
