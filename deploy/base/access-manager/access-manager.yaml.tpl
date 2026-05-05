apiVersion: v1
kind: ServiceAccount
metadata:
  name: access-manager
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: access-manager
    app.kubernetes.io/part-of: kodex
---
apiVersion: v1
kind: Service
metadata:
  name: access-manager
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: access-manager
    app.kubernetes.io/part-of: kodex
spec:
  selector:
    app.kubernetes.io/name: access-manager
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
  name: access-manager
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: access-manager
    app.kubernetes.io/part-of: kodex
spec:
  replicas: {{ envOr "KODEX_ACCESS_MANAGER_REPLICAS" "2" }}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: access-manager
      app.kubernetes.io/part-of: kodex
  template:
    metadata:
      labels:
        app.kubernetes.io/name: access-manager
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: internal-service
    spec:
      serviceAccountName: access-manager
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
      initContainers:
        - name: wait-databases
          image: {{ envOr "KODEX_POSTGRES_IMAGE" "pgvector/pgvector:pg16" }}
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
              until psql "$KODEX_ACCESS_MANAGER_DATABASE_DSN" -v ON_ERROR_STOP=1 -tAc 'SELECT 1' >/dev/null 2>&1; do
                sleep 2
              done
              until psql "$KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN" -v ON_ERROR_STOP=1 -tAc 'SELECT 1' >/dev/null 2>&1; do
                sleep 2
              done
          env:
            - name: KODEX_ACCESS_MANAGER_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_ACCESS_MANAGER_DATABASE_DSN
            - name: KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
      containers:
        - name: access-manager
          image: {{ envOr "KODEX_ACCESS_MANAGER_IMAGE" "" }}
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 8080
            - name: grpc
              containerPort: 9090
          env:
            - name: KODEX_ACCESS_MANAGER_HTTP_ADDR
              value: "{{ envOr "KODEX_ACCESS_MANAGER_HTTP_ADDR" ":8080" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_ADDR
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_ADDR" ":9090" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN
            - name: KODEX_ACCESS_MANAGER_GRPC_AUTH_REQUIRED
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_AUTH_REQUIRED" "true" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_MAX_IN_FLIGHT
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_MAX_IN_FLIGHT" "128" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_MAX_CONCURRENT_STREAMS
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_MAX_CONCURRENT_STREAMS" "128" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_UNARY_TIMEOUT
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_UNARY_TIMEOUT" "30s" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_TIME
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_TIME" "2m" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_TIMEOUT
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_TIMEOUT" "20s" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_MIN_TIME
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_MIN_TIME" "30s" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_PERMIT_WITHOUT_STREAM
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_PERMIT_WITHOUT_STREAM" "false" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES" "4194304" }}"
            - name: KODEX_ACCESS_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES
              value: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES" "4194304" }}"
            - name: KODEX_ACCESS_MANAGER_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_ACCESS_MANAGER_DATABASE_DSN
            - name: KODEX_ACCESS_MANAGER_DATABASE_MAX_CONNS
              value: "{{ envOr "KODEX_ACCESS_MANAGER_DATABASE_MAX_CONNS" "8" }}"
            - name: KODEX_ACCESS_MANAGER_DATABASE_MIN_CONNS
              value: "{{ envOr "KODEX_ACCESS_MANAGER_DATABASE_MIN_CONNS" "1" }}"
            - name: KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
            - name: KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS
              value: "{{ envOr "KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS" "4" }}"
            - name: KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS
              value: "{{ envOr "KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS" "0" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_DISPATCH_ENABLED
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_DISPATCH_ENABLED" "true" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_PUBLISHER_KIND
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_PUBLISHER_KIND" "postgres-event-log" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_EVENT_LOG_SOURCE
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_EVENT_LOG_SOURCE" "access-manager" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_BATCH_SIZE
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_BATCH_SIZE" "100" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_POLL_INTERVAL
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_POLL_INTERVAL" "1s" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_LOCK_TTL
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_LOCK_TTL" "30s" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_PUBLISH_TIMEOUT
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_PUBLISH_TIMEOUT" "10s" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN" "5s" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_RETRY_INITIAL_DELAY
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_RETRY_INITIAL_DELAY" "1s" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_RETRY_MAX_DELAY
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_RETRY_MAX_DELAY" "1m" }}"
            - name: KODEX_ACCESS_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT
              value: "{{ envOr "KODEX_ACCESS_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT" "512" }}"
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
              cpu: "{{ envOr "KODEX_ACCESS_MANAGER_CPU_REQUEST" "100m" }}"
              memory: "{{ envOr "KODEX_ACCESS_MANAGER_MEMORY_REQUEST" "128Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_ACCESS_MANAGER_CPU_LIMIT" "1" }}"
              memory: "{{ envOr "KODEX_ACCESS_MANAGER_MEMORY_LIMIT" "512Mi" }}"
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
