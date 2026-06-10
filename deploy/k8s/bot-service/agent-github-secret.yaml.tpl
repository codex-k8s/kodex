apiVersion: v1
kind: Secret
metadata:
  name: ${MATTERCODEX_AGENT_GITHUB_SECRET}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: github-agent-secret
type: Opaque
data:
  github-token: ${AGENT_GITHUB_TOKEN_B64}
  github-username: ${AGENT_GITHUB_USERNAME_B64}
  github-email: ${AGENT_GITHUB_EMAIL_B64}
