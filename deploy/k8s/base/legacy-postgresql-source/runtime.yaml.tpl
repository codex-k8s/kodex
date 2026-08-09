apiVersion: v1
kind: Service
metadata:
  name: mattermost-postgres-migration
  namespace: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-postgres
    app.kubernetes.io/component: migration-source
spec:
  ports:
    - name: postgres-tls
      port: 5432
      targetPort: postgres
      protocol: TCP
  selector:
    app.kubernetes.io/name: mattermost-postgres
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mattermost-postgres-migration-hba-g1
  namespace: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-postgres
    app.kubernetes.io/component: migration-source
    mattercodex.dev/credential-generation: "1"
data:
  pg_hba.conf: |
    local all all trust
    host all all 127.0.0.1/32 trust
    host all all ::1/128 trust
    local replication all trust
    host replication all 127.0.0.1/32 trust
    host replication all ::1/128 trust
    hostnossl all matter_codex_migration_g1 all reject
    hostssl "${MATTERCODEX_POSTGRES_DB}" matter_codex_migration_g1 all scram-sha-256
    hostssl all matter_codex_migration_g1 all reject
    host all all all scram-sha-256
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: legacy-postgresql-source-readback
  namespace: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
  labels:
    app.kubernetes.io/name: legacy-postgresql-source-readback
    app.kubernetes.io/component: migration-source-readback
automountServiceAccountToken: false
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: mattermost-postgres-exact-ingress
  namespace: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-postgres
    app.kubernetes.io/component: migration-source
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: mattermost-postgres
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app.kubernetes.io/name: mattermost
        - podSelector:
            matchLabels:
              app.kubernetes.io/name: matter-codex-bot-service
        - podSelector:
            matchLabels:
              app.kubernetes.io/name: legacy-postgresql-source-readback
      ports:
        - protocol: TCP
          port: 5432
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: ${MATTERCODEX_POSTGRES_CLIENT_NAMESPACE}
          podSelector:
            matchLabels:
              app.kubernetes.io/name: legacy-data-migration
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: ${MATTERCODEX_POSTGRES_CLIENT_NAMESPACE}
          podSelector:
            matchLabels:
              app.kubernetes.io/name: legacy-postgresql-source-readback
      ports:
        - protocol: TCP
          port: 5432
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: legacy-postgresql-source-readback-deny-all
  namespace: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
  labels:
    app.kubernetes.io/name: legacy-postgresql-source-readback
    app.kubernetes.io/component: migration-source-readback
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: legacy-postgresql-source-readback
  policyTypes:
    - Ingress
    - Egress
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: legacy-postgresql-source-readback-exact-egress
  namespace: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
  labels:
    app.kubernetes.io/name: legacy-postgresql-source-readback
    app.kubernetes.io/component: migration-source-readback
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: legacy-postgresql-source-readback
  policyTypes:
    - Egress
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
    - to:
        - podSelector:
            matchLabels:
              app.kubernetes.io/name: mattermost-postgres
      ports:
        - protocol: TCP
          port: 5432
