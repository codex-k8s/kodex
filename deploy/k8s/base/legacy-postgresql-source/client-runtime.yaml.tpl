apiVersion: v1
kind: ServiceAccount
metadata:
  name: legacy-postgresql-source-readback
  namespace: ${MATTERCODEX_POSTGRES_CLIENT_NAMESPACE}
  labels:
    app.kubernetes.io/name: legacy-postgresql-source-readback
    app.kubernetes.io/component: migration-source-readback
automountServiceAccountToken: false
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: legacy-postgresql-source-readback-deny-all
  namespace: ${MATTERCODEX_POSTGRES_CLIENT_NAMESPACE}
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
  namespace: ${MATTERCODEX_POSTGRES_CLIENT_NAMESPACE}
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
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
          podSelector:
            matchLabels:
              app.kubernetes.io/name: mattermost-postgres
      ports:
        - protocol: TCP
          port: 5432
