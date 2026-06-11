apiVersion: v1
kind: ResourceQuota
metadata:
  name: matter-codex-runtime-quota
  namespace: ${MATTERCODEX_RUNTIME_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: runtime-limits
spec:
  hard:
    pods: "${MATTERCODEX_RUNTIME_QUOTA_PODS}"
    count/jobs.batch: "${MATTERCODEX_RUNTIME_QUOTA_JOBS}"
    persistentvolumeclaims: "${MATTERCODEX_RUNTIME_QUOTA_PVCS}"
    requests.storage: "${MATTERCODEX_RUNTIME_QUOTA_REQUESTS_STORAGE}"
    requests.cpu: "${MATTERCODEX_RUNTIME_QUOTA_REQUESTS_CPU}"
    requests.memory: "${MATTERCODEX_RUNTIME_QUOTA_REQUESTS_MEMORY}"
    limits.cpu: "${MATTERCODEX_RUNTIME_QUOTA_LIMITS_CPU}"
    limits.memory: "${MATTERCODEX_RUNTIME_QUOTA_LIMITS_MEMORY}"
---
apiVersion: v1
kind: LimitRange
metadata:
  name: matter-codex-runtime-container-defaults
  namespace: ${MATTERCODEX_RUNTIME_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: runtime-limits
spec:
  limits:
    - type: Container
      default:
        cpu: "${MATTERCODEX_RUNTIME_LIMIT_DEFAULT_CPU}"
        memory: "${MATTERCODEX_RUNTIME_LIMIT_DEFAULT_MEMORY}"
      defaultRequest:
        cpu: "${MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_CPU}"
        memory: "${MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_MEMORY}"
