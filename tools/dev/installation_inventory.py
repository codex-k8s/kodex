#!/usr/bin/env python3
"""Адресный read-only inventory установки: API metadata без Secret data и last-applied."""

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import ssl
import stat
import re
import subprocess
import tempfile
import urllib.error
import urllib.parse
import urllib.request

NAMESPACES = frozenset({"kodex-system", "kodex-runtime", "kodex-trust", "kodex-secret-drafts",
                        "kodex-infra", "identity", "observability"})
MAX_RESPONSE = 16 << 20
ERROR = "Installation inventory failed: target, transport, metadata or output validation rejected"


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """Запретить перенос authenticated запроса на иной endpoint."""
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None


def unique_object(pairs):
    """Не принимать неоднозначный JSON в проверяемой identity и inventory."""
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(ERROR)
        result[key] = value
    return result


def decode(payload):
    """Разобрать ограниченный UTF-8 JSON без повторяющихся ключей."""
    if len(payload) > MAX_RESPONSE:
        raise ValueError(ERROR)
    return json.loads(payload, object_pairs_hook=unique_object)


def metadata(item, resource, version, namespace=""):
    """Проецировать только identity/ownership; last-applied и произвольные annotations не сохраняются."""
    source = item["metadata"]
    if not all(isinstance(source.get(key), str) and source[key] for key in ("name", "uid", "resourceVersion")):
        raise ValueError(ERROR)
    labels = source.get("labels", {})
    annotations = source.get("annotations", {})
    return {"resource": resource, "apiVersion": version, "namespace": source.get("namespace", namespace),
            "name": source["name"], "uid": source["uid"], "resourceVersion": source["resourceVersion"],
            "labels": {key: value for key, value in labels.items() if (key in ("owner", "name", "status", "version") or key.startswith("app.kubernetes.io/") or key.split("/", 1)[0] == "kodex.dev" or key.split("/", 1)[0].endswith(".kodex.dev"))},
            "helm": {key: annotations[key] for key in ("meta.helm.sh/release-name", "meta.helm.sh/release-namespace") if key in annotations},
            "ownerReferences": [{key: owner[key] for key in ("apiVersion", "kind", "name", "uid", "controller") if key in owner}
                                for owner in source.get("ownerReferences", [])],
            "managers": sorted({entry["manager"] for entry in source.get("managedFields", []) if entry.get("manager")}),
            "deleting": bool(source.get("deletionTimestamp"))}


