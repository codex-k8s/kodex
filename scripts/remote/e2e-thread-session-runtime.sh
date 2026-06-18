#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"
TIMEOUT_SECONDS=1200
KEEP=false

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
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

mattercodex_load_env_file "$ENV_FILE"
mattercodex_validate_base_env
mattercodex_require_commands ssh base64

RUN_SUFFIX="$(date +%Y%m%d%H%M%S)"
E2E_PREFIX="mc-e2e-session-$RUN_SUFFIX"
E2E_POD="$E2E_PREFIX-py"
E2E_USERNAME="mc-e2e-session-$RUN_SUFFIX"
E2E_EMAIL="$E2E_USERNAME@local.invalid"
set +o pipefail
E2E_PASSWORD="$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32)"
set -o pipefail
[ -n "$E2E_PASSWORD" ] || mattercodex_die "не удалось сгенерировать e2e пароль"

PROJECT_NAME="E2E Session Runtime $RUN_SUFFIX"
PROJECT_SLUG="e2e-session-$RUN_SUFFIX"
MANAGER_ROLE_NAME="e2e-manager-$RUN_SUFFIX"
WORKER_ROLE_NAME="e2e-worker-$RUN_SUFFIX"
REVIEWER_ROLE_NAME="e2e-reviewer-$RUN_SUFFIX"
CHAT_NAME="e2e-session-chat-$RUN_SUFFIX"
CHAT_SLUG="$CHAT_NAME"
E2E_MARKER="matter-codex session e2e $RUN_SUFFIX"
E2E_REPO_OWNER="${MATTERCODEX_E2E_REPO_OWNER:-codex-k8s}"
E2E_REPO_NAME="${MATTERCODEX_E2E_REPO_NAME:-matter-codex-e2e-sandbox}"
E2E_CODEX_MODEL="${MATTERCODEX_E2E_CODEX_MODEL:-gpt-5.3-codex-spark}"

NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
BOT_SECRET_Q="$(mattercodex_shell_quote "$MATTERCODEX_BOT_SERVICE_SECRET")"
POSTGRES_SECRET_Q="$(mattercodex_shell_quote "$MATTERCODEX_POSTGRES_SECRET")"
POSTGRES_DB_Q="$(mattercodex_shell_quote "$MATTERCODEX_POSTGRES_DB")"
POSTGRES_USER_Q="$(mattercodex_shell_quote "$MATTERCODEX_POSTGRES_USER")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

TEMP_FILES=()
PROJECT_ID=""
CHAT_ID=""
MANAGER_ROLE_ID=""
WORKER_ROLE_ID=""
REVIEWER_ROLE_ID=""

