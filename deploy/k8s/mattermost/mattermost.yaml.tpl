apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mattermost-data
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost
    app.kubernetes.io/component: app
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: ${MATTERCODEX_MATTERMOST_STORAGE_SIZE}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mattermost
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost
    app.kubernetes.io/component: app
automountServiceAccountToken: false
---
apiVersion: v1
kind: Service
metadata:
  name: mattermost
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost
    app.kubernetes.io/component: app
spec:
  ports:
    - name: http
      port: 8065
      targetPort: http
  selector:
    app.kubernetes.io/name: mattermost
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mattermost
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost
    app.kubernetes.io/component: app
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: mattermost
  template:
    metadata:
      labels:
        app.kubernetes.io/name: mattermost
        app.kubernetes.io/component: app
    spec:
      serviceAccountName: mattermost
      securityContext:
        fsGroup: 2000
      initContainers:
        - name: wait-for-postgres
          image: ${MATTERCODEX_BUSYBOX_IMAGE}
          imagePullPolicy: IfNotPresent
          command:
            - sh
            - -c
            - |
              until nc -z mattermost-postgres.${MATTERCODEX_NAMESPACE}.svc.cluster.local 5432; do
                sleep 2
              done
      containers:
        - name: mattermost
          image: ${MATTERCODEX_MATTERMOST_IMAGE}
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 8065
          env:
            - name: MM_SERVICESETTINGS_SITEURL
              value: "${MATTERCODEX_MATTERMOST_SITE_URL}"
            - name: MM_SERVICESETTINGS_LISTENADDRESS
              value: ":8065"
            - name: MM_SQLSETTINGS_DRIVERNAME
              value: postgres
            - name: MM_SQLSETTINGS_DATASOURCE
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_POSTGRES_SECRET}
                  key: mattermost-datasource
            - name: MM_FILESETTINGS_DIRECTORY
              value: /mattermost/data
            - name: MM_LOGSETTINGS_ENABLECONSOLE
              value: "true"
            - name: MM_LOGSETTINGS_CONSOLELEVEL
              value: INFO
          readinessProbe:
            httpGet:
              path: /api/v4/system/ping
              port: http
            initialDelaySeconds: 30
            periodSeconds: 10
            failureThreshold: 12
          livenessProbe:
            httpGet:
              path: /api/v4/system/ping
              port: http
            initialDelaySeconds: 90
            periodSeconds: 30
            failureThreshold: 6
          volumeMounts:
            - name: mattermost-data
              mountPath: /mattermost/data
      volumes:
        - name: mattermost-data
          persistentVolumeClaim:
            claimName: mattermost-data
