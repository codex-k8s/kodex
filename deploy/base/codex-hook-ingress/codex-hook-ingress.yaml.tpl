apiVersion: v1
kind: ServiceAccount
metadata:
  name: codex-hook-ingress
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: codex-hook-ingress
    app.kubernetes.io/part-of: kodex
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: codex-hook-ingress-config
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: codex-hook-ingress
    app.kubernetes.io/part-of: kodex
data:
  KODEX_CODEX_HOOK_INGRESS_HTTP_ADDR: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_HTTP_ADDR" ":8080" }}"
  KODEX_CODEX_HOOK_INGRESS_READINESS_TIMEOUT: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_READINESS_TIMEOUT" "2s" }}"
  KODEX_CODEX_HOOK_INGRESS_SHUTDOWN_TIMEOUT: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_SHUTDOWN_TIMEOUT" "10s" }}"
  KODEX_CODEX_HOOK_INGRESS_SCHEMA_VERSION: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_SCHEMA_VERSION" "codex-hook-ingress.normalized-hook-envelope.v1" }}"
  KODEX_CODEX_HOOK_INGRESS_SANITIZER_CONTRACT_ID: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_SANITIZER_CONTRACT_ID" "codex-hook-ingress.sanitizer.v1" }}"
  KODEX_CODEX_HOOK_INGRESS_SUPPORTED_HOOK_EVENTS: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_SUPPORTED_HOOK_EVENTS" "SessionStart,UserPromptSubmit,PreToolUse,PermissionRequest,PostToolUse,Stop" }}"
  KODEX_CODEX_HOOK_INGRESS_MAX_ENVELOPE_BYTES: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_MAX_ENVELOPE_BYTES" "65536" }}"
  KODEX_CODEX_HOOK_INGRESS_MAX_TEXT_PREVIEW_BYTES: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_MAX_TEXT_PREVIEW_BYTES" "4096" }}"
  KODEX_CODEX_HOOK_INGRESS_MAX_BOUNDED_ERROR_BYTES: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_MAX_BOUNDED_ERROR_BYTES" "8192" }}"
  KODEX_CODEX_HOOK_INGRESS_DISABLED_ROUTES: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_DISABLED_ROUTES" "" }}"
  KODEX_CODEX_HOOK_INGRESS_ROUTE_FAILURE_POLICY: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_ROUTE_FAILURE_POLICY" "diagnostic" }}"
  KODEX_CODEX_HOOK_INGRESS_OPS_FEED_CAPACITY: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_OPS_FEED_CAPACITY" "1024" }}"
  KODEX_CODEX_HOOK_INGRESS_OPS_FEED_RETENTION: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_OPS_FEED_RETENTION" "15m" }}"
  KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_WINDOW: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_WINDOW" "1m" }}"
  KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_BURST: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_BURST" "300" }}"
  KODEX_CODEX_HOOK_INGRESS_DECISION_BRIDGE_TIMEOUT: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_DECISION_BRIDGE_TIMEOUT" "30s" }}"
  KODEX_CODEX_HOOK_INGRESS_PERMISSION_DECISION_FAILURE_POLICY: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_PERMISSION_DECISION_FAILURE_POLICY" "fail_closed" }}"
  KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_FAILURE_POLICY: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_FAILURE_POLICY" "no_decision" }}"
  KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_RISK_CLASSES: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_RISK_CLASSES" "medium,high,unknown" }}"
  KODEX_CODEX_HOOK_INGRESS_LOGICAL_TRANSPORT_READ_ONLY: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_LOGICAL_TRANSPORT_READ_ONLY" "true" }}"
---
apiVersion: v1
kind: Service
metadata:
  name: codex-hook-ingress
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: codex-hook-ingress
    app.kubernetes.io/part-of: kodex
spec:
  selector:
    app.kubernetes.io/name: codex-hook-ingress
    app.kubernetes.io/part-of: kodex
  ports:
    - name: http
      port: 8080
      targetPort: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: codex-hook-ingress
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: codex-hook-ingress
    app.kubernetes.io/part-of: kodex
spec:
  replicas: {{ envOr "KODEX_CODEX_HOOK_INGRESS_REPLICAS" "2" }}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: codex-hook-ingress
      app.kubernetes.io/part-of: kodex
  template:
    metadata:
      labels:
        app.kubernetes.io/name: codex-hook-ingress
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: internal-service
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/path: "/metrics"
        prometheus.io/port: "8080"
    spec:
      serviceAccountName: codex-hook-ingress
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
      containers:
        - name: codex-hook-ingress
          image: {{ image "codex-hook-ingress" }}
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 8080
          envFrom:
            - configMapRef:
                name: codex-hook-ingress-config
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
              cpu: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_CPU_REQUEST" "100m" }}"
              memory: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_MEMORY_REQUEST" "128Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_CPU_LIMIT" "1" }}"
              memory: "{{ envOr "KODEX_CODEX_HOOK_INGRESS_MEMORY_LIMIT" "256Mi" }}"
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
