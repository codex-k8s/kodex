apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: ${MATTERCODEX_CLUSTER_ISSUER}
  labels:
    app.kubernetes.io/name: matter-codex
    app.kubernetes.io/component: mattermost-tls
spec:
  acme:
    email: ${LETSENCRYPT_EMAIL}
    server: ${MATTERCODEX_ACME_SERVER}
    privateKeySecretRef:
      name: ${MATTERCODEX_CLUSTER_ISSUER}-account-key
    solvers:
      - http01:
          ingress:
            class: ${MATTERCODEX_INGRESS_CLASS}