cleanup() {
  local file
  for file in "${TEMP_FILES[@]}"; do
    rm -f "$file"
  done
  if mattercodex_bool "$KEEP"; then
    return
  fi
  local session_keys
  session_keys=""
  if [ -n "${PROJECT_ID:-}" ]; then
    session_keys="$(remote_psql "select coalesce(string_agg(session_key, ' '), '') from matter_codex_agent_sessions where project_id = $PROJECT_ID;" 2>/dev/null || true)"
  fi
  mattercodex_ssh "set -eu
    SESSION_KEYS=$(mattercodex_shell_quote "$session_keys")
    $REMOTE_KUBECTL -n $NAMESPACE_Q delete pod $E2E_POD --ignore-not-found >/dev/null 2>&1 || true
    for session_key in \$SESSION_KEYS; do
      [ -n \"\$session_key\" ] || continue
      $REMOTE_KUBECTL -n $NAMESPACE_Q delete pod,pvc -l app.kubernetes.io/name=matter-codex-agent-runner,matter-codex.dev/session-key=\"\$session_key\" --ignore-not-found >/dev/null 2>&1 || true
      $REMOTE_KUBECTL -n $NAMESPACE_Q delete secret -l app.kubernetes.io/name=matter-codex-agent-runner,matter-codex.dev/session-key=\"\$session_key\" --ignore-not-found >/dev/null 2>&1 || true
    done
    for role_id in $(mattercodex_shell_quote "${MANAGER_ROLE_ID:-}") $(mattercodex_shell_quote "${WORKER_ROLE_ID:-}") $(mattercodex_shell_quote "${REVIEWER_ROLE_ID:-}"); do
      [ -n \"\$role_id\" ] || continue
      $REMOTE_KUBECTL -n $NAMESPACE_Q delete secret \"matter-codex-mm-bot-\$role_id\" --ignore-not-found >/dev/null 2>&1 || true
    done
  " </dev/null || true
  if [ -n "${PROJECT_ID:-}" ]; then
    remote_psql "delete from matter_codex_projects where id = $PROJECT_ID;" >/dev/null 2>&1 || true
  fi
  if [ -n "${PROJECT_ID:-}" ]; then
    cleanup_mm_domain >/dev/null 2>&1 || true
  fi
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q delete pod $E2E_POD --ignore-not-found >/dev/null 2>&1 || true" </dev/null || true
}

trap cleanup EXIT

temp_file() {
  local path
  path="$(mktemp)"
  TEMP_FILES+=("$path")
  printf '%s\n' "$path"
}

sql_literal() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/''/g")"
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

quote_args() {
  local arg
  for arg in "$@"; do
    mattercodex_shell_quote "$arg"
    printf ' '
  done
}

remote_mmctl() {
  local mattermost_pod
  local mattermost_pod_q
  local args
  mattermost_pod="$(mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q get pod -l app.kubernetes.io/name=mattermost -o jsonpath='{.items[0].metadata.name}'")"
  mattermost_pod_q="$(mattercodex_shell_quote "$mattermost_pod")"
  args="$(quote_args "$@")"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec $mattermost_pod_q -c mattermost -- mmctl --local --suppress-warnings $args"
}

ensure_e2e_user() {
  local username_sql
  local user_count
  username_sql="$(sql_literal "$E2E_USERNAME")"
  user_count="$(remote_psql "select count(*) from users where username = $username_sql;")"
  if [ "$user_count" = "0" ]; then
    mattercodex_log "live e2e session: create temporary Mattermost user through mmctl"
    remote_mmctl user create \
      --email "$E2E_EMAIL" \
      --username "$E2E_USERNAME" \
      --password "$E2E_PASSWORD" \
      --email-verified \
      --disable-welcome-email >/dev/null
  fi
}

ensure_test_pod() {
  local overrides
  local overrides_q
  overrides="$(printf '{"spec":{"containers":[{"name":"%s","image":"python:3.12-alpine","command":["sleep","7200"],"resources":{"requests":{"cpu":"25m","memory":"64Mi"},"limits":{"cpu":"100m","memory":"128Mi"}}}]}}' "$E2E_POD")"
  overrides_q="$(mattercodex_shell_quote "$overrides")"
  mattercodex_ssh "set -eu
    if ! $REMOTE_KUBECTL -n $NAMESPACE_Q get pod $E2E_POD >/dev/null 2>&1; then
      $REMOTE_KUBECTL -n $NAMESPACE_Q run $E2E_POD \
        --image=python:3.12-alpine \
        --restart=Never \
        --labels=app.kubernetes.io/name=matter-codex-e2e,app.kubernetes.io/instance=$E2E_PREFIX \
        --overrides=$overrides_q >/dev/null
    fi
    $REMOTE_KUBECTL -n $NAMESPACE_Q wait --for=condition=Ready pod/$E2E_POD --timeout=180s >/dev/null
  " </dev/null
}

copy_python() {
  local path="$1"
  local target="$2"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec -i $E2E_POD -- sh -c 'cat > $target'" < "$path"
}

run_python() {
  local target="$1"
  shift
  local extra_env="$*"
  local mm_url_q bot_url_q username_q email_q password_q project_name_q project_slug_q
  local chat_name_q chat_slug_q marker_q repo_owner_q repo_name_q model_q timeout_q
  mm_url_q="$(mattercodex_shell_quote "$MATTERCODEX_MATTERMOST_INTERNAL_URL")"
  bot_url_q="$(mattercodex_shell_quote "$MATTERCODEX_BOT_SERVICE_INTERNAL_URL")"
  username_q="$(mattercodex_shell_quote "$E2E_USERNAME")"
  email_q="$(mattercodex_shell_quote "$E2E_EMAIL")"
  password_q="$(mattercodex_shell_quote "$E2E_PASSWORD")"
  project_name_q="$(mattercodex_shell_quote "$PROJECT_NAME")"
  project_slug_q="$(mattercodex_shell_quote "$PROJECT_SLUG")"
  chat_name_q="$(mattercodex_shell_quote "$CHAT_NAME")"
  chat_slug_q="$(mattercodex_shell_quote "$CHAT_SLUG")"
  marker_q="$(mattercodex_shell_quote "$E2E_MARKER")"
  repo_owner_q="$(mattercodex_shell_quote "$E2E_REPO_OWNER")"
  repo_name_q="$(mattercodex_shell_quote "$E2E_REPO_NAME")"
  model_q="$(mattercodex_shell_quote "$E2E_CODEX_MODEL")"
  timeout_q="$(mattercodex_shell_quote "$TIMEOUT_SECONDS")"

  mattercodex_ssh "set -eu
    MM_BOT_TOKEN=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get secret $BOT_SECRET_Q -o jsonpath='{.data.mattermost-bot-token}' | base64 -d)\"
    MM_SLASH_TOKEN=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get secret $BOT_SECRET_Q -o jsonpath='{.data.mattermost-slash-token}' | base64 -d)\"
    $REMOTE_KUBECTL -n $NAMESPACE_Q exec $E2E_POD -- env \
      MM_URL=$mm_url_q \
      BOT_URL=$bot_url_q \
      MM_ADMIN_TOKEN=\"\$MM_BOT_TOKEN\" \
      MM_BOT_TOKEN=\"\$MM_BOT_TOKEN\" \
      MM_SLASH_TOKEN=\"\$MM_SLASH_TOKEN\" \
      E2E_USERNAME=$username_q \
      E2E_EMAIL=$email_q \
      E2E_PASSWORD=$password_q \
      E2E_PROJECT_NAME=$project_name_q \
      E2E_PROJECT_SLUG=$project_slug_q \
      E2E_MANAGER_ROLE_NAME=$(mattercodex_shell_quote "$MANAGER_ROLE_NAME") \
      E2E_WORKER_ROLE_NAME=$(mattercodex_shell_quote "$WORKER_ROLE_NAME") \
      E2E_REVIEWER_ROLE_NAME=$(mattercodex_shell_quote "$REVIEWER_ROLE_NAME") \
      E2E_CHAT_NAME=$chat_name_q \
      E2E_CHAT_SLUG=$chat_slug_q \
      E2E_MARKER=$marker_q \
      E2E_REPO_OWNER=$repo_owner_q \
      E2E_REPO_NAME=$repo_name_q \
      E2E_CODEX_MODEL=$model_q \
      E2E_TIMEOUT_SECONDS=$timeout_q \
      $extra_env \
      python $target
  " </dev/null
}

capture_python() {
  local output_var="$1"
  local target="$2"
  shift 2
  local logs
  local status
  set +e
  logs="$(run_python "$target" "$@" 2>&1)"
  status=$?
  set -e
  printf '%s\n' "$logs"
  printf -v "$output_var" '%s' "$logs"
  if [ "$status" -ne 0 ]; then
    mattercodex_die "e2e python шаг завершился с ошибкой: $target"
  fi
}