class Client:
    """Один TLS endpoint и exact kube-context; credentials остаются в памяти и временных0600 файлах."""
    def __init__(self, kubectl, kubeconfig, context, temporary):
        result = subprocess.run([kubectl, "--kubeconfig", kubeconfig, "--context", context,
                                 "config", "view", "--minify", "--flatten", "--raw", "-o", "json"],
                                capture_output=True, timeout=20, check=False)
        if result.returncode:
            raise ValueError(ERROR)
        config = decode(result.stdout)
        if (len(config.get("contexts", [])) != 1 or config["contexts"][0]["name"] != context
                or len(config.get("clusters", [])) != 1 or len(config.get("users", [])) != 1):
            raise ValueError(ERROR)
        selection = config["contexts"][0]["context"]
        if selection["cluster"] != config["clusters"][0]["name"] or selection["user"] != config["users"][0]["name"]:
            raise ValueError(ERROR)
        cluster, user = config["clusters"][0]["cluster"], config["users"][0]["user"]
        endpoint = urllib.parse.urlsplit(cluster["server"])
        if (endpoint.scheme != "https" or not endpoint.hostname or endpoint.username or endpoint.password
                or endpoint.query or endpoint.fragment or endpoint.path not in ("", "/")
                or cluster.get("insecure-skip-tls-verify") or cluster.get("proxy-url")
                or cluster.get("tls-server-name", endpoint.hostname) != endpoint.hostname
                or any(key in user for key in ("exec", "auth-provider", "tokenFile"))):
            raise ValueError(ERROR)
        ca = base64.b64decode(cluster["certificate-authority-data"], validate=True)
        tls = ssl.create_default_context(cadata=ca.decode("ascii"))
        self.headers = {}
        if "client-certificate-data" in user and "client-key-data" in user:
            paths = []
            for key in ("client-certificate-data", "client-key-data"):
                path = Path(temporary) / key
                fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                with os.fdopen(fd, "wb") as stream:
                    stream.write(base64.b64decode(user[key], validate=True))
                paths.append(path)
            tls.load_cert_chain(*paths)
        elif isinstance(user.get("token"), str) and user["token"]:
            self.headers["Authorization"] = "Bearer " + user["token"]
        else:
            raise ValueError(ERROR)
        self.endpoint = cluster["server"].rstrip("/")
        self.ca_digest = hashlib.sha256(ca).hexdigest()
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                                  urllib.request.HTTPSHandler(context=tls), NoRedirect())

    def get(self, path, partial=""):
        """Сделать только GET на выбранный endpoint; metadata не имеет full-object fallback."""
        if not path.startswith(("/api/", "/apis")) or ".." in path or "#" in path:
            raise ValueError(ERROR)
        headers = {**self.headers, "Accept": "application/json"}
        if partial:
            headers["Accept"] += f";as={partial};g=meta.k8s.io;v=v1"
        request = urllib.request.Request(self.endpoint + path, headers=headers, method="GET")
        with self.opener.open(request, timeout=20) as response:
            result = decode(response.read(MAX_RESPONSE + 1))
        if partial and (result.get("kind") != partial or result.get("apiVersion") != "meta.k8s.io/v1"):
            raise ValueError(ERROR)
        return result

    def listing(self, path, partial=True):
        """Прочитать все страницы с ограничением cardinality, не принимая незавершённый inventory."""
        result, continuation, pages = [], "", 0
        while True:
            query = urllib.parse.urlencode({"limit": 500, "continue": continuation})
            page = self.get(path + "?" + query, "PartialObjectMetadataList" if partial else "")
            items = page["items"]
            if items is not None and not isinstance(items, list):
                raise ValueError(ERROR)
            result.extend(items or [])
            pages += 1
            continuation = page.get("metadata", {}).get("continue", "")
            if len(result) > 20000 or pages > 100:
                raise ValueError(ERROR)
            if not continuation:
                return result



