"""Явная материализация trusted-cluster без изменения защищённого источника."""

import argparse
import copy
import json
import re
import sys

from local_hot_reload import PROFILE, PROFILE_LABEL, pod_specs, require


AUTHORITY_PREFIX = "internal-rpc-authority"
RPC_WORKLOADS = {
    "control-plane", "control-api-gateway", "secret-broker", "stt-tts-service",
    "email-bridge", "runtime-controller", "integration-gateway", "interaction-gateway",
    "automation-scheduler", "session-archive", "role-image-builder", "image-admission-controller",
}
# Публичный TLS gateway не относится к удаляемому внутреннему RPC TLS.
RPC_VOLUMES = {
    "control-plane": {"workload-tls", "internal-ca", "authority-policy"},
    "secret-broker": {"server-tls", "client-ca", "workload-tls", "control-plane-ca"},
    "automation-scheduler": {"workload-tls", "control-plane-ca", "application-grant"},
    "session-archive": {"workload-tls", "control-plane-ca", "application-grant"},
    "integration-gateway": {"workload-tls", "control-plane-ca", "application-grant"},
    "email-bridge": {"application-grant"},
    "interaction-gateway": {"workload-tls", "control-plane-ca", "application-grant"},
    "role-image-builder": {"workload-tls", "control-plane-ca", "application-grant"},
    "runtime-controller": {"workload-tls", "control-plane-ca", "application-grant", "callback-server-tls", "callback-client-ca"},
}
PUBLIC_SERVICES = {"staff-control-center", "control-api-gateway", "interaction-gateway"}
CORE_EDGES = {
    ("control-api-gateway", "control-plane"),
    ("control-api-gateway", "secret-broker"),
    ("secret-broker", "control-plane"),
    ("control-plane", "secret-broker"),
    ("automation-scheduler", "control-plane"),
    ("session-archive", "control-plane"),
    ("integration-gateway", "control-plane"),
    ("email-bridge", "control-plane"),
    ("runtime-controller", "control-plane"),
    ("runtime-controller", "secret-broker"),
    ("stt-tts-service", "control-plane"),
    ("stt-tts-service", "secret-broker"),
    ("control-api-gateway", "stt-tts-service"),
    ("interaction-gateway", "control-plane"),
    ("role-image-builder", "control-plane"),
}

RUNTIME_PROFILE_MESSAGE = "runtime Pod requires the configured trusted cluster profile"


def runtime_admission_profile(resource):
    """Меняет только восемь требований callback keys, сохраняя остальные CEL gates."""
    if resource.get("kind") != "ValidatingAdmissionPolicy" or resource["metadata"]["name"] != "runtime-role-pod-exact-secret-projection":
        return
    validations = resource["spec"]["validations"]
    if any(item.get("message") == RUNTIME_PROFILE_MESSAGE for item in validations):
        return
    pattern = re.compile(
        r"size\((?:object\.spec\.volumes|(?:variables\.(?:roleContainers|relayContainers)\[0\]|"
        r"object\.spec\.initContainers\[1\])\.volumeMounts)\.filter\((volume|mount),\s*"
        r"\1\.name == 'callback-(?:ca|client)'.*?\)\) == 1", re.DOTALL)
    changed = 0
    for item in validations:
        expression, count = pattern.subn(lambda match: match.group(0)[:-1] + "0", item["expression"])
        changed += count
        # Закрытые allowlists дополнительно запрещают даже malformed callback mounts.
        item["expression"] = expression.replace("'callback-ca',", "").replace("'callback-client',", "")
    require(changed == 8, "RUNTIME_ADMISSION_CALLBACK_CONTRACT_DRIFT")
    validations.append({"expression": "has(object.metadata.labels) && 'kodex.dev/security-profile' in object.metadata.labels && object.metadata.labels['kodex.dev/security-profile'] == 'trusted-cluster'",
                        "message": RUNTIME_PROFILE_MESSAGE})


def authority_name(name):
    return name.startswith(AUTHORITY_PREFIX) or name == "platform-worker-grant-agent" or name.endswith("-platform-worker-grant-agent")


def namespace(resource):
    return resource["metadata"].get("namespace", "default")