cleanup_mm_domain() {
  local cleanup_py
  cleanup_py="$(temp_file)"
  cat >"$cleanup_py" <<'PY'
import json
import os
import urllib.error
import urllib.request

MM_URL = os.environ["MM_URL"].rstrip("/")
ADMIN = os.environ["MM_ADMIN_TOKEN"]
PROJECT_SLUG = os.environ["E2E_PROJECT_SLUG"]
USER_ID = os.environ.get("E2E_USER_ID", "")
BOT_USER_IDS = [item for item in os.environ.get("E2E_BOT_USER_IDS", "").split(",") if item]

def request(method, path, payload=None):
    data = None
    headers = {"Authorization": "Bearer " + ADMIN}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(MM_URL + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            raw = response.read().decode("utf-8")
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError:
        return {}

team = request("GET", "/api/v4/teams/name/" + PROJECT_SLUG)
if team.get("id"):
    request("DELETE", "/api/v4/teams/" + team["id"])
for user_id in [USER_ID] + BOT_USER_IDS:
    if user_id:
        request("DELETE", "/api/v4/users/" + user_id)
print("mattermost_cleanup=ok")
PY
  ensure_test_pod
  copy_python "$cleanup_py" /tmp/mc-cleanup.py
  local bot_ids
  bot_ids="$(remote_psql "select coalesce(string_agg(mattermost_user_id, ','), '') from matter_codex_mattermost_bot_identities where project_id = ${PROJECT_ID:-0};" || true)"
  run_python /tmp/mc-cleanup.py \
    "E2E_USER_ID=$(mattercodex_shell_quote "${E2E_USER_ID:-}")" \
    "E2E_BOT_USER_IDS=$(mattercodex_shell_quote "$bot_ids")"
}

wait_for_sql_value() {
  local sql="$1"
  local expected="$2"
  local label="$3"
  local value
  for _ in $(seq 1 "$TIMEOUT_SECONDS"); do
    value="$(remote_psql "$sql" || true)"
    if [ "$value" = "$expected" ]; then
      return
    fi
    sleep 1
  done
  mattercodex_die "таймаут ожидания SQL $label: ожидалось $expected"
}

setup_py="$(temp_file)"
cat >"$setup_py" <<'PY'
import json
import os
import sys
import urllib.error
import urllib.request

MM_URL = os.environ["MM_URL"].rstrip("/")
BOT_URL = os.environ["BOT_URL"].rstrip("/")
ADMIN = os.environ["MM_ADMIN_TOKEN"]

def fail(label, detail=""):
    print("setup_failed=" + label)
    if detail:
        print(detail[:2000])
    sys.exit(1)

def request(method, url, payload=None, token=None, ok=(200, 201, 400, 403)):
    data = None
    headers = {}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=90) as response:
            raw = response.read().decode("utf-8")
            if response.status not in ok:
                fail("status_%s" % response.status, raw)
            return response.status, json.loads(raw) if raw else {}, response.headers
    except urllib.error.HTTPError as err:
        raw = err.read().decode("utf-8", "replace")
        if err.code not in ok:
            fail("http_%s_%s" % (err.code, url), raw)
        return err.code, json.loads(raw) if raw else {}, err.headers

def dialog_submit(callback_id, state, submission, user_id):
    _, body, _ = request("POST", BOT_URL + "/mattermost/dialogs/agents", {
        "callback_id": callback_id,
        "state": json.dumps(state, separators=(",", ":")),
        "user_id": user_id,
        "channel_id": "e2e-control",
        "submission": submission,
    }, ok=(200,))
    if body.get("type") != "ok":
        fail("dialog_" + callback_id, json.dumps(body, ensure_ascii=False))

status, body, headers = request("POST", MM_URL + "/api/v4/users/login", {
    "login_id": os.environ["E2E_USERNAME"],
    "password": os.environ["E2E_PASSWORD"],
}, ok=(200,))
if not headers.get("Token"):
    fail("login_token_missing")
user_id = body.get("id", "")
if not user_id:
    fail("user_id_missing")

state = {"view": "projects", "channel_id": "e2e-control", "post_id": "e2e-control", "user_name": os.environ["E2E_USERNAME"]}
dialog_submit("agents_project_upsert", state, {
    "project_name": os.environ["E2E_PROJECT_NAME"],
    "project_slug": os.environ["E2E_PROJECT_SLUG"],
    "description": "thread/session runtime e2e project",
    "advanced_settings": "{}",
}, user_id)

print("user_id=" + user_id)
print("project_dialog=ok")
PY

roles_py="$(temp_file)"
cat >"$roles_py" <<'PY'
import json
import os
import sys
import urllib.error
import urllib.request

BOT_URL = os.environ["BOT_URL"].rstrip("/")

def fail(label, detail=""):
    print("roles_failed=" + label)
    if detail:
        print(detail[:2000])
    sys.exit(1)

def request(method, url, payload):
    req = urllib.request.Request(url, data=json.dumps(payload).encode("utf-8"), method=method, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=120) as response:
            raw = response.read().decode("utf-8")
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as err:
        fail("http_%s" % err.code, err.read().decode("utf-8", "replace"))

def dialog(callback_id, submission):
    state = {"view": "roles", "channel_id": "e2e-control", "post_id": "e2e-control", "user_name": os.environ["E2E_USERNAME"]}
    body = request("POST", BOT_URL + "/mattermost/dialogs/agents", {
        "callback_id": callback_id,
        "state": json.dumps(state, separators=(",", ":")),
        "user_id": os.environ["E2E_USER_ID"],
        "channel_id": "e2e-control",
        "submission": submission,
    })
    if body.get("type") != "ok":
        fail(callback_id, json.dumps(body, ensure_ascii=False))

def role(name, role_type, github_account, prompt_template=""):
    prompt_mode = "template" if prompt_template else "raw"
    dialog("agents_agent_role_upsert", {
        "project_id": os.environ["E2E_PROJECT_ID"],
        "role": name,
        "role_type": role_type,
        "openai_account": "main",
        "github_account": github_account or "__none__",
        "prompt_mode": prompt_mode,
        "prompt_template": prompt_template,
        "kubernetes_access": "read-only",
        "sandbox_mode": "danger-full-access",
        "description": "thread/session runtime e2e " + role_type,
        "config_overlay": 'model = "' + os.environ["E2E_CODEX_MODEL"] + '"',
        "advanced_settings": '{"e2e":true}',
        "bot_identity": "",
    })

reviewer_prompt = """You are an e2e reviewer.

Language: {{.Locale.Language}}.
Use the user instruction as the source of truth.
If asked to reply with an exact marker, reply with that marker and no extra review.

User instruction:
{{.Task.Body}}
"""

role(os.environ["E2E_MANAGER_ROLE_NAME"], "manager", "")
role(os.environ["E2E_WORKER_ROLE_NAME"], "worker", "primary")
role(os.environ["E2E_REVIEWER_ROLE_NAME"], "reviewer", "primary", reviewer_prompt)
print("roles_dialog=ok")
PY

chat_posts_py="$(temp_file)"
cat >"$chat_posts_py" <<'PY'
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request

