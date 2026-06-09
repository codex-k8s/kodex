#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      ENV_FILE="$2"
      shift 2
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

mattercodex_load_env_file "$ENV_FILE"
mattercodex_validate_base_env
mattercodex_require_commands ssh base64 python3 openssl

NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"
MATTERMOST_POD="$(mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q get pod -l app.kubernetes.io/name=mattermost -o jsonpath='{.items[0].metadata.name}'")"
MATTERMOST_POD_Q="$(mattercodex_shell_quote "$MATTERMOST_POD")"

quote_args() {
  local arg
  for arg in "$@"; do
    mattercodex_shell_quote "$arg"
    printf ' '
  done
}

remote_mmctl() {
  local args
  args="$(quote_args "$@")"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec $MATTERMOST_POD_Q -c mattermost -- mmctl --local --suppress-warnings $args"
}

remote_psql_scalar() {
  local query_q
  query_q="$(mattercodex_shell_quote "$1")"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec statefulset/mattermost-postgres -- psql -U mattermost -d mattermost -Atc $query_q"
}

parse_mmctl_token() {
  python3 -c '
import json, os, re, sys
raw = os.environ["MMCTL_OUTPUT"]
try:
    data = json.loads(raw)
except json.JSONDecodeError:
    data = None

def find_token(obj):
    if isinstance(obj, dict):
        for key, value in obj.items():
            if key.lower() == "token" and isinstance(value, str) and value:
                return value
        for value in obj.values():
            found = find_token(value)
            if found:
                return found
    if isinstance(obj, list):
        for value in obj:
            found = find_token(value)
            if found:
                return found
    return ""

token = find_token(data) if data is not None else ""
if not token:
    match = re.search(r"(?i)token[^A-Za-z0-9_-]+([A-Za-z0-9_-]{20,})", raw)
    token = match.group(1) if match else ""
if not token:
    print("[matter-codex] ОШИБКА: mmctl token output не распознан", file=sys.stderr)
    raise SystemExit(1)
print(token)
'
}

ensure_personal_access_tokens_enabled() {
  local current
  current="$(remote_mmctl config get ServiceSettings.EnableUserAccessTokens 2>/dev/null | tail -n 1 | tr -d '\r')"
  if [ "$current" = "true" ]; then
    mattercodex_log "Mattermost personal access tokens: уже включены"
    return
  fi

  mattercodex_log "Mattermost personal access tokens: включаются"
  remote_mmctl config set ServiceSettings.EnableUserAccessTokens true >/dev/null
  remote_mmctl config reload >/dev/null
}

ensure_bot_user() {
  local user_count bot_count password
  user_count="$(remote_psql_scalar "select count(*) from users where username='${MATTERCODEX_MATTERMOST_BOT_USERNAME}';")"
  if [ "$user_count" = "0" ]; then
    mattercodex_log "Mattermost bot user: создается"
    password="$(mattercodex_generate_password)"
    remote_mmctl user create \
      --email "$MATTERCODEX_MATTERMOST_BOT_EMAIL" \
      --username "$MATTERCODEX_MATTERMOST_BOT_USERNAME" \
      --password "$password" \
      --email-verified \
      --disable-welcome-email >/dev/null
  else
    mattercodex_log "Mattermost bot user: уже существует"
  fi

  bot_count="$(remote_psql_scalar "select count(*) from bots b join users u on u.id=b.userid where u.username='${MATTERCODEX_MATTERMOST_BOT_USERNAME}' and b.deleteat=0;")"
  if [ "$bot_count" = "0" ]; then
    mattercodex_log "Mattermost bot user: конвертируется в bot"
    remote_mmctl user convert "$MATTERCODEX_MATTERMOST_BOT_USERNAME" --bot >/dev/null
  else
    mattercodex_log "Mattermost bot user: уже bot"
  fi
}

generate_bot_token() {
  local raw token
  mattercodex_log "Mattermost bot token: генерируется" >&2
  raw="$(remote_mmctl --json token generate "$MATTERCODEX_MATTERMOST_BOT_USERNAME" "matter-codex-bot-service-$(date -u +%Y%m%d%H%M%S)")"
  token="$(MMCTL_OUTPUT="$raw" parse_mmctl_token)"
  if [ -z "$token" ]; then
    mattercodex_die "Mattermost bot token не был получен"
  fi
  printf '%s' "$token"
}

ensure_team() {
  local team_count
  team_count="$(remote_psql_scalar "select count(*) from teams where name='${MATTERCODEX_DEFAULT_TEAM_NAME}' and deleteat=0;")"
  if [ "$team_count" = "0" ]; then
    mattercodex_log "Mattermost team ${MATTERCODEX_DEFAULT_TEAM_NAME}: создается"
    remote_mmctl team create --name "$MATTERCODEX_DEFAULT_TEAM_NAME" --display-name "$MATTERCODEX_DEFAULT_TEAM_DISPLAY_NAME" >/dev/null
  else
    mattercodex_log "Mattermost team ${MATTERCODEX_DEFAULT_TEAM_NAME}: уже существует"
  fi
}

ensure_bot_team_membership() {
  local member_count
  member_count="$(remote_psql_scalar "select count(*) from teammembers tm join teams t on t.id=tm.teamid join users u on u.id=tm.userid where t.name='${MATTERCODEX_DEFAULT_TEAM_NAME}' and u.username='${MATTERCODEX_MATTERMOST_BOT_USERNAME}' and tm.deleteat=0;")"
  if [ "$member_count" = "0" ]; then
    mattercodex_log "Mattermost bot user: добавляется в team"
    remote_mmctl team users add "$MATTERCODEX_DEFAULT_TEAM_NAME" "$MATTERCODEX_MATTERMOST_BOT_USERNAME" >/dev/null
  fi
}

