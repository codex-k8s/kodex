apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: matter-codex-bot-service-deny-all
  namespace: ${MATTERCODEX_NAMESPACE}
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: matter-codex-bot-service
      app.kubernetes.io/component: bot-service
  policyTypes: [Ingress, Egress]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: matter-codex-bot-service-exact-paths
  namespace: ${MATTERCODEX_NAMESPACE}
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: matter-codex-bot-service
      app.kubernetes.io/component: bot-service
  policyTypes: [Ingress, Egress]
  ingress:
    - from:
        - podSelector: {matchLabels: {app.kubernetes.io/name: interaction-gateway, app.kubernetes.io/component: external-gateway}}
        - podSelector: {matchLabels: {app.kubernetes.io/name: runtime-controller, app.kubernetes.io/component: role-runtime, runtime.mattercodex.dev/managed: "true"}}
      ports: [{protocol: TCP, port: 8443}]
    - from:
        - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: ingress-nginx}}
          podSelector: {matchLabels: {app.kubernetes.io/name: ingress-nginx}}
      ports: [{protocol: TCP, port: ${MATTERCODEX_BOT_SERVICE_PORT}}]
    - from:
        - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: monitoring}}
          podSelector: {matchLabels: {app.kubernetes.io/name: prometheus}}
      ports: [{protocol: TCP, port: ${MATTERCODEX_BOT_SERVICE_PORT}}, {protocol: TCP, port: 9091}]
  egress:
    - to:
        - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: kube-system}}
          podSelector: {matchLabels: {k8s-app: kube-dns}}
      ports: [{protocol: UDP, port: 53}, {protocol: TCP, port: 53}]
    - to:
        - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: kube-system}}
          podSelector: {matchLabels: {component: kube-apiserver}}
      ports: [{protocol: TCP, port: 6443}]
    - to:
        - podSelector: {matchLabels: {app.kubernetes.io/name: mattermost-postgres}}
      ports: [{protocol: TCP, port: 5432}]
    - to:
        - podSelector: {matchLabels: {app.kubernetes.io/name: mattermost}}
      ports: [{protocol: TCP, port: 8065}]
    - to:
        - podSelector: {matchLabels: {app.kubernetes.io/name: vault}}
      ports: [{protocol: TCP, port: 8200}]
    - to:
        - podSelector: {matchLabels: {app.kubernetes.io/name: internal-rpc-authority-postgresql}}
      ports: [{protocol: TCP, port: 5432}]
    - to:
        - podSelector: {matchLabels: {app.kubernetes.io/name: internal-rpc-authority-readback-attestor}}
        - podSelector: {matchLabels: {app.kubernetes.io/name: internal-rpc-authority-restore-controller}}
      ports: [{protocol: TCP, port: 8443}]