MM_URL = os.environ["MM_URL"].rstrip("/")
BOT_URL = os.environ["BOT_URL"].rstrip("/")
TIMEOUT = int(os.environ["E2E_TIMEOUT_SECONDS"])
MARKER = os.environ["E2E_MARKER"]

def fail(label, detail=""):
    print("chat_failed=" + label)
    if detail:
        print(detail[:2000])
    sys.exit(1)

def request(method, url, payload=None, token=None):
    data = None
    headers = {}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=90) as response:
            raw = response.read().decode("utf-8")
            return json.loads(raw) if raw else {}, response.headers
    except urllib.error.HTTPError as err:
        if err.code == 400 and url.endswith("/members"):
            return {}, err.headers
        fail("http_%s_%s" % (err.code, url), err.read().decode("utf-8", "replace"))

def login():
    _, headers = request("POST", MM_URL + "/api/v4/users/login", {
        "login_id": os.environ["E2E_USERNAME"],
        "password": os.environ["E2E_PASSWORD"],
    })
    token = headers.get("Token", "")
    if not token:
        fail("login_token_missing")
    return token

def dialog(callback_id, state, submission):
    body, _ = request("POST", BOT_URL + "/mattermost/dialogs/agents", {
        "callback_id": callback_id,
        "state": json.dumps(state, separators=(",", ":")),
        "user_id": os.environ["E2E_USER_ID"],
        "channel_id": "e2e-control",
        "submission": submission,
    })
    if body.get("type") != "ok":
        fail(callback_id, json.dumps(body, ensure_ascii=False))

def create_chat():
    state = {"view": "chats", "channel_id": "e2e-control", "post_id": "e2e-control", "user_name": os.environ["E2E_USERNAME"], "resource_type": "project", "resource_id": os.environ["E2E_PROJECT_ID"]}
    dialog("agents_chat_create", state, {
        "project_id": os.environ["E2E_PROJECT_ID"],
        "chat_name": os.environ["E2E_CHAT_NAME"],
        "chat_type": "multi_role_custom",
        "primary_role_id": os.environ["E2E_MANAGER_ROLE_ID"],
        "secondary_role_id": os.environ["E2E_WORKER_ROLE_ID"],
        "repository_id": os.environ["E2E_REPOSITORY_ID"],
        "root_issue": "",
        "work_policy": "Thread/session runtime e2e chat.",
    })

def get_team_channel(token):
    team, _ = request("GET", MM_URL + "/api/v4/teams/name/" + os.environ["E2E_PROJECT_SLUG"], token=token)
    if not team.get("id"):
        fail("team_missing")
    channel, _ = request("GET", MM_URL + "/api/v4/teams/" + team["id"] + "/channels/name/" + os.environ["E2E_CHAT_SLUG"], token=token)
    if not channel.get("id"):
        fail("channel_missing")
    return team["id"], channel["id"]

def ensure_team_member(team_id, user_id):
    if not user_id:
        return
    request("POST", MM_URL + "/api/v4/teams/" + team_id + "/members", {"team_id": team_id, "user_id": user_id}, token=os.environ["MM_ADMIN_TOKEN"])

def ensure_channel_member(channel_id, user_id):
    if not user_id:
        return
    request("POST", MM_URL + "/api/v4/channels/" + channel_id + "/members", {"user_id": user_id}, token=os.environ["MM_ADMIN_TOKEN"])

def post(channel_id, message, token, root_id=""):
    payload = {"channel_id": channel_id, "message": message}
    if root_id:
        payload["root_id"] = root_id
    body, _ = request("POST", MM_URL + "/api/v4/posts", payload, token=token)
    if not body.get("id"):
        fail("post_id_missing")
    return body["id"]

def thread_text(root_id, token):
    body, _ = request("GET", MM_URL + "/api/v4/posts/" + root_id + "/thread", token=token)
    posts = list(body.get("posts", {}).values())
    posts.sort(key=lambda item: item.get("create_at", 0))
    return "\n".join(item.get("message", "") for item in posts if item.get("root_id") == root_id)

def wait_thread(root_id, token, expected, label):
    deadline = time.time() + TIMEOUT
    while time.time() < deadline:
        text = thread_text(root_id, token)
        if expected in text:
            return text
        if "runner error" in text or "failed" in text.lower() or "ошиб" in text.lower():
            fail(label + "_failed", text)
        time.sleep(8)
    fail(label + "_timeout", thread_text(root_id, token))

def extract_run(text, label):
    found = re.findall(r"run: `([^`]+)`", text)
    if not found:
        fail(label + "_run_missing", text)
    return found[-1]

token = login()
create_chat()
team_id, channel_id = get_team_channel(token)
ensure_team_member(team_id, os.environ.get("E2E_REVIEWER_BOT_USER_ID", ""))
ensure_channel_member(channel_id, os.environ.get("E2E_REVIEWER_BOT_USER_ID", ""))

manager_root_1 = post(channel_id, "E2E manager turn 1. Reply exactly: " + MARKER + " manager-one", token)
manager_text_1 = wait_thread(manager_root_1, token, MARKER + " manager-one", "manager_one")
manager_run_1 = extract_run(manager_text_1, "manager_one")

manager_root_2 = post(channel_id, "E2E manager turn 2. Continue the same Codex session and reply exactly: " + MARKER + " manager-two", token)
manager_text_2 = wait_thread(manager_root_2, token, MARKER + " manager-two", "manager_two")
manager_run_2 = extract_run(manager_text_2, "manager_two")

manager_mention = "@" + os.environ["E2E_MANAGER_BOT_USERNAME"]
worker_mention = "@" + os.environ["E2E_WORKER_BOT_USERNAME"]
worker_root = post(channel_id, worker_mention + " E2E worker turn. Run `gh auth status` if available, then reply with two lines: " + MARKER + " worker-one and gh-auth-ok", token)
worker_text = wait_thread(worker_root, token, MARKER + " worker-one", "worker_one")
if "gh-auth-ok" not in worker_text:
    fail("worker_gh_auth_missing", worker_text)
worker_run_1 = extract_run(worker_text, "worker_one")

