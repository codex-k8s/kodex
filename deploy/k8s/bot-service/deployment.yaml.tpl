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
      containers:
        - name: bot-service
          image: ${MATTERCODEX_BOT_SERVICE_IMAGE}
          imagePullPolicy: IfNotPresent
          command:
            - python
            - /app/app.py
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
            - name: bot-service-code
              mountPath: /app/app.py
              subPath: app.py
              readOnly: true
      volumes:
        - name: bot-service-code
          configMap:
            name: ${MATTERCODEX_BOT_SERVICE_CODE_CONFIGMAP}
