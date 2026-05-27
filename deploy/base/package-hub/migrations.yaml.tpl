apiVersion: batch/v1
kind: Job
metadata:
  name: package-hub-migrations
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: package-hub-migrations
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: migrations
spec:
  backoffLimit: 6
  template:
    metadata:
      labels:
        app.kubernetes.io/name: package-hub-migrations
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: migrations
    spec:
      restartPolicy: OnFailure
      securityContext:
        runAsNonRoot: true
      initContainers:
        - name: wait-package-hub-db
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
              until psql "$KODEX_PACKAGE_HUB_DATABASE_DSN" -v ON_ERROR_STOP=1 -tAc 'SELECT 1' >/dev/null 2>&1; do
                sleep 2
              done
          env:
            - name: KODEX_PACKAGE_HUB_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_PACKAGE_HUB_DATABASE_DSN
      containers:
        - name: migrations
          image: {{ image "package-hub-migrations" }}
          imagePullPolicy: IfNotPresent
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
            - exec /usr/local/bin/goose -dir /migrations postgres "$KODEX_PACKAGE_HUB_DATABASE_DSN" up
          env:
            - name: KODEX_PACKAGE_HUB_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: kodex-platform-runtime
                  key: KODEX_PACKAGE_HUB_DATABASE_DSN
