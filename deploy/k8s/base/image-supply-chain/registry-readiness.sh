#!/bin/sh
set -eu

for tool in base64 cmp curl jq openssl pgrep; do
  command -v "$tool" >/dev/null || {
    echo "registry readiness tool is unavailable" >&2
    exit 1
  }
done

read_pull_credentials() {
  [ -n "${DOCKER_CONFIG_FILE:-}" ] || return 1
  echo "${PULL_CREDENTIAL_GENERATION:-}" | grep -Eq '^[1-9][0-9]*$' || return 1
  [ -r "$DOCKER_CONFIG_FILE" ] || return 1
  auth=$(jq -er --arg host "$SERVER_NAME" '
    [.auths | to_entries[] |
      select(.key == $host or .key == ("https://" + $host)) |
      .value.auth] | if length == 1 then .[0] else error("ambiguous registry auth") end
  ' "$DOCKER_CONFIG_FILE") || return 1
  decoded=$(printf '%s' "$auth" | base64 -d 2>/dev/null) || return 1
  case "$decoded" in
    *:*) ;;
    *) return 1 ;;
  esac
  registry_username=${decoded%%:*}
  registry_password=${decoded#*:}
  [ -n "$registry_username" ] && [ -n "$registry_password" ] || return 1
  return 0
}

if [ "${1:-}" = validate-docker-config ]; then
  read_pull_credentials || {
    echo "registry pull credential is invalid" >&2
    exit 1
  }
  echo "registry pull credential is valid"
  exit 0
fi

certificate_failures=0
while :; do
  rm -f /tmp/registry-ready
  tls_args=""
  curl_args=""
  if [ "${CLIENT_AUTH_REQUIRED:-false}" = true ]; then
    tls_args="-cert /identity/probe.crt -key /identity/probe.key"
    curl_args="--cert /identity/probe.crt --key /identity/probe.key"
  fi
  mounted_certificate_valid=false
  served_certificate_available=false
  certificates_match=false
  if openssl x509 -in /identity/tls.crt -checkend 900 -noout >/dev/null 2>&1; then
    mounted_certificate_valid=true
  fi
  if [ "$mounted_certificate_valid" = true ] &&
    openssl s_client -connect "127.0.0.1:${TARGET_PORT}" \
      -servername "$SERVER_NAME" -verify_hostname "$SERVER_NAME" \
      -verify_return_error -CAfile /identity/ca.pem $tls_args </dev/null 2>/dev/null |
      openssl x509 -outform DER > /tmp/served.der 2>/dev/null; then
    served_certificate_available=true
  fi
  if [ "$served_certificate_available" = true ] &&
    openssl x509 -in /identity/tls.crt -outform DER > /tmp/mounted.der 2>/dev/null &&
    cmp -s /tmp/served.der /tmp/mounted.der; then
    certificates_match=true
    certificate_failures=0
  elif [ "$served_certificate_available" = true ]; then
    certificate_failures=$((certificate_failures + 1))
  fi

  if [ "$certificates_match" = true ]; then
    if [ -n "${DOCKER_CONFIG_FILE:-}" ]; then
      read_pull_credentials || {
        sleep 10
        continue
      }
      case "${READBACK_IMAGE:-}" in
        "$SERVER_NAME"/*@sha256:*) ;;
        *)
          sleep 10
          continue
          ;;
      esac
      readback_path=${READBACK_IMAGE#"$SERVER_NAME/"}
      readback_repository=${readback_path%@*}
      readback_digest=${readback_path##*@}
      echo "$readback_digest" | grep -Eq '^sha256:[a-f0-9]{64}$' || {
        sleep 10
        continue
      }
      umask 077
      printf 'machine %s login %s password %s\n' \
        "$SERVER_NAME" "$registry_username" "$registry_password" >/tmp/registry.netrc
      if ! curl --fail --silent --show-error --max-time 3 \
        --cacert /identity/ca.pem $curl_args \
        --resolve "${SERVER_NAME}:${TARGET_PORT}:127.0.0.1" \
        --netrc-file /tmp/registry.netrc \
        -H 'Accept: application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json' \
        -D /tmp/registry-headers -o /dev/null \
        "https://${SERVER_NAME}:${TARGET_PORT}/v2/${readback_repository}/manifests/${readback_digest}"; then
        rm -f /tmp/registry.netrc
        sleep 10
        continue
      fi
      rm -f /tmp/registry.netrc
      served_digest=$(awk 'tolower($1) == "docker-content-digest:" {gsub("\\r", "", $2); print $2}' /tmp/registry-headers)
      [ "$served_digest" = "$readback_digest" ] || {
        sleep 10
        continue
      }
    fi
    if [ -n "${USERNAME_FILE:-}" ]; then
      curl --fail --silent --show-error --max-time 3 \
        --cacert /identity/ca.pem $curl_args \
        --resolve "${SERVER_NAME}:${TARGET_PORT}:127.0.0.1" \
        --user "$(tr -d '\r\n' <"$USERNAME_FILE"):$(tr -d '\r\n' <"$PASSWORD_FILE")" \
        "https://${SERVER_NAME}:${TARGET_PORT}/v2/" >/dev/null || {
          sleep 10
          continue
        }
    fi
    touch /tmp/registry-ready
  fi
  if [ "$certificate_failures" -ge 3 ]; then
    # shareProcessNamespace и одинаковый uid позволяют закрыто перезапустить
    # только registry при доказанном drift обслуживаемого CSI certificate.
    # Неготовность backend, credential или readback artifact не является
    # основанием для убийства процесса: иначе startup превращается в restart loop.
    registry_pid=$(pgrep -x registry | head -n 1 || true)
    [ -z "$registry_pid" ] || kill -TERM "$registry_pid"
    certificate_failures=0
  fi
  sleep 10
done
