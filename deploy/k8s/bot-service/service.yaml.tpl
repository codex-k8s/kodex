apiVersion: v1
kind: Service
metadata:
  name: matter-codex-bot-service
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: bot-service
spec:
  selector:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: bot-service
  ports:
    - name: http
      port: ${MATTERCODEX_BOT_SERVICE_PORT}
      targetPort: http
    - name: runtime-mtls
      port: 8443
      targetPort: runtime-mtls
