apiVersion: v1
kind: Secret
metadata:
  name: ${MATTERCODEX_ARTIFACT_STORAGE_SECRET}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: artifact-storage-secret
type: Opaque
data:
  access-key-id: ${ARTIFACT_S3_ACCESS_KEY_ID_B64}
  secret-access-key: ${ARTIFACT_S3_SECRET_ACCESS_KEY_B64}
  bucket: ${ARTIFACT_S3_BUCKET_B64}
  endpoint: ${ARTIFACT_S3_ENDPOINT_B64}
