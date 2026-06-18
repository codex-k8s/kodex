#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"
TIMEOUT_SECONDS=900
KEEP=false
KEEP_DOMAIN=false
KEEP_RUNTIME=false

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      ENV_FILE="$2"
      shift 2
      ;;
    --timeout-seconds)
      TIMEOUT_SECONDS="$2"
      shift 2
      ;;
    --keep)
      KEEP=true
      shift
      ;;
    --keep-domain)
      KEEP_DOMAIN=true
      shift
      ;;
    --keep-runtime)
      KEEP_RUNTIME=true
      shift
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

mattercodex_load_env_file "$ENV_FILE"
mattercodex_set_defaults
mattercodex_validate_base_env
mattercodex_require_commands ssh base64

RUN_SUFFIX="$(date +%Y%m%d%H%M%S)"
E2E_PREFIX="mc-e2e-owner-$RUN_SUFFIX"
E2E_USERNAME="mc-e2e-$RUN_SUFFIX"
E2E_EMAIL="$E2E_USERNAME@local.invalid"
set +o pipefail
E2E_PASSWORD="$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32)"
set -o pipefail
[ -n "$E2E_PASSWORD" ] || mattercodex_die "не удалось сгенерировать e2e пароль"
PROJECT_SLUG="e2e-owner-$RUN_SUFFIX"
PROJECT_NAME="E2E Owner MVP $RUN_SUFFIX"
MANAGER_ROLE_NAME="e2e-manager-$RUN_SUFFIX"
WORKER_ROLE_NAME="e2e-worker-$RUN_SUFFIX"
REVIEWER_ROLE_NAME="e2e-reviewer-$RUN_SUFFIX"
BROKEN_ROLE_NAME="e2e-broken-$RUN_SUFFIX"
MANAGER_CHAT_NAME="e2e-manager-chat-$RUN_SUFFIX"
MANAGER_CHAT_SLUG="e2e-manager-chat-$RUN_SUFFIX"
WORKER_CHAT_NAME="e2e-worker-review-chat-$RUN_SUFFIX"
WORKER_CHAT_SLUG="e2e-worker-review-chat-$RUN_SUFFIX"
BROKEN_CHAT_NAME="e2e-broken-chat-$RUN_SUFFIX"
BROKEN_CHAT_SLUG="e2e-broken-chat-$RUN_SUFFIX"
E2E_MARKER="matter-codex e2e ok $RUN_SUFFIX"
E2E_REPO_OWNER="${MATTERCODEX_E2E_REPO_OWNER:-codex-k8s}"
E2E_REPO_NAME="${MATTERCODEX_E2E_REPO_NAME:-matter-codex-e2e-sandbox}"
E2E_GITHUB_ACCOUNT="e2e-gh-$RUN_SUFFIX"
E2E_OPENAI_COPY_ACCOUNT="e2e-openai-$RUN_SUFFIX"
E2E_OPENAI_PENDING_ACCOUNT="e2e-auth-$RUN_SUFFIX"
E2E_CODEX_MODEL="${MATTERCODEX_E2E_CODEX_MODEL:-gpt-5.3-codex-spark}"
E2E_PRUNE_ACTIVE_RUN_ID="prune-active-$RUN_SUFFIX"
E2E_PRUNE_OLD_RUN_ID="prune-old-$RUN_SUFFIX"
MATTERMOST_ADMIN_TOKEN=""

NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
BOT_SECRET_Q="$(mattercodex_shell_quote "$MATTERCODEX_BOT_SERVICE_SECRET")"
POSTGRES_SECRET_Q="$(mattercodex_shell_quote "$MATTERCODEX_POSTGRES_SECRET")"
POSTGRES_DB_Q="$(mattercodex_shell_quote "$MATTERCODEX_POSTGRES_DB")"
POSTGRES_USER_Q="$(mattercodex_shell_quote "$MATTERCODEX_POSTGRES_USER")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

TEMP_FILES=()

cleanup() {
	local file
	for file in "${TEMP_FILES[@]}"; do
		rm -f "$file"
	done
	if ! mattercodex_bool "$KEEP"; then
    mattercodex_ssh "set -eu
      $REMOTE_KUBECTL -n $NAMESPACE_Q delete pod,configmap,secret -l app.kubernetes.io/name=matter-codex-e2e,app.kubernetes.io/instance=$E2E_PREFIX --ignore-not-found >/dev/null 2>&1 || true
      $REMOTE_KUBECTL -n $NAMESPACE_Q delete job,pvc,configmap -l matter-codex.dev/e2e-instance=$E2E_PREFIX --ignore-not-found >/dev/null 2>&1 || true
      $REMOTE_KUBECTL -n $NAMESPACE_Q delete secret \"matter-codex-codex-auth-$E2E_OPENAI_COPY_ACCOUNT\" \"matter-codex-codex-auth-$E2E_OPENAI_PENDING_ACCOUNT\" \"matter-codex-github-$E2E_GITHUB_ACCOUNT\" --ignore-not-found >/dev/null 2>&1 || true
      $REMOTE_KUBECTL -n $NAMESPACE_Q delete job \"mc-codex-auth-$E2E_OPENAI_PENDING_ACCOUNT\" --ignore-not-found >/dev/null 2>&1 || true
    " </dev/null || true
    remote_psql "delete from matter_codex_openai_accounts where name in ('$E2E_OPENAI_COPY_ACCOUNT', '$E2E_OPENAI_PENDING_ACCOUNT'); delete from matter_codex_github_accounts where name = '$E2E_GITHUB_ACCOUNT'; delete from matter_codex_credentials where name in ('openai:$E2E_OPENAI_COPY_ACCOUNT', 'openai:$E2E_OPENAI_PENDING_ACCOUNT', 'github:$E2E_GITHUB_ACCOUNT');" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT

temp_file() {
  local path
  path="$(mktemp)"
  TEMP_FILES+=("$path")
  printf '%s\n' "$path"
}

yaml_quote() {
	printf '"%s"' "$(printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g')"
}

apply_remote_manifest() {
	mattercodex_remote_kubectl_apply_stdin "none"
}

remote_psql() {
  local sql="$1"
  local sql_q
  sql_q="$(mattercodex_shell_quote "$sql")"
  mattercodex_ssh "set -eu
    PASS=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get secret $POSTGRES_SECRET_Q -o jsonpath='{.data.postgres-password}' | base64 -d)\"
    $REMOTE_KUBECTL -n $NAMESPACE_Q exec mattermost-postgres-0 -- env PGPASSWORD=\"\$PASS\" psql -U $POSTGRES_USER_Q -d $POSTGRES_DB_Q -Atc $sql_q
	" </dev/null
}

prepare_openai_copy_account() {
  local account="$1"
  local secret_name="matter-codex-codex-auth-$account"
  local account_q
  local secret_q
  account_q="$(mattercodex_shell_quote "$account")"
  secret_q="$(mattercodex_shell_quote "$secret_name")"
  mattercodex_ssh "set -eu
    AUTH_DATA=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get secret matter-codex-codex-auth-main -o jsonpath='{.data.auth\\.json}')\"
    cat <<YAML | $REMOTE_KUBECTL -n $NAMESPACE_Q apply -f - >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: $secret_name
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: codex-auth-secret
    matter-codex.dev/openai-account: $account
type: Opaque
data:
  auth.json: \${AUTH_DATA}
YAML
  " </dev/null
  remote_psql "with credential as (
    insert into matter_codex_credentials(name, credential_type, provider, secret_ref, status)
    values ('openai:' || $account_q, 'codex_auth', 'openai', $secret_q, 'authorized')
    on conflict (name) do update set secret_ref = excluded.secret_ref, status = excluded.status, updated_at = now()
    returning id
  )
  insert into matter_codex_openai_accounts(name, credential_id, status)
  values ($account_q, (select id from credential), 'authorized')
  on conflict (name) do update set credential_id = excluded.credential_id, status = excluded.status, updated_at = now();" >/dev/null
}

