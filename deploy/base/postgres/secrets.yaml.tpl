apiVersion: v1
kind: Secret
metadata:
  name: kodex-postgres
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: postgres
    app.kubernetes.io/part-of: kodex
type: Opaque
stringData:
  KODEX_POSTGRES_DB: "{{ envOr "KODEX_POSTGRES_DB" "kodex" }}"
  KODEX_POSTGRES_USER: "{{ envOr "KODEX_POSTGRES_USER" "kodex" }}"
  KODEX_POSTGRES_PASSWORD: "{{ envOr "KODEX_POSTGRES_PASSWORD" "" }}"
---
apiVersion: v1
kind: Secret
metadata:
  name: kodex-platform-runtime
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: kodex-platform-runtime
    app.kubernetes.io/part-of: kodex
type: Opaque
stringData:
  KODEX_ACCESS_MANAGER_DATABASE_DSN: "{{ envOr "KODEX_ACCESS_MANAGER_DATABASE_DSN" "" }}"
  KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN: "{{ envOr "KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN" "" }}"
  KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN: "{{ envOr "KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN" "" }}"
