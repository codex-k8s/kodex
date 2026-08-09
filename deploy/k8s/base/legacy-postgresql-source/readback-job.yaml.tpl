apiVersion: batch/v1
kind: Job
metadata:
  generateName: legacy-postgresql-source-readback-
  namespace: ${MATTERCODEX_POSTGRES_READBACK_NAMESPACE}
  labels:
    app.kubernetes.io/name: legacy-postgresql-source-readback
    app.kubernetes.io/component: migration-source-readback
  annotations:
    mattercodex.dev/legacy-postgresql-source-revision: "${MATTERCODEX_POSTGRES_MIGRATION_REVISION}"
    mattercodex.dev/legacy-postgresql-certificate-revision: "${MATTERCODEX_POSTGRES_MIGRATION_CERTIFICATE_REVISION}"
spec:
  backoffLimit: 0
  activeDeadlineSeconds: 120
  ttlSecondsAfterFinished: 600
  template:
    metadata:
      labels:
        app.kubernetes.io/name: legacy-postgresql-source-readback
        app.kubernetes.io/component: migration-source-readback
    spec:
      serviceAccountName: legacy-postgresql-source-readback
      automountServiceAccountToken: false
      restartPolicy: Never
      enableServiceLinks: false
      terminationGracePeriodSeconds: 10
      securityContext:
        runAsNonRoot: true
        runAsUser: 999
        runAsGroup: 999
        fsGroup: 999
        fsGroupChangePolicy: OnRootMismatch
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: readback
          image: ${MATTERCODEX_POSTGRES_IMAGE}
          imagePullPolicy: IfNotPresent
          command:
            - bash
            - /var/run/config/mattercodex/legacy-postgresql-source/readback.sh
          env:
            - name: PGHOST
              value: ${MATTERCODEX_POSTGRES_MIGRATION_TLS_SERVER_NAME}
            - name: PGPORT
              value: "5432"
            - name: PGSSLMODE
              value: verify-full
            - name: PGSSLROOTCERT
              value: /var/run/config/mattercodex/legacy-postgresql-source/trust/ca.pem
            - name: PGSSLMINPROTOCOLVERSION
              value: TLSv1.3
            - name: PGSSLMAXPROTOCOLVERSION
              value: TLSv1.3
            - name: PGCONNECT_TIMEOUT
              value: "10"
            - name: PGAPPNAME
              value: legacy-postgresql-source-readback
          resources:
            requests:
              cpu: 25m
              memory: 32Mi
            limits:
              cpu: 250m
              memory: 128Mi
          securityContext:
            runAsNonRoot: true
            runAsUser: 999
            runAsGroup: 999
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          volumeMounts:
            - name: credentials
              mountPath: /var/run/secrets/mattercodex/legacy-postgresql-source
              readOnly: true
            - name: trust
              mountPath: /var/run/config/mattercodex/legacy-postgresql-source/trust
              readOnly: true
            - name: readback
              mountPath: /var/run/config/mattercodex/legacy-postgresql-source
              readOnly: true
            - name: scratch
              mountPath: /var/run/mattercodex/legacy-postgresql-source
      volumes:
        - name: credentials
          secret:
            secretName: legacy-data-migration-source-postgresql-g1
            defaultMode: 0440
        - name: trust
          configMap:
            name: ${MATTERCODEX_POSTGRES_READBACK_TRUST_CONFIGMAP}
            defaultMode: 0440
        - name: readback
          configMap:
            name: legacy-postgresql-source-readback
            defaultMode: 0440
        - name: scratch
          emptyDir:
            medium: Memory
            sizeLimit: 8Mi
