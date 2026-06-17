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
E2E_ROLE_TYPE="${MATTERCODEX_E2E_ROLE_TYPE:-manager}"
ROLE_NAME="e2e-$E2E_ROLE_TYPE-$RUN_SUFFIX"
CHAT_NAME="e2e-chat-$RUN_SUFFIX"
CHAT_SLUG="e2e-chat-$RUN_SUFFIX"
E2E_MARKER="matter-codex e2e ok $RUN_SUFFIX"
E2E_REPO_OWNER="${MATTERCODEX_E2E_REPO_OWNER:-codex-k8s}"
E2E_REPO_NAME="${MATTERCODEX_E2E_REPO_NAME:-matter-codex}"
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
    " </dev/null || true
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
        - name: E2E_ROLE_NAME
          value: $(yaml_quote "$ROLE_NAME")
        - name: E2E_ROLE_TYPE
          value: $(yaml_quote "$E2E_ROLE_TYPE")
        - name: E2E_CHAT_NAME
          value: $(yaml_quote "$CHAT_NAME")
        - name: E2E_CHAT_SLUG
          value: $(yaml_quote "$CHAT_SLUG")
        - name: E2E_MARKER
          value: $(yaml_quote "$E2E_MARKER")
        - name: E2E_KEEP_DOMAIN
          value: $(yaml_quote "$KEEP_DOMAIN")
        - name: MM_BOT_TOKEN
          valueFrom:
            secretKeyRef:
              name: $BOT_SECRET_Q
              key: mattermost-bot-token
        - name: MM_ADMIN_TOKEN
          valueFrom:
            secretKeyRef:
              name: $E2E_PREFIX-secret
              key: admin-token
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

script_repo_role="$(temp_file)"
cat >"$script_repo_role" <<'SCRIPT'
#!/bin/sh
set -eu

json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

state=$(printf '{"view":"projects","channel_id":"e2e-control","post_id":"e2e-control","user_name":"%s"}' "$(json_escape "$E2E_USERNAME")")
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

cat >/tmp/role.json <<JSON
{
  "callback_id": "agents_agent_role_upsert",
  "state": "$(json_escape "$state")",
  "user_id": "$E2E_USER_ID",
  "submission": {
    "project_id": "$E2E_PROJECT_ID",
    "role": "$(json_escape "$E2E_ROLE_NAME")",
    "role_type": "$(json_escape "$E2E_ROLE_TYPE")",
    "openai_account": "main",
    "github_account": "primary",
    "prompt_mode": "raw",
    "prompt_template": "",
    "kubernetes_access": "read-only",
    "sandbox_mode": "danger-full-access",
    "description": "E2E manager role with raw prompt mode.",
    "config_overlay": "",
    "advanced_settings": "{}"
  }
}
JSON
role_status=$(curl -sS -o /tmp/role-response.json -w '%{http_code}' -H 'Content-Type: application/json' -d @/tmp/role.json "$BOT_URL/mattermost/dialogs/agents" || true)
if [ "$role_status" != "200" ] || ! grep -q '"type":"ok"' /tmp/role-response.json; then
  echo "role_submit_status=$role_status"
  cat /tmp/role-response.json
  exit 1
fi
echo "role_submit=ok"
SCRIPT

script_chat_post="$(temp_file)"
cat >"$script_chat_post" <<'SCRIPT'
#!/bin/sh
set -eu

json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

login_body=$(printf '{"login_id":"%s","password":"%s"}' "$(json_escape "$E2E_USERNAME")" "$(json_escape "$E2E_PASSWORD")")
login_status=$(curl -sS -D /tmp/login.headers -o /tmp/login.json -w '%{http_code}' -H 'Content-Type: application/json' -d "$login_body" "$MM_URL/api/v4/users/login" || true)
if [ "$login_status" != "200" ]; then
  echo "user_login_status=$login_status"
  exit 1
fi
user_token=$(awk 'tolower($1)=="token:" {print $2}' /tmp/login.headers | tr -d '\r')
test -n "$user_token"

