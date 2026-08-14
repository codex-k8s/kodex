#!/usr/bin/env bash
set -euo pipefail

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
verifier="$script_directory/verify-dark-ingresses.jq"

approved=$(jq -n '{
  metadata:{
    name:"control-center-public",namespace:"mattercodex-system",
    labels:{"app.kubernetes.io/name":"control-center-public-bridge","app.kubernetes.io/component":"public-entrypoint"},
    annotations:{
      "cert-manager.io/cluster-issuer":"letsencrypt-prod",
      "kubernetes.io/ingress.class":"kodex-public",
      "traefik.ingress.kubernetes.io/router.entrypoints":"websecure",
      "traefik.ingress.kubernetes.io/router.tls":"true"
    }
  },
  spec:{
    ingressClassName:"kodex-public",
    tls:[{hosts:["control.kodex.works"],secretName:"control-center-public-tls"}],
    rules:[{host:"control.kodex.works",http:{paths:[{
      backend:{service:{name:"control-center-public-bridge",port:{name:"http"}}},
      path:"/",pathType:"Prefix"
    }]}}]
  }
}')

jq -n '{items:[]}' | jq -e -f "$verifier" >/dev/null
jq -n --argjson ingress "$approved" '{items:[$ingress]}' | jq -e -f "$verifier" >/dev/null

unexpected=$(jq -n '{metadata:{name:"unexpected",namespace:"mattercodex-system"},spec:{}}')
if jq -n --argjson ingress "$unexpected" '{items:[$ingress]}' | jq -e -f "$verifier" >/dev/null; then
  printf 'unexpected Ingress was accepted\n' >&2
  exit 1
fi

if jq -n --argjson approved "$approved" --argjson unexpected "$unexpected" \
  '{items:[$approved,$unexpected]}' | jq -e -f "$verifier" >/dev/null; then
  printf 'additional Ingress was accepted\n' >&2
  exit 1
fi

wrong_backend=$(jq -n --argjson ingress "$approved" \
  '$ingress | .spec.rules[0].http.paths[0].backend.service.name = "control-api-gateway"')
if jq -n --argjson ingress "$wrong_backend" '{items:[$ingress]}' | jq -e -f "$verifier" >/dev/null; then
  printf 'Ingress with an unapproved backend was accepted\n' >&2
  exit 1
fi

printf 'Dark Ingress verification tests passed\n'
