apiVersion: apps/v1
kind: Deployment
metadata:
  name: matter-codex-bot-service
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: bot-service
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: matter-codex-bot-service
      app.kubernetes.io/component: bot-service
  template:
    metadata:
      annotations:
        matter-codex.kodex.works/pod-input-revision: "${MATTERCODEX_BOT_SERVICE_POD_INPUT_REVISION}"
      labels:
        app.kubernetes.io/name: matter-codex-bot-service
        app.kubernetes.io/component: bot-service
    spec:
      serviceAccountName: matter-codex-bot-service
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        runAsGroup: 10001
        seccompProfile:
          type: RuntimeDefault
      initContainers:
        - name: wait-for-postgres
          image: ${MATTERCODEX_BUSYBOX_IMAGE}
          imagePullPolicy: IfNotPresent
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          command:
            - sh
            - -c
            - |
              until nc -z mattermost-postgres.${MATTERCODEX_NAMESPACE}.svc.cluster.local 5432; do
                sleep 2
              done
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
            limits:
              cpu: 50m
              memory: 64Mi
      containers:
        - name: bot-service
          image: ${MATTERCODEX_BOT_SERVICE_IMAGE}
          imagePullPolicy: ${MATTERCODEX_BOT_SERVICE_IMAGE_PULL_POLICY}
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          ports:
            - name: http
              containerPort: ${MATTERCODEX_BOT_SERVICE_PORT}
          envFrom:
            - configMapRef:
                name: ${MATTERCODEX_BOT_SERVICE_CONFIG_CONFIGMAP}
          env:
            - name: MATTERCODEX_MATTERMOST_BOT_TOKEN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_BOT_SERVICE_SECRET}
                  key: mattermost-bot-token
                  optional: true
            - name: MATTERCODEX_MATTERMOST_SLASH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_BOT_SERVICE_SECRET}
                  key: mattermost-slash-token
                  optional: true
            - name: MATTERCODEX_MATTERMOST_ADMIN_TOKEN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_BOT_SERVICE_SECRET}
                  key: mattermost-admin-token
                  optional: true
            - name: MATTERCODEX_CONTROL_CENTER_READ_TOKEN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_BOT_SERVICE_SECRET}
                  key: control-center-read-token
                  optional: true
            - name: MATTERCODEX_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_POSTGRES_SECRET}
                  key: bot-service-runtime-datasource
            - name: MATTERCODEX_MIGRATIONS_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_POSTGRES_SECRET}
                  key: mattermost-datasource
            - name: MATTERCODEX_GITHUB_TOKEN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_GITHUB_SECRET}
                  key: github-token
                  optional: true
            - name: MATTERCODEX_GITHUB_WEBHOOK_SECRET
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_GITHUB_SECRET}
                  key: github-webhook-secret
                  optional: true
          startupProbe:
            httpGet:
              path: /healthz
              port: http
            failureThreshold: 30
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
            initialDelaySeconds: 3
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 10
            periodSeconds: 20
          resources:
            requests:
              cpu: "${MATTERCODEX_BOT_SERVICE_CPU_REQUEST}"
              memory: "${MATTERCODEX_BOT_SERVICE_MEMORY_REQUEST}"
            limits:
              memory: "${MATTERCODEX_BOT_SERVICE_MEMORY_LIMIT}"
