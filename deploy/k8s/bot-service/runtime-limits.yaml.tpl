apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: ${MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: runtime-limits
value: 0
globalDefault: false
preemptionPolicy: Never
description: "Непривилегированный класс agent workloads с aggregate memory admission."
---
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
---
apiVersion: v1
kind: ResourceQuota
metadata:
  name: matter-codex-agent-memory-quota
  namespace: ${MATTERCODEX_RUNTIME_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: runtime-limits
spec:
  scopeSelector:
    matchExpressions:
      - scopeName: PriorityClass
        operator: In
        values:
          - ${MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS}
  hard:
    requests.memory: "${MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET}"
    limits.memory: "${MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET}"
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
      defaultRequest:
        cpu: "${MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_CPU}"
        memory: "${MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_MEMORY}"