ensure_channel() {
  local name="$1"
  local display_name="$2"
  local channel_count member_count
  channel_count="$(remote_psql_scalar "select count(*) from channels c join teams t on t.id=c.teamid where t.name='${MATTERCODEX_DEFAULT_TEAM_NAME}' and c.name='${name}' and c.deleteat=0;")"
  if [ "$channel_count" = "0" ]; then
    mattercodex_log "Mattermost channel ${name}: создается"
    remote_mmctl channel create --team "$MATTERCODEX_DEFAULT_TEAM_NAME" --name "$name" --display-name "$display_name" >/dev/null
  else
    mattercodex_log "Mattermost channel ${name}: уже существует"
  fi

  member_count="$(remote_psql_scalar "select count(*) from channelmembers cm join channels c on c.id=cm.channelid join teams t on t.id=c.teamid join users u on u.id=cm.userid where t.name='${MATTERCODEX_DEFAULT_TEAM_NAME}' and c.name='${name}' and u.username='${MATTERCODEX_MATTERMOST_BOT_USERNAME}';")"
  if [ "$member_count" = "0" ]; then
    remote_mmctl channel users add "${MATTERCODEX_DEFAULT_TEAM_NAME}:${name}" "$MATTERCODEX_MATTERMOST_BOT_USERNAME" >/dev/null
  fi
}

ensure_default_channels() {
  local item name display_name
  IFS=',' read -r -a items <<< "$MATTERCODEX_DEFAULT_CHANNELS"
  for item in "${items[@]}"; do
    name="${item%%:*}"
    display_name="${item#*:}"
    name="$(printf '%s' "$name" | sed 's/^ *//; s/ *$//')"
    display_name="$(printf '%s' "$display_name" | sed 's/^ *//; s/ *$//')"
    if [ -z "$name" ]; then
      continue
    fi
    if [ "$display_name" = "$item" ] || [ -z "$display_name" ]; then
      display_name="$name"
    fi
    ensure_channel "$name" "$display_name"
  done
}

ensure_slash_command() {
  local command_id callback_url
  callback_url="${MATTERCODEX_BOT_SERVICE_INTERNAL_URL%/}/mattermost/slash/agents"
  command_id="$(remote_psql_scalar "select c.id from commands c join teams t on t.id=c.teamid where t.name='${MATTERCODEX_DEFAULT_TEAM_NAME}' and c.trigger='agents' and c.deleteat=0 order by c.createat desc limit 1;")"
  if [ -z "$command_id" ]; then
    mattercodex_log "Mattermost slash command /agents: создается" >&2
    remote_mmctl command create "$MATTERCODEX_DEFAULT_TEAM_NAME" \
      --title "matter-codex agents" \
      --description "Matter Codex agent control command" \
      --trigger-word agents \
      --url "$callback_url" \
      --creator "$MATTERCODEX_MATTERMOST_BOT_USERNAME" \
      --response-username "$MATTERCODEX_MATTERMOST_BOT_USERNAME" \
      --autocomplete \
      --autocompleteDesc "Показать статус matter-codex" \
      --autocompleteHint "status" \
      --post >/dev/null
  else
    mattercodex_log "Mattermost slash command /agents: обновляется" >&2
    remote_mmctl command modify "$command_id" \
      --title "matter-codex agents" \
      --description "Matter Codex agent control command" \
      --trigger-word agents \
      --url "$callback_url" \
      --creator "$MATTERCODEX_MATTERMOST_BOT_USERNAME" \
      --response-username "$MATTERCODEX_MATTERMOST_BOT_USERNAME" \
      --autocomplete \
      --autocompleteDesc "Показать статус matter-codex" \
      --autocompleteHint "status" \
      --post >/dev/null
  fi

  remote_psql_scalar "select c.token from commands c join teams t on t.id=c.teamid where t.name='${MATTERCODEX_DEFAULT_TEAM_NAME}' and c.trigger='agents' and c.deleteat=0 order by c.createat desc limit 1;"
}

save_secret_and_restart() {
  local bot_token="$1"
  local slash_token="$2"
  local bot_token_b64 slash_token_b64
  bot_token_b64="$(printf '%s' "$bot_token" | base64 | tr -d '\n')"
  slash_token_b64="$(printf '%s' "$slash_token" | base64 | tr -d '\n')"

  mattercodex_log "bot-service tokens: сохраняются в Kubernetes Secret"
  cat <<EOF | mattercodex_remote_kubectl_apply_stdin "none"
apiVersion: v1
kind: Secret
metadata:
  name: ${MATTERCODEX_BOT_SERVICE_SECRET}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: bot-service-secret
type: Opaque
data:
  mattermost-bot-token: ${bot_token_b64}
  mattermost-slash-token: ${slash_token_b64}
EOF

  if mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q get deployment matter-codex-bot-service >/dev/null 2>&1"; then
    mattercodex_log "bot-service: перезапускается для применения Secret"
    mattercodex_ssh "set -eu
      $REMOTE_KUBECTL -n $NAMESPACE_Q rollout restart deployment/matter-codex-bot-service >/dev/null
      $REMOTE_KUBECTL -n $NAMESPACE_Q rollout status deployment/matter-codex-bot-service --timeout=180s >/dev/null"
  fi
}

ensure_personal_access_tokens_enabled
ensure_bot_user
BOT_TOKEN="$(generate_bot_token)"
ensure_team
ensure_bot_team_membership
ensure_default_channels
SLASH_TOKEN="$(ensure_slash_command)"
if [ -z "$SLASH_TOKEN" ]; then
  mattercodex_die "Mattermost slash token не был получен"
fi
save_secret_and_restart "$BOT_TOKEN" "$SLASH_TOKEN"

mattercodex_log "Mattermost bot bootstrap завершен"
