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
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: agent-runner-cluster-admin
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
    verbs: ["create", "get", "list", "delete"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["create", "get", "list", "update", "delete"]
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
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: matter-codex-agent-runner-readonly
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: agent-runner-rbac
rules:
  - apiGroups: [""]
    resources: ["pods", "pods/log", "services", "endpoints", "configmaps", "persistentvolumeclaims", "events"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets", "replicasets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["batch"]
    resources: ["jobs", "cronjobs"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: matter-codex-agent-runner-readonly
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: agent-runner-rbac
subjects:
  - kind: ServiceAccount
    name: ${MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT}
    namespace: ${MATTERCODEX_NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: matter-codex-agent-runner-readonly
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: matter-codex-agent-runner-cluster-admin
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: agent-runner-cluster-admin-rbac
subjects:
  - kind: ServiceAccount
    name: ${MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT}
    namespace: ${MATTERCODEX_NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
