"""Read-only проверка переносимого render и внешних зависимостей без credentials."""
import argparse
import hashlib
import http.client
import ipaddress
import json
from pathlib import Path
import socket
import ssl
import subprocess

import yaml
from deployment_profile import Rejected, require, https_endpoint, read_render_profile, verify_proxy_controller, verify_proxy_pods


def resolve_exact(host):
    # libc DNS выполняется в ограниченном дочернем процессе, не блокирует preflight.
    result = subprocess.run(["python3", "-c",
        "import socket,json,sys; print(json.dumps(sorted({r[4][0] for r in socket.getaddrinfo(sys.argv[1],443,type=socket.SOCK_STREAM)})))",
        host], capture_output=True, timeout=10, check=False)
    require(result.returncode == 0, "OIDC_DNS_FAILED")
    return {ipaddress.ip_address(value) for value in json.loads(result.stdout)}


def fetch_json(url, context, address):
    # Exact IP из проверенного snapshot + исходный SNI/Host. Повторного DNS и
    # ambient HTTP(S)_PROXY нет; redirects никогда не выполняются.
    endpoint = https_endpoint(url)
    connection = http.client.HTTPConnection(endpoint.hostname, 443, timeout=8)
    raw_socket = socket.create_connection((str(address), 443), timeout=8)
    try:
        connection.sock = context.wrap_socket(raw_socket, server_hostname=endpoint.hostname)
        connection.request("GET", endpoint.path or "/", headers={"Accept": "application/json", "Host": endpoint.netloc})
        response = connection.getresponse()
        raw = response.read(1048577)
        require(response.status == 200 and len(raw) <= 1048576, "OIDC_DOCUMENT_REJECTED")
        return json.loads(raw)
    finally:
        connection.close()
        raw_socket.close()


def verify_documents(profile, discovery, jwks):
    identity = profile["identity"]
    require(discovery.get("issuer") == identity["issuer"] and discovery.get("jwks_uri") == identity["jwks_url"],
            "OIDC_DISCOVERY_MISMATCH")
    issuer = https_endpoint(identity["issuer"])
    for key in ("authorization_endpoint", "token_endpoint", "end_session_endpoint"):
        require(https_endpoint(discovery.get(key)).netloc == issuer.netloc, "OIDC_ENDPOINT_AUTHORITY_MISMATCH")
    require(discovery.get("authorization_endpoint") == identity["authorization_url"] and
            discovery.get("token_endpoint") == identity["token_url"], "OIDC_ENDPOINT_CONFIG_DRIFT")
    require("S256" in discovery.get("code_challenge_methods_supported", []) and
            "code" in discovery.get("response_types_supported", []), "OIDC_PKCE_CODE_REQUIRED")
    require(isinstance(jwks.get("keys"), list) and 0 < len(jwks["keys"]) <= 32 and
            any(k.get("kty") == "RSA" and k.get("use", "sig") == "sig" and k.get("alg", "RS256") == "RS256"
                and k.get("kid") and k.get("n") and k.get("e") for k in jwks["keys"]), "OIDC_SIGNING_KEYS_REQUIRED")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--render", required=True)
    parser.add_argument("--sha256", required=True)
    parser.add_argument("--context")
    parser.add_argument("--oidc-ca-file")
    parser.add_argument("--ingress-deployment")
    parser.add_argument("--ingress-namespace")
    parser.add_argument("--ingress-service")
    parser.add_argument("--plan", action="store_true")
    args = parser.parse_args()
    try:
        raw = Path(args.render).read_bytes()
        require(len(raw) <= 32 * 1024 * 1024 and hashlib.sha256(raw).hexdigest() == args.sha256,
                "RENDER_DIGEST_MISMATCH")
        resources = list(filter(None, yaml.safe_load_all(raw)))
        profile = read_render_profile(resources)
        if args.plan:
            print(json.dumps({"status": "PLANNED", "renderSHA256": args.sha256,
                              "identityMode": profile["identity"]["mode"], "publicTLSMode": profile["public_tls"]["mode"]}))
            return
        require(args.context, "EXACT_CONTEXT_REQUIRED")
        def kube(arguments):
            result = subprocess.run(["kubectl", "--context", args.context, "--request-timeout=10s", *arguments],
                                    capture_output=True, timeout=15, check=False)
            require(result.returncode == 0, "KUBERNETES_READ_FAILED")
            return result.stdout
        if profile["identity"]["mode"] == "external":
            identity = profile["identity"]
            require(args.oidc_ca_file, "OIDC_CA_REQUIRED")
            networks = [ipaddress.ip_network(c) for c in identity["endpoint_cidrs"]]
            # Полный DNS snapshot, не только первый доступный A/AAAA.
            snapshots = {}
            for host in {identity["connect_host"], https_endpoint(identity["issuer"]).hostname}:
                addresses = resolve_exact(host)
                snapshots[host] = addresses
                require(addresses and all(any(address in network for network in networks) for address in addresses),
                        "OIDC_DNS_OUTSIDE_ALLOWLIST")
            context = ssl.create_default_context(cafile=args.oidc_ca_file)
            context.minimum_version = ssl.TLSVersion.TLSv1_3
            address = sorted(snapshots[identity["connect_host"]], key=str)[0]
            discovery = fetch_json(identity["issuer"].rstrip("/") + "/.well-known/openid-configuration", context, address)
            jwks = fetch_json(identity["jwks_url"], context, address)
            verify_documents(profile, discovery, jwks)
        if profile["public_tls"]["mode"] == "external":
            require(args.ingress_deployment and args.ingress_namespace, "EXACT_INGRESS_DEPLOYMENT_REQUIRED")
            controller = json.loads(kube(["-n", args.ingress_namespace, "get", "deployment", args.ingress_deployment, "-o", "json"]))
            verify_proxy_controller(controller, profile)
            require(args.ingress_service, "EXACT_INGRESS_SERVICE_REQUIRED")
            service = json.loads(kube(["-n", args.ingress_namespace, "get", "service", args.ingress_service, "-o", "json"]))
            selector = ",".join(k + "=" + v for k, v in service.get("spec", {}).get("selector", {}).items())
            require(selector, "PROXY_SERVICE_SELECTOR_REQUIRED")
            pods = json.loads(kube(["-n", args.ingress_namespace, "get", "pods", "-l", selector, "-o", "json"]))["items"]
            replica_sets = json.loads(kube(["-n", args.ingress_namespace, "get", "replicasets", "-l", selector, "-o", "json"]))["items"]
            slices = json.loads(kube(["-n", args.ingress_namespace, "get", "endpointslices", "-l", "kubernetes.io/service-name=" + args.ingress_service, "-o", "json"]))["items"]
            verify_proxy_pods(controller, pods, replica_sets, service, slices)
            # Этот путь не удаляет старые Certificate и не меняет shared ingress.
            current = kube(["-n", "kodex-system", "get", "certificate", "staff-control-center-public", "--ignore-not-found", "-o", "name"])
            require(not current.strip(), "EXISTING_PUBLIC_CERTIFICATE_REQUIRES_SEPARATE_MIGRATION")
        print(json.dumps({"status": "PREFLIGHT_PASSED", "renderSHA256": args.sha256,
                          "workloadConnectivity": "NOT_RUN", "login": "NOT_RUN"}))
    except (Rejected, ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError):
        raise SystemExit("Portable preflight rejected") from None


if __name__ == "__main__":
    main()
