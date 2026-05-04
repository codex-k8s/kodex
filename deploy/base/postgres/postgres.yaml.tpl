apiVersion: v1
kind: ConfigMap
metadata:
  name: kodex-postgres-bootstrap
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
data:
  bootstrap-databases.sh: |
    #!/bin/sh
    set -eu

    POSTGRES_HOST="${POSTGRES_HOST:-postgres}"
    POSTGRES_PORT="${POSTGRES_PORT:-5432}"
    export PGPASSWORD="${POSTGRES_PASSWORD}"

    wait_for_postgres() {
      until pg_isready -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" >/dev/null 2>&1; do
        sleep 2
      done
    }

    create_database() {
      database="$1"
      escaped_ident="$(printf '%s' "${database}" | sed 's/"/""/g')"
      escaped_literal="$(printf '%s' "${database}" | sed "s/'/''/g")"

      if psql -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -tAc "SELECT 1 FROM pg_database WHERE datname = '${escaped_literal}'" | grep -q 1; then
        echo "database ${database} already exists"
        return
      fi

      psql -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${escaped_ident}\""
    }

    wait_for_postgres
    create_database "${KODEX_ACCESS_MANAGER_DATABASE_NAME:-kodex_access_manager}"
    create_database "${KODEX_PLATFORM_EVENT_LOG_DATABASE_NAME:-kodex_platform_event_log}"
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: postgres
    app.kubernetes.io/part-of: kodex
spec:
  selector:
    app.kubernetes.io/name: postgres
  ports:
    - name: pg
      port: 5432
      targetPort: 5432
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: postgres
    app.kubernetes.io/part-of: kodex
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: postgres
  template:
    metadata:
      labels:
        app.kubernetes.io/name: postgres
        app.kubernetes.io/part-of: kodex
    spec:
      containers:
        - name: postgres
          image: {{ envOr "KODEX_POSTGRES_IMAGE" "pgvector/pgvector:pg16" }}
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 5432
              name: pg
          env:
            - name: POSTGRES_DB
              valueFrom:
                secretKeyRef:
                  name: kodex-postgres
                  key: KODEX_POSTGRES_DB
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef:
                  name: kodex-postgres
                  key: KODEX_POSTGRES_USER
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: kodex-postgres
                  key: KODEX_POSTGRES_PASSWORD
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
          readinessProbe:
            exec:
              command: ["sh", "-c", "pg_isready -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\""]
            initialDelaySeconds: 10
            periodSeconds: 10
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: {{ envOr "KODEX_POSTGRES_STORAGE_SIZE" "20Gi" }}
---
apiVersion: batch/v1
kind: Job
metadata:
  name: kodex-postgres-bootstrap-databases
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: kodex-postgres-bootstrap
    app.kubernetes.io/part-of: kodex
spec:
  backoffLimit: 6
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kodex-postgres-bootstrap
        app.kubernetes.io/part-of: kodex
    spec:
      restartPolicy: OnFailure
      containers:
        - name: bootstrap-databases
          image: {{ envOr "KODEX_POSTGRES_IMAGE" "pgvector/pgvector:pg16" }}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "/scripts/bootstrap-databases.sh"]
          env:
            - name: POSTGRES_HOST
              value: postgres
            - name: POSTGRES_PORT
              value: "5432"
            - name: POSTGRES_DB
              valueFrom:
                secretKeyRef:
                  name: kodex-postgres
                  key: KODEX_POSTGRES_DB
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef:
                  name: kodex-postgres
                  key: KODEX_POSTGRES_USER
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: kodex-postgres
                  key: KODEX_POSTGRES_PASSWORD
            - name: KODEX_ACCESS_MANAGER_DATABASE_NAME
              value: {{ envOr "KODEX_ACCESS_MANAGER_DATABASE_NAME" "kodex_access_manager" }}
            - name: KODEX_PLATFORM_EVENT_LOG_DATABASE_NAME
              value: {{ envOr "KODEX_PLATFORM_EVENT_LOG_DATABASE_NAME" "kodex_platform_event_log" }}
          volumeMounts:
            - name: bootstrap-script
              mountPath: /scripts/bootstrap-databases.sh
              subPath: bootstrap-databases.sh
      volumes:
        - name: bootstrap-script
          configMap:
            name: kodex-postgres-bootstrap
            defaultMode: 0755
