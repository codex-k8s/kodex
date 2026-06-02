apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: web-console-public-tls
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: web-console-public
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web
spec:
  secretName: web-console-public-tls
  dnsNames:
    - {{ envOr "KODEX_PRODUCTION_DOMAIN" "platform.kodex.works" }}
  issuerRef:
    name: kodex-letsencrypt-production
    kind: ClusterIssuer
    group: cert-manager.io