fifo_a = post(channel_id, "E2E FIFO A. Reply exactly: " + MARKER + " fifo-a", token, worker_root)
fifo_b = post(channel_id, "E2E FIFO B. Reply exactly: " + MARKER + " fifo-b", token, worker_root)
wait_thread(worker_root, token, MARKER + " fifo-a", "fifo_a")
wait_thread(worker_root, token, MARKER + " fifo-b", "fifo_b")

multi_root = post(channel_id, manager_mention + " Reply exactly: " + MARKER + " multi-manager\n" + worker_mention + " Reply exactly: " + MARKER + " multi-worker", token)
multi_text = wait_thread(multi_root, token, MARKER + " multi-worker", "multi_worker")
wait_thread(multi_root, token, MARKER + " multi-manager", "multi_manager")
multi_runs = re.findall(r"run: `([^`]+)`", multi_text)

bot_post = post(channel_id, "E2E role bot self-message; this must not create a turn.", os.environ["E2E_MANAGER_BOT_TOKEN"])
time.sleep(20)

print("channel_id=" + channel_id)
print("manager_root_1=" + manager_root_1)
print("manager_root_2=" + manager_root_2)
print("worker_root=" + worker_root)
print("fifo_a_post=" + fifo_a)
print("fifo_b_post=" + fifo_b)
print("multi_root=" + multi_root)
print("bot_post=" + bot_post)
print("manager_run_1=" + manager_run_1)
print("manager_run_2=" + manager_run_2)
print("worker_run_1=" + worker_run_1)
print("multi_runs=" + ",".join(sorted(set(multi_runs))))
print("chat_posts=ok")
PY

mcp_py="$(temp_file)"
cat >"$mcp_py" <<'PY'
import json
import os
import sys
import time
import urllib.error
import urllib.request

BOT_URL = os.environ["BOT_URL"].rstrip("/")
MM_URL = os.environ["MM_URL"].rstrip("/")
SESSION_KEY = os.environ["E2E_SESSION_KEY"]
SESSION_TOKEN = os.environ["E2E_SESSION_TOKEN"]
MARKER = os.environ["E2E_MARKER"]
TIMEOUT = int(os.environ["E2E_TIMEOUT_SECONDS"])

def fail(label, detail=""):
    print("mcp_failed=" + label)
    if detail:
        print(detail[:2000])
    sys.exit(1)

def parse_rpc(raw, content_type):
    if "text/event-stream" in content_type:
        for line in raw.splitlines():
            line = line.strip()
            if line.startswith("data:"):
                data = line[5:].strip()
                if data:
                    return json.loads(data)
        return {}
    return json.loads(raw) if raw else {}

class MCP:
    def __init__(self):
        self.url = BOT_URL + "/mcp/sessions/" + SESSION_KEY
        self.session_id = ""
        self.next_id = 1
        self.call("initialize", {
            "protocolVersion": "2025-06-18",
            "capabilities": {},
            "clientInfo": {"name": "matter-codex-e2e", "version": "0"},
        }, initialize=True)
        self.notify("notifications/initialized", {})

    def post(self, payload, expect_response=True):
        headers = {
            "Authorization": "Bearer " + SESSION_TOKEN,
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        }
        if self.session_id:
            headers["Mcp-Session-Id"] = self.session_id
        req = urllib.request.Request(self.url, data=json.dumps(payload).encode("utf-8"), method="POST", headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=90) as response:
                if response.headers.get("Mcp-Session-Id"):
                    self.session_id = response.headers["Mcp-Session-Id"]
                raw = response.read().decode("utf-8")
                if not expect_response:
                    return {}
                return parse_rpc(raw, response.headers.get("Content-Type", ""))
        except urllib.error.HTTPError as err:
            fail("http_%s" % err.code, err.read().decode("utf-8", "replace"))

    def call(self, method, params, initialize=False):
        payload = {"jsonrpc": "2.0", "id": self.next_id, "method": method, "params": params}
        self.next_id += 1
        body = self.post(payload)
        if "error" in body:
            fail(method + "_error", json.dumps(body, ensure_ascii=False))
        if initialize and not self.session_id:
            self.session_id = ""
        return body.get("result", {})

    def notify(self, method, params):
        self.post({"jsonrpc": "2.0", "method": method, "params": params}, expect_response=False)

    def tool(self, name, arguments):
        result = self.call("tools/call", {"name": name, "arguments": arguments})
        if result.get("isError"):
            fail(name + "_tool_error", json.dumps(result, ensure_ascii=False))
        return result

def request(method, url, payload=None, token=None):
    data = None
    headers = {}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=90) as response:
            raw = response.read().decode("utf-8")
            return json.loads(raw) if raw else {}, response.headers
    except urllib.error.HTTPError as err:
        fail("mm_http_%s" % err.code, err.read().decode("utf-8", "replace"))

def login():
    _, headers = request("POST", MM_URL + "/api/v4/users/login", {
        "login_id": os.environ["E2E_USERNAME"],
        "password": os.environ["E2E_PASSWORD"],
    })
    token = headers.get("Token", "")
    if not token:
        fail("login_token_missing")
    return token

def thread_text(root_id, token):
    body, _ = request("GET", MM_URL + "/api/v4/posts/" + root_id + "/thread", token=token)
    posts = list(body.get("posts", {}).values())
    posts.sort(key=lambda item: item.get("create_at", 0))
    return "\n".join(item.get("message", "") for item in posts if item.get("root_id") == root_id)

def wait_thread(root_id, token, expected, label):
    deadline = time.time() + TIMEOUT
    while time.time() < deadline:
        text = thread_text(root_id, token)
        if expected in text:
            return text
        if "runner error" in text or "failed" in text.lower() or "ошиб" in text.lower():
            fail(label + "_failed", text)
        time.sleep(8)
    fail(label + "_timeout", thread_text(root_id, token))

client = MCP()
history = client.tool("mattermost_get_thread", {"limit": 10})
if "content" not in history and "structuredContent" not in history:
    fail("history_shape", json.dumps(history, ensure_ascii=False))
search = client.tool("mattermost_search_chat", {"query": MARKER, "limit": 10})
if "content" not in search and "structuredContent" not in search:
    fail("search_shape", json.dumps(search, ensure_ascii=False))
