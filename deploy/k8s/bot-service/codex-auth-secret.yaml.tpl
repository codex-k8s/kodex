apiVersion: v1
kind: Secret
metadata:
  name: ${CODEX_AUTH_SECRET_NAME}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: codex-auth-secret
    matter-codex.dev/openai-account: ${CODEX_AUTH_ACCOUNT}
type: Opaque
data:
  auth.json: ${CODEX_AUTH_JSON_B64}
