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
      labels:
        app.kubernetes.io/name: matter-codex-bot-service
        app.kubernetes.io/component: bot-service
    spec:
      initContainers:
        - name: wait-for-postgres
          image: ${MATTERCODEX_BUSYBOX_IMAGE}
          imagePullPolicy: IfNotPresent
          command:
            - sh
            - -c
            - |
              until nc -z mattermost-postgres.${MATTERCODEX_NAMESPACE}.svc.cluster.local 5432; do
                sleep 2
              done
      containers:
        - name: bot-service
          image: ${MATTERCODEX_BOT_SERVICE_IMAGE}
          imagePullPolicy: IfNotPresent
          command:
            - sh
            - -ec
            - |
              mkdir -p /workspace
              base64 -d /source/source.tar.gz.b64 | tar -xz -C /workspace
              cd /workspace
              go mod download
              exec go run ./services/external/bot-service/cmd/bot-service
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
            - name: MATTERCODEX_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_POSTGRES_SECRET}
                  key: mattermost-datasource
                  optional: true
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
            - name: GOMODCACHE
              value: /tmp/go/pkg/mod
            - name: GOCACHE
              value: /tmp/go-build
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
          volumeMounts:
            - name: bot-service-source
              mountPath: /source
              readOnly: true
            - name: go-cache
              mountPath: /tmp/go
      volumes:
        - name: bot-service-source
          configMap:
            name: ${MATTERCODEX_BOT_SERVICE_CODE_CONFIGMAP}
        - name: go-cache
          emptyDir: {}
