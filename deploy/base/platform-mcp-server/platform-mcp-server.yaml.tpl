apiVersion: v1
kind: ServiceAccount
metadata:
  name: platform-mcp-server
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: platform-mcp-server
    app.kubernetes.io/part-of: kodex
---
apiVersion: v1
kind: Service
metadata:
  name: platform-mcp-server
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: platform-mcp-server
    app.kubernetes.io/part-of: kodex
spec:
  selector:
    app.kubernetes.io/name: platform-mcp-server
    app.kubernetes.io/part-of: kodex
  ports:
    - name: http
      port: 8080
      targetPort: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: platform-mcp-server
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: platform-mcp-server
    app.kubernetes.io/part-of: kodex
spec:
  replicas: {{ envOr "KODEX_PLATFORM_MCP_SERVER_REPLICAS" "2" }}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: platform-mcp-server
      app.kubernetes.io/part-of: kodex
  template:
    metadata:
      labels:
        app.kubernetes.io/name: platform-mcp-server
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: internal-service
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/path: "/metrics"
        prometheus.io/port: "8080"
    spec:
      serviceAccountName: platform-mcp-server
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
      initContainers:
        - name: wait-owner-services
          image: {{ imageOr "busybox" "KODEX_BUSYBOX_IMAGE" }}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-ec"]
          args:
            - |
              for service in access-manager agent-manager project-catalog provider-hub governance-manager runtime-manager fleet-manager package-hub interaction-hub; do
                until wget -q -T 2 -O - "http://${service}:8080/health/readyz" >/dev/null 2>&1; do
                  sleep 2
                done
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
        - name: platform-mcp-server
          image: {{ image "platform-mcp-server" }}
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 8080
          env:
            - name: KODEX_PLATFORM_MCP_SERVER_MCP_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_PLATFORM_MCP_SERVER_MCP_AUTH_TOKEN
            - name: KODEX_PLATFORM_MCP_SERVER_ACCESS_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN
            - name: KODEX_PLATFORM_MCP_SERVER_AGENT_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN
            - name: KODEX_PLATFORM_MCP_SERVER_PROJECT_CATALOG_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN
            - name: KODEX_PLATFORM_MCP_SERVER_PROVIDER_HUB_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN
            - name: KODEX_PLATFORM_MCP_SERVER_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN
            - name: KODEX_PLATFORM_MCP_SERVER_RUNTIME_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN
            - name: KODEX_PLATFORM_MCP_SERVER_FLEET_MANAGER_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_FLEET_MANAGER_GRPC_AUTH_TOKEN
            - name: KODEX_PLATFORM_MCP_SERVER_PACKAGE_HUB_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_PACKAGE_HUB_GRPC_AUTH_TOKEN
            - name: KODEX_PLATFORM_MCP_SERVER_INTERACTION_HUB_GRPC_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN
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
              cpu: "{{ envOr "KODEX_PLATFORM_MCP_SERVER_CPU_REQUEST" "100m" }}"
              memory: "{{ envOr "KODEX_PLATFORM_MCP_SERVER_MEMORY_REQUEST" "128Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_PLATFORM_MCP_SERVER_CPU_LIMIT" "1" }}"
              memory: "{{ envOr "KODEX_PLATFORM_MCP_SERVER_MEMORY_LIMIT" "256Mi" }}"
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
