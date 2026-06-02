apiVersion: batch/v1
kind: Job
metadata:
  name: platform-event-log-migrations
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: platform-event-log-migrations
    app.kubernetes.io/part-of: kodex
spec:
  backoffLimit: 6
  template:
    metadata:
      labels:
        app.kubernetes.io/name: platform-event-log-migrations
        app.kubernetes.io/part-of: kodex
    spec:
      restartPolicy: OnFailure
      securityContext:
        runAsNonRoot: true
      initContainers:
        - name: wait-platform-event-log-db
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
              until psql "$KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN" -v ON_ERROR_STOP=1 -tAc 'SELECT 1' >/dev/null 2>&1; do
                sleep 2
              done
          env:
            - name: KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
      containers:
        - name: migrations
          image: {{ image "platform-event-log-migrations" }}
          imagePullPolicy: Always
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
          command: ["/bin/sh", "-ec"]
          args:
            - exec /usr/local/bin/goose -dir /migrations postgres "$KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN" up
          env:
            - name: KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