client.tool("mattermost_post_thread_update", {"message": MARKER + " mcp-update"})
request_result = client.tool("mattermost_request_agent", {
    "target_agent": os.environ["E2E_REVIEWER_ROLE_NAME"],
    "message": "E2E delegated reviewer request. Reply exactly: " + MARKER + " delegated-reviewer",
})
token = login()
wait_thread(os.environ["E2E_MANAGER_ROOT_2"], token, MARKER + " mcp-update", "mcp_update")
wait_thread(os.environ["E2E_MANAGER_ROOT_2"], token, MARKER + " delegated-reviewer", "mcp_request_agent")
print("mcp_request=" + json.dumps(request_result, ensure_ascii=False, sort_keys=True))
print("mcp_tools=ok")
PY

mattercodex_log "live e2e session: preflight"
bot_admin="$(remote_psql "select case when position('system_admin' in roles) > 0 then 'yes' else 'no' end from users where username = $(sql_literal "$MATTERCODEX_MATTERMOST_BOT_USERNAME");")"
[ "$bot_admin" = "yes" ] || mattercodex_die "Mattermost bot user должен иметь system_admin для role bot identities"

goose_version="$(remote_psql "select max(version_id) from goose_db_version where is_applied = true;")"
[ "$goose_version" = "14" ] || mattercodex_die "goose version должен быть 14, сейчас $goose_version"
mattercodex_ssh "set -eu
  $REMOTE_KUBECTL -n $NAMESPACE_Q get secret matter-codex-codex-auth-main >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q auth can-i create pods --as=system:serviceaccount:$MATTERCODEX_NAMESPACE:matter-codex-bot-service | grep -qx yes
  $REMOTE_KUBECTL -n $NAMESPACE_Q auth can-i delete pods --as=system:serviceaccount:$MATTERCODEX_NAMESPACE:matter-codex-bot-service | grep -qx yes
" </dev/null

ensure_e2e_user
ensure_test_pod
copy_python "$setup_py" /tmp/mc-setup.py
capture_python setup_logs /tmp/mc-setup.py
E2E_USER_ID="$(printf '%s\n' "$setup_logs" | awk -F= '/^user_id=/ {print $2}' | tail -n 1)"
[ -n "$E2E_USER_ID" ] || mattercodex_die "e2e user_id не найден"

PROJECT_ID="$(remote_psql "select id from matter_codex_projects where slug = $(sql_literal "$PROJECT_SLUG") limit 1;")"
[ -n "$PROJECT_ID" ] || mattercodex_die "project не найден после создания"

mattercodex_log "live e2e session: seed accounts/repository"
REPOSITORY_ID="$(remote_psql "
with openai_credential as (
  insert into matter_codex_credentials(name, credential_type, provider, secret_ref, status)
  values ('openai:main', 'codex_auth', 'openai', 'matter-codex-codex-auth-main', 'authorized')
  on conflict (name) do update set secret_ref = excluded.secret_ref, status = excluded.status, updated_at = now()
  returning id
), openai_account as (
  insert into matter_codex_openai_accounts(name, credential_id, status)
  values ('main', (select id from openai_credential), 'authorized')
  on conflict (name) do update set credential_id = excluded.credential_id, status = excluded.status, updated_at = now()
), github_credential as (
  insert into matter_codex_credentials(name, credential_type, provider, secret_ref, status)
  values ('github:primary', 'github_token', 'github', 'matter-codex-github', 'configured')
  on conflict (name) do update set secret_ref = excluded.secret_ref, status = excluded.status, updated_at = now()
  returning id
), github_account as (
  insert into matter_codex_github_accounts(name, credential_id, secret_ref, status)
  values ('primary', (select id from github_credential), 'matter-codex-github', 'configured')
  on conflict (name) do update set credential_id = excluded.credential_id, secret_ref = excluded.secret_ref, status = excluded.status, updated_at = now()
), repo as (
  insert into matter_codex_repositories(provider, owner, name, default_branch, github_account_name, status)
  values ('github', $(sql_literal "$E2E_REPO_OWNER"), $(sql_literal "$E2E_REPO_NAME"), 'main', 'primary', 'active')
  on conflict (provider, owner, name) do update set github_account_name = excluded.github_account_name, status = excluded.status, updated_at = now()
  returning id
), project_repo as (
  insert into matter_codex_project_repositories(project_id, repository_id, is_default)
  values ($PROJECT_ID, (select id from repo), true)
  on conflict (project_id, repository_id) do update set is_default = true, updated_at = now()
)
select id from repo;
")"
[ -n "$REPOSITORY_ID" ] || mattercodex_die "repository id не получен"

copy_python "$roles_py" /tmp/mc-roles.py
capture_python role_logs /tmp/mc-roles.py \
  "E2E_USER_ID=$(mattercodex_shell_quote "$E2E_USER_ID")" \
  "E2E_PROJECT_ID=$(mattercodex_shell_quote "$PROJECT_ID")"

MANAGER_ROLE_ID="$(remote_psql "select id from matter_codex_agent_roles where project_id = $PROJECT_ID and name = $(sql_literal "$MANAGER_ROLE_NAME") limit 1;")"
WORKER_ROLE_ID="$(remote_psql "select id from matter_codex_agent_roles where project_id = $PROJECT_ID and name = $(sql_literal "$WORKER_ROLE_NAME") limit 1;")"
REVIEWER_ROLE_ID="$(remote_psql "select id from matter_codex_agent_roles where project_id = $PROJECT_ID and name = $(sql_literal "$REVIEWER_ROLE_NAME") limit 1;")"
[ -n "$MANAGER_ROLE_ID" ] && [ -n "$WORKER_ROLE_ID" ] && [ -n "$REVIEWER_ROLE_ID" ] || mattercodex_die "не все роли созданы"

MANAGER_BOT_USERNAME="$(remote_psql "select username from matter_codex_mattermost_bot_identities where role_id = $MANAGER_ROLE_ID;")"
WORKER_BOT_USERNAME="$(remote_psql "select username from matter_codex_mattermost_bot_identities where role_id = $WORKER_ROLE_ID;")"
REVIEWER_BOT_USERNAME="$(remote_psql "select username from matter_codex_mattermost_bot_identities where role_id = $REVIEWER_ROLE_ID;")"
REVIEWER_BOT_USER_ID="$(remote_psql "select mattermost_user_id from matter_codex_mattermost_bot_identities where role_id = $REVIEWER_ROLE_ID;")"
[ -n "$MANAGER_BOT_USERNAME" ] && [ -n "$WORKER_BOT_USERNAME" ] && [ -n "$REVIEWER_BOT_USERNAME" ] || mattercodex_die "role bot usernames не найдены"

