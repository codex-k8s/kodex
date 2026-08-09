apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: mattermost-postgres-migration-ca-g1
  namespace: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-postgres
    app.kubernetes.io/component: migration-source-pki
    mattercodex.dev/credential-generation: "1"
spec:
  ca:
    # Secret создаётся manage.sh ровно один раз как immutable CA generation.
    # cert-manager не владеет lifecycle trust anchor и не заменяет g1 in-place.
    secretName: mattermost-postgres-migration-ca-g1
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: mattermost-postgres-migration-server-g1
  namespace: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-postgres
    app.kubernetes.io/component: migration-source-pki
    mattercodex.dev/credential-generation: "1"
spec:
  secretName: mattermost-postgres-migration-server-g1
  duration: 2160h
  renewBefore: 720h
  revisionHistoryLimit: 3
  dnsNames:
    - ${MATTERCODEX_POSTGRES_MIGRATION_TLS_SERVER_NAME}
  privateKey:
    algorithm: ECDSA
    size: 256
    encoding: PKCS8
    rotationPolicy: Always
  usages:
    - digital signature
    - server auth
  issuerRef:
    name: mattermost-postgres-migration-ca-g1
    kind: Issuer
    group: cert-manager.io
