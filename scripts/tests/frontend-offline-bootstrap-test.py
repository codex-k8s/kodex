#!/usr/bin/env python3
"""Реальный prime и offline Vite с read-only исходниками и runtime config."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
FRONTEND = ROOT / "services/staff/control-center"


def run(*args, **kwargs):
    result = subprocess.run(args, text=True, capture_output=True, **kwargs)
    if result.returncode:
        raise AssertionError(result.stderr)
    return result


def main():
    # Те же пустые mountpoints, которые renderer создаёт перед read-only bind.
    (FRONTEND / "node_modules").mkdir(exist_ok=True)
    (FRONTEND / "public/config").mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="kodex-frontend-") as temporary:
        work = Path(temporary)
        cache = Path(os.environ.get("KODEX_FRONTEND_TEST_CACHE", work / "cache"))
        prime = ["bash", str(ROOT / "tools/dev/prime-frontend-cache.sh"), str(ROOT), str(cache)]
        modules = run(*prime, timeout=600).stdout.strip()
        assert run(*prime, timeout=60).stdout.strip() == modules
        image = next(line.split()[1] for line in (FRONTEND / "Dockerfile").read_text().splitlines()
                     if line.startswith("FROM docker.io/library/node:"))
        common = ["docker", "run", "--read-only", "--network", "none", "--cap-drop", "ALL",
                  "--security-opt", "no-new-privileges", "--user", "0:0",
                  "--tmpfs", "/tmp:rw,nosuid,nodev,mode=1777", "-e", "HOME=/tmp",
                  "-e", f"KODEX_DEV_NODE_IMAGE={image}", "-e", "KODEX_DEV_CACHE_DIR=/tmp/vite-cache",
                  "-e", "KODEX_DEV_PUBLIC_HOST=kodex.invalid",
                  "-e", "KODEX_DEV_API_TARGET=https://control-api-gateway.kodex-system.svc:8443",
                  "-v", f"{FRONTEND}:/workspace/services/staff/control-center:ro",
                  "-v", f"{modules}:/workspace/services/staff/control-center/node_modules:ro",
                  "-v", f"{ROOT / 'tools/dev'}:/workspace/tools/dev:ro",
                  "-v", f"{ROOT / 'deploy/k8s/base/staff-control-center'}:/workspace/services/staff/control-center/public/config:ro",
                  "-w", "/workspace/services/staff/control-center"]
        container = run(*common, "--rm", "-d", image, "sh", "/workspace/tools/dev/run-frontend.sh").stdout.strip()
        try:
            for attempt in range(60):
                result = subprocess.run(["docker", "exec", container, "node", "-e",
                    'fetch("http://127.0.0.1:8080/src/main.ts").then(r=>{if(r.status!==200)process.exit(1)}).catch(()=>process.exit(1))'],
                    capture_output=True, timeout=10)
                if result.returncode == 0:
                    break
                time.sleep(1)
            else:
                raise AssertionError("offline Vite startup failed: " + run("docker", "logs", container).stdout)
            run("docker", "exec", container, "node", "-e", '''
                const fs = require("node:fs");
                (async () => {
                  for (const path of ["/", "/src/main.ts", "/manifest.webmanifest", "/sw.js", "/config/runtime-config.json"]) {
                    const r = await fetch("http://127.0.0.1:8080" + path);
                    if (r.status !== 200) throw Error("HTTP readback failed: " + path);
                    if (path.endsWith("runtime-config.json")) {
                      const expected = JSON.parse(fs.readFileSync("public/config/runtime-config.json"));
                      if (JSON.stringify(await r.json()) !== JSON.stringify(expected)) throw Error("runtime config mismatch");
                    }
                  }
                  for (const path of ["package.json", "node_modules/.kodex-cache-identity", "public/config/runtime-config.json"]) {
                    try { fs.accessSync(path, fs.constants.W_OK); throw Error("writable input: " + path); }
                    catch (error) { if (error.code !== "EROFS" && error.code !== "EACCES") throw error; }
                  }
                })().catch(error => { console.error(error.message); process.exit(1); });
            ''', timeout=30)
        finally:
            run("docker", "rm", "-f", container)
        # Изменённый lock, package и ABI receipt не должны запускать установку или Vite.
        for filename in ["package-lock.json", "package.json"]:
            changed = work / filename
            changed.write_text((FRONTEND / filename).read_text() + "\n")
            result = subprocess.run(common + ["--rm", "-v", f"{changed}:/workspace/services/staff/control-center/{filename}:ro",
                image, "sh", "/workspace/tools/dev/run-frontend.sh"], capture_output=True, text=True, timeout=20)
            assert result.returncode != 0 and "absent or stale" in result.stderr
        result = subprocess.run(common + ["--rm", "-e", "KODEX_DEV_NODE_IMAGE=wrong-abi",
            image, "sh", "/workspace/tools/dev/run-frontend.sh"], capture_output=True, text=True, timeout=20)
        assert result.returncode != 0 and "absent or stale" in result.stderr
        empty_modules = work / "empty-modules"
        empty_modules.mkdir()
        missing = [value.replace(f"{modules}:", f"{empty_modules}:") for value in common]
        result = subprocess.run(missing + ["--rm", image, "sh", "/workspace/tools/dev/run-frontend.sh"],
                                capture_output=True, text=True, timeout=20)
        assert result.returncode != 0 and "absent or stale" in result.stderr
        broken = work / "broken"
        (broken / "services/staff/control-center").mkdir(parents=True)
        shutil.copytree(ROOT / "tools/dev", broken / "tools/dev")
        for name in ["package.json", "Dockerfile"]:
            shutil.copy(FRONTEND / name, broken / "services/staff/control-center" / name)
        (broken / "services/staff/control-center/package-lock.json").write_text("invalid JSON\n")
        failed_cache = work / "failed-cache"
        result = subprocess.run(["bash", str(ROOT / "tools/dev/prime-frontend-cache.sh"), str(broken), str(failed_cache)],
                                capture_output=True, text=True, timeout=60)
        assert result.returncode != 0
        assert not list(failed_cache.rglob(".kodex-cache-identity"))
        assert not [path for path in failed_cache.glob("frontend-v1/.prime.*") if path.is_dir()]
        (broken / "services/staff/control-center/package-lock.json").write_text(
            (FRONTEND / "package-lock.json").read_text() + "\n")
        refreshed = run("bash", str(ROOT / "tools/dev/prime-frontend-cache.sh"), str(broken), str(failed_cache), timeout=600).stdout.strip()
        assert Path(refreshed).parent.name != Path(modules).parent.name
        assert (Path(refreshed) / ".kodex-cache-identity").read_text().strip() == Path(refreshed).parent.name
        print("PASS: trusted prime/reuse/reprime after lock change, offline Vite, read-only source/dependencies/config, HTTP/config readback, absent cache, stale lock/package/ABI, failed install cleanup")


if __name__ == "__main__":
    main()