prepare_retention_fixture() {
  local active_run="$1"
  local old_run="$2"
  mattercodex_remote_kubectl_apply_stdin "none" <<YAML >/dev/null
apiVersion: batch/v1
kind: Job
metadata:
  name: mc-run-$active_run
  namespace: $(yaml_quote "$MATTERCODEX_NAMESPACE")
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: agent-run
    matter-codex.dev/run-id: $active_run
    matter-codex.dev/agent-role: e2e
    matter-codex.dev/e2e-instance: $E2E_PREFIX
spec:
  backoffLimit: 0
  template:
    metadata:
      labels:
        app.kubernetes.io/name: matter-codex-agent-runner
        app.kubernetes.io/component: agent-run
        matter-codex.dev/run-id: $active_run
        matter-codex.dev/agent-role: e2e
        matter-codex.dev/e2e-instance: $E2E_PREFIX
    spec:
      restartPolicy: Never
      containers:
        - name: sleep
          image: busybox:1.36
          command: ["sh", "-c", "sleep 600"]
---
apiVersion: batch/v1
kind: Job
metadata:
  name: mc-run-$old_run
  namespace: $(yaml_quote "$MATTERCODEX_NAMESPACE")
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: agent-run
    matter-codex.dev/run-id: $old_run
    matter-codex.dev/agent-role: e2e
    matter-codex.dev/e2e-instance: $E2E_PREFIX
spec:
  backoffLimit: 0
  template:
    metadata:
      labels:
        app.kubernetes.io/name: matter-codex-agent-runner
        app.kubernetes.io/component: agent-run
        matter-codex.dev/run-id: $old_run
        matter-codex.dev/agent-role: e2e
        matter-codex.dev/e2e-instance: $E2E_PREFIX
    spec:
      restartPolicy: Never
      containers:
        - name: done
          image: busybox:1.36
          command: ["true"]
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mc-ws-$old_run
  namespace: $(yaml_quote "$MATTERCODEX_NAMESPACE")
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: agent-run
    matter-codex.dev/run-id: $old_run
    matter-codex.dev/agent-role: e2e
    matter-codex.dev/e2e-instance: $E2E_PREFIX
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 1Gi
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mc-prompt-$old_run
  namespace: $(yaml_quote "$MATTERCODEX_NAMESPACE")
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: agent-run
    matter-codex.dev/run-id: $old_run
    matter-codex.dev/agent-role: e2e
    matter-codex.dev/e2e-instance: $E2E_PREFIX
data:
  prompt.md: "e2e retention fixture"
YAML
  local active_job_q
  local old_job_q
  active_job_q="$(mattercodex_shell_quote "mc-run-$active_run")"
  old_job_q="$(mattercodex_shell_quote "mc-run-$old_run")"
  mattercodex_ssh "set -eu
    for _ in \$(seq 1 60); do
      succeeded=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get job $old_job_q -o jsonpath='{.status.succeeded}' 2>/dev/null || true)\"
      active=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get job $active_job_q -o jsonpath='{.status.active}' 2>/dev/null || true)\"
      if [ \"\$succeeded\" = \"1\" ] && [ \"\$active\" = \"1\" ]; then
        break
      fi
      sleep 2
    done
    test \"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get job $old_job_q -o jsonpath='{.status.succeeded}')\" = \"1\"
    test \"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get job $active_job_q -o jsonpath='{.status.active}')\" = \"1\"
  " </dev/null
}

render_configmap_manifest() {
  local name="$1"
  local script_file="$2"
  cat <<YAML
apiVersion: v1
kind: ConfigMap
metadata:
  name: $name
  namespace: $(yaml_quote "$MATTERCODEX_NAMESPACE")
  labels:
    app.kubernetes.io/name: matter-codex-e2e
    app.kubernetes.io/instance: $E2E_PREFIX
data:
  run.sh: |
YAML
  sed 's/^/    /' "$script_file"
}

render_secret_manifest() {
  cat <<YAML
apiVersion: v1
kind: Secret
metadata:
  name: $E2E_PREFIX-secret
  namespace: $(yaml_quote "$MATTERCODEX_NAMESPACE")
  labels:
    app.kubernetes.io/name: matter-codex-e2e
    app.kubernetes.io/instance: $E2E_PREFIX
type: Opaque
stringData:
  password: $(yaml_quote "$E2E_PASSWORD")
  admin-token: $(yaml_quote "$MATTERMOST_ADMIN_TOKEN")
YAML
}

render_pod_manifest() {
  local pod_name="$1"
  local configmap_name="$2"
  cat <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
  namespace: $(yaml_quote "$MATTERCODEX_NAMESPACE")
  labels:
    app.kubernetes.io/name: matter-codex-e2e
    app.kubernetes.io/instance: $E2E_PREFIX
spec:
  restartPolicy: Never
  containers:
    - name: curl
      image: curlimages/curl:8.10.1
      command: ["sh", "/e2e/run.sh"]
      env:
        - name: MM_URL
          value: $(yaml_quote "$MATTERCODEX_MATTERMOST_INTERNAL_URL")
        - name: BOT_URL
          value: $(yaml_quote "$MATTERCODEX_BOT_SERVICE_INTERNAL_URL")
        - name: E2E_USERNAME
          value: $(yaml_quote "$E2E_USERNAME")
        - name: E2E_EMAIL
          value: $(yaml_quote "$E2E_EMAIL")
        - name: E2E_PASSWORD
          valueFrom:
            secretKeyRef:
              name: $E2E_PREFIX-secret
              key: password
        - name: E2E_PROJECT_NAME
          value: $(yaml_quote "$PROJECT_NAME")
        - name: E2E_PROJECT_SLUG
          value: $(yaml_quote "$PROJECT_SLUG")
        - name: E2E_MANAGER_ROLE_NAME
          value: $(yaml_quote "$MANAGER_ROLE_NAME")
        - name: E2E_WORKER_ROLE_NAME
          value: $(yaml_quote "$WORKER_ROLE_NAME")
        - name: E2E_REVIEWER_ROLE_NAME
          value: $(yaml_quote "$REVIEWER_ROLE_NAME")
        - name: E2E_BROKEN_ROLE_NAME
          value: $(yaml_quote "$BROKEN_ROLE_NAME")
        - name: E2E_MANAGER_CHAT_NAME
          value: $(yaml_quote "$MANAGER_CHAT_NAME")
        - name: E2E_MANAGER_CHAT_SLUG
          value: $(yaml_quote "$MANAGER_CHAT_SLUG")
        - name: E2E_WORKER_CHAT_NAME
          value: $(yaml_quote "$WORKER_CHAT_NAME")
        - name: E2E_WORKER_CHAT_SLUG
          value: $(yaml_quote "$WORKER_CHAT_SLUG")
        - name: E2E_BROKEN_CHAT_NAME
          value: $(yaml_quote "$BROKEN_CHAT_NAME")
        - name: E2E_BROKEN_CHAT_SLUG
          value: $(yaml_quote "$BROKEN_CHAT_SLUG")
        - name: E2E_MARKER
          value: $(yaml_quote "$E2E_MARKER")
        - name: E2E_REPO_OWNER
          value: $(yaml_quote "$E2E_REPO_OWNER")
        - name: E2E_REPO_NAME
          value: $(yaml_quote "$E2E_REPO_NAME")
        - name: E2E_GITHUB_ACCOUNT
          value: $(yaml_quote "$E2E_GITHUB_ACCOUNT")
        - name: E2E_OPENAI_COPY_ACCOUNT
          value: $(yaml_quote "$E2E_OPENAI_COPY_ACCOUNT")
        - name: E2E_OPENAI_PENDING_ACCOUNT
          value: $(yaml_quote "$E2E_OPENAI_PENDING_ACCOUNT")
        - name: E2E_CODEX_MODEL
          value: $(yaml_quote "$E2E_CODEX_MODEL")
        - name: E2E_PRUNE_ACTIVE_RUN_ID
          value: $(yaml_quote "$E2E_PRUNE_ACTIVE_RUN_ID")
        - name: E2E_PRUNE_OLD_RUN_ID
          value: $(yaml_quote "$E2E_PRUNE_OLD_RUN_ID")
        - name: E2E_KEEP_DOMAIN
          value: $(yaml_quote "$KEEP_DOMAIN")
        - name: MM_BOT_TOKEN
          valueFrom:
            secretKeyRef:
              name: $BOT_SECRET_Q
              key: mattermost-bot-token
        - name: MM_SLASH_TOKEN
          valueFrom:
            secretKeyRef:
              name: $BOT_SECRET_Q
              key: mattermost-slash-token
        - name: MM_ADMIN_TOKEN
          valueFrom:
            secretKeyRef:
              name: $E2E_PREFIX-secret
              key: admin-token
        - name: E2E_GITHUB_TOKEN
          valueFrom:
            secretKeyRef:
              name: matter-codex-github
              key: github-token
        - name: E2E_GITHUB_USERNAME
          valueFrom:
            secretKeyRef:
              name: matter-codex-github
              key: github-username
        - name: E2E_GITHUB_EMAIL
          valueFrom:
            secretKeyRef:
              name: matter-codex-github
              key: github-email
      volumeMounts:
        - name: script
          mountPath: /e2e
          readOnly: true
  volumes:
    - name: script
      configMap:
        name: $configmap_name
        defaultMode: 0555
YAML
}