copy_python "$chat_posts_py" /tmp/mc-chat-posts.py
set +e
chat_logs="$(mattercodex_ssh "set -eu
  MM_BOT_TOKEN=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get secret $BOT_SECRET_Q -o jsonpath='{.data.mattermost-bot-token}' | base64 -d)\"
  MANAGER_BOT_TOKEN=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get secret matter-codex-mm-bot-$MANAGER_ROLE_ID -o jsonpath='{.data.token}' | base64 -d)\"
  $REMOTE_KUBECTL -n $NAMESPACE_Q exec $E2E_POD -- env \
    MM_URL=$(mattercodex_shell_quote "$MATTERCODEX_MATTERMOST_INTERNAL_URL") \
    BOT_URL=$(mattercodex_shell_quote "$MATTERCODEX_BOT_SERVICE_INTERNAL_URL") \
    MM_ADMIN_TOKEN=\"\$MM_BOT_TOKEN\" \
    E2E_USERNAME=$(mattercodex_shell_quote "$E2E_USERNAME") \
    E2E_PASSWORD=$(mattercodex_shell_quote "$E2E_PASSWORD") \
    E2E_USER_ID=$(mattercodex_shell_quote "$E2E_USER_ID") \
    E2E_PROJECT_ID=$(mattercodex_shell_quote "$PROJECT_ID") \
    E2E_PROJECT_SLUG=$(mattercodex_shell_quote "$PROJECT_SLUG") \
    E2E_CHAT_NAME=$(mattercodex_shell_quote "$CHAT_NAME") \
    E2E_CHAT_SLUG=$(mattercodex_shell_quote "$CHAT_SLUG") \
    E2E_REPOSITORY_ID=$(mattercodex_shell_quote "$REPOSITORY_ID") \
    E2E_MANAGER_ROLE_ID=$(mattercodex_shell_quote "$MANAGER_ROLE_ID") \
    E2E_WORKER_ROLE_ID=$(mattercodex_shell_quote "$WORKER_ROLE_ID") \
    E2E_REVIEWER_ROLE_ID=$(mattercodex_shell_quote "$REVIEWER_ROLE_ID") \
    E2E_MANAGER_BOT_USERNAME=$(mattercodex_shell_quote "$MANAGER_BOT_USERNAME") \
    E2E_WORKER_BOT_USERNAME=$(mattercodex_shell_quote "$WORKER_BOT_USERNAME") \
    E2E_REVIEWER_BOT_USERNAME=$(mattercodex_shell_quote "$REVIEWER_BOT_USERNAME") \
    E2E_REVIEWER_BOT_USER_ID=$(mattercodex_shell_quote "$REVIEWER_BOT_USER_ID") \
    E2E_MANAGER_BOT_TOKEN=\"\$MANAGER_BOT_TOKEN\" \
    E2E_MARKER=$(mattercodex_shell_quote "$E2E_MARKER") \
    E2E_TIMEOUT_SECONDS=$(mattercodex_shell_quote "$TIMEOUT_SECONDS") \
    python /tmp/mc-chat-posts.py
" </dev/null 2>&1)"
chat_status=$?
set -e
printf '%s\n' "$chat_logs"
if [ "$chat_status" -ne 0 ]; then
  mattercodex_die "e2e chat-posts шаг завершился с ошибкой"
fi

CHANNEL_ID="$(printf '%s\n' "$chat_logs" | awk -F= '/^channel_id=/ {print $2}' | tail -n 1)"
MANAGER_ROOT_1="$(printf '%s\n' "$chat_logs" | awk -F= '/^manager_root_1=/ {print $2}' | tail -n 1)"
MANAGER_ROOT_2="$(printf '%s\n' "$chat_logs" | awk -F= '/^manager_root_2=/ {print $2}' | tail -n 1)"
WORKER_ROOT="$(printf '%s\n' "$chat_logs" | awk -F= '/^worker_root=/ {print $2}' | tail -n 1)"
MULTI_ROOT="$(printf '%s\n' "$chat_logs" | awk -F= '/^multi_root=/ {print $2}' | tail -n 1)"
BOT_POST="$(printf '%s\n' "$chat_logs" | awk -F= '/^bot_post=/ {print $2}' | tail -n 1)"
[ -n "$CHANNEL_ID" ] && [ -n "$MANAGER_ROOT_2" ] && [ -n "$WORKER_ROOT" ] || mattercodex_die "chat output неполный"

CHAT_ID="$(remote_psql "select id from matter_codex_chats where project_id = $PROJECT_ID and mattermost_channel_id = $(sql_literal "$CHANNEL_ID") limit 1;")"
[ -n "$CHAT_ID" ] || mattercodex_die "chat не найден в базе"

MANAGER_SESSION_KEY="$(remote_psql "select session_key from matter_codex_agent_sessions where chat_id = $CHAT_ID and role_id = $MANAGER_ROLE_ID and session_scope = 'chat_default' limit 1;")"
WORKER_SESSION_KEY="$(remote_psql "select session_key from matter_codex_agent_sessions where chat_id = $CHAT_ID and role_id = $WORKER_ROLE_ID and session_scope = 'thread_role' order by created_at limit 1;")"
[ -n "$MANAGER_SESSION_KEY" ] && [ -n "$WORKER_SESSION_KEY" ] || mattercodex_die "session keys не найдены"

MANAGER_TOKEN_SECRET="$(remote_psql "select token_secret_ref from matter_codex_agent_sessions where session_key = $(sql_literal "$MANAGER_SESSION_KEY");")"
[ -n "$MANAGER_TOKEN_SECRET" ] || mattercodex_die "manager session token secret не найден"

