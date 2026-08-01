#!/bin/sh
set -eu

for tool in cmp curl openssl pgrep; do
  command -v "$tool" >/dev/null || {
    echo "registry readiness tool is unavailable" >&2
    exit 1
  }
done

failures=0
while :; do
  rm -f /tmp/registry-ready
  tls_args=""
  curl_args=""
  if [ "${CLIENT_AUTH_REQUIRED:-false}" = true ]; then
    tls_args="-cert /identity/probe.crt -key /identity/probe.key"
    curl_args="--cert /identity/probe.crt --key /identity/probe.key"
  fi
  if openssl x509 -in /identity/tls.crt -checkend 900 -noout >/dev/null 2>&1 &&
    openssl s_client -connect "127.0.0.1:${TARGET_PORT}" \
      -servername "$SERVER_NAME" -verify_hostname "$SERVER_NAME" \
      -verify_return_error -CAfile /identity/ca.pem $tls_args </dev/null 2>/dev/null |
      openssl x509 -outform DER > /tmp/served.der 2>/dev/null &&
    openssl x509 -in /identity/tls.crt -outform DER > /tmp/mounted.der 2>/dev/null &&
    cmp -s /tmp/served.der /tmp/mounted.der; then
    if [ -n "${USERNAME_FILE:-}" ]; then
      curl --fail --silent --show-error --max-time 3 \
        --cacert /identity/ca.pem $curl_args \
        --resolve "${SERVER_NAME}:${TARGET_PORT}:127.0.0.1" \
        --user "$(tr -d '\r\n' <"$USERNAME_FILE"):$(tr -d '\r\n' <"$PASSWORD_FILE")" \
        "https://${SERVER_NAME}:${TARGET_PORT}/v2/" >/dev/null || {
          failures=$((failures + 1))
          sleep 10
          continue
        }
    fi
    touch /tmp/registry-ready
    failures=0
  else
    failures=$((failures + 1))
  fi
  if [ "$failures" -ge 3 ]; then
    # shareProcessNamespace и одинаковый uid позволяют закрыто перезапустить
    # только registry, чтобы он перечитал уже ротированный CSI certificate.
    registry_pid=$(pgrep -x registry | head -n 1 || true)
    [ -z "$registry_pid" ] || kill -TERM "$registry_pid"
    failures=0
  fi
  sleep 10
done