render_python_configmap_manifest() {
  local name="$1"
  local script_file="$2"
  cat <<YAML
apiVersion: v1
kind: ConfigMap
metadata:
  name: $name
  namespace: $(yaml_quote "$MATTERCODEX_NAMESPACE")
  labels:
    app.kubernetes.io/name: matter-codex-e2e
    app.kubernetes.io/instance: $E2E_PREFIX
data:
  run.py: |
YAML
  sed 's/^/    /' "$script_file"
}

render_python_pod_manifest() {
  local pod_name="$1"
  local configmap_name="$2"
  render_pod_manifest "$pod_name" "$configmap_name" | sed \
    -e 's#image: curlimages/curl:8.10.1#image: python:3.12-alpine#' \
    -e 's#command: .*#command: ["python", "/e2e/run.py"]#'
}

run_script_pod() {
  local step="$1"
  local script_file="$2"
  local pod_name="$E2E_PREFIX-$step"
  local configmap_name="$pod_name-script"
  local configmap_manifest
  local pod_manifest
  configmap_manifest="$(temp_file)"
  pod_manifest="$(temp_file)"
  render_configmap_manifest "$configmap_name" "$script_file" >"$configmap_manifest"
  render_pod_manifest "$pod_name" "$configmap_name" >"$pod_manifest"
  mattercodex_log "e2e: apply pod $pod_name"
  apply_remote_manifest <"$configmap_manifest" >/dev/null
  apply_remote_manifest <"$pod_manifest" >/dev/null

  local logs
  local status
  set +e
  logs="$(mattercodex_ssh "set -eu
    for _ in \$(seq 1 15); do
      if $REMOTE_KUBECTL -n $NAMESPACE_Q get pod $pod_name >/dev/null 2>&1; then
        break
      fi
      sleep 1
    done
    $REMOTE_KUBECTL -n $NAMESPACE_Q get pod $pod_name >/dev/null
    for _ in \$(seq 1 $((TIMEOUT_SECONDS / 2 + 1))); do
      phase=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null || true)\"
      if [ \"\$phase\" = Succeeded ] || [ \"\$phase\" = Failed ]; then
        break
      fi
      sleep 2
    done
    phase=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null || true)\"
    $REMOTE_KUBECTL -n $NAMESPACE_Q logs $pod_name || true
    printf 'pod_phase=%s\n' \"\$phase\"
    test \"\$phase\" = Succeeded
  " </dev/null)"
  status=$?
  set -e
  printf '%s\n' "$logs"
  if [ "$status" -ne 0 ]; then
    return "$status"
  fi
}

run_python_script_pod() {
  local step="$1"
  local script_file="$2"
  local pod_name="$E2E_PREFIX-$step"
  local configmap_name="$pod_name-script"
  local configmap_manifest
  local pod_manifest
  configmap_manifest="$(temp_file)"
  pod_manifest="$(temp_file)"
  render_python_configmap_manifest "$configmap_name" "$script_file" >"$configmap_manifest"
  render_python_pod_manifest "$pod_name" "$configmap_name" >"$pod_manifest"
  mattercodex_log "e2e: apply python pod $pod_name"
  apply_remote_manifest <"$configmap_manifest" >/dev/null
  apply_remote_manifest <"$pod_manifest" >/dev/null

  local logs
  local status
  set +e
  logs="$(mattercodex_ssh "set -eu
    for _ in \$(seq 1 15); do
      if $REMOTE_KUBECTL -n $NAMESPACE_Q get pod $pod_name >/dev/null 2>&1; then
        break
      fi
      sleep 1
    done
    $REMOTE_KUBECTL -n $NAMESPACE_Q get pod $pod_name >/dev/null
    for _ in \$(seq 1 $((TIMEOUT_SECONDS / 2 + 1))); do
      phase=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null || true)\"
      if [ \"\$phase\" = Succeeded ] || [ \"\$phase\" = Failed ]; then
        break
      fi
      sleep 2
    done
    phase=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null || true)\"
    $REMOTE_KUBECTL -n $NAMESPACE_Q logs $pod_name || true
    printf 'pod_phase=%s\n' \"\$phase\"
    test \"\$phase\" = Succeeded
  " </dev/null)"
  status=$?
  set -e
  printf '%s\n' "$logs"
  if [ "$status" -ne 0 ]; then
    return "$status"
  fi
}

capture_script_pod() {
  local step="$1"
  local script_file="$2"
  local output_var="$3"
  local logs
  local status
  set +e
  logs="$(run_script_pod "$step" "$script_file")"
  status=$?
  set -e
  printf '%s\n' "$logs"
  printf -v "$output_var" '%s' "$logs"
  if [ "$status" -ne 0 ]; then
    mattercodex_die "e2e шаг $step завершился с ошибкой"
  fi
}

capture_python_script_pod() {
  local step="$1"
  local script_file="$2"
  local output_var="$3"
  local logs
  local status
  set +e
  logs="$(run_python_script_pod "$step" "$script_file")"
  status=$?
  set -e
  printf '%s\n' "$logs"
  printf -v "$output_var" '%s' "$logs"
  if [ "$status" -ne 0 ]; then
    mattercodex_die "e2e python шаг $step завершился с ошибкой"
  fi
}


script_user_project="$(temp_file)"
cat >"$script_user_project" <<'SCRIPT'
#!/bin/sh
set -eu

json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

create_body=$(printf '{"email":"%s","username":"%s","password":"%s"}' "$(json_escape "$E2E_EMAIL")" "$(json_escape "$E2E_USERNAME")" "$(json_escape "$E2E_PASSWORD")")
create_status=$(curl -sS -o /tmp/create-user.json -w '%{http_code}' -H 'Content-Type: application/json' -d "$create_body" "$MM_URL/api/v4/users" || true)
if [ "$create_status" = "403" ]; then
  create_status=$(curl -sS -o /tmp/create-user.json -w '%{http_code}' -H "Authorization: Bearer $MM_ADMIN_TOKEN" -H 'Content-Type: application/json' -d "$create_body" "$MM_URL/api/v4/users" || true)