mattercodex_log "live e2e session: assert database/kubernetes state"
remote_psql "select count(*) from matter_codex_agent_sessions where project_id = $PROJECT_ID and session_archive_gzip_base64 <> '';" | grep -Eq '^[1-9][0-9]*$' || mattercodex_die "session snapshot не сохранен"
remote_psql "select count(*) from matter_codex_agent_session_turns t join matter_codex_agent_sessions s on s.id = t.session_id where s.project_id = $PROJECT_ID and t.status = 'succeeded';" | grep -Eq '^[1-9][0-9]*$' || mattercodex_die "нет succeeded session turns"
remote_psql "select count(*) from matter_codex_agent_session_turns where mattermost_post_id = $(sql_literal "$BOT_POST");" | grep -qx '0' || mattercodex_die "сообщение role bot создало turn"
remote_psql "select count(*) from matter_codex_agent_session_turns t join matter_codex_agent_sessions s on s.id = t.session_id where s.project_id = $PROJECT_ID and t.mattermost_root_post_id = $(sql_literal "$MULTI_ROOT");" | grep -Eq '^[2-9][0-9]*$' || mattercodex_die "multi-agent root не создал минимум два turns"

manager_ttl="$(remote_psql "select ttl_seconds from matter_codex_agent_sessions where session_key = $(sql_literal "$MANAGER_SESSION_KEY");")"
worker_ttl="$(remote_psql "select ttl_seconds from matter_codex_agent_sessions where session_key = $(sql_literal "$WORKER_SESSION_KEY");")"
[ "$manager_ttl" = "604800" ] || mattercodex_die "manager ttl ожидался 604800, сейчас $manager_ttl"
[ "$worker_ttl" = "259200" ] || mattercodex_die "worker ttl ожидался 259200, сейчас $worker_ttl"

mattercodex_ssh "set -eu
  $REMOTE_KUBECTL -n $NAMESPACE_Q get pod -l matter-codex.dev/session-key=$(mattercodex_shell_quote "$MANAGER_SESSION_KEY") >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get pvc -l matter-codex.dev/session-key=$(mattercodex_shell_quote "$MANAGER_SESSION_KEY") >/dev/null
  pod=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get pod -l matter-codex.dev/session-key=$(mattercodex_shell_quote "$MANAGER_SESSION_KEY") -o jsonpath='{.items[0].metadata.name}')\"
  $REMOTE_KUBECTL -n $NAMESPACE_Q get pod \"\$pod\" -o jsonpath='{.spec.containers[0].env[*].name}' | grep -q MATTERCODEX_MCP_URL
  if $REMOTE_KUBECTL -n $NAMESPACE_Q get pod \"\$pod\" -o jsonpath='{.spec.containers[0].env[*].name}' | grep -q OPENAI_API_KEY; then
    echo 'OPENAI_API_KEY unexpectedly present'
    exit 1
  fi
" </dev/null

mattercodex_log "live e2e session: delete manager pod and verify resume/recreate path"
mattercodex_ssh "set -eu
  pod=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get pod -l matter-codex.dev/session-key=$(mattercodex_shell_quote "$MANAGER_SESSION_KEY") -o jsonpath='{.items[0].metadata.name}')\"
  $REMOTE_KUBECTL -n $NAMESPACE_Q delete pod \"\$pod\" --wait=true --timeout=60s >/dev/null
" </dev/null

copy_python "$mcp_py" /tmp/mc-mcp.py
set +e
mcp_logs="$(mattercodex_ssh "set -eu
  SESSION_TOKEN=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get secret $(mattercodex_shell_quote "$MANAGER_TOKEN_SECRET") -o jsonpath='{.data.token}' | base64 -d)\"
  $REMOTE_KUBECTL -n $NAMESPACE_Q exec $E2E_POD -- env \
    MM_URL=$(mattercodex_shell_quote "$MATTERCODEX_MATTERMOST_INTERNAL_URL") \
    BOT_URL=$(mattercodex_shell_quote "$MATTERCODEX_BOT_SERVICE_INTERNAL_URL") \
    E2E_USERNAME=$(mattercodex_shell_quote "$E2E_USERNAME") \
    E2E_PASSWORD=$(mattercodex_shell_quote "$E2E_PASSWORD") \
    E2E_SESSION_KEY=$(mattercodex_shell_quote "$MANAGER_SESSION_KEY") \
    E2E_SESSION_TOKEN=\"\$SESSION_TOKEN\" \
    E2E_MANAGER_ROOT_2=$(mattercodex_shell_quote "$MANAGER_ROOT_2") \
    E2E_REVIEWER_ROLE_NAME=$(mattercodex_shell_quote "$REVIEWER_ROLE_NAME") \
    E2E_MARKER=$(mattercodex_shell_quote "$E2E_MARKER") \
    E2E_TIMEOUT_SECONDS=$(mattercodex_shell_quote "$TIMEOUT_SECONDS") \
    python /tmp/mc-mcp.py
" </dev/null 2>&1)"
mcp_status=$?
set -e
printf '%s\n' "$mcp_logs"
if [ "$mcp_status" -ne 0 ]; then
  mattercodex_die "e2e MCP шаг завершился с ошибкой"
fi

wait_for_sql_value "select count(*) from matter_codex_agent_session_turns t join matter_codex_agent_sessions s on s.id = t.session_id where s.project_id = $PROJECT_ID and t.message like '%delegated-reviewer%' and t.status = 'succeeded';" "1" "delegated reviewer turn"

mattercodex_log "live e2e session: final readback"
summary="$(remote_psql "select 'sessions=' || count(*) || ' turns=' || (select count(*) from matter_codex_agent_session_turns t join matter_codex_agent_sessions s on s.id = t.session_id where s.project_id = $PROJECT_ID) from matter_codex_agent_sessions where project_id = $PROJECT_ID;")"
printf 'project_id=%s\n' "$PROJECT_ID"
printf 'chat_id=%s\n' "$CHAT_ID"
printf 'channel_id=%s\n' "$CHANNEL_ID"
printf 'manager_session_key=%s\n' "$MANAGER_SESSION_KEY"
printf 'worker_session_key=%s\n' "$WORKER_SESSION_KEY"
printf 'manager_root_1=%s\n' "$MANAGER_ROOT_1"
printf 'manager_root_2=%s\n' "$MANAGER_ROOT_2"
printf 'worker_root=%s\n' "$WORKER_ROOT"
printf 'multi_root=%s\n' "$MULTI_ROOT"
printf 'summary=%s\n' "$summary"
printf 'marker=%s\n' "$E2E_MARKER"
mattercodex_log "live e2e session завершен"
