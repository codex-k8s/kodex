apiVersion: v1
kind: Namespace
metadata:
  name: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex
    app.kubernetes.io/component: mattermost-runtime
    matter-codex.dev/runtime-namespace: "true"
