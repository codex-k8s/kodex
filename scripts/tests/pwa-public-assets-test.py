#!/usr/bin/env python3
"""Проверка exact PWA ingress и настоящего HTTP proxy без живых credentials."""

import argparse
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import tempfile
import time
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[2]
PUBLIC = ROOT / "services/staff/control-center/public"
BASE = ROOT / "deploy/k8s/base/staff-control-center"
ASSETS = {"/manifest.webmanifest": "application/manifest+json", "/logo.png": "image/png", "/sw.js": "application/javascript"}
TRAEFIK = "docker.io/library/traefik:v3.6.6@sha256:82d3d16dde0474a51fef00b28de143d48b67f7a27453224d5e7b5aaefff26a97"


def run(*args, data=None, timeout=120):
    result = subprocess.run(args, input=data, text=True, capture_output=True, timeout=timeout)
    if result.returncode:
        # Не печатаем stdout/stderr: fixture может генерировать TLS material.
        raise RuntimeError(f"fixture command failed: {args[0]} (exit {result.returncode})")
    return result.stdout


def documents(path):
    return [json.loads(line) for line in run("yq", "-o=json", "-I=0", ".", str(path)).splitlines() if line != "null"]


def ingress(resources, name):
    found = [r for r in resources if r.get("kind") == "Ingress" and r["metadata"]["name"] == name]
    assert len(found) == 1, f"missing or duplicate ingress: {name}"
    return found[0]


def verify(resources, hot=False):
    public = ingress(resources, "staff-control-center-public-assets")
    private = ingress(resources, "staff-control-center")
    api = ingress(resources, "staff-control-center-api")
    annotation = "traefik.ingress.kubernetes.io/"
    assert not public["metadata"]["annotations"].get(annotation + "router.middlewares")
    assert public["metadata"]["annotations"][annotation + "router.priority"] == "300"
    assert api["metadata"]["annotations"][annotation + "router.priority"] == "200"
    for item in (public, private, api):
        assert item["metadata"]["annotations"][annotation + "router.tls"] == "true"
        assert item["metadata"]["annotations"][annotation + "router.entrypoints"] == "websecure"
        assert item["spec"]["tls"] == private["spec"]["tls"]
        assert item["spec"]["ingressClassName"] == private["spec"]["ingressClassName"]
        assert item["spec"]["rules"][0]["host"] == private["spec"]["rules"][0]["host"]
    assert "oauth2-control-center-chain@kubernetescrd" in private["metadata"]["annotations"][annotation + "router.middlewares"]
    assert api["metadata"]["annotations"][annotation + "router.middlewares"] == "kodex-system-oauth2-control-center-auth@kubernetescrd"
    paths = public["spec"]["rules"][0]["http"]["paths"]
    assert len(paths) == len(ASSETS) and {p["path"] for p in paths} == set(ASSETS)
    for p in paths:
        assert p["pathType"] == "Exact"
        assert p["backend"]["service"] == {"name": "staff-control-center", "port": {"name": "http" if hot else "https"}}
    assert private["spec"]["rules"][0]["http"]["paths"][0]["path"] == "/"
    assert api["spec"]["rules"][0]["http"]["paths"][0]["path"] == "/api/v1"
    if not hot:
        service = next(r for r in resources if r.get("kind") == "Service" and r["metadata"]["name"] == "staff-control-center")
        assert service["metadata"]["annotations"][annotation + "service.serverstransport"] == "kodex-system-staff-control-center@kubernetescrd"
        transport = next(r["spec"] for r in resources if r.get("kind") == "ServersTransport" and r["metadata"]["name"] == "staff-control-center")
        assert transport == {
            "serverName": "staff-control-center.kodex-system.svc.cluster.local",
            "insecureSkipVerify": False,
            "rootCAsSecrets": ["staff-control-center-backend-tls"],
            "certificatesSecrets": ["staff-control-center-ingress-client-tls"],
        }
    return public, private, api


