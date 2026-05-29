apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: kodex-letsencrypt-production
  labels:
    app.kubernetes.io/name: kodex-letsencrypt-production
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web-foundation
spec:
  acme:
    email: "{{ env "KODEX_LETSENCRYPT_EMAIL" }}"
    server: "{{ envOr "KODEX_LETSENCRYPT_ACME_SERVER" "https://acme-v02.api.letsencrypt.org/directory" }}"
    privateKeySecretRef:
      name: kodex-letsencrypt-production-account-key
    solvers:
      - http01:
          ingress:
            ingressClassName: {{ envOr "KODEX_PUBLIC_INGRESS_CLASS" "kodex-public" }}
