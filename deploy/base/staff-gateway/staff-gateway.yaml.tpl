apiVersion: v1
kind: ServiceAccount
metadata:
  name: staff-gateway
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: staff-gateway
    app.kubernetes.io/part-of: kodex
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: staff-gateway-config
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: staff-gateway
    app.kubernetes.io/part-of: kodex
data:
  KODEX_STAFF_GATEWAY_OPENAPI_SPEC_PATH: "/etc/kodex/staff-gateway/openapi/staff-gateway.v1.yaml"
---
apiVersion: v1
kind: Service
metadata:
  name: staff-gateway
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: staff-gateway
    app.kubernetes.io/part-of: kodex
spec:
  selector:
    app.kubernetes.io/name: staff-gateway
    app.kubernetes.io/part-of: kodex
  ports:
    - name: http
      port: 8080
      targetPort: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: staff-gateway
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: staff-gateway
    app.kubernetes.io/part-of: kodex
spec:
  replicas: {{ envOr "KODEX_STAFF_GATEWAY_REPLICAS" "2" }}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: staff-gateway
      app.kubernetes.io/part-of: kodex
  template:
    metadata:
      labels:
        app.kubernetes.io/name: staff-gateway
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: staff-gateway
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/path: "/metrics"
        prometheus.io/port: "8080"
    spec:
      serviceAccountName: staff-gateway
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
      initContainers:
        - name: wait-agent-manager
          image: {{ imageOr "busybox" "KODEX_BUSYBOX_IMAGE" }}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-ec"]
          args:
            - |
              until wget -q -T 2 -O - http://agent-manager:8080/health/readyz >/dev/null 2>&1; do
                sleep 2
              done
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
        - name: wait-interaction-hub
          image: {{ imageOr "busybox" "KODEX_BUSYBOX_IMAGE" }}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-ec"]
          args:
            - |
              until wget -q -T 2 -O - http://interaction-hub:8080/health/readyz >/dev/null 2>&1; do
                sleep 2
              done
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
        - name: wait-governance-manager
          image: {{ imageOr "busybox" "KODEX_BUSYBOX_IMAGE" }}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-ec"]
          args:
            - |
              until wget -q -T 2 -O - http://governance-manager:8080/health/readyz >/dev/null 2>&1; do
                sleep 2
              done
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
      containers:
        - name: staff-gateway
          image: {{ image "staff-gateway" }}
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 8080
          envFrom:
            - configMapRef:
                name: staff-gateway-config
          env:
            - name: KODEX_STAFF_GATEWAY_INTERACTION_HUB_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN
            - name: KODEX_STAFF_GATEWAY_AGENT_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN
            - name: KODEX_STAFF_GATEWAY_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN
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
              cpu: "{{ envOr "KODEX_STAFF_GATEWAY_CPU_REQUEST" "100m" }}"
              memory: "{{ envOr "KODEX_STAFF_GATEWAY_MEMORY_REQUEST" "128Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_STAFF_GATEWAY_CPU_LIMIT" "1" }}"
              memory: "{{ envOr "KODEX_STAFF_GATEWAY_MEMORY_LIMIT" "256Mi" }}"
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