def references():
    manifest = json.loads((PUBLIC / "manifest.webmanifest").read_text())
    assert {icon["src"] for icon in manifest["icons"]} <= set(ASSETS)
    html = (PUBLIC.parent / "index.html").read_text()
    for tag in re.findall(r"<link\b[^>]+>", html):
        if re.search(r'rel="(?:manifest|icon|apple-touch-icon)"', tag):
            assert re.search(r'href="([^"]+)"', tag).group(1) in ASSETS
    main = (PUBLIC.parent / "src/main.ts").read_text()
    assert '.register("/sw.js", { scope: "/", updateViaCache: "none" })' in main
    worker = (PUBLIC / "sw.js").read_text()
    assert "importScripts" not in worker and "cache.put" not in worker and "cache.add" not in worker
    assert 'fetch(event.request, { cache: "no-store" })' in worker


def certificate(directory, name, extension):
    run("openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes", "-subj", f"/CN={name}", "-keyout", str(directory / f"{name}.key"), "-out", str(directory / f"{name}.csr"))
    ext = directory / f"{name}.ext"
    ext.write_text(extension)
    run("openssl", "x509", "-req", "-in", str(directory / f"{name}.csr"), "-CA", str(directory / "ca.crt"), "-CAkey", str(directory / "ca.key"), "-CAcreateserial", "-days", "1", "-extfile", str(ext), "-out", str(directory / f"{name}.crt"))


