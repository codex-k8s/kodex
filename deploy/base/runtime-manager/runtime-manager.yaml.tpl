apiVersion: v1
kind: ServiceAccount
metadata:
  name: runtime-manager
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: runtime-manager
    app.kubernetes.io/part-of: kodex
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: runtime-deployer
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: runtime-deployer
    app.kubernetes.io/part-of: kodex
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: runtime-manager-kubernetes-executor
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: runtime-manager
    app.kubernetes.io/part-of: kodex
rules:
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["create", "get", "list", "watch", "update", "patch"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["persistentvolumeclaims", "secrets", "serviceaccounts"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: runtime-manager-kubernetes-executor
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: runtime-manager
    app.kubernetes.io/part-of: kodex
subjects:
  - kind: ServiceAccount
    name: runtime-manager
    namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: runtime-manager-kubernetes-executor
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: runtime-deployer
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: runtime-deployer
    app.kubernetes.io/part-of: kodex
rules:
  - apiGroups: [""]
    resources: ["configmaps", "services", "serviceaccounts"]
    verbs: ["get", "list", "watch", "create", "patch", "update"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets"]
    verbs: ["get", "list", "watch", "create", "patch", "update"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "list", "watch", "create", "patch", "update"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings"]
    verbs: ["get", "list", "watch", "create", "patch", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: runtime-deployer
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: runtime-deployer
    app.kubernetes.io/part-of: kodex
subjects:
  - kind: ServiceAccount
    name: runtime-deployer
    namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: runtime-deployer
---
apiVersion: v1
kind: Service
metadata:
  name: runtime-manager
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: runtime-manager
    app.kubernetes.io/part-of: kodex
spec:
  selector:
    app.kubernetes.io/name: runtime-manager
    app.kubernetes.io/part-of: kodex
  ports:
    - name: http
      port: 8080
      targetPort: http
    - name: grpc
      port: 9090
      targetPort: grpc
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: runtime-manager
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: runtime-manager
    app.kubernetes.io/part-of: kodex
spec:
  replicas: {{ envOr "KODEX_RUNTIME_MANAGER_REPLICAS" "2" }}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: runtime-manager
      app.kubernetes.io/part-of: kodex
  template:
    metadata:
      labels:
        app.kubernetes.io/name: runtime-manager
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: internal-service
    spec:
      serviceAccountName: runtime-manager
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
      initContainers:
        - name: wait-database
          image: {{ imageOr "postgres" "KODEX_POSTGRES_IMAGE" }}
          imagePullPolicy: IfNotPresent
          securityContext:
            runAsNonRoot: true
            runAsUser: 999
            runAsGroup: 999
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
          command: ["/bin/sh", "-ec"]
          args:
            - |
              until psql "$KODEX_RUNTIME_MANAGER_DATABASE_DSN" -v ON_ERROR_STOP=1 -tAc 'SELECT 1' >/dev/null 2>&1; do
                sleep 2
              done
              until psql "$KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN" -v ON_ERROR_STOP=1 -tAc 'SELECT 1' >/dev/null 2>&1; do
                sleep 2
              done
          env:
            - name: KODEX_RUNTIME_MANAGER_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_RUNTIME_MANAGER_DATABASE_DSN
            - name: KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN
      containers:
        - name: runtime-manager
          image: {{ image "runtime-manager" }}
          imagePullPolicy: Always
          ports:
            - name: http
              containerPort: 8080
            - name: grpc
              containerPort: 9090
          env:
            - name: KODEX_RUNTIME_MANAGER_HTTP_ADDR
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_HTTP_ADDR" ":8080" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_ADDR
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_ADDR" ":9090" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN
            - name: KODEX_RUNTIME_MANAGER_GRPC_AUTH_REQUIRED
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_AUTH_REQUIRED" "true" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_MAX_IN_FLIGHT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_MAX_IN_FLIGHT" "128" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_MAX_CONCURRENT_STREAMS
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_MAX_CONCURRENT_STREAMS" "128" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_UNARY_TIMEOUT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_UNARY_TIMEOUT" "30s" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_KEEPALIVE_TIME
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_KEEPALIVE_TIME" "2m" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_KEEPALIVE_TIMEOUT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_KEEPALIVE_TIMEOUT" "20s" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_KEEPALIVE_MIN_TIME
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_KEEPALIVE_MIN_TIME" "30s" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_PERMIT_WITHOUT_STREAM
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_PERMIT_WITHOUT_STREAM" "false" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES" "4194304" }}"
            - name: KODEX_RUNTIME_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES" "4194304" }}"
            - name: KODEX_RUNTIME_MANAGER_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_RUNTIME_MANAGER_DATABASE_DSN
            - name: KODEX_RUNTIME_MANAGER_DATABASE_MAX_CONNS
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_DATABASE_MAX_CONNS" "8" }}"
            - name: KODEX_RUNTIME_MANAGER_DATABASE_MIN_CONNS
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_DATABASE_MIN_CONNS" "1" }}"
            - name: KODEX_RUNTIME_MANAGER_ACCESS_CHECK_ENABLED
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_ACCESS_CHECK_ENABLED" "true" }}"
            - name: KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_GRPC_ADDR
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_GRPC_ADDR" "access-manager:9090" }}"
            - name: KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN
            - name: KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_CHECK_TIMEOUT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_CHECK_TIMEOUT" "3s" }}"
            - name: KODEX_RUNTIME_MANAGER_FLEET_MANAGER_GRPC_ADDR
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_FLEET_MANAGER_GRPC_ADDR" "fleet-manager:9090" }}"
            - name: KODEX_RUNTIME_MANAGER_FLEET_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_RUNTIME_MANAGER_FLEET_MANAGER_GRPC_AUTH_TOKEN
            - name: KODEX_RUNTIME_MANAGER_FLEET_MANAGER_RESOLVE_TIMEOUT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_FLEET_MANAGER_RESOLVE_TIMEOUT" "5s" }}"
            - name: KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_ENV_ENABLED
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_ENV_ENABLED" "true" }}"
            - name: KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_MOUNTED_KUBERNETES_ROOT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_MOUNTED_KUBERNETES_ROOT" "" }}"
            - name: KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_MOUNTED_KUBERNETES_MAX_SECRET_BYTES
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_MOUNTED_KUBERNETES_MAX_SECRET_BYTES" "1048576" }}"
            - name: KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_VAULT_ADDR
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_VAULT_ADDR" "" }}"
            - name: KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_VAULT_NAMESPACE
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_VAULT_NAMESPACE" "" }}"
            - name: KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_VAULT_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_VAULT_TOKEN
                  optional: true
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_ENABLED
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_ENABLED" "false" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_WORKER_ID
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_WORKER_ID" "runtime-manager-kubernetes-executor" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_NAMESPACE
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_NAMESPACE" (envOr "KODEX_PRODUCTION_NAMESPACE" "") }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_SERVICE_ACCOUNT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_SERVICE_ACCOUNT" "" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEPLOY_SERVICE_ACCOUNT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEPLOY_SERVICE_ACCOUNT" "runtime-deployer" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_IMAGE
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_IMAGE" "" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_IMAGE_PULL_POLICY
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_IMAGE_PULL_POLICY" "IfNotPresent" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_POLL_INTERVAL
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_POLL_INTERVAL" "5s" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_CLAIM_LEASE_TTL
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_CLAIM_LEASE_TTL" "5m" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_JOB_TIMEOUT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_JOB_TIMEOUT" "2m" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_KUBERNETES_POLL_INTERVAL
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_KUBERNETES_POLL_INTERVAL" "2s" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_BACKOFF_LIMIT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_BACKOFF_LIMIT" "0" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_TTL_SECONDS_AFTER_FINISHED
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_TTL_SECONDS_AFTER_FINISHED" "300" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_LOG_TAIL_BYTES
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_LOG_TAIL_BYTES" "16384" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_ADDR
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_ADDR" "agent-manager:9090" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_NAME
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_NAME" "kodex-platform-runtime" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_KEY
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_KEY" "KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_REPORT_TIMEOUT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_REPORT_TIMEOUT" "3s" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_NAME
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_NAME" "kodex-platform-runtime" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_KEY
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_KEY" "KODEX_GIT_BOT_TOKEN" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_BUILD_CONTEXT_STORAGE_SIZE
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_BUILD_CONTEXT_STORAGE_SIZE" "2Gi" }}"
            - name: KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_BUILD_CONTEXT_STORAGE_CLASS
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_BUILD_CONTEXT_STORAGE_CLASS" "" }}"
            - name: KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN
            - name: KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS" "4" }}"
            - name: KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS" "0" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_DISPATCH_ENABLED
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_DISPATCH_ENABLED" "true" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_PUBLISHER_KIND
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_PUBLISHER_KIND" "postgres-event-log" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_EVENT_LOG_SOURCE
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_EVENT_LOG_SOURCE" "runtime-manager" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_BATCH_SIZE
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_BATCH_SIZE" "100" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_POLL_INTERVAL
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_POLL_INTERVAL" "1s" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_LOCK_TTL
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_LOCK_TTL" "30s" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_PUBLISH_TIMEOUT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_PUBLISH_TIMEOUT" "10s" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN" "5s" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_RETRY_INITIAL_DELAY
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_RETRY_INITIAL_DELAY" "1s" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_RETRY_MAX_DELAY
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_RETRY_MAX_DELAY" "1m" }}"
            - name: KODEX_RUNTIME_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT" "512" }}"
            - name: KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_FLEET_SCOPE_ID
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_FLEET_SCOPE_ID" "00000000-0000-0000-0000-000000000001" }}"
            - name: KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_CLUSTER_ID
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_CLUSTER_ID" "00000000-0000-0000-0000-000000000002" }}"
            - name: KODEX_RUNTIME_MANAGER_SLOT_NAMESPACE_PREFIX
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_SLOT_NAMESPACE_PREFIX" "kodex-rt" }}"
            - name: KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_LEASE_TTL
              value: "{{ envOr "KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_LEASE_TTL" "30m" }}"
          readinessProbe:
            httpGet:
              path: /health/readyz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
            timeoutSeconds: 2
            failureThreshold: 6
          livenessProbe:
            httpGet:
              path: /health/livez
              port: http
            initialDelaySeconds: 10
            periodSeconds: 20
            timeoutSeconds: 2
            failureThreshold: 3
          resources:
            requests:
              cpu: "{{ envOr "KODEX_RUNTIME_MANAGER_CPU_REQUEST" "100m" }}"
              memory: "{{ envOr "KODEX_RUNTIME_MANAGER_MEMORY_REQUEST" "128Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_RUNTIME_MANAGER_CPU_LIMIT" "2" }}"
              memory: "{{ envOr "KODEX_RUNTIME_MANAGER_MEMORY_LIMIT" "512Mi" }}"
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