fi
case "$create_status" in
  201|400) ;;
  *)
    echo "user_create_status=$create_status"
    sed 's/"password":"[^"]*"/"password":"***"/g' /tmp/create-user.json || true
    exit 1
    ;;
esac

login_body=$(printf '{"login_id":"%s","password":"%s"}' "$(json_escape "$E2E_USERNAME")" "$(json_escape "$E2E_PASSWORD")")
login_status=$(curl -sS -D /tmp/login.headers -o /tmp/login.json -w '%{http_code}' -H 'Content-Type: application/json' -d "$login_body" "$MM_URL/api/v4/users/login" || true)
if [ "$login_status" != "200" ]; then
  echo "user_login_status=$login_status"
  exit 1
fi
user_id=$(grep -o '"id":"[^"]*"' /tmp/login.json | head -n 1 | cut -d '"' -f 4)
test -n "$user_id"
echo "user_id=$user_id"

state=$(printf '{"view":"projects","channel_id":"e2e-control","post_id":"e2e-control","user_name":"%s"}' "$(json_escape "$E2E_USERNAME")")
cat >/tmp/project.json <<JSON
{
  "callback_id": "agents_project_upsert",
  "state": "$(json_escape "$state")",
  "user_id": "$user_id",
  "submission": {
    "project_name": "$(json_escape "$E2E_PROJECT_NAME")",
    "project_slug": "$(json_escape "$E2E_PROJECT_SLUG")",
    "description": "owner MVP e2e project",
    "advanced_settings": "{}"
  }
}
JSON
project_status=$(curl -sS -o /tmp/project-response.json -w '%{http_code}' -H 'Content-Type: application/json' -d @/tmp/project.json "$BOT_URL/mattermost/dialogs/agents" || true)
if [ "$project_status" != "200" ] || ! grep -q '"type":"ok"' /tmp/project-response.json; then
  echo "project_submit_status=$project_status"
  cat /tmp/project-response.json
  exit 1
fi
echo "project_submit=ok"
SCRIPT

script_ui_preflight="$(temp_file)"
cat >"$script_ui_preflight" <<'PY'
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

BOT_URL = os.environ["BOT_URL"].rstrip("/")
USER_ID = os.environ["E2E_USER_ID"]
USER_NAME = os.environ["E2E_USERNAME"]
PROJECT_ID = os.environ["E2E_PROJECT_ID"]
PROJECT_NAME = os.environ["E2E_PROJECT_NAME"]
PROJECT_SLUG = os.environ["E2E_PROJECT_SLUG"]
REPO_FULL = os.environ["E2E_REPO_OWNER"] + "/" + os.environ["E2E_REPO_NAME"]
REPO_RESOURCE = "github:" + REPO_FULL
GH_ACCOUNT = os.environ["E2E_GITHUB_ACCOUNT"]
OPENAI_COPY = os.environ["E2E_OPENAI_COPY_ACCOUNT"]
OPENAI_PENDING = os.environ["E2E_OPENAI_PENDING_ACCOUNT"]


def fail(message):
    print("e2e_ui_failed=" + message)
    sys.exit(1)


def request_json(method, url, payload=None, headers=None, form=None):
    headers = dict(headers or {})
    body = None
    if payload is not None:
        body = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if form is not None:
        body = urllib.parse.urlencode(form).encode("utf-8")
        headers["Content-Type"] = "application/x-www-form-urlencoded"
    req = urllib.request.Request(url, data=body, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=90) as response:
            raw = response.read().decode("utf-8")
            if not raw:
                return {}
            return json.loads(raw)
    except urllib.error.HTTPError as err:
        err.read()
        fail("http_status_%s_%s" % (err.code, urllib.parse.urlparse(url).path))
    except Exception as err:
        fail(type(err).__name__ + "_" + urllib.parse.urlparse(url).path)


def dump(obj):
    return json.dumps(obj, ensure_ascii=False, sort_keys=True)


def expect(condition, message):
    if not condition:
        fail(message)


def expect_text(obj, token, message):
    expect(token in dump(obj), message)


def menu_action(view, action="", resource="", resource_id="", dialog="", page=0):
    context = {"kind": "agents_menu", "view": view}
    if action:
        context["action"] = action
    if resource:
        context["resource_type"] = resource
    if resource_id:
        context["resource_id"] = resource_id
    if dialog:
        context["dialog"] = dialog
    if page:
        context["page"] = page
    return request_json("POST", BOT_URL + "/mattermost/actions/agents", {
        "user_id": USER_ID,
        "user_name": USER_NAME,
        "channel_id": "e2e-control",
        "post_id": "e2e-control",
        "context": context,
    })


def dialog_state(view, resource_type="", resource_id=""):
    state = {
        "view": view,
        "channel_id": "e2e-control",
        "post_id": "e2e-control",
        "user_name": USER_NAME,
    }
    if resource_type:
        state["resource_type"] = resource_type
    if resource_id:
        state["resource_id"] = resource_id
    return state


def dialog_submit(callback_id, state, submission):
    if isinstance(state, str):
        state_raw = state
    else:
        state_raw = json.dumps(state, separators=(",", ":"))
    return request_json("POST", BOT_URL + "/mattermost/dialogs/agents", {
        "callback_id": callback_id,
        "state": state_raw,
        "user_id": USER_ID,
        "channel_id": "e2e-control",
        "submission": submission,
    })


def first_option(form, element_name, token):
    for element in form.get("elements", []):
        if element.get("name") != element_name:
            continue
        for option in element.get("options", []):
            if token in option.get("text", "") or token in option.get("value", ""):
                return option
    fail("option_not_found_" + element_name)


slash = request_json("POST", BOT_URL + "/mattermost/slash/agents", form={
    "token": os.environ["MM_SLASH_TOKEN"],
    "text": "",
    "user_id": USER_ID,
    "user_name": USER_NAME,
    "channel_id": "e2e-control",
    "channel_name": "e2e-control",
    "team_id": "e2e-team",
    "team_domain": PROJECT_SLUG,
})
expect_text(slash, "Projects", "slash_menu_missing_projects")
expect("/agents repo list" not in dump(slash), "slash_menu_exposes_typed_repo_command")
expect("/agents project list" not in dump(slash), "slash_menu_exposes_typed_project_command")
print("ui_slash_menu=ok")

for view in ["projects", "accounts", "repositories", "roles", "chats", "runtime", "advanced"]:
    response = menu_action(view)
    expect_text(response, "matter-codex", "menu_view_missing_" + view)
print("ui_menu_navigation=ok")

expect_text(menu_action("main"), "matter-codex", "menu_main_missing")
expect_text(menu_action("projects"), "Projects", "menu_back_target_missing")
print("ui_menu_back_main=ok")

invalid_project = dialog_submit("agents_project_upsert", dialog_state("projects"), {
    "project_name": "x",
    "project_slug": "Bad Slug",
    "description": "",
    "advanced_settings": "not-json",
})
expect("errors" in invalid_project, "project_validation_errors_missing")
expect("project_name" in invalid_project.get("errors", {}), "project_name_validation_missing")
expect("project_slug" in invalid_project.get("errors", {}), "project_slug_validation_missing")
expect("advanced_settings" in invalid_project.get("errors", {}), "advanced_settings_validation_missing")
print("ui_dialog_validation=ok")

project_card = menu_action("projects", "show", "project", PROJECT_ID)
expect_text(project_card, PROJECT_NAME, "project_dashboard_name_missing")
expect_text(project_card, "dialogchatcreate", "project_dashboard_create_chat_action_missing")
expect_text(project_card, "dialogroleadd", "project_dashboard_add_role_action_missing")
expect_text(project_card, "dialogprojectrepo", "project_dashboard_add_repo_action_missing")
print("ui_project_dashboard=ok")

