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
      annotations:
        matter-codex.kodex.works/pod-input-revision: "${MATTERCODEX_BOT_SERVICE_POD_INPUT_REVISION}"
      labels:
        app.kubernetes.io/name: matter-codex-bot-service
        app.kubernetes.io/component: bot-service
    spec:
      serviceAccountName: matter-codex-bot-service
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        runAsGroup: 10001
        fsGroup: 29000
        fsGroupChangePolicy: OnRootMismatch
        seccompProfile:
          type: RuntimeDefault
      initContainers:
        - name: internal-rpc-authority-socket-init
          image: ${MATTERCODEX_INTERNAL_RPC_AUTHORITY_IMAGE}
          command: [/usr/local/bin/internal-rpc-authority-socket-init]
          securityContext:
            runAsNonRoot: true
            runAsUser: 29000
            runAsGroup: 29000
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: {drop: [ALL]}
          volumeMounts:
            - {name: internal-rpc-authority-sockets, mountPath: /run/mattercodex}
          resources:
            requests: {cpu: 10m, memory: 16Mi}
            limits: {cpu: 50m, memory: 32Mi}
        - name: wait-for-postgres
          image: ${MATTERCODEX_BUSYBOX_IMAGE}
          imagePullPolicy: IfNotPresent
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          command:
            - sh
            - -c
            - |
              until nc -z mattermost-postgres.${MATTERCODEX_NAMESPACE}.svc.cluster.local 5432; do
                sleep 2
              done
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
            limits:
              cpu: 50m
              memory: 64Mi
      containers:
        - name: bot-service
          image: ${MATTERCODEX_BOT_SERVICE_IMAGE}
          imagePullPolicy: ${MATTERCODEX_BOT_SERVICE_IMAGE_PULL_POLICY}
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          ports:
            - name: http
              containerPort: ${MATTERCODEX_BOT_SERVICE_PORT}
            - name: runtime-mtls
              containerPort: 8443
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
            - name: MATTERCODEX_MATTERMOST_ADMIN_TOKEN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_BOT_SERVICE_SECRET}
                  key: mattermost-admin-token
                  optional: true
            - name: MATTERCODEX_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_POSTGRES_SECRET}
                  key: bot-service-runtime-datasource
            - name: MATTERCODEX_MIGRATIONS_DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_POSTGRES_SECRET}
                  key: mattermost-datasource
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
            - name: INTERNAL_RPC_AUTHORITY_ISSUER_SOCKET
              value: /run/mattercodex/internal-rpc-authority/issuer.sock
            - name: INTERNAL_RPC_AUTHORITY_LOCAL_ROLE
              value: issuer
          volumeMounts:
            - name: runtime-server-tls
              mountPath: /var/run/secrets/mattercodex/bot-service-runtime-tls
              readOnly: true
            - name: runtime-client-ca
              mountPath: /var/run/config/mattercodex/bot-service-runtime-client-ca
              readOnly: true
            - name: internal-rpc-authority-sockets
              mountPath: /run/mattercodex
              readOnly: true
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
          resources:
            requests:
              cpu: "${MATTERCODEX_BOT_SERVICE_CPU_REQUEST}"
              memory: "${MATTERCODEX_BOT_SERVICE_MEMORY_REQUEST}"
            limits:
              memory: "${MATTERCODEX_BOT_SERVICE_MEMORY_LIMIT}"
        - name: internal-rpc-authority-issuer
          image: ${MATTERCODEX_INTERNAL_RPC_AUTHORITY_IMAGE}
          command: [/usr/local/bin/internal-rpc-authority-issuer]
          env:
            - {name: INTERNAL_RPC_AUTHORITY_WORKLOAD_ID, value: bot-service}
            - {name: INTERNAL_RPC_AUTHORITY_WORKLOAD_SPIFFE_ID, value: "spiffe://mattercodex.local/ns/${MATTERCODEX_NAMESPACE}/sa/matter-codex-bot-service"}
            - {name: INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_ADDRESS, value: internal-rpc-authority-readback-attestor.mattercodex-system.svc:8443}
            - {name: INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_TLS_SERVER_NAME, value: internal-rpc-authority-readback-attestor.mattercodex-system.svc}
            - {name: INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_CA_FILE, value: /var/run/config/mattercodex/internal-rpc-authority/readback/ca.pem}
            - {name: INTERNAL_RPC_AUTHORITY_VAULT_AUTH_ROLE, value: internal-rpc-authority-bot-service}
            - {name: INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_CA_FILE, value: /var/run/config/mattercodex/internal-rpc-authority/restore/ca.pem}
            - {name: INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN, value: ":9091"}
            - {name: INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_UID, value: "10001"}
            - {name: INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_GID, value: "10001"}
            - {name: INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE, value: /var/run/secrets/mattercodex/internal-rpc-authority/postgres/dsn}
            - name: INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER
              valueFrom: {secretKeyRef: {name: internal-rpc-authority-bot-service-issuer-postgresql, key: username}}
          ports: [{name: auth-metrics, containerPort: 9091, protocol: TCP}]
          readinessProbe: {httpGet: {path: /readyz, port: auth-metrics}, periodSeconds: 5, failureThreshold: 3}
          livenessProbe: {httpGet: {path: /livez, port: auth-metrics}, periodSeconds: 10, failureThreshold: 3}
          securityContext:
            runAsNonRoot: true
            runAsUser: 29001
            runAsGroup: 29000
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: {drop: [ALL]}
          resources:
            requests: {cpu: 25m, memory: 32Mi}
            limits: {cpu: 250m, memory: 128Mi}
          volumeMounts:
            - {name: internal-rpc-authority-sockets, mountPath: /run/mattercodex}
            - {name: authority-snapshot, mountPath: /var/run/config/mattercodex/internal-rpc-authority/snapshot, readOnly: true}
            - {name: authority-manifest-trust, mountPath: /var/run/config/mattercodex/internal-rpc-authority/manifest-trust, readOnly: true}
            - {name: authority-proof-trust, mountPath: /var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust, readOnly: true}
            - {name: authority-issuer-key, mountPath: /var/run/secrets/mattercodex/internal-rpc-authority/issuer, readOnly: true}
            - {name: control-plane-tls, mountPath: /var/run/secrets/mattercodex/internal-rpc-authority/workload-tls, readOnly: true}
            - {name: authority-readback-ca, mountPath: /var/run/config/mattercodex/internal-rpc-authority/readback, readOnly: true}
            - {name: authority-vault-ca, mountPath: /var/run/config/mattercodex/internal-rpc-authority/vault, readOnly: true}
            - {name: authority-vault-token, mountPath: /var/run/secrets/tokens/vault, readOnly: true}
            - {name: authority-restore-ca, mountPath: /var/run/config/mattercodex/internal-rpc-authority/restore, readOnly: true}
            - {name: authority-postgres, mountPath: /var/run/secrets/mattercodex/internal-rpc-authority/postgres, readOnly: true}
            - {name: authority-postgres-ca, mountPath: /var/run/config/mattercodex/internal-rpc-authority/postgresql, readOnly: true}
      volumes:
        - name: runtime-server-tls
          secret:
            secretName: ${MATTERCODEX_BOT_SERVICE_RUNTIME_TLS_SECRET}
            defaultMode: 288
            items:
              - {key: tls.crt, path: tls.crt}
              - {key: tls.key, path: tls.key}
        - name: runtime-client-ca
          configMap:
            name: ${MATTERCODEX_BOT_SERVICE_RUNTIME_CLIENT_CA_CONFIGMAP}
            defaultMode: 288
            items:
              - {key: ca.pem, path: ca.pem}
        - name: control-plane-tls
          secret:
            secretName: ${MATTERCODEX_BOT_SERVICE_CONTROL_PLANE_TLS_SECRET}
            defaultMode: 288
        - name: control-plane-ca
          configMap:
            name: ${MATTERCODEX_BOT_SERVICE_CONTROL_PLANE_CA_CONFIGMAP}
            defaultMode: 288
            items: [{key: ca.pem, path: ca.pem}]
        - name: internal-rpc-authority-sockets
          emptyDir: {sizeLimit: 32Mi}
        - {name: authority-snapshot, secret: {secretName: internal-rpc-authority-snapshot, defaultMode: 288}}
        - {name: authority-manifest-trust, secret: {secretName: internal-rpc-authority-bot-service-manifest-trust, defaultMode: 288}}
        - {name: authority-proof-trust, secret: {secretName: internal-rpc-authority-bot-service-proof-trust, defaultMode: 288}}
        - {name: authority-issuer-key, secret: {secretName: internal-rpc-authority-bot-service-issuer-key, defaultMode: 288}}
        - {name: authority-readback-ca, configMap: {name: internal-rpc-authority-readback-attestor-ca, defaultMode: 288}}
        - {name: authority-vault-ca, configMap: {name: internal-rpc-authority-vault-ca, defaultMode: 288}}
        - name: authority-vault-token
          projected:
            defaultMode: 256
            sources: [{serviceAccountToken: {path: token, audience: vault, expirationSeconds: 600}}]
        - {name: authority-restore-ca, configMap: {name: internal-rpc-authority-restore-controller-ca, defaultMode: 288}}
        - {name: authority-postgres, secret: {secretName: internal-rpc-authority-bot-service-issuer-postgresql, defaultMode: 288}}
        - {name: authority-postgres-ca, configMap: {name: internal-rpc-authority-postgresql-ca, defaultMode: 288}}
