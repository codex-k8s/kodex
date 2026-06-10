apiVersion: v1
kind: ServiceAccount
metadata:
  name: matter-codex-bot-service
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: bot-service
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: agent-runner
automountServiceAccountToken: false
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: matter-codex-bot-service-runtime
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: runtime-rbac
rules:
  - apiGroups: [""]
    resources: ["persistentvolumeclaims"]
    verbs: ["create", "get", "list", "delete"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["create", "get", "list", "delete"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["create", "get", "update"]
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["create", "get", "list", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: matter-codex-bot-service-runtime
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: runtime-rbac
subjects:
  - kind: ServiceAccount
    name: matter-codex-bot-service
    namespace: ${MATTERCODEX_NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: matter-codex-bot-service-runtime
