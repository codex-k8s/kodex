#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
python3 -B "$repository_root/scripts/tests/provider-bootstrap-test.py"
python3 - "$repository_root" <<'PY'
from pathlib import Path
import sys
root = Path(sys.argv[1])
assert not (root / "tools/dev/provider-account.sh").exists()
dev = (root / "dev.sh").read_text()
installer = (root / "tools/install/materialize-secrets.sh").read_text()
sql = (root / "tools/dev/reconcile-provider-account.sql").read_text()
assert 'ON CONFLICT (organization_id, stable_key)' not in sql
assert "account.ref = :'account_ref'" in sql
assert "installation_owner.stable_key = 'installation-owner'" in sql
assert "expected_version" in sql and "expected_credential_ref" in sql
assert "FOR UPDATE OF account" in sql
assert "provider-bootstrap.py\" verify-metadata" in installer
assert "restore_selected_provider_metadata_from_auth" not in installer
assert 'provider-bootstrap.py" seed' in installer
assert "runtime-provider-openai-default-r1" not in installer
assert "preserve_selected_provider_metadata" not in installer
assert 'provider_mode=deferred' in dev
assert "protected profile requires KODEX_MODEL_PROVIDER_API_KEY_FILE" in dev
assert '--security-profile "$security_profile" --provider-mode "$provider_mode"' in dev
assert 'model-provider.py" prepare-auth' in dev
assert '--api-key-file "$KODEX_MODEL_PROVIDER_API_KEY_FILE"' in dev
assert '--model-provider-configuration "$KODEX_MODEL_PROVIDER_CONFIGURATION_FILE"' in dev
assert 'codex login' not in dev and 'provider_metadata=(' not in dev
assert dev.index('retired CLI bootstrap is unsupported') < dev.index('kubectl get --raw=/readyz')
for path in ("tools/dev/deploy-local.sh", "tools/install/deploy-platform.sh"):
    source = (root / path).read_text()
    import_at = source.index('provider-bootstrap.py" recover')
    cp_at = source.rindex("rollout status deployment/control-plane", 0, import_at)
    wait_at = source.index("wait_warm_runtime\n" if "deploy-local" in path else "wait_system_assistant\n", import_at)
    assert cp_at < import_at < wait_at
print("Kodex local provider account persistence contract test passed")
PY