def http_fixture(directory, resources):
    """Настоящий Traefik router/forwardAuth и production nginx; auth только fixture."""
    nginx = re.findall(r"^FROM (docker.io/library/nginx:[^\s]+)$", (PUBLIC.parent / "Dockerfile").read_text(), re.M)[0]
    for image in (nginx, TRAEFIK):
        run("docker", "image", "inspect", image)
    network = "kodex-pwa-fixture-" + uuid.uuid4().hex[:12]
    names = [network + "-nginx", network + "-traefik"]
    tls = directory / "tls"
    tls.mkdir()
    run("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=Kodex disposable PWA fixture CA", "-keyout", str(tls / "ca.key"), "-out", str(tls / "ca.crt"))
    certificate(tls, "server", "subjectAltName=DNS:staff-control-center.kodex-system.svc.cluster.local,DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n")
    certificate(tls, "client", "extendedKeyUsage=clientAuth\n")
    config = directory / "runtime"
    config.mkdir()
    for f in ("security-headers.conf", "public-assets.conf"):
        (config / f).write_text((BASE / f).read_text().replace("__KODEX_OIDC_ORIGIN__", "https://oidc.invalid"))
    (config / "runtime-config.json").write_text('{"fixture":true}')
    (config / "readiness.json").write_text('{"ready":true}')
    assets = directory / "assets"
    assets.mkdir()
    for asset in ASSETS:
        shutil.copyfile(PUBLIC / asset[1:], assets / asset[1:])
    (assets / "index.html").write_text("<html>Private fixture shell</html>")
    # Внешний OIDC/provider не вызывается. Только контракт /auth 202/401.
    (directory / "auth.conf").write_text('''server {
      listen 8081;
      location = /oauth2/auth {
        if ($http_x_fixture_session != "granted") { return 401; }
        return 202;
      }
      location = /oauth2/sign_in { return 302 /oauth2/start; }
      location / { return 200 '{"fixture":true}'; }
    }
    ''')
    # Один worker соответствует CPU budget fixture; production routes неизменны.
    nginx_config = (PUBLIC.parent / "nginx.conf").read_text()
    nginx_config = nginx_config.replace("worker_processes auto;", "worker_processes 1;", 1)
    nginx_config = nginx_config.replace("http {", "http {\n  include /fixture/auth.conf;", 1)
    (directory / "nginx.conf").write_text(nginx_config)
    routes = {}
    annotation = "traefik.ingress.kubernetes.io/"
    for resource in verify(resources):
        name = resource["metadata"]["name"]
        annotations = resource["metadata"]["annotations"]
        for index, path in enumerate(resource["spec"]["rules"][0]["http"]["paths"]):
            matcher = "Path" if path["pathType"] == "Exact" else "PathPrefix"
            middlewares = annotations.get(annotation + "router.middlewares", "")
            middleware_names = ["errors", "auth"] if "-chain@" in middlewares else ["auth"] if "-auth@" in middlewares else []
            match = f"{matcher}(`{path['path']}`)"
            if path["pathType"] == "Prefix" and path["path"] != "/":
                match = f"(Path(`{path['path']}`) || PathPrefix(`{path['path']}/`))"
            routes[f"{name}-{index}"] = {"rule": f"Host(`localhost`) && {match}", "priority": int(annotations.get(annotation + "router.priority", "1")), "entryPoints": ["websecure"], "tls": {}, "service": "api" if name.endswith("-api") else "pwa", "middlewares": middleware_names}
    # Те же auth/errors настройки из настоящего management source; меняется только fixture адрес.
    management = documents(ROOT / "infra/management-surfaces/routes.yaml")
    auth = next(r["spec"] for r in management if r.get("kind") == "Middleware" and r["metadata"]["name"] == "oauth2-control-center-auth")
    auth["forwardAuth"]["address"] = "http://backend:8081/oauth2/auth"
    auth["forwardAuth"].pop("tls", None)
    errors = next(r["spec"] for r in management if r.get("kind") == "Middleware" and r["metadata"]["name"] == "oauth2-control-center-errors")
    errors["errors"]["service"] = "auth"
    dynamic = {"http": {"routers": routes, "middlewares": {"auth": auth, "errors": errors}, "services": {"pwa": {"loadBalancer": {"servers": [{"url": "https://backend:8443"}], "serversTransport": "backend"}}, "api": {"loadBalancer": {"servers": [{"url": "http://backend:8081"}]}}, "auth": {"loadBalancer": {"servers": [{"url": "http://backend:8081"}]}}}, "serversTransports": {"backend": {"serverName": "staff-control-center.kodex-system.svc.cluster.local", "rootCAs": ["/fixture/tls/ca.crt"], "certificates": [{"certFile": "/fixture/tls/client.crt", "keyFile": "/fixture/tls/client.key"}]}}}, "tls": {"certificates": [{"certFile": "/fixture/tls/server.crt", "keyFile": "/fixture/tls/server.key"}]}}
    (directory / "dynamic.yaml").write_text(json.dumps(dynamic))
    for f in directory.rglob("*"):
        f.chmod(0o755 if f.is_dir() else 0o444)
    directory.chmod(0o755)
    run("docker", "network", "create", "--internal", network)
    common = ["--detach", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "64", "--memory", "128m", "--cpus", "1", "--network", network]
    mount = lambda source, target: ["--mount", f"type=bind,src={source},dst={target},readonly"]
    try:
        args = ["docker", "run", *common, "--name", names[0], "--network-alias", "backend", "--user", "101:101", "--entrypoint", "nginx", "--add-host", "control-api-gateway.kodex-system.svc:127.0.0.1", "--tmpfs", "/var/cache/nginx:uid=101,gid=101", "--tmpfs", "/var/run/nginx:uid=101,gid=101", "--tmpfs", "/tmp:uid=101,gid=101", *mount(directory, "/fixture"), *mount(directory / "nginx.conf", "/etc/nginx/nginx.conf"), *mount(assets, "/usr/share/nginx/html"), *mount(config, "/var/run/config/kodex/staff-control-center/runtime")]
        for source, destination in [("server.crt", "backend-tls/tls.crt"), ("server.key", "backend-tls/tls.key"), ("ca.crt", "ingress-client-tls/ca.crt")]:
            args += mount(tls / source, "/var/run/secrets/kodex/staff-control-center/" + destination)
        args += mount(tls / "ca.crt", "/var/run/config/kodex/staff-control-center/control-api-ca/ca.crt")
        run(*args, nginx, "-g", "daemon off;")
        run("docker", "run", *common, "--name", names[1], "--network-alias", "frontend", "--user", "65532:65532", *mount(directory, "/fixture"), TRAEFIK, "--entrypoints.websecure.address=:8443", "--providers.file.filename=/fixture/dynamic.yaml", "--log.level=ERROR")

        def request(path, authenticated=False, method="GET"):
            # Клиент тоже внутри internal network: host port и внешний egress не нужны.
            command = ["docker", "exec", names[0], "curl", "--silent", "--show-error",
                       "--max-time", "5", "--http1.1", "--path-as-is", "--noproxy", "*",
                       "--cacert", "/fixture/tls/ca.crt", "--connect-to", "localhost:8443:frontend:8443",
                       "--header", "Host: localhost", "--header", "Origin: https://untrusted.invalid",
                       "--dump-header", "-"]
            if authenticated:
                command += ["--header", "X-Fixture-Session: granted"]
            command += ["--head"] if method == "HEAD" else ["--request", method]
            command += ["https://localhost:8443" + path]
            result = subprocess.run(command, capture_output=True, timeout=8)
            if result.returncode:
                raise RuntimeError("fixture HTTPS request failed")
            header, _, body = result.stdout.partition(b"\r\n\r\n")
            lines = header.decode("ascii").split("\r\n")
            headers = dict(line.split(": ", 1) for line in lines[1:] if ": " in line)
            return int(lines[0].split()[1]), headers, body

        deadline = time.monotonic() + 20
        while True:
            try:
                if request("/manifest.webmanifest")[0] == 200:
                    break
            except (OSError, RuntimeError):
                pass
            if time.monotonic() >= deadline:
                raise RuntimeError("fixture startup did not become ready")
            time.sleep(0.2)
        count = 0
        for authenticated in (False, True):
            for path, content_type in ASSETS.items():
                for suffix in ("", "?revision=fixture"):
                    status, headers, body = request(path + suffix, authenticated)
                    assert status == 200 and "Location" not in headers, path
                    assert headers["Content-Type"].split(";")[0] == content_type, path
                    assert "Access-Control-Allow-Origin" not in headers, path
                    assert hashlib.sha256(body).digest() == hashlib.sha256((PUBLIC / path[1:]).read_bytes()).digest(), path
                    if path == "/sw.js":
                        assert "no-store" in headers["Cache-Control"]
                    count += 1
                assert request(path, authenticated, "HEAD")[0] == 200
                count += 1
                for method in ("POST", "OPTIONS"):
                    status, headers, _ = request(path, authenticated, method)
                    assert status == 405 and "Access-Control-Allow-Origin" not in headers, path
                    count += 1
        for path in ("/", "/projects", "/config/runtime-config.json", "/assets/private.js", "/manifest.webmanifest/private", "/manifest.webmanifest/", "/Manifest.webmanifest", "/logo.png/private", "/sw.js.map", "/sw.js/", "/api/v1/bootstrap", "/api/v1/manifest.webmanifest", "/api/v10"):
            status, _, _ = request(path)
            assert status == (401 if path.startswith("/api/v1/") else 302), path
            count += 1
        for path in ("/", "/config/runtime-config.json", "/api/v1/bootstrap"):
            assert request(path, True)[0] == 200, path
            count += 1
        for path in ASSETS:
            present = assets / path[1:]
            hidden = assets / (path[1:] + ".fixture-missing")
            present.rename(hidden)
            try:
                assert request(path)[0] == 404, path
            finally:
                hidden.rename(present)
            count += 1
        print(f"PWA public assets HTTP fixture PASS: {count} requests; real Traefik/nginx, synthetic auth, isolated network")
    except Exception:
        # Только журналы созданных оснасткой контейнеров: живых данных здесь нет.
        for name in names:
            result = subprocess.run(["docker", "logs", "--tail", "12", name], capture_output=True, text=True, timeout=10)
            for line in (result.stdout + result.stderr).splitlines():
                if "error" in line.lower() or "emerg" in line.lower():
                    print("Fixture diagnostic: " + line[:1000])
        raise
    finally:
        subprocess.run(["docker", "rm", "--force", *names], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=20)
        subprocess.run(["docker", "network", "rm", network], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=20)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--http", action="store_true", help="Run isolated Docker HTTP fixture; pinned images must exist")
    parser.add_argument("--render", type=pathlib.Path, help="Verify final public-acme hot-reload render")
    args = parser.parse_args()
    references()
    with tempfile.TemporaryDirectory(prefix="kodex-pwa-public-") as temp:
        directory = pathlib.Path(temp)
        resources = None
        for profile in ("web-only", "web-with-mattermost"):
            rendered = directory / (profile + ".yaml")
            rendered.write_text(run("kubectl", "kustomize", str(ROOT / "deploy/k8s/profiles" / profile)))
            resources = documents(rendered)
            verify(resources)
            print(f"PWA public assets render PASS: {profile}")
        if args.render:
            verify(documents(args.render), hot=True)
            print("PWA public assets render PASS: public-acme hot reload")
        if args.http:
            http_fixture(directory, resources)


if __name__ == "__main__":
    main()