def object_links(item, resource, version):
    """Извлечь только имена зависимостей workload/PKI/RBAC; env values, Secret data и annotations не сохранять."""
    identity = metadata(item, resource, version)
    namespace = identity["namespace"]
    references = []
    def add(kind, name, target_namespace=None, relation="CONSUMES"):
        if isinstance(name, str) and name:
            references.append({"resource": kind, "name": name,
                               "namespace": namespace if target_namespace is None else target_namespace,
                               "relation": relation})
    spec = item.get("spec", {})
    if resource == "cronjobs":
        spec = spec.get("jobTemplate", {}).get("spec", {})
    pod = spec if resource == "pods" else spec.get("template", {}).get("spec", {})
    images = []
    if pod:
        add("serviceaccounts", pod.get("serviceAccountName", "default"))
        for pull in pod.get("imagePullSecrets", []):
            add("secrets", pull.get("name"))
        for container in pod.get("containers", []) + pod.get("initContainers", []) + pod.get("ephemeralContainers", []):
            if container.get("image"):
                images.append(container["image"])
            for source in container.get("envFrom", []):
                add("secrets", source.get("secretRef", {}).get("name"))
                add("configmaps", source.get("configMapRef", {}).get("name"))
            for entry in container.get("env", []):
                source = entry.get("valueFrom", {})
                add("secrets", source.get("secretKeyRef", {}).get("name"))
                add("configmaps", source.get("configMapKeyRef", {}).get("name"))
        for volume in pod.get("volumes", []):
            add("secrets", volume.get("secret", {}).get("secretName"))
            add("configmaps", volume.get("configMap", {}).get("name"))
            add("persistentvolumeclaims", volume.get("persistentVolumeClaim", {}).get("claimName"))
            for projection in volume.get("projected", {}).get("sources", []):
                add("secrets", projection.get("secret", {}).get("name"))
                add("configmaps", projection.get("configMap", {}).get("name"))
    if resource == "services":
        transport = item["metadata"].get("annotations", {}).get("traefik.ingress.kubernetes.io/service.serverstransport", "")
        if transport.startswith(namespace + "-") and transport.endswith("@kubernetescrd"):
            add("serverstransports", transport[len(namespace) + 1:-len("@kubernetescrd")])
    if resource == "serverstransports":
        for root in spec.get("rootCAs", []):
            add("secrets", root.get("secret"))
        for certificate in spec.get("certificatesSecrets", []):
            add("secrets", certificate)
    if resource == "bundles":
        target = spec.get("target", {})
        selector = target.get("namespaceSelector", {})
        labels = selector.get("matchLabels", {})
        if (set(selector) == {"matchLabels"} and set(labels) == {"kubernetes.io/metadata.name"}
                and target.get("configMap", {}).get("key")):
            add("configmaps", identity["name"], labels["kubernetes.io/metadata.name"], "PRODUCES")
    if resource == "certificates":
        add("secrets", spec.get("secretName"), relation="PRODUCES")
        issuer = spec.get("issuerRef", {})
        add("clusterissuers" if issuer.get("kind") == "ClusterIssuer" else "issuers", issuer.get("name"),
            "" if issuer.get("kind") == "ClusterIssuer" else namespace)
    if resource == "issuers":
        add("secrets", spec.get("ca", {}).get("secretName"))
    if resource in {"mutatingwebhookconfigurations", "validatingwebhookconfigurations"}:
        for webhook in item.get("webhooks", []):
            service = webhook.get("clientConfig", {}).get("service", {})
            add("services", service.get("name"), service.get("namespace", ""))
    if resource in ("rolebindings", "clusterrolebindings"):
        role = item.get("roleRef", {})
        add("clusterroles" if role.get("kind") == "ClusterRole" else "roles", role.get("name"),
            "" if role.get("kind") == "ClusterRole" else namespace)
        for subject in item.get("subjects", []):
            if subject.get("kind") == "ServiceAccount":
                add("serviceaccounts", subject.get("name"), subject.get("namespace", namespace), "GRANTS")
    if resource == "ingresses":
        for tls in spec.get("tls", []):
            add("secrets", tls.get("secretName"))
        add("services", spec.get("defaultBackend", {}).get("service", {}).get("name"))
        for rule in spec.get("rules", []):
            for path in rule.get("http", {}).get("paths", []):
                add("services", path.get("backend", {}).get("service", {}).get("name"))
    if resource in ("events",):
        reference = item.get("regarding", item.get("involvedObject", {}))
        event_reference = {key: reference[key] for key in ("apiVersion", "kind", "namespace", "name", "uid") if key in reference}
    else:
        event_reference = None
    return {"identity": identity, "references": references, "images": sorted(set(images)),
            "hostPaths": [volume["hostPath"]["path"] for volume in pod.get("volumes", []) if volume.get("hostPath", {}).get("path")],
            "nodeName": pod.get("nodeName", ""), "eventReference": event_reference,
            "podState": {"phase": item.get("status", {}).get("phase", "Unknown"),
                         "containers": [{key: status[key] for key in ("name", "image", "imageID", "containerID", "ready", "restartCount") if key in status}
                                        for status in item.get("status", {}).get("containerStatuses", []) + item.get("status", {}).get("initContainerStatuses", []) + item.get("status", {}).get("ephemeralContainerStatuses", [])]}
                        if resource == "pods" else None,
            "serviceSelector": spec.get("selector", {}) if resource == "services" else {},
            "controllerSelector": spec.get("selector", {}) if resource in {"deployments", "statefulsets", "daemonsets", "replicasets"} else {},
            "policySelector": spec.get("podSelector", {}) if resource == "networkpolicies" else (spec.get("selector", {}) if resource == "poddisruptionbudgets" else {}),
            "claimTemplates": [claim["metadata"]["name"] for claim in spec.get("volumeClaimTemplates", [])] if resource == "statefulsets" else [],
            "transportServerName": spec.get("serverName", "") if resource == "serverstransports" else "",
            "transportInsecure": spec.get("insecureSkipVerify", False) if resource == "serverstransports" else False}