state=$(printf '{"view":"chats","channel_id":"e2e-control","post_id":"e2e-control","user_name":"%s"}' "$(json_escape "$E2E_USERNAME")")
cat >/tmp/chat.json <<JSON
{
  "callback_id": "agents_chat_create",
  "state": "$(json_escape "$state")",
  "user_id": "$E2E_USER_ID",
  "submission": {
    "project_id": "$E2E_PROJECT_ID",
    "chat_name": "$(json_escape "$E2E_CHAT_NAME")",
    "chat_type": "single_custom",
    "primary_role_id": "$E2E_ROLE_ID",
    "secondary_role_id": "__none__",
    "repository_id": "$E2E_REPOSITORY_ID",
    "root_issue": "",
    "work_policy": "E2E: answer in the source message thread."
  }
}
JSON
chat_status=$(curl -sS -o /tmp/chat-response.json -w '%{http_code}' -H 'Content-Type: application/json' -d @/tmp/chat.json "$BOT_URL/mattermost/dialogs/agents" || true)
if [ "$chat_status" != "200" ] || ! grep -q '"type":"ok"' /tmp/chat-response.json; then
  echo "chat_submit_status=$chat_status"
  cat /tmp/chat-response.json
  exit 1
fi
echo "chat_submit=ok"

team_json=$(curl -fsS -H "Authorization: Bearer $user_token" "$MM_URL/api/v4/teams/name/$E2E_PROJECT_SLUG")
team_id=$(printf '%s' "$team_json" | grep -o '"id":"[^"]*"' | head -n 1 | cut -d '"' -f 4)
test -n "$team_id"
channel_json=$(curl -fsS -H "Authorization: Bearer $user_token" "$MM_URL/api/v4/teams/$team_id/channels/name/$E2E_CHAT_SLUG")
channel_id=$(printf '%s' "$channel_json" | grep -o '"id":"[^"]*"' | head -n 1 | cut -d '"' -f 4)
test -n "$channel_id"
echo "channel_id=$channel_id"

message=$(printf 'E2E request. Reply exactly: %s. Do not modify files. Do not create pull requests.' "$E2E_MARKER")
post_body=$(printf '{"channel_id":"%s","message":"%s"}' "$channel_id" "$(json_escape "$message")")
post_status=$(curl -sS -o /tmp/post.json -w '%{http_code}' -H "Authorization: Bearer $user_token" -H 'Content-Type: application/json' -d "$post_body" "$MM_URL/api/v4/posts" || true)
if [ "$post_status" != "201" ]; then
  echo "post_status=$post_status"
  cat /tmp/post.json
  exit 1
fi
root_post_id=$(grep -o '"id":"[^"]*"' /tmp/post.json | head -n 1 | cut -d '"' -f 4)
test -n "$root_post_id"
echo "root_post_id=$root_post_id"

deadline=$(( $(date +%s) + E2E_TIMEOUT_SECONDS ))
cleanup_domain() {
  if [ "${E2E_KEEP_DOMAIN:-false}" = "true" ]; then
    return
  fi
  curl -sS -X DELETE -H "Authorization: Bearer $MM_ADMIN_TOKEN" "$MM_URL/api/v4/teams/$team_id" >/dev/null || true
  curl -sS -X DELETE -H "Authorization: Bearer $MM_ADMIN_TOKEN" "$MM_URL/api/v4/users/$E2E_USER_ID" >/dev/null || true
  echo "mattermost_domain_cleanup=ok"
}

while [ "$(date +%s)" -lt "$deadline" ]; do
  curl -fsS -H "Authorization: Bearer $user_token" "$MM_URL/api/v4/posts/$root_post_id/thread" >/tmp/thread.json || true
  if tr '{' '\n' </tmp/thread.json | grep "\"root_id\":\"$root_post_id\"" | grep -q "$E2E_MARKER"; then
    echo "thread_final=ok"
    cleanup_domain
    exit 0
  fi
  if tr '{' '\n' </tmp/thread.json | grep "\"root_id\":\"$root_post_id\"" | grep -q 'chat.run.failed\|ошиб'; then
    echo "thread_failure_seen=true"
    cat /tmp/thread.json | head -c 2000
    exit 1
  fi
  sleep 10
done

echo "thread_final=timeout"
exit 1
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

