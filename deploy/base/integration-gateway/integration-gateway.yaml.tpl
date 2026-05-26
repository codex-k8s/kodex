apiVersion: v1
kind: ServiceAccount
metadata:
  name: integration-gateway
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: integration-gateway
    app.kubernetes.io/part-of: kodex
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: integration-gateway-config
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: integration-gateway
    app.kubernetes.io/part-of: kodex
data:
  KODEX_INTEGRATION_GATEWAY_HTTP_ADDR: "{{ envOr "KODEX_INTEGRATION_GATEWAY_HTTP_ADDR" ":8080" }}"
  KODEX_INTEGRATION_GATEWAY_OPENAPI_SPEC_PATH: "{{ envOr "KODEX_INTEGRATION_GATEWAY_OPENAPI_SPEC_PATH" "/etc/kodex/integration-gateway/openapi/integration-gateway.v1.yaml" }}"
  KODEX_INTEGRATION_GATEWAY_HTTP_READ_HEADER_TIMEOUT: "{{ envOr "KODEX_INTEGRATION_GATEWAY_HTTP_READ_HEADER_TIMEOUT" "5s" }}"
  KODEX_INTEGRATION_GATEWAY_HTTP_REQUEST_TIMEOUT: "{{ envOr "KODEX_INTEGRATION_GATEWAY_HTTP_REQUEST_TIMEOUT" "10s" }}"
  KODEX_INTEGRATION_GATEWAY_HTTP_SHUTDOWN_TIMEOUT: "{{ envOr "KODEX_INTEGRATION_GATEWAY_HTTP_SHUTDOWN_TIMEOUT" "10s" }}"
  KODEX_INTEGRATION_GATEWAY_HTTP_READINESS_TIMEOUT: "{{ envOr "KODEX_INTEGRATION_GATEWAY_HTTP_READINESS_TIMEOUT" "2s" }}"
  KODEX_INTEGRATION_GATEWAY_HTTP_MAX_BODY_BYTES: "{{ envOr "KODEX_INTEGRATION_GATEWAY_HTTP_MAX_BODY_BYTES" "1048576" }}"
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ENABLED: "{{ envOr "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ENABLED" "true" }}"
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ALLOWED_PROVIDER_SLUGS: "{{ envOr "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ALLOWED_PROVIDER_SLUGS" "github" }}"
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE: "{{ envOr "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE" "env" }}"
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_REF: "{{ envOr "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_REF" "KODEX_GITHUB_WEBHOOK_SECRET" }}"
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_MAX_IN_FLIGHT: "{{ envOr "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_MAX_IN_FLIGHT" "32" }}"
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_RATE_LIMIT_BURST: "{{ envOr "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_RATE_LIMIT_BURST" "120" }}"
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_RATE_LIMIT_WINDOW: "{{ envOr "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_RATE_LIMIT_WINDOW" "1s" }}"
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_RETRY_AFTER: "{{ envOr "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_RETRY_AFTER" "1s" }}"
  KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_GRPC_ADDR: "{{ envOr "KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_GRPC_ADDR" "provider-hub:9090" }}"
  KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_TIMEOUT: "{{ envOr "KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_TIMEOUT" "3s" }}"
  KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_ENV_ENABLED: "{{ envOr "KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_ENV_ENABLED" "true" }}"
  KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_MOUNTED_KUBERNETES_ROOT: "{{ envOr "KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_MOUNTED_KUBERNETES_ROOT" "" }}"
  KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_MOUNTED_KUBERNETES_MAX_SECRET_BYTES: "{{ envOr "KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_MOUNTED_KUBERNETES_MAX_SECRET_BYTES" "1048576" }}"
  KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_ADDR: "{{ envOr "KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_ADDR" "" }}"
  KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_NAMESPACE: "{{ envOr "KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_NAMESPACE" "" }}"
---
apiVersion: v1
kind: Service
metadata:
  name: integration-gateway
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: integration-gateway
    app.kubernetes.io/part-of: kodex
spec:
  selector:
    app.kubernetes.io/name: integration-gateway
    app.kubernetes.io/part-of: kodex
  ports:
    - name: http
      port: 8080
      targetPort: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: integration-gateway
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: integration-gateway
    app.kubernetes.io/part-of: kodex
spec:
  replicas: {{ envOr "KODEX_INTEGRATION_GATEWAY_REPLICAS" "2" }}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: integration-gateway
      app.kubernetes.io/part-of: kodex
  template:
    metadata:
      labels:
        app.kubernetes.io/name: integration-gateway
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: external-gateway
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/path: "/metrics"
        prometheus.io/port: "8080"
    spec:
      serviceAccountName: integration-gateway
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
      containers:
        - name: integration-gateway
          image: {{ envOr "KODEX_INTEGRATION_GATEWAY_IMAGE" "" }}
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 8080
          envFrom:
            - configMapRef:
                name: integration-gateway-config
          env:
            - name: KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN
            - name: KODEX_GITHUB_WEBHOOK_SECRET
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_GITHUB_WEBHOOK_SECRET
            - name: KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_TOKEN
                  optional: true
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
              cpu: "{{ envOr "KODEX_INTEGRATION_GATEWAY_CPU_REQUEST" "100m" }}"
              memory: "{{ envOr "KODEX_INTEGRATION_GATEWAY_MEMORY_REQUEST" "128Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_INTEGRATION_GATEWAY_CPU_LIMIT" "2" }}"
              memory: "{{ envOr "KODEX_INTEGRATION_GATEWAY_MEMORY_LIMIT" "256Mi" }}"
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