openai_list = menu_action("openai", "list", "openai_account")
expect_text(openai_list, "main", "openai_list_missing_main")
expect_text(menu_action("openai", "show", "openai_account", "main"), "main", "openai_show_main_missing")
menu_action("openai", "openai_status", "openai_account", "main")
dialog_submit("agents_openai_auth", dialog_state("openai"), {"account": OPENAI_PENDING})
menu_action("openai", "openai_auth", "openai_account", OPENAI_PENDING)
menu_action("openai", "delete", "openai_account", OPENAI_PENDING)
menu_action("openai", "delete", "openai_account", OPENAI_COPY)
print("ui_openai_accounts=ok")

github_add = dialog_submit("agents_github_account_add", dialog_state("github"), {
    "account": GH_ACCOUNT,
    "token": os.environ["E2E_GITHUB_TOKEN"],
})
expect(github_add.get("type") == "ok", "github_add_not_ok")
github_list = menu_action("github", "list", "github_account")
expect_text(github_list, GH_ACCOUNT, "github_list_missing_e2e_account")
expect_text(github_list, os.environ["E2E_GITHUB_USERNAME"], "github_list_missing_username")
github_show = menu_action("github", "show", "github_account", GH_ACCOUNT)
expect_text(github_show, GH_ACCOUNT, "github_show_missing_account")
github_edit = dialog_submit("agents_github_account_edit", dialog_state("github", "github_account", GH_ACCOUNT), {
    "account": GH_ACCOUNT,
    "token": os.environ["E2E_GITHUB_TOKEN"],
})
expect(github_edit.get("type") == "ok", "github_edit_not_ok")
menu_action("github", "confirm_delete", "github_account", GH_ACCOUNT)
menu_action("github", "delete", "github_account", GH_ACCOUNT)
print("ui_github_accounts=ok")

repo_accounts = menu_action("repositories", "repository_onboard", "repository")
expect_text(repo_accounts, "primary", "repository_onboard_missing_primary_account")
search = dialog_submit("agents_repo_search", dialog_state("repositories", "github_account", "primary"), {
    "search": REPO_FULL,
})
expect(search.get("type") == "form", "repository_search_did_not_return_pick_form")
pick_form = search.get("form", {})
expect(pick_form.get("callback_id") == "agents_repo_search_pick", "repository_pick_callback_missing")
repo_option = first_option(pick_form, "repository_choice", REPO_FULL)
branch = dialog_submit("agents_repo_search_pick", pick_form.get("state", ""), {
    "repository_choice": repo_option["value"],
})
expect(branch.get("type") == "form", "repository_pick_did_not_return_branch_form")
branch_form = branch.get("form", {})
expect(branch_form.get("callback_id") == "agents_repo_search_branch", "repository_branch_callback_missing")
branch_option = first_option(branch_form, "branch_choice", "main")
connected = dialog_submit("agents_repo_search_branch", branch_form.get("state", ""), {
    "branch_choice": branch_option["value"],
})
expect(connected.get("type") == "ok", "repository_branch_connect_not_ok")
expect_text(menu_action("repositories", "show", "repository", REPO_RESOURCE), REPO_FULL, "repository_card_missing_repo")
expect_text(menu_action("repositories", "repository_check", "repository", REPO_RESOURCE), "GitHub", "repository_check_missing_github")
expect_text(menu_action("repositories", "repository_webhook", "repository", REPO_RESOURCE), "webhook", "repository_webhook_missing_text")
print("ui_repository_onboarding=ok")
PY

script_repo_role="$(temp_file)"
cat >"$script_repo_role" <<'SCRIPT'
#!/bin/sh
set -eu

json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

state=$(printf '{"view":"projects","channel_id":"e2e-control","post_id":"e2e-control","user_name":"%s"}' "$(json_escape "$E2E_USERNAME")")
config_overlay=$(printf 'model = "%s"' "$E2E_CODEX_MODEL")
reviewer_prompt='You are the reviewer agent.

Language: {{.Locale.Language}}.
Review the pull request from the user instruction.

User instruction:
{{.Task.Body}}

Return a concise review summary. End the final answer with:
decision: comment
review_submitted: false
'

cat >/tmp/repo-bind.json <<JSON
{
  "callback_id": "agents_project_repository_bind",
  "state": "$(json_escape "$state")",
  "user_id": "$E2E_USER_ID",
  "submission": {
    "project_id": "$E2E_PROJECT_ID",
    "repository_id": "$E2E_REPOSITORY_ID",
    "status": "true"
  }
}
JSON
repo_status=$(curl -sS -o /tmp/repo-response.json -w '%{http_code}' -H 'Content-Type: application/json' -d @/tmp/repo-bind.json "$BOT_URL/mattermost/dialogs/agents" || true)
if [ "$repo_status" != "200" ] || ! grep -q '"type":"ok"' /tmp/repo-response.json; then
  echo "repo_bind_status=$repo_status"
  cat /tmp/repo-response.json
  exit 1
fi
echo "repo_bind=ok"

submit_role() {
  role_name="$1"
  role_type="$2"
  openai_account="$3"
  github_account="$4"
  prompt_mode="$5"
  prompt_template="$6"
  description="$7"
  cat >/tmp/role.json <<JSON
{
  "callback_id": "agents_agent_role_upsert",
  "state": "$(json_escape "$state")",
  "user_id": "$E2E_USER_ID",
  "submission": {
    "project_id": "$E2E_PROJECT_ID",
    "role": "$(json_escape "$role_name")",
    "role_type": "$(json_escape "$role_type")",
    "openai_account": "$(json_escape "$openai_account")",
    "github_account": "$(json_escape "$github_account")",
    "prompt_mode": "$(json_escape "$prompt_mode")",
    "prompt_template": "$(json_escape "$prompt_template")",
    "kubernetes_access": "read-only",
    "sandbox_mode": "danger-full-access",
    "description": "$(json_escape "$description")",
    "config_overlay": "$(json_escape "$config_overlay")",
    "advanced_settings": "{\"e2e\":true}"
  }
}
JSON
  role_status=$(curl -sS -o /tmp/role-response.json -w '%{http_code}' -H 'Content-Type: application/json' -d @/tmp/role.json "$BOT_URL/mattermost/dialogs/agents" || true)
  if [ "$role_status" != "200" ] || ! grep -q '"type":"ok"' /tmp/role-response.json; then
    echo "role_submit_status=$role_status"
    cat /tmp/role-response.json
    exit 1
  fi
  echo "role_submit=$role_name"
}

submit_role "$E2E_MANAGER_ROLE_NAME" "manager" "main" "__none__" "raw" "" "E2E manager role with raw prompt mode."
submit_role "$E2E_WORKER_ROLE_NAME" "worker" "main" "primary" "raw" "" "E2E worker role with raw prompt mode."
submit_role "$E2E_REVIEWER_ROLE_NAME" "reviewer" "main" "primary" "template" "$reviewer_prompt" "E2E reviewer role with a minimal prompt template."
submit_role "$E2E_BROKEN_ROLE_NAME" "manager" "__none__" "__none__" "raw" "" "E2E broken role without OpenAI account for negative checks."
SCRIPT

script_chats_runs="$(temp_file)"
cat >"$script_chats_runs" <<'PY'
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

MM_URL = os.environ["MM_URL"].rstrip("/")
BOT_URL = os.environ["BOT_URL"].rstrip("/")
USER_ID = os.environ["E2E_USER_ID"]
USER_NAME = os.environ["E2E_USERNAME"]
PROJECT_ID = os.environ["E2E_PROJECT_ID"]
PROJECT_SLUG = os.environ["E2E_PROJECT_SLUG"]
REPOSITORY_ID = os.environ["E2E_REPOSITORY_ID"]
REPO_FULL = os.environ["E2E_REPO_OWNER"] + "/" + os.environ["E2E_REPO_NAME"]
TIMEOUT = int(os.environ.get("E2E_TIMEOUT_SECONDS", "900"))
MARKER = os.environ["E2E_MARKER"]


