#!/bin/sh
set -eu

tls_digest() {
  sha256sum \
    /var/run/secrets/kodex/staff-control-center/backend-tls/tls.crt \
    /var/run/secrets/kodex/staff-control-center/backend-tls/tls.key \
    /var/run/secrets/kodex/staff-control-center/ingress-client-tls/ca.crt |
    sha256sum | awk '{print $1}'
}

nginx -t
served_digest=$(tls_digest)
nginx -g 'daemon off;' &
nginx_pid=$!

terminate() {
  kill -TERM "$nginx_pid" 2>/dev/null || true
  wait "$nginx_pid" || true
  exit 0
}
trap terminate TERM INT

while kill -0 "$nginx_pid" 2>/dev/null; do
  sleep 15 &
  wait $! || true
  candidate_digest=$(tls_digest)
  if [ "$candidate_digest" != "$served_digest" ]; then
    if nginx -t; then
      nginx -s reload
      served_digest=$candidate_digest
      printf '%s\n' 'TLS material reloaded' >&2
    else
      printf '%s\n' 'TLS material reload rejected' >&2
    fi
  fi
done

wait "$nginx_pid"
