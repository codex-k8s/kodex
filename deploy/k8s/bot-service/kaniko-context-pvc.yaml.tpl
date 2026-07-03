apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${MATTERCODEX_KANIKO_CONTEXT_PVC}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-kaniko
    app.kubernetes.io/component: build-context
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: ${MATTERCODEX_KANIKO_CONTEXT_STORAGE_SIZE}