def fail(message):
    print("e2e_chat_failed=" + message)
    sys.exit(1)


def request(method, url, payload=None, token=None):
    headers = {}
    data = None
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=90) as response:
            raw = response.read().decode("utf-8")
            if not raw:
                return {}
            return json.loads(raw)
    except urllib.error.HTTPError as err:
        err.read()
        fail("http_status_%s_%s" % (err.code, urllib.parse.urlparse(url).path))
    except Exception as err:
        fail(type(err).__name__ + "_" + urllib.parse.urlparse(url).path)


def request_form(method, url, form):
    data = urllib.parse.urlencode(form).encode("utf-8")
    req = urllib.request.Request(url, data=data, method=method, headers={"Content-Type": "application/x-www-form-urlencoded"})
    try:
        with urllib.request.urlopen(req, timeout=90) as response:
            raw = response.read().decode("utf-8")
            if not raw:
                return {}
            return json.loads(raw)
    except urllib.error.HTTPError as err:
        err.read()
        fail("http_status_%s_%s" % (err.code, urllib.parse.urlparse(url).path))
    except Exception as err:
        fail(type(err).__name__ + "_" + urllib.parse.urlparse(url).path)


def dialog_state(view, resource_type="", resource_id=""):
    state = {
        "view": view,
        "channel_id": "e2e-control",
        "post_id": "e2e-control",
        "user_name": USER_NAME,
    }
    if resource_type:
        state["resource_type"] = resource_type
    if resource_id:
        state["resource_id"] = resource_id
    return json.dumps(state, separators=(",", ":"))


def dialog_submit(callback_id, state, submission):
    return request("POST", BOT_URL + "/mattermost/dialogs/agents", {
        "callback_id": callback_id,
        "state": state,
        "user_id": USER_ID,
        "channel_id": "e2e-control",
        "submission": submission,
    })


def menu_action(view, action="", resource="", resource_id="", dialog=""):
    context = {"kind": "agents_menu", "view": view}
    if action:
        context["action"] = action
    if resource:
        context["resource_type"] = resource
    if resource_id:
        context["resource_id"] = resource_id
    if dialog:
        context["dialog"] = dialog
    return request("POST", BOT_URL + "/mattermost/actions/agents", {
        "user_id": USER_ID,
        "user_name": USER_NAME,
        "channel_id": "e2e-control",
        "post_id": "e2e-control",
        "context": context,
    })


def slash_command(text):
    return request_form("POST", BOT_URL + "/mattermost/slash/agents", {
        "token": os.environ["MM_SLASH_TOKEN"],
        "text": text,
        "user_id": USER_ID,
        "user_name": USER_NAME,
        "channel_id": "e2e-control",
        "channel_name": "e2e-control",
        "team_id": "e2e-team",
        "team_domain": PROJECT_SLUG,
    })


def response_text(obj):
    return json.dumps(obj, ensure_ascii=False, sort_keys=True)


def expect_response_contains(obj, token, label):
    if token not in response_text(obj):
        fail(label)


def extract_run_id(text, label):
    matches = re.findall(r"run: `([^`]+)`", text)
    if not matches:
        fail(label + "_run_id_missing")
    return matches[-1]


def login():
    response = urllib.request.urlopen(urllib.request.Request(
        MM_URL + "/api/v4/users/login",
        data=json.dumps({"login_id": os.environ["E2E_USERNAME"], "password": os.environ["E2E_PASSWORD"]}).encode("utf-8"),
        method="POST",
        headers={"Content-Type": "application/json"},
    ), timeout=90)
    token = response.headers.get("Token", "").strip()
    response.read()
    if not token:
        fail("mattermost_login_token_missing")
    return token


def create_chat(name, slug, chat_type, primary_role_id, secondary_role_id="", repository_id="", root_issue="", policy=""):
    response = dialog_submit("agents_chat_create", dialog_state("chats", "project", PROJECT_ID), {
        "project_id": PROJECT_ID,
        "chat_name": name,
        "chat_type": chat_type,
        "primary_role_id": str(primary_role_id),
        "secondary_role_id": str(secondary_role_id or "__none__"),
        "repository_id": str(repository_id or "__none__"),
        "root_issue": root_issue,
        "work_policy": policy,
    })
    if response.get("type") != "ok":
        fail("chat_create_not_ok_" + slug)


def post(channel_id, message, token):
    response = request("POST", MM_URL + "/api/v4/posts", {"channel_id": channel_id, "message": message}, token)
    post_id = response.get("id", "")
    if not post_id:
        fail("post_id_missing")
    return post_id


def thread_text(root_post_id, token):
    response = request("GET", MM_URL + "/api/v4/posts/" + root_post_id + "/thread", token=token)
    posts = response.get("posts", {})
    parts = []
    for item in posts.values():
        if item.get("root_id") == root_post_id:
            parts.append(item.get("message", ""))
    return "\n".join(parts)


def wait_for(root_post_id, token, predicate, label):
    deadline = time.time() + TIMEOUT
    while time.time() < deadline:
        text = thread_text(root_post_id, token)
        if predicate(text):
            return text
        if "chat.run.failed" in text or "agent run заверш" in text and "ошиб" in text:
            fail(label + "_failed")
        time.sleep(10)
    fail(label + "_timeout")


def get_team_channel(token, channel_slug):
    team = request("GET", MM_URL + "/api/v4/teams/name/" + PROJECT_SLUG, token=token)
    team_id = team.get("id", "")
    if not team_id:
        fail("team_id_missing")
    channel = request("GET", MM_URL + "/api/v4/teams/" + team_id + "/channels/name/" + channel_slug, token=token)
    channel_id = channel.get("id", "")
    if not channel_id:
        fail("channel_id_missing_" + channel_slug)
    return team_id, channel_id


user_token = login()

create_chat(
    os.environ["E2E_MANAGER_CHAT_NAME"],
    os.environ["E2E_MANAGER_CHAT_SLUG"],
    "manager",
    os.environ["E2E_MANAGER_ROLE_ID"],
    policy="E2E manager chat. Answer in the source message thread.",
)
_, manager_channel = get_team_channel(user_token, os.environ["E2E_MANAGER_CHAT_SLUG"])

bot_root = post(manager_channel, "E2E bot self-message; this must not start a run.", os.environ["MM_BOT_TOKEN"])
print("bot_root_post_id=" + bot_root)

manager_root = post(manager_channel, "E2E manager request. Reply exactly: " + MARKER + " manager. Do not modify files.", user_token)
manager_text = wait_for(manager_root, user_token, lambda value: MARKER + " manager" in value, "manager_run")
manager_run_id = extract_run_id(manager_text, "manager")
print("manager_root_post_id=" + manager_root)
print("manager_run_id=" + manager_run_id)
print("manager_thread_final=ok")

create_chat(
    os.environ["E2E_BROKEN_CHAT_NAME"],
    os.environ["E2E_BROKEN_CHAT_SLUG"],
    "single_custom",
    os.environ["E2E_BROKEN_ROLE_ID"],
    policy="E2E negative chat without OpenAI account.",
)
_, broken_channel = get_team_channel(user_token, os.environ["E2E_BROKEN_CHAT_SLUG"])
broken_root = post(broken_channel, "E2E negative request; should fail before runtime because OpenAI account is missing.", user_token)
wait_for(broken_root, user_token, lambda value: "OpenAI" in value and os.environ["E2E_BROKEN_ROLE_NAME"] in value, "broken_openai")
print("broken_root_post_id=" + broken_root)
print("broken_openai_error=ok")

