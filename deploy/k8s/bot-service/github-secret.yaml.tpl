apiVersion: v1
kind: Secret
metadata:
  name: ${MATTERCODEX_GITHUB_SECRET}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: github-secret
type: Opaque
data:
  github-token: ${GITHUB_TOKEN_B64}
  github-webhook-secret: ${GITHUB_WEBHOOK_SECRET_B64}
  github-username: ${GITHUB_USERNAME_B64}
  github-email: ${GITHUB_EMAIL_B64}
