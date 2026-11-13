#!/usr/bin/env python3
"""Адресный план очистки Kodex по exact inventory; этот этап не меняет Kubernetes."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import posixpath

from installation_inventory import NAMESPACES, decode, write_private

MANAGERS = frozenset({"kodex-local-dev", "kodex-install", "kodex-identity", "kodex-identity-material",
                      "kodex-management", "kodex-kubernetes-api-egress", "kodex-secret-broker-bootstrap"})
HELM_RELEASES = frozenset({("kodex-monitoring", "observability"), ("kodex-trust-manager", "cert-manager"),
                          ("oauth2-control-center", "kodex-system"), ("oauth2-grafana", "observability")})
SHARED_CLUSTER_RESOURCES = frozenset({"nodes", "storageclasses", "csidrivers", "csinodes", "runtimeclasses",
                                      "priorityclasses", "apiservices"})
ERROR = "Installation cleanup plan failed: exact inventory or scope validation rejected"


def digest(value):
    """Хешировать однозначное представление плана без credential-полей."""
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def read_private(path, expected):
    """Прочитать ровно проверенные immutable bytes private regular файла, не следуя ссылкам."""
    metadata = path.lstat()
    if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.getuid()
            or metadata.st_nlink != 1 or stat.S_IMODE(metadata.st_mode) != 0o600):
        raise ValueError(ERROR)
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, "rb") as stream:
        if os.fstat(stream.fileno()) != metadata:
            raise ValueError(ERROR)
        payload = stream.read((16 << 20) + 1)
    if hashlib.sha256(payload).hexdigest() != expected:
        raise ValueError(ERROR)
    return decode(payload)


def address(item):
    """Описать точный API object address независимо от server-owned UID."""
    return item["namespace"], item["resource"], item["name"]


def namespace_proof(item):
    """Namespace должен иметь закрытое имя и установленный Kodex marker; один префикс имени недостаточен."""
    return item["name"] in NAMESPACES and (
        item["labels"].get("app.kubernetes.io/part-of") == "kodex" or "kodex-install" in item["managers"])


def seed_proof(item, namespaces):
    """Признать только конкретный installer/Helm/runtime producer в допустимой области; shared capabilities сохраняются."""
    resource, namespace, name = item["resource"], item["namespace"], item["name"]
    labels, managers = item["labels"], set(item["managers"])
    if resource == "namespaces" or resource == "persistentvolumes" or resource in SHARED_CLUSTER_RESOURCES:
        return None
    if resource == "customresourcedefinitions" and not name.endswith(".kodex.dev"):
        return None
    release = (item["helm"].get("meta.helm.sh/release-name"), item["helm"].get("meta.helm.sh/release-namespace"))
    if release in HELM_RELEASES:
        return {"kind": "EXACT_HELM_RELEASE", "release": list(release)}
    if namespace == "" and (labels.get("app.kubernetes.io/part-of") == "kodex" and managers & MANAGERS):
        return {"kind": "KODEX_CLUSTER_INSTALLER", "managers": sorted(managers & MANAGERS)}
    if resource == "secrets" and labels.get("owner") == "helm" and (labels.get("name"), namespace) in HELM_RELEASES:
        return {"kind": "HELM_RELEASE_RECEIPT", "release": [labels["name"], namespace]}
    if namespace not in namespaces:
        return None
    if managers & MANAGERS:
        return {"kind": "KODEX_NAMESPACE_INSTALLER", "managers": sorted(managers & MANAGERS)}
    if labels.get("app.kubernetes.io/part-of") == "kodex":
        return {"kind": "KODEX_NAMESPACE_LABEL"}
    if labels.get("app.kubernetes.io/managed-by", "").startswith("kodex-"):
        return {"kind": "KODEX_NAMED_MANAGER", "manager": labels["app.kubernetes.io/managed-by"]}
    if namespace == "kodex-runtime":
        if labels.get("runtime.kodex.dev/managed") == "true" and managers & {"runtime-controller", "main"}:
            return {"kind": "RUNTIME_CONTROLLER"}
        if labels.get("runtime-secrets.kodex.dev/managed") == "true" and managers & {"secret-broker", "main"}:
            return {"kind": "RUNTIME_SECRET_BROKER"}
        if (resource == "secrets" and labels.get("provider-credentials.kodex.dev/authorization-attempt")
                and re.fullmatch(r"provider-auth-[a-f0-9]{30,32}", name) and "main" in managers):
            return {"kind": "PROVIDER_AUTHORIZATION_PRODUCER"}
    if (resource, name) in {("configmaps", "kube-root-ca.crt"), ("serviceaccounts", "default")}:
        return {"kind": "OWNED_NAMESPACE_BOOTSTRAP"}
    if (namespace == "kodex-system" and resource == "configmaps" and managers & {"control-plane", "main"}
            and labels.get("app.kubernetes.io/name") == "egress-gateway"
            and re.fullmatch(r"egress-gateway-(mail|integration)-[a-f0-9]{24}", name)):
        return {"kind": "OWNER_EGRESS_PROJECTION"}
    leases = {"runtime-controller-leader": "runtime-controller", "image-admission-controller-leader": "image-admission-controller"}
    if namespace == "kodex-system" and resource == "leases" and leases.get(name) in managers:
        return {"kind": "REGISTERED_CONTROLLER_LEASE", "controller": leases[name]}
    return None


def reference_origin(item, consumers, owned, objects):
    """Подтвердить закрытые repo-owned private state по exact producer, имени и всем consumers; не читать значения."""
    key = address(item)
    users = consumers.get(key, set())
    if item["resource"] != "secrets" or not users or not users <= owned.keys():
        return None
    canonical = {
        ("kodex-system", "kodex-ssh-compose-signing"): ("control-plane", "deploy/k8s/components/control-plane-ssh-compose-signing/signing-patch.yaml", "kubectl-create"),
        ("kodex-system", "proxy-session-store-auth-v1"): ("proxy-session-store", "tools/release/proxy-session-store.mjs", "kubectl-create"),
        ("observability", "kodex-monitoring-kube-prom-admission"): ("kodex-monitoring-kube-prom-operator", "Helm:kodex-monitoring/admission-hook", "kube-webhook-certgen"),
    }
    for role in ("evidence", "promotion", "push"):
        name = "kodex-image-registry-" + role
        canonical[("kodex-system", name + "-http-state-stable")] = (name, "tools/release/stable-registry-model.mjs", "kubectl-create")
    profile = canonical.get((item["namespace"], item["name"]))
    if not profile or profile[2] not in item["managers"]:
        return None
    producers = sorted(uid for uid in users if objects[uid]["resource"] in {"deployments", "statefulsets"}
                       and objects[uid]["name"] == profile[0])
    if producers:
        return {"kind": "CANONICAL_PRIVATE_STATE", "source": profile[1], "producerUIDs": producers,
                "consumerUIDs": sorted(users)}
    return None


def controller_origin(item, link, owned, objects, links, consumers, retired=()):
    """Связать generated PVC, policies и исторические события с конкретным существующим контроллером."""
    namespace = item["namespace"]
    if (item["resource"] == "certificates" and not item["ownerReferences"]
            and "cert-manager-ingress-shim" in item["managers"]
            and item["labels"].get("app.kubernetes.io/part-of", "kodex") == "kodex"):
        references = link.get("references", [])
        outputs = [reference for reference in references if reference["relation"] == "PRODUCES"]
        if len(outputs) == 1 and address(outputs[0]) == (namespace, "secrets", item["name"]):
            previous = sorted(uid for uid, parent in objects.items()
                              if uid in owned and uid in retired and parent["resource"] == "certificates"
                              and parent["namespace"] == namespace and parent["name"] == item["name"]
                              and links.get(uid, {}).get("references") == references)
            ingresses = sorted(uid for uid, parent in objects.items()
                               if uid in owned and uid in retired and parent["resource"] == "ingresses"
                               and parent["namespace"] == namespace and any(address(reference) == address(outputs[0])
                               for reference in links.get(uid, {}).get("references", [])))
            if previous and ingresses:
                return {"kind": "RECONCILED_INGRESS_SHIM_CERTIFICATE", "retiredIngressUIDs": ingresses,
                        "retiredCertificateUIDs": previous, "outputSecret": item["name"],
                        "source": "cert-manager/ingress-shim", "references": references}
    if item["resource"] == "persistentvolumeclaims" and "k3s" in item["managers"]:
        if consumers.get(address(item), set()) - owned.keys():
            return None
        for uid, parent in objects.items():
            if uid not in owned or parent["namespace"] != namespace or parent["resource"] != "statefulsets":
                continue
            for claim in links.get(uid, {}).get("claimTemplates", []):
                if re.fullmatch(re.escape(claim + "-" + parent["name"]) + r"-[0-9]+", item["name"]):
                    return {"kind": "STATEFULSET_CLAIM_TEMPLATE", "controllerUID": uid, "claimTemplate": claim}
    if item["resource"] in {"networkpolicies", "poddisruptionbudgets"}:
        selector = link.get("policySelector", {})
        if not selector.get("matchLabels") or set(selector) != {"matchLabels"}:
            return None
        matches = sorted(uid for uid, parent in objects.items() if uid in owned and parent["namespace"] == namespace
                         and parent["resource"] in {"deployments", "statefulsets", "daemonsets"}
                         and links.get(uid, {}).get("controllerSelector") == selector)
        foreign = [uid for uid, pod in objects.items() if uid not in owned and pod["namespace"] == namespace
                   and pod["resource"] == "pods" and all(pod["labels"].get(key) == value for key, value in selector["matchLabels"].items())]
        if matches and not foreign:
            return {"kind": "EXACT_OWNED_CONTROLLER_POLICY", "controllers": matches, "selector": selector}
    if item["resource"] == "events" and "k3s" in item["managers"]:
        reference = link.get("eventReference") or {}
        if reference.get("uid") and reference.get("kind") == "Pod" and reference.get("namespace") == namespace:
            for uid, parent in objects.items():
                if (uid in owned and parent["namespace"] == namespace and parent["resource"] in {"replicasets", "daemonsets", "jobs"}
                        and re.fullmatch(re.escape(parent["name"]) + r"-[a-z0-9]{5}", reference.get("name", ""))):
                    return {"kind": "CONTROLLER_HISTORICAL_POD_EVENT", "controllerUID": uid,
                            "historicalPodUID": reference.get("uid", ""), "namespace": namespace}
    if (item["resource"] == "serverstransports" and namespace == "kodex-system" and item["name"] == "staff-control-center"
            and link.get("transportServerName") == "staff-control-center.kodex-system.svc.cluster.local"
            and link.get("transportInsecure") is False and "kubectl-create" in item["managers"]):
        users = consumers.get(address(item), set())
        if users and users <= owned.keys():
            return {"kind": "CANONICAL_STAFF_TLS_TRANSPORT", "source": "deploy/k8s/base/staff-control-center/servers-transport.yaml", "consumerUIDs": sorted(users)}
    return None


def plan_inventory(snapshot, context, cluster_uid, node_uids, source_sha, retired=(), preserve_namespaces=()):
    """Построить проверяемый граф принадлежности; неизвестные объекты и shared consumers блокируют зависимую очистку."""
    if (snapshot.get("schema") != "kodex.installation-inventory/v1" or snapshot.get("status") != "COMPLETE"
            or snapshot.get("incomplete") or snapshot.get("context") != context or snapshot.get("clusterUID") != cluster_uid
            or not node_uids or len(node_uids) != len(set(node_uids)) or {item["uid"] for item in snapshot["nodes"]} != set(node_uids)
            or not snapshot.get("endpoint", "").startswith("https://") or not re.fullmatch(r"[a-f0-9]{64}", snapshot.get("caSHA256", ""))
            or not re.fullmatch(r"[a-f0-9]{40}", source_sha)):
        raise ValueError(ERROR)
    preserved = set(preserve_namespaces)
    observed_namespaces = {item["name"] for item in snapshot["namespaces"]}
    if (len(preserve_namespaces) != len(preserved) or len(preserved) > 32
            or any(not isinstance(name, str) or not re.fullmatch(r"[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?", name)
                   or name not in observed_namespaces for name in preserved)):
        raise ValueError(ERROR)
    namespaces = {item["name"]: item for item in snapshot["namespaces"]
                  if namespace_proof(item) and item["name"] not in preserved}
    unknown_namespaces = [item for item in snapshot["namespaces"]
                          if item["name"] in NAMESPACES and item["name"] not in preserved and item["name"] not in namespaces]
    # Один UID может обслуживаться через два API versions (например, core/events).
    objects = {}
    for item in sorted(snapshot["objects"], key=lambda entry: (entry["apiVersion"] != "v1", entry["apiVersion"])):
        previous = objects.get(item["uid"])
        if previous and address(previous) != address(item):
            raise ValueError(ERROR)
        objects.setdefault(item["uid"], item)
    by_address = {address(item): uid for uid, item in objects.items()}
    owned = {}
    for uid, item in objects.items():
        if (item["namespace"] in preserved
                or item["helm"].get("meta.helm.sh/release-namespace") in preserved):
            continue
        proof = seed_proof(item, namespaces)
        if proof:
            owned[uid] = proof
    links = {item["identity"]["uid"]: item for item in snapshot["links"]}
    consumers = {}
    for uid, item in links.items():
        for reference in item["references"]:
            consumers.setdefault(address(reference), set()).add(uid)
    changed = True
    while changed:
        changed = False
        for uid, item in objects.items():
            if (item["namespace"] in preserved
                    or item["helm"].get("meta.helm.sh/release-namespace") in preserved):
                continue
            if uid in owned or item["resource"] in SHARED_CLUSTER_RESOURCES or item["resource"] in {"namespaces", "persistentvolumes", "customresourcedefinitions"}:
                continue
            proof = None
            owners = item["ownerReferences"]
            if owners and all(owner.get("uid") in owned and objects[owner["uid"]]["namespace"] == item["namespace"] for owner in owners):
                proof = {"kind": "OWNER_UID_CLOSURE", "owners": sorted(owner["uid"] for owner in owners)}
            key = address(item)
            if item["namespace"] in namespaces:
                proof = proof or reference_origin(item, consumers, owned, objects)
                proof = proof or controller_origin(item, links.get(uid, {}), owned, objects, links, consumers, retired)
            if item["resource"] == "secrets":
                writers = sorted(parent for parent, link in links.items() if parent in owned
                                 and objects[parent]["namespace"] == item["namespace"] and any(
                    address(ref) == key and ref["relation"] == "PRODUCES" for ref in link["references"]))
                if writers:
                    proof = {"kind": "OWNED_EXACT_PRODUCER", "producers": writers}
            if item["namespace"] in namespaces:
                if item["resource"] == "endpoints":
                    service = by_address.get((item["namespace"], "services", item["name"]))
                    if service in owned and "k3s" in item["managers"]:
                        proof = {"kind": "OWNED_SERVICE_ENDPOINTS", "serviceUID": service}
                if item["resource"] in {"secrets", "configmaps", "persistentvolumeclaims"}:
                    writers = sorted(parent for parent, link in links.items() if parent in owned and any(
                        address(ref) == key and ref["relation"] == "PRODUCES" for ref in link["references"]))
                    if writers:
                        proof = {"kind": "OWNED_EXACT_PRODUCER", "producers": writers}
                    elif consumers.get(key) and consumers[key] <= owned.keys():
                        # Использование само по себе ещё не доказывает origin: такие строки остаются review required.
                        pass
                if item["resource"] == "events":
                    reference = links.get(uid, {}).get("eventReference") or {}
                    owner = reference.get("uid")
                    if owner in owned:
                        proof = {"kind": "OWNED_OBJECT_EVENT", "objectUID": owner}
            if proof:
                owned[uid] = proof
                changed = True
    # Нельзя удалить общую role/secret/config, когда имеется статически известный чужой consumer.
    shared = []
    for uid in sorted(owned):
        item = objects[uid]
        foreign = sorted(consumers.get(address(item), set()) - owned.keys())
        if item["resource"] == "customresourcedefinitions":
            plural, separator, group = item["name"].partition(".")
            if not separator:
                raise ValueError(ERROR)
            foreign = sorted(other_uid for other_uid, other in objects.items() if other_uid not in owned
                             and other["resource"] == plural and other["apiVersion"].split("/", 1)[0] == group)
        foreign_children = sorted(other_uid for other_uid, other in objects.items() if other_uid not in owned
                                  and any(owner.get("uid") == uid for owner in other["ownerReferences"]))
        if foreign_children:
            shared.append({"identity": item, "foreignChildUIDs": foreign_children})
        if foreign and item["resource"] in {"secrets", "configmaps", "roles", "clusterroles", "serviceaccounts", "persistentvolumeclaims", "customresourcedefinitions", "services", "issuers", "clusterissuers", "certificates", "bundles"}:
            shared.append({"identity": item, "foreignConsumerUIDs": foreign})
    unresolved = [{"identity": item, "ownedConsumerUIDs": sorted(consumers.get(address(item), set()) & owned.keys())}
                  for uid, item in objects.items() if item["namespace"] in namespaces and uid not in owned]
    volume_plans = []
    for volume in snapshot["volumes"]:
        claim = volume["claim"]
        if claim.get("uid") in owned:
            pvc = objects[claim["uid"]]
            if pvc["resource"] != "persistentvolumeclaims" or address(pvc) != (claim.get("namespace"), "persistentvolumeclaims", claim.get("name")):
                raise ValueError(ERROR)
            volume_plans.append({**volume, "claimProof": owned[claim["uid"]],
                                 "automaticReclamation": volume["reclaimPolicy"] == "Delete"})
            local = volume.get("localPath", "")
            if local:
                for uid, link in links.items():
                    if uid in owned:
                        continue
                    for mount in link.get("hostPaths", []):
                        normalized = posixpath.normpath(mount)
                        if normalized.startswith("/") and posixpath.commonpath([normalized, local]) in {normalized, local}:
                            shared.append({"identity": pvc, "foreignHostPathConsumerUID": uid})
    value = {"schema": "kodex.installation-cleanup-plan/v1", "context": context, "clusterUID": cluster_uid,
             "nodes": snapshot["nodes"], "endpoint": snapshot.get("endpoint", ""), "caSHA256": snapshot.get("caSHA256", ""),
             "sourceSHA": source_sha, "inventoryDigest": digest(snapshot),
             "namespaces": list(namespaces.values()), "preserveNamespaces": [item for item in snapshot["namespaces"] if item["name"] not in namespaces],
             "deleteCandidates": [{"identity": objects[uid], "proof": proof} for uid, proof in sorted(owned.items())],
             "volumes": volume_plans, "physicalStorage": snapshot.get("physicalStorage"), "unresolved": unresolved, "shared": shared,
             "unprovenNamespaces": unknown_namespaces,
             "status": "OWNERSHIP_REVIEW_REQUIRED" if unresolved or shared or unknown_namespaces else "OWNERSHIP_CLASSIFIED",
             "applyStatus": "NOT_EXECUTED"}
    if preserved:
        value["preservedNamespaces"] = sorted(preserved)
        if value["physicalStorage"] is not None:
            selected_volumes = {item["identity"]["uid"] for item in volume_plans}
            value["physicalStorage"] = {**value["physicalStorage"],
                                        "volumes": [item for item in value["physicalStorage"]["volumes"]
                                                    if item["volumeUID"] in selected_volumes]}
    value["digest"] = digest(value)
    return value


def main():
    """Сохранить адресный план без каких-либо mutation-команд; expected identity задаётся оператором явно."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", required=True, type=Path)
    parser.add_argument("--inventory-sha256", required=True)
    parser.add_argument("--context", required=True)
    parser.add_argument("--cluster-uid", required=True)
    parser.add_argument("--node-uid", required=True, action="append")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--preserve-namespace", action="append", default=[],
                        help="retain one observed infrastructure namespace; its consumers still constrain deletion")
    args = parser.parse_args()
    snapshot = read_private(args.inventory, args.inventory_sha256)
    source = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    result = plan_inventory(snapshot, args.context, args.cluster_uid, args.node_uid, source,
                            preserve_namespaces=args.preserve_namespace)
    write_private(args.output, result)
    print(json.dumps({"status": result["status"], "candidates": len(result["deleteCandidates"]),
                      "unresolved": len(result["unresolved"]), "shared": len(result["shared"]),
                      "volumes": len(result["volumes"]), "digest": result["digest"]}))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError, subprocess.SubprocessError):
        print(ERROR, file=__import__("sys").stderr)
        raise SystemExit(1) from None