create_chat(
    os.environ["E2E_WORKER_CHAT_NAME"],
    os.environ["E2E_WORKER_CHAT_SLUG"],
    "worker_reviewer",
    os.environ["E2E_WORKER_ROLE_ID"],
    os.environ["E2E_REVIEWER_ROLE_ID"],
    REPOSITORY_ID,
    root_issue="https://github.com/" + REPO_FULL + "/issues/1",
    policy="E2E worker/reviewer chat. Worker creates a draft PR; reviewer comments on the PR.",
)
_, worker_channel = get_team_channel(user_token, os.environ["E2E_WORKER_CHAT_SLUG"])
worker_marker = MARKER + " pr"
worker_message = (
    "E2E developer task for repository " + REPO_FULL + ".\n"
    "Edit file e2e-state.txt and append exactly this line:\n"
    + worker_marker + "\n"
    "Do not change other files. Stop after the edit and summarize the marker."
)
worker_root = post(worker_channel, worker_message, user_token)
worker_text = wait_for(worker_root, user_token, lambda value: "https://github.com/" in value and "/pull/" in value, "worker_pr")
matches = re.findall(r"https://github\.com/[^\s)]+/pull/[0-9]+", worker_text)
if not matches:
    fail("worker_pr_url_missing")
pr_url = matches[-1]
worker_run_id = extract_run_id(worker_text, "worker")
print("worker_root_post_id=" + worker_root)
print("worker_run_id=" + worker_run_id)
print("worker_pr_url=" + pr_url)

reviewer_message = (
    "Review this pull request and post a concise GitHub review comment: " + pr_url + "\n"
    "Use decision comment unless there is a clear blocker. Final answer must include decision and review_submitted fields."
)
reviewer_root = post(worker_channel, reviewer_message, user_token)
reviewer_text = wait_for(reviewer_root, user_token, lambda value: "review-submitted" in value and "true" in value, "reviewer_run")
reviewer_run_id = extract_run_id(reviewer_text, "reviewer")
print("reviewer_root_post_id=" + reviewer_root)
print("reviewer_run_id=" + reviewer_run_id)
print("reviewer_thread_final=ok")
print("worker_channel_id=" + worker_channel)

role_list = menu_action("roles", "list", "agent_role", PROJECT_ID)
expect_response_contains(role_list, os.environ["E2E_WORKER_ROLE_NAME"], "role_list_missing_worker")
role_card = menu_action("roles", "show", "agent_role", os.environ["E2E_WORKER_ROLE_ID"])
expect_response_contains(role_card, "Codex", "role_card_missing_codex_config")
expect_response_contains(role_card, "Advanced", "role_card_missing_advanced_settings")
chat_list = menu_action("chats", "list", "chat", PROJECT_ID)
expect_response_contains(chat_list, os.environ["E2E_WORKER_CHAT_NAME"], "chat_list_missing_worker_chat")
project_card = menu_action("projects", "show", "project", PROJECT_ID)
expect_response_contains(project_card, "dialogchatcreate", "project_dashboard_missing_create_chat_action")
expect_response_contains(project_card, "dialogroleadd", "project_dashboard_missing_add_role_action")
expect_response_contains(project_card, "dialogprojectrepo", "project_dashboard_missing_add_repo_action")
runtime_list = menu_action("runtime", "list", "run")
expect_response_contains(runtime_list, worker_run_id, "runtime_list_missing_worker_run")
runtime_card = menu_action("runtime", "show", "run", worker_run_id)
expect_response_contains(runtime_card, "mc-run-" + worker_run_id, "runtime_card_missing_job")
expect_response_contains(runtime_card, "mc-ws-" + worker_run_id, "runtime_card_missing_pvc")
menu_action("runtime", "runtime_prune_dry", "runtime")
time.sleep(2)
prune_dry = slash_command("runtime prune 1s")
expect_response_contains(prune_dry, os.environ["E2E_PRUNE_OLD_RUN_ID"], "runtime_prune_dry_missing_old_fixture")
prune_dry_text = response_text(prune_dry)
if "active jobs skipped: `1`" not in prune_dry_text and "активные jobs пропущены: `1`" not in prune_dry_text:
    fail("runtime_prune_dry_missing_skipped_active")
menu_action("runtime", "runtime_cleanup", "run", manager_run_id)
time.sleep(2)
retention = dialog_submit("agents_runtime_prune_apply", dialog_state("runtime"), {
    "older_than": "1s",
    "confirm": "apply",
})
if retention.get("type") != "ok":
    fail("runtime_retention_apply_not_ok")
print("runtime_ui=ok")
PY

script_domain_cleanup="$(temp_file)"
cat >"$script_domain_cleanup" <<'SCRIPT'
#!/bin/sh
set -eu

team_json=$(curl -sS -H "Authorization: Bearer $MM_ADMIN_TOKEN" "$MM_URL/api/v4/teams/name/$E2E_PROJECT_SLUG" || true)
team_id=$(printf '%s' "$team_json" | grep -o '"id":"[^"]*"' | head -n 1 | cut -d '"' -f 4 || true)
if [ -n "$team_id" ]; then
  curl -sS -X DELETE -H "Authorization: Bearer $MM_ADMIN_TOKEN" "$MM_URL/api/v4/teams/$team_id" >/dev/null || true
fi
if [ -n "${E2E_USER_ID:-}" ]; then
  curl -sS -X DELETE -H "Authorization: Bearer $MM_ADMIN_TOKEN" "$MM_URL/api/v4/users/$E2E_USER_ID" >/dev/null || true
fi
echo "mattermost_domain_cleanup=ok"
SCRIPT

MATTERMOST_ADMIN_TOKEN="$(remote_psql "select s.token from sessions s join users u on u.id = s.userid where position('system_admin' in u.roles) > 0 and s.expiresat > (extract(epoch from now()) * 1000)::bigint order by s.lastactivityat desc limit 1;")"
[ -n "$MATTERMOST_ADMIN_TOKEN" ] || mattercodex_die "активная Mattermost system_admin session не найдена для e2e bootstrap"

render_secret_manifest | apply_remote_manifest >/dev/null

mattercodex_log "live e2e: create user and project through Mattermost dialog endpoint"
capture_script_pod "user-project" "$script_user_project" user_project_logs
E2E_USER_ID="$(printf '%s\n' "$user_project_logs" | awk -F= '/^user_id=/ {print $2}' | tail -n 1)"
[ -n "$E2E_USER_ID" ] || mattercodex_die "e2e user_id не найден в логах"

PROJECT_ID="$(remote_psql "select id from matter_codex_projects where slug = '$PROJECT_SLUG' limit 1;")"
[ -n "$PROJECT_ID" ] || mattercodex_die "e2e project не найден в базе"

mattercodex_log "live e2e: prepare temporary OpenAI account copy for delete UI check"
prepare_openai_copy_account "$E2E_OPENAI_COPY_ACCOUNT"

mattercodex_log "live e2e: verify Mattermost menu, accounts, repository onboarding, project dashboard"
script_ui_preflight_env="$(temp_file)"
sed \
  -e "1a import os" \
  -e "1a os.environ['E2E_USER_ID'] = $(mattercodex_shell_quote "$E2E_USER_ID")" \
  -e "1a os.environ['E2E_PROJECT_ID'] = $(mattercodex_shell_quote "$PROJECT_ID")" \
  "$script_ui_preflight" >"$script_ui_preflight_env"
capture_python_script_pod "ui-preflight" "$script_ui_preflight_env" ui_preflight_logs

if mattercodex_ssh "set -eu; $REMOTE_KUBECTL -n $NAMESPACE_Q get secret \"matter-codex-codex-auth-$E2E_OPENAI_COPY_ACCOUNT\" >/dev/null 2>&1" </dev/null; then
  mattercodex_die "temporary OpenAI auth secret was not deleted by UI flow"
fi

REPOSITORY_ID="$(remote_psql "select id from matter_codex_repositories where provider = 'github' and owner = '$E2E_REPO_OWNER' and name = '$E2E_REPO_NAME' limit 1;")"
[ -n "$REPOSITORY_ID" ] || mattercodex_die "e2e repository github:$E2E_REPO_OWNER/$E2E_REPO_NAME не найден в базе"

