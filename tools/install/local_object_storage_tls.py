#!/usr/bin/env python3
"""Материализует TLS локального S3 и штатные CA consumers immutable-установки."""

import argparse
import copy
from pathlib import Path
import re
import ssl

import yaml

HOST = "seaweedfs-s3.kodex-system.svc.cluster.local"
ENDPOINT = "https://" + HOST + ":8333"
CA_NAME = "kodex-object-storage-ca"
CA_MOUNT = "/var/run/config/kodex/object-storage-ca"
TLS_MOUNT = "/var/run/secrets/kodex/seaweedfs-tls"


def one(resources, kind, name):
    matches = [r for r in resources if r.get("kind") == kind and r["metadata"]["name"] == name]
    if len(matches) != 1:
        raise ValueError("Exact object storage consumer is required")
    return matches[0]


def add_ca(pod, container, variable="SSL_CERT_FILE"):
    if any(v["name"] == "object-storage-ca" for v in pod.get("volumes", [])):
        raise ValueError("Object storage CA volume already exists")
    pod.setdefault("volumes", []).append({"name": "object-storage-ca", "configMap": {
        "name": CA_NAME, "items": [{"key": "ca.crt", "path": "ca.crt"}]}})
    container.setdefault("volumeMounts", []).append({"name": "object-storage-ca", "mountPath": CA_MOUNT, "readOnly": True})
    if any(e["name"] == variable for e in container.get("env", [])):
        raise ValueError("Object storage trust environment already exists")
    container.setdefault("env", []).append({"name": variable, "value": CA_MOUNT + "/ca.crt"})


def storage(resources, ca, issuer):
    ssl.create_default_context(cadata=ca)
    if not re.fullmatch(r"[a-z0-9][a-z0-9.-]*", issuer):
        raise ValueError("Exact certificate issuer is required")
    result = copy.deepcopy(resources)
    pod = one(result, "StatefulSet", "seaweedfs")["spec"]["template"]["spec"]
    container = next(c for c in pod["containers"] if c["name"] == "seaweedfs")
    container["args"] += ["-s3.cert.file=" + TLS_MOUNT + "/tls.crt", "-s3.key.file=" + TLS_MOUNT + "/tls.key", "-s3.port.https=0"]
    pod["volumes"].append({"name": "s3-tls", "secret": {"secretName": "seaweedfs-s3-tls", "defaultMode": 0o440}})
    container["volumeMounts"].append({"name": "s3-tls", "mountPath": TLS_MOUNT, "readOnly": True})
    for probe in ["startupProbe", "readinessProbe"]:
        command = container[probe]["exec"]["command"]
        if command[-1].count("http://127.0.0.1:8333/") != 1:
            raise ValueError("Exact S3 readiness probe is required")
        command[-1] = command[-1].replace("http://127.0.0.1:8333/", f"--cacert {TLS_MOUNT}/ca.crt --resolve {HOST}:8333:127.0.0.1 {ENDPOINT}/")
    job = one(result, "Job", "seaweedfs-bucket-bootstrap")["spec"]["template"]["spec"]
    bootstrap = job["containers"][0]
    if bootstrap["args"][0].count("endpoint=http://seaweedfs-s3:8333") != 1:
        raise ValueError("Exact S3 bucket bootstrap is required")
    bootstrap["args"][0] = bootstrap["args"][0].replace("endpoint=http://seaweedfs-s3:8333", "endpoint=" + ENDPOINT)
    add_ca(job, bootstrap, "AWS_CA_BUNDLE")
    result.append({"apiVersion": "cert-manager.io/v1", "kind": "Certificate", "metadata": {
        "name": "seaweedfs-s3-tls", "namespace": "kodex-system"}, "spec": {
        "secretName": "seaweedfs-s3-tls", "issuerRef": {"kind": "ClusterIssuer", "name": issuer},
        "dnsNames": [HOST], "privateKey": {"rotationPolicy": "Always"}}})
    for namespace in ["kodex-system", "kodex-runtime"]:
        result.append({"apiVersion": "v1", "kind": "ConfigMap", "metadata": {
            "name": CA_NAME, "namespace": namespace}, "immutable": True, "data": {"ca.crt": ca}})
    # Фоновый archive worker принадлежит kodex-runtime; cross-namespace peer
    # ограничен одновременно namespace и server-assigned managed label.
    policy = one(result, "NetworkPolicy", "seaweedfs-exact-local-paths")
    policy["spec"]["ingress"].append({"from": [{"namespaceSelector": {"matchLabels": {
        "kubernetes.io/metadata.name": "kodex-runtime"}}, "podSelector": {"matchLabels": {
        "session-archive.kodex.dev/managed": "true"}}}], "ports": [{"protocol": "TCP", "port": 8333}]})
    dns_rule = {"to": [{"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "kube-system"}},
        "podSelector": {"matchLabels": {"k8s-app": "kube-dns"}}}],
        "ports": [{"protocol": protocol, "port": 53} for protocol in ["UDP", "TCP"]]}
    result.append({"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "metadata": {
        "name": "session-archive-local-object-storage", "namespace": "kodex-runtime"}, "spec": {
        "podSelector": {"matchLabels": {"session-archive.kodex.dev/managed": "true"}}, "policyTypes": ["Egress"],
        "egress": [copy.deepcopy(dns_rule), {"to": [{"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "kodex-system"}},
            "podSelector": {"matchLabels": {"app.kubernetes.io/name": "seaweedfs", "app.kubernetes.io/component": "object-storage"}}}],
            "ports": [{"protocol": "TCP", "port": 8333}]}]}})
    result.append({"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "metadata": {
        "name": "local-object-storage-dns", "namespace": "kodex-system"}, "spec": {
        "podSelector": {"matchExpressions": [{"key": "app.kubernetes.io/name", "operator": "In",
            "values": ["seaweedfs", "seaweedfs-bucket-bootstrap"]}]}, "policyTypes": ["Egress"],
        "egress": [copy.deepcopy(dns_rule)]}})
    for resource in result:
        resource["metadata"].setdefault("labels", {})["app.kubernetes.io/part-of"] = "kodex"
    return result


def consumers(resources):
    result = copy.deepcopy(resources)
    for name in ["control-plane", "artifact-retention", "backup-controller", "session-archive"]:
        deployment = one(result, "Deployment", name)
        pod = deployment["spec"]["template"]["spec"]
        container = next(c for c in pod["containers"] if c["name"] == name)
        add_ca(pod, container)
    config = one(result, "ConfigMap", "session-archive-runtime")
    config["data"]["SESSION_ARCHIVE_OBJECT_STORAGE_CA_CONFIG_MAP"] = CA_NAME
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=["storage", "consumers"])
    parser.add_argument("--input", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--ca-file")
    parser.add_argument("--cluster-issuer")
    args = parser.parse_args()
    resources = [r for r in yaml.safe_load_all(Path(args.input).read_text()) if r]
    if args.phase == "storage":
        if not args.ca_file or not args.cluster_issuer:
            parser.error("CA and issuer are required")
        output = storage(resources, Path(args.ca_file).read_text(), args.cluster_issuer)
    else:
        if args.ca_file or args.cluster_issuer:
            parser.error("Storage arguments are not accepted for consumers")
        output = consumers(resources)
    Path(args.output).write_text(yaml.safe_dump_all(output, sort_keys=False))


if __name__ == "__main__":
    main()
