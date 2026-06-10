apiVersion: v1
kind: Secret
metadata:
  name: ${MATTERCODEX_POSTGRES_SECRET}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-postgres
    app.kubernetes.io/component: database
type: Opaque
data:
  postgres-db: ${POSTGRES_DB_B64}
  postgres-user: ${POSTGRES_USER_B64}
  postgres-password: ${POSTGRES_PASSWORD_B64}
  mattermost-datasource: ${POSTGRES_DSN_B64}