def inventory(client, context):
    """Собрать metadata всего exact кластера для поиска чужих consumers; inventory не разрешает удаление."""
    cluster = client.get("/api/v1/namespaces/kube-system", "PartialObjectMetadata")
    namespaces = client.listing("/api/v1/namespaces")
    resources = client.get("/api/v1")["resources"]
    discovery = [("v1", "/api/v1", resources)]
    groups = client.get("/apis")["groups"]
    for group in groups:
        version = group["preferredVersion"]["groupVersion"]
        path = "/apis/" + version
        discovery.append((version, path, client.get(path)["resources"]))
    objects, failures = [], []
    for version, prefix, definitions in discovery:
        for definition in definitions:
            resource = definition["name"]
            if "/" in resource or "list" not in definition.get("verbs", []):
                continue
            # Metrics/discovery-only endpoints не являются удаляемым состоянием установки.
            if "delete" not in definition.get("verbs", []):
                continue
            path = prefix + "/" + resource
            try:
                records = client.listing(path)
                objects.extend(metadata(item, resource, version) for item in records)
            except urllib.error.HTTPError as error:
                failures.append({"resource": resource, "apiVersion": version, "namespace": "*" if definition["namespaced"] else "",
                                 "status": error.code})
    links = []
    link_resources = {
        "v1": {"pods", "services", "events"},
        "apps/v1": {"deployments", "replicasets", "statefulsets", "daemonsets"},
        "batch/v1": {"jobs", "cronjobs"},
        "cert-manager.io/v1": {"certificates", "issuers"},
        "admissionregistration.k8s.io/v1": {"mutatingwebhookconfigurations", "validatingwebhookconfigurations"},
        "trust.cert-manager.io/v1alpha1": {"bundles"},
        "rbac.authorization.k8s.io/v1": {"rolebindings", "clusterrolebindings"},
        "networking.k8s.io/v1": {"ingresses", "networkpolicies"},
        "policy/v1": {"poddisruptionbudgets"},
        "traefik.io/v1alpha1": {"serverstransports"},
        "events.k8s.io/v1": {"events"},
    }
    for version, prefix, definitions in discovery:
        for definition in definitions:
            resource = definition["name"]
            if resource not in link_resources.get(version, set()) or "list" not in definition.get("verbs", []):
                continue
            for item in client.listing(prefix + "/" + resource, partial=False):
                links.append(object_links(item, resource, version))
    # PV/PVC topology не содержит credentials; только явные поля linkage и reclamation.
    volumes = []
    for item in client.listing("/api/v1/persistentvolumes", partial=False):
        spec = item["spec"]
        volumes.append({"identity": metadata(item, "persistentvolumes", "v1"),
                        "claim": {key: value for key, value in spec.get("claimRef", {}).items() if key in ("name", "namespace", "uid")},
                        "reclaimPolicy": spec.get("persistentVolumeReclaimPolicy", ""),
                        "localPath": spec.get("local", {}).get("path", spec.get("hostPath", {}).get("path", "")),
                        "csiDriver": spec.get("csi", {}).get("driver", ""),
                        "nodeAffinity": spec.get("nodeAffinity", {})})
    return {"schema": "kodex.installation-inventory/v1", "context": context,
            "clusterUID": cluster["metadata"]["uid"], "endpoint": client.endpoint, "caSHA256": client.ca_digest,
            "namespaces": [metadata(item, "namespaces", "v1") for item in namespaces],
            "nodes": [metadata(item, "nodes", "v1") for item in client.listing("/api/v1/nodes")],
            "objects": sorted(objects, key=lambda item: (item["apiVersion"], item["resource"], item["namespace"], item["name"])),
            "volumes": volumes, "links": links, "incomplete": failures, "status": "COMPLETE" if not failures else "INCOMPLETE"}