def selector_matches(selector, labels):
    # Для этой границы нет неявной интерпретации matchExpressions.
    return not selector.get("matchExpressions") and all(
        labels.get(key) == value for key, value in selector.get("matchLabels", {}).items())


def selects(policy, workload):
    return namespace(policy) == namespace(workload) and selector_matches(
        policy["spec"]["podSelector"], workload["spec"]["template"]["metadata"]["labels"])


def peer_matches(peer, policy, workload):
    if "podSelector" not in peer or "ipBlock" in peer:
        return False
    if "namespaceSelector" in peer:
        if peer["namespaceSelector"] != {"matchLabels": {"kubernetes.io/metadata.name": namespace(workload)}}:
            return False
    elif namespace(policy) != namespace(workload):
        return False
    labels = peer["podSelector"].get("matchLabels", {})
    return "app.kubernetes.io/name" in labels and selector_matches(
        peer["podSelector"], workload["spec"]["template"]["metadata"]["labels"])


def allowed_edge(policies, source, destination, direction):
    selected, peer = (source, destination) if direction == "egress" else (destination, source)
    peers_key = "to" if direction == "egress" else "from"
    return any(selects(policy, selected) and any(
        any(port.get("port") == 8443 and port.get("protocol", "TCP") == "TCP" and
            "endPort" not in port for port in rule.get("ports", [])) and
        any(peer_matches(candidate, policy, peer) for candidate in rule.get(peers_key, []))
        for rule in policy["spec"].get(direction, [])) for policy in policies)


def authority_peer(peer):
    labels = peer.get("podSelector", {}).get("matchLabels", {})
    return any(authority_name(value) for value in labels.values() if isinstance(value, str)) or any(
        key.startswith("kodex.dev/internal-rpc-authority") for key in labels)


def materialize(resources, profile):
    require(profile == PROFILE, "EXPLICIT_TRUSTED_PROFILE_REQUIRED")
    resources = [copy.deepcopy(resource) for resource in resources
                 if not authority_name(resource.get("metadata", {}).get("name", ""))]
    for resource in resources:
        runtime_admission_profile(resource)
        resource.setdefault("metadata", {}).setdefault("labels", {})[PROFILE_LABEL] = PROFILE
        if resource.get("kind") == "NetworkPolicy":
            for direction, peers_key in (("ingress", "from"), ("egress", "to")):
                rules = []
                for rule in resource["spec"].get(direction, []):
                    if peers_key in rule:
                        rule[peers_key] = [peer for peer in rule[peers_key] if not authority_peer(peer)]
                        # Пустой список peers разрешает всех: удаляем целое правило.
                        if not rule[peers_key]:
                            continue
                    rules.append(rule)
                if direction in resource["spec"]:
                    resource["spec"][direction] = rules
    for resource, template in pod_specs(resources):
        name = resource["metadata"]["name"]
        labels = template.setdefault("metadata", {}).setdefault("labels", {})
        for key in list(labels):
            if key.startswith("kodex.dev/internal-rpc-authority"):
                del labels[key]
        labels[PROFILE_LABEL] = PROFILE
        spec = template["spec"]
        removed_volumes = set(RPC_VOLUMES.get(name, set()))
        for volume in spec.get("volumes", []):
            if authority_name(volume["name"]) or authority_name(volume.get("secret", {}).get("secretName", "")):
                removed_volumes.add(volume["name"])
        for group in ("containers", "initContainers"):
            if group not in spec:
                continue
            spec[group] = [container for container in spec[group] if not authority_name(container["name"])]
            for container in spec[group]:
                container["volumeMounts"] = [mount for mount in container.get("volumeMounts", [])
                                             if mount["name"] not in removed_volumes]
                container["env"] = [entry for entry in container.get("env", [])
                                    if not entry["name"].startswith("INTERNAL_RPC_AUTHORITY_") and
                                    entry["name"] != "KODEX_RPC_PROFILE"]
                if name in RPC_WORKLOADS and container["name"] == name:
                    container["env"].append({"name": "KODEX_RPC_PROFILE", "value": PROFILE})
        used = {mount["name"] for group in ("containers", "initContainers")
                for container in spec.get(group, []) for mount in container.get("volumeMounts", [])}
        spec["volumes"] = [volume for volume in spec.get("volumes", []) if volume["name"] in used]
        if name == "email-bridge":
            # CA этого тома также нужна PostgreSQL; RPC private key больше не доставляется.
            for volume in spec["volumes"]:
                if volume["name"] == "tls" and "secret" in volume:
                    volume["secret"]["items"] = [{"key": "ca.crt", "path": "ca.crt"}]
    verify(resources, profile)
    return resources