mattercodex_log "live e2e: bind repository and create manager/worker/reviewer roles"
script_repo_role_env="$(temp_file)"
sed \
  -e "1a export E2E_USER_ID=$(mattercodex_shell_quote "$E2E_USER_ID")" \
  -e "1a export E2E_PROJECT_ID=$(mattercodex_shell_quote "$PROJECT_ID")" \
  -e "1a export E2E_REPOSITORY_ID=$(mattercodex_shell_quote "$REPOSITORY_ID")" \
  "$script_repo_role" >"$script_repo_role_env"
capture_script_pod "repo-role" "$script_repo_role_env" repo_role_logs

MANAGER_ROLE_ID="$(remote_psql "select id from matter_codex_agent_roles where project_id = $PROJECT_ID and name = '$MANAGER_ROLE_NAME' limit 1;")"
WORKER_ROLE_ID="$(remote_psql "select id from matter_codex_agent_roles where project_id = $PROJECT_ID and name = '$WORKER_ROLE_NAME' limit 1;")"
REVIEWER_ROLE_ID="$(remote_psql "select id from matter_codex_agent_roles where project_id = $PROJECT_ID and name = '$REVIEWER_ROLE_NAME' limit 1;")"
BROKEN_ROLE_ID="$(remote_psql "select id from matter_codex_agent_roles where project_id = $PROJECT_ID and name = '$BROKEN_ROLE_NAME' limit 1;")"
[ -n "$MANAGER_ROLE_ID" ] || mattercodex_die "manager role не найдена в базе"
[ -n "$WORKER_ROLE_ID" ] || mattercodex_die "worker role не найдена в базе"
[ -n "$REVIEWER_ROLE_ID" ] || mattercodex_die "reviewer role не найдена в базе"
[ -n "$BROKEN_ROLE_ID" ] || mattercodex_die "broken role не найдена в базе"

mattercodex_log "live e2e: prepare runtime retention fixture"
prepare_retention_fixture "$E2E_PRUNE_ACTIVE_RUN_ID" "$E2E_PRUNE_OLD_RUN_ID"

mattercodex_log "live e2e: create chats, post owner messages, wait for manager/worker/reviewer runs"
script_chats_runs_env="$(temp_file)"
sed \
  -e "1a import os" \
  -e "1a os.environ['E2E_TIMEOUT_SECONDS'] = $(mattercodex_shell_quote "$TIMEOUT_SECONDS")" \
  -e "1a os.environ['E2E_USER_ID'] = $(mattercodex_shell_quote "$E2E_USER_ID")" \
  -e "1a os.environ['E2E_PROJECT_ID'] = $(mattercodex_shell_quote "$PROJECT_ID")" \
  -e "1a os.environ['E2E_REPOSITORY_ID'] = $(mattercodex_shell_quote "$REPOSITORY_ID")" \
  -e "1a os.environ['E2E_MANAGER_ROLE_ID'] = $(mattercodex_shell_quote "$MANAGER_ROLE_ID")" \
  -e "1a os.environ['E2E_WORKER_ROLE_ID'] = $(mattercodex_shell_quote "$WORKER_ROLE_ID")" \
  -e "1a os.environ['E2E_REVIEWER_ROLE_ID'] = $(mattercodex_shell_quote "$REVIEWER_ROLE_ID")" \
  -e "1a os.environ['E2E_BROKEN_ROLE_ID'] = $(mattercodex_shell_quote "$BROKEN_ROLE_ID")" \
  "$script_chats_runs" >"$script_chats_runs_env"
capture_python_script_pod "chats-runs" "$script_chats_runs_env" chat_logs

CHANNEL_ID="$(printf '%s\n' "$chat_logs" | awk -F= '/^worker_channel_id=/ {print $2}' | tail -n 1)"
MANAGER_ROOT_POST_ID="$(printf '%s\n' "$chat_logs" | awk -F= '/^manager_root_post_id=/ {print $2}' | tail -n 1)"
WORKER_ROOT_POST_ID="$(printf '%s\n' "$chat_logs" | awk -F= '/^worker_root_post_id=/ {print $2}' | tail -n 1)"
REVIEWER_ROOT_POST_ID="$(printf '%s\n' "$chat_logs" | awk -F= '/^reviewer_root_post_id=/ {print $2}' | tail -n 1)"
MANAGER_RUN_ID="$(printf '%s\n' "$chat_logs" | awk -F= '/^manager_run_id=/ {print $2}' | tail -n 1)"
WORKER_RUN_ID="$(printf '%s\n' "$chat_logs" | awk -F= '/^worker_run_id=/ {print $2}' | tail -n 1)"
REVIEWER_RUN_ID="$(printf '%s\n' "$chat_logs" | awk -F= '/^reviewer_run_id=/ {print $2}' | tail -n 1)"
PR_URL="$(printf '%s\n' "$chat_logs" | awk -F= '/^worker_pr_url=/ {print substr($0, index($0,$2))}' | tail -n 1)"

RUN_INFO="$(remote_psql "select string_agg(run_id || ' status=' || status || ' job=' || job_name || ' pvc=' || pvc_name, E'\n' order by created_at) from matter_codex_agent_runs where run_id in ('$MANAGER_RUN_ID', '$WORKER_RUN_ID', '$REVIEWER_RUN_ID');")"
[ -n "$RUN_INFO" ] || mattercodex_die "e2e agent runs не найдены в базе"

if ! mattercodex_bool "$KEEP_DOMAIN"; then
  script_domain_cleanup_env="$(temp_file)"
  sed \
    -e "1a export E2E_USER_ID=$(mattercodex_shell_quote "$E2E_USER_ID")" \
    "$script_domain_cleanup" >"$script_domain_cleanup_env"
  capture_script_pod "domain-cleanup" "$script_domain_cleanup_env" domain_cleanup_logs
  remote_psql "delete from matter_codex_projects where slug = '$PROJECT_SLUG';" >/dev/null
fi
if ! mattercodex_bool "$KEEP_RUNTIME"; then
  mattercodex_ssh "set -eu
    for run_id in \"$MANAGER_RUN_ID\" \"$WORKER_RUN_ID\" \"$REVIEWER_RUN_ID\"; do
      [ -n \"\$run_id\" ] || continue
      $REMOTE_KUBECTL -n $NAMESPACE_Q delete job \"mc-run-\$run_id\" --ignore-not-found >/dev/null 2>&1 || true
      $REMOTE_KUBECTL -n $NAMESPACE_Q delete pvc \"mc-ws-\$run_id\" --ignore-not-found >/dev/null 2>&1 || true
      $REMOTE_KUBECTL -n $NAMESPACE_Q delete configmap \"mc-prompt-\$run_id\" --ignore-not-found >/dev/null 2>&1 || true
    done
  " </dev/null
fi

mattercodex_log "live e2e завершен"
printf 'project_id=%s\n' "$PROJECT_ID"
printf 'manager_role_id=%s\n' "$MANAGER_ROLE_ID"
printf 'worker_role_id=%s\n' "$WORKER_ROLE_ID"
printf 'reviewer_role_id=%s\n' "$REVIEWER_ROLE_ID"
printf 'channel_id=%s\n' "${CHANNEL_ID:-unknown}"
printf 'manager_root_post_id=%s\n' "${MANAGER_ROOT_POST_ID:-unknown}"
printf 'worker_root_post_id=%s\n' "${WORKER_ROOT_POST_ID:-unknown}"
printf 'reviewer_root_post_id=%s\n' "${REVIEWER_ROOT_POST_ID:-unknown}"
printf 'runs=%s\n' "$RUN_INFO"
printf 'pr_url=%s\n' "${PR_URL:-unknown}"
printf 'marker=%s\n' "$E2E_MARKER"