def k3d_storage(snapshot):
    """Связать local-path PV с exact k3d container, node UID и inode; только read-only metadata файлов."""
    context = snapshot["context"]
    if not re.fullmatch(r"k3d-[a-z0-9][a-z0-9-]*", context):
        raise ValueError(ERROR)
    cluster = context[4:]
    result = subprocess.run(["k3d", "node", "list", "-o", "json"], capture_output=True, timeout=20, check=True)
    registry = [entry for entry in decode(result.stdout) if entry.get("runtimeLabels", {}).get("k3d.cluster") == cluster
                and entry.get("role") in ("server", "agent")]
    nodes = {entry["name"]: entry["uid"] for entry in snapshot["nodes"]}
    if len(registry) != len(nodes) or {entry["name"] for entry in registry} != set(nodes):
        raise ValueError(ERROR)
    containers = {}
    for name in nodes:
        result = subprocess.run(["docker", "inspect", "--format", '{{json .Id}}', name], capture_output=True, timeout=20, check=True)
        identifier = json.loads(result.stdout)
        if not re.fullmatch(r"[a-f0-9]{64}", identifier):
            raise ValueError(ERROR)
        result = subprocess.run(["docker", "inspect", "--format", '{{index .Config.Labels "k3d.cluster"}}', identifier],
                                capture_output=True, timeout=20, check=True)
        if result.stdout.decode().strip() != cluster:
            raise ValueError(ERROR)
        containers[name] = identifier
    volumes, inodes = [], set()
    for volume in snapshot["volumes"]:
        claim = volume["claim"]
        if claim.get("namespace") not in NAMESPACES:
            continue
        if not all(isinstance(claim.get(key), str) and re.fullmatch(r"[a-z0-9][a-z0-9.-]*", claim[key]) for key in ("uid", "namespace", "name")):
            raise ValueError(ERROR)
        expected = "/var/lib/rancher/k3s/storage/pvc-" + claim["uid"] + "_" + claim["namespace"] + "_" + claim["name"]
        if volume["localPath"] != expected or volume["csiDriver"]:
            raise ValueError(ERROR)
        terms = volume["nodeAffinity"].get("required", {}).get("nodeSelectorTerms", [])
        if len(terms) != 1 or set(terms[0]) != {"matchExpressions"} or len(terms[0]["matchExpressions"]) != 1:
            raise ValueError(ERROR)
        expression = terms[0]["matchExpressions"][0]
        if (expression.get("key") != "kubernetes.io/hostname" or expression.get("operator") != "In"
                or len(expression.get("values", [])) != 1 or expression["values"][0] not in nodes):
            raise ValueError(ERROR)
        node = expression["values"][0]
        base = ["docker", "exec", containers[node]]
        real = subprocess.run(base + ["readlink", "-f", expected], capture_output=True, timeout=20, check=True)
        if real.stdout.decode().strip() != expected:
            raise ValueError(ERROR)
        actual = subprocess.run(base + ["stat", "-c", "%d:%i:%f", expected], capture_output=True, timeout=20, check=True)
        device, inode, mode = actual.stdout.decode().strip().split(":")
        if not device.isdigit() or not inode.isdigit() or not stat.S_ISDIR(int(mode, 16)) or (node, device, inode) in inodes:
            raise ValueError(ERROR)
        inodes.add((node, device, inode))
        volumes.append({"volumeUID": volume["identity"]["uid"], "claimUID": claim["uid"], "node": node,
                        "nodeUID": nodes[node], "containerID": containers[node], "path": expected,
                        "device": device, "inode": inode})
    return {"schema": "kodex.k3d-local-storage/v1", "containers": containers, "volumes": volumes}


def write_private(path, value):
    """Опубликовать новый inventory без перезаписи и без содержимого credentials в stdout."""
    directory = path.parent
    mode = directory.stat()
    if (not path.is_absolute() or directory.resolve() != directory or mode.st_uid != os.getuid()
            or mode.st_mode & 0o077):
        raise ValueError(ERROR)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())


def main():
    """Требовать явные client/config/context и сохранить только read-only результат."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kubectl", required=True)
    parser.add_argument("--kubeconfig", required=True)
    parser.add_argument("--context", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--with-k3d-storage", action="store_true")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="kodex-inventory-") as temporary:
        client = Client(args.kubectl, args.kubeconfig, args.context, temporary)
        result = inventory(client, args.context)
        if args.with_k3d_storage:
            result["physicalStorage"] = k3d_storage(result)
        write_private(args.output, result)
        print(json.dumps({"status": result["status"], "objects": len(result["objects"]),
                          "namespaces": len(result["namespaces"]), "volumes": len(result["volumes"]),
                          "incomplete": len(result["incomplete"])}))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        print(ERROR, file=__import__("sys").stderr)
        raise SystemExit(1) from None
