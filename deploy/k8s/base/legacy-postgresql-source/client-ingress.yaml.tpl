apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: mattermost-postgres-migration-client-ingress
  namespace: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-postgres
    app.kubernetes.io/component: migration-source-client-ingress
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: mattermost-postgres
  policyTypes:
    - Ingress
  ingress:
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