def verify(resources, profile):
    require(profile == PROFILE, "EXPLICIT_TRUSTED_PROFILE_REQUIRED")
    workloads = {}
    policies = [resource for resource in resources if resource.get("kind") == "NetworkPolicy"]
    for resource in resources:
        name = resource["metadata"]["name"]
        require(not authority_name(name), "AUTHORITY_RESOURCE_FORBIDDEN")
        if resource.get("kind") == "Service":
            require(resource["spec"].get("type", "ClusterIP") == "ClusterIP" and
                    not resource["spec"].get("externalIPs"), "PUBLIC_INTERNAL_SERVICE_FORBIDDEN")
        if resource.get("kind") == "Ingress":
            backends = [path["backend"] for rule in resource["spec"].get("rules", [])
                        for path in rule.get("http", {}).get("paths", [])]
            if "defaultBackend" in resource["spec"]:
                backends.append(resource["spec"]["defaultBackend"])
            require(bool(resource["spec"].get("tls")) and all(
                backend.get("service", {}).get("name") in PUBLIC_SERVICES for backend in backends),
                "PUBLIC_INGRESS_BOUNDARY_INVALID")
    for resource, template in pod_specs(resources):
        spec = template["spec"]
        name = resource["metadata"]["name"]
        require(template["metadata"].get("labels", {}).get(PROFILE_LABEL) == PROFILE,
                "TRUSTED_PROFILE_LABEL_REQUIRED")
        if name in RPC_WORKLOADS:
            application = [container for container in spec.get("containers", []) if container["name"] == name]
            require(len(application) == 1 and
                    [entry for entry in application[0].get("env", []) if entry["name"] == "KODEX_RPC_PROFILE"] ==
                    [{"name": "KODEX_RPC_PROFILE", "value": PROFILE}], "EXPLICIT_RPC_PROFILE_REQUIRED:" + name)
        require(not spec.get("hostNetwork") and not spec.get("hostPID"), "HOST_NETWORK_FORBIDDEN")
        for group in ("containers", "initContainers"):
            for container in spec.get(group, []):
                require(not authority_name(container["name"]), "AUTHORITY_CONTAINER_FORBIDDEN")
                require(not any(entry["name"].startswith("INTERNAL_RPC_AUTHORITY_")
                                for entry in container.get("env", [])), "AUTHORITY_ENV_FORBIDDEN")
        require(not any(authority_name(volume["name"]) or
                        authority_name(volume.get("secret", {}).get("secretName", ""))
                        for volume in spec.get("volumes", [])), "AUTHORITY_VOLUME_FORBIDDEN")
        if resource.get("kind") == "Deployment":
            workloads[resource["metadata"]["name"]] = resource
            require(any(selects(policy, resource) and
                        set(policy["spec"].get("policyTypes", [])) == {"Ingress", "Egress"} and
                        not policy["spec"].get("ingress") and not policy["spec"].get("egress")
                        for policy in policies), "DENY_BY_DEFAULT_REQUIRED:" + resource["metadata"]["name"])
    for source, destination in CORE_EDGES:
        if source in workloads and destination in workloads:
            for direction in ("ingress", "egress"):
                require(allowed_edge(policies, workloads[source], workloads[destination], direction),
                        "EXACT_RPC_NETWORK_EDGE_REQUIRED:" + source + ":" + destination + ":" + direction)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("materialize", "verify"))
    parser.add_argument("--profile", required=True, choices=(PROFILE,))
    args = parser.parse_args()
    resources = json.load(sys.stdin)
    if args.action == "materialize":
        json.dump(materialize(resources, args.profile), sys.stdout, indent=2)
    else:
        verify(resources, args.profile)


if __name__ == "__main__":
    try:
        main()
    except (KeyError, TypeError, ValueError) as error:
        sys.exit("Trusted cluster render rejected: " + str(error))
