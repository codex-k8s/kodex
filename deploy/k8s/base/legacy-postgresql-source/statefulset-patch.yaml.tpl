apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: mattermost-postgres
  namespace: ${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}
spec:
  revisionHistoryLimit: 10
  template:
    metadata:
      annotations:
        mattercodex.dev/legacy-postgresql-source-revision: "${MATTERCODEX_POSTGRES_MIGRATION_REVISION}"
        mattercodex.dev/legacy-postgresql-certificate-revision: "${MATTERCODEX_POSTGRES_MIGRATION_CERTIFICATE_REVISION}"
    spec:
      initContainers:
        - name: migration-tls-materializer
          image: ${MATTERCODEX_POSTGRES_IMAGE}
          imagePullPolicy: IfNotPresent
          command:
            - bash
            - -ceu
            - |
              source_dir=/var/run/secrets/mattercodex/postgresql-migration-source
              runtime_dir=/var/run/postgresql-migration-tls
              expected_name='${MATTERCODEX_POSTGRES_MIGRATION_TLS_SERVER_NAME}'
              test -s "${source_dir}/tls.crt"
              test -s "${source_dir}/tls.key"
              test -s "${source_dir}/ca.crt"
              openssl verify -CAfile "${source_dir}/ca.crt" "${source_dir}/tls.crt" >/dev/null
              cert_key_sha="$(openssl x509 -in "${source_dir}/tls.crt" -pubkey -noout | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum | awk '{print $1}')"
              private_key_sha="$(openssl pkey -in "${source_dir}/tls.key" -pubout -outform DER 2>/dev/null | sha256sum | awk '{print $1}')"
              test "${cert_key_sha}" = "${private_key_sha}"
              actual_san="$(openssl x509 -in "${source_dir}/tls.crt" -noout -ext subjectAltName | tail -n +2 | tr -d '[:space:]')"
              test "${actual_san}" = "DNS:${expected_name}"
              install -o 999 -g 999 -m 0644 "${source_dir}/tls.crt" "${runtime_dir}/tls.crt"
              install -o 999 -g 999 -m 0600 "${source_dir}/tls.key" "${runtime_dir}/tls.key"
              install -o 999 -g 999 -m 0644 "${source_dir}/ca.crt" "${runtime_dir}/ca.crt"
              install -o 999 -g 999 -m 0600 /var/run/config/mattercodex/postgresql-migration-source/pg_hba.conf "${runtime_dir}/pg_hba.conf"
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
            limits:
              cpu: 100m
              memory: 64Mi
          securityContext:
            runAsUser: 0
            runAsGroup: 0
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
              add:
                - CHOWN
          volumeMounts:
            - name: migration-tls-source
              mountPath: /var/run/secrets/mattercodex/postgresql-migration-source
              readOnly: true
            - name: migration-hba-source
              mountPath: /var/run/config/mattercodex/postgresql-migration-source
              readOnly: true
            - name: migration-tls-runtime
              mountPath: /var/run/postgresql-migration-tls
      containers:
        - name: postgres
          args:
            - postgres
            - -c
            - ssl=on
            - -c
            - ssl_cert_file=/var/run/postgresql-migration-tls/tls.crt
            - -c
            - ssl_key_file=/var/run/postgresql-migration-tls/tls.key
            - -c
            - ssl_min_protocol_version=TLSv1.3
            - -c
            - ssl_max_protocol_version=TLSv1.3
            - -c
            - password_encryption=scram-sha-256
            - -c
            - hba_file=/var/run/postgresql-migration-tls/pg_hba.conf
          volumeMounts:
            - name: migration-tls-runtime
              mountPath: /var/run/postgresql-migration-tls
              readOnly: true
      volumes:
        - name: migration-tls-source
          secret:
            secretName: mattermost-postgres-migration-server-g1
            defaultMode: 0440
            items:
              - key: tls.crt
                path: tls.crt
              - key: tls.key
                path: tls.key
              - key: ca.crt
                path: ca.crt
        - name: migration-hba-source
          configMap:
            name: mattermost-postgres-migration-hba-g1
            defaultMode: 0440
            items:
              - key: pg_hba.conf
                path: pg_hba.conf
        - name: migration-tls-runtime
          emptyDir:
            sizeLimit: 8Mi