REPOSITORY_ID="$(remote_psql "select id from matter_codex_repositories where provider = 'github' and owner = '$E2E_REPO_OWNER' and name = '$E2E_REPO_NAME' limit 1;")"
[ -n "$REPOSITORY_ID" ] || mattercodex_die "e2e repository github:$E2E_REPO_OWNER/$E2E_REPO_NAME не найден в базе"

mattercodex_log "live e2e: bind repository and create raw $E2E_ROLE_TYPE role"
script_repo_role_env="$(temp_file)"
sed \
  -e "1a export E2E_USER_ID=$(mattercodex_shell_quote "$E2E_USER_ID")" \
  -e "1a export E2E_PROJECT_ID=$(mattercodex_shell_quote "$PROJECT_ID")" \
  -e "1a export E2E_REPOSITORY_ID=$(mattercodex_shell_quote "$REPOSITORY_ID")" \
  "$script_repo_role" >"$script_repo_role_env"
capture_script_pod "repo-role" "$script_repo_role_env" repo_role_logs

ROLE_ID="$(remote_psql "select id from matter_codex_agent_roles where project_id = $PROJECT_ID and name = '$ROLE_NAME' limit 1;")"
[ -n "$ROLE_ID" ] || mattercodex_die "e2e role не найдена в базе"

mattercodex_log "live e2e: create chat, post owner message, wait for thread final answer"
script_chat_post_env="$(temp_file)"
sed \
  -e "1a export E2E_TIMEOUT_SECONDS=$(mattercodex_shell_quote "$TIMEOUT_SECONDS")" \
  -e "1a export E2E_USER_ID=$(mattercodex_shell_quote "$E2E_USER_ID")" \
  -e "1a export E2E_PROJECT_ID=$(mattercodex_shell_quote "$PROJECT_ID")" \
  -e "1a export E2E_ROLE_ID=$(mattercodex_shell_quote "$ROLE_ID")" \
  -e "1a export E2E_REPOSITORY_ID=$(mattercodex_shell_quote "$REPOSITORY_ID")" \
  "$script_chat_post" >"$script_chat_post_env"
capture_script_pod "chat-post" "$script_chat_post_env" chat_logs

CHANNEL_ID="$(printf '%s\n' "$chat_logs" | awk -F= '/^channel_id=/ {print $2}' | tail -n 1)"
ROOT_POST_ID="$(printf '%s\n' "$chat_logs" | awk -F= '/^root_post_id=/ {print $2}' | tail -n 1)"

RUN_INFO="$(remote_psql "select run_id || ' status=' || status || ' job=' || job_name || ' pvc=' || pvc_name from matter_codex_agent_runs where flow_id = 'chat-' || (select id::text from matter_codex_chats where project_id = $PROJECT_ID and slug = '$CHAT_SLUG' limit 1) order by created_at desc limit 1;")"
[ -n "$RUN_INFO" ] || mattercodex_die "e2e agent run не найден в базе"

if ! mattercodex_bool "$KEEP_DOMAIN"; then
  remote_psql "delete from matter_codex_projects where slug = '$PROJECT_SLUG';" >/dev/null
fi
if ! mattercodex_bool "$KEEP_RUNTIME"; then
  E2E_RUN_ID="${RUN_INFO%% *}"
  mattercodex_ssh "set -eu
    $REMOTE_KUBECTL -n $NAMESPACE_Q delete job \"mc-run-$E2E_RUN_ID\" --ignore-not-found >/dev/null 2>&1 || true
    $REMOTE_KUBECTL -n $NAMESPACE_Q delete pvc \"mc-ws-$E2E_RUN_ID\" --ignore-not-found >/dev/null 2>&1 || true
    $REMOTE_KUBECTL -n $NAMESPACE_Q delete configmap \"mc-prompt-$E2E_RUN_ID\" --ignore-not-found >/dev/null 2>&1 || true
  " </dev/null
fi

mattercodex_log "live e2e завершен"
printf 'project_id=%s\n' "$PROJECT_ID"
printf 'role_id=%s\n' "$ROLE_ID"
printf 'channel_id=%s\n' "${CHANNEL_ID:-unknown}"
printf 'root_post_id=%s\n' "${ROOT_POST_ID:-unknown}"
printf 'run=%s\n' "$RUN_INFO"
printf 'role_type=%s\n' "$E2E_ROLE_TYPE"
printf 'marker=%s\n' "$E2E_MARKER"
