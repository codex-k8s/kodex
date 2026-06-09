apiVersion: v1
kind: Service
metadata:
  name: mattermost-postgres
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-postgres
    app.kubernetes.io/component: database
spec:
  ports:
    - name: postgres
      port: 5432
      targetPort: postgres
  selector:
    app.kubernetes.io/name: mattermost-postgres
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: mattermost-postgres
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-postgres
    app.kubernetes.io/component: database
spec:
  serviceName: mattermost-postgres
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: mattermost-postgres
  template:
    metadata:
      labels:
        app.kubernetes.io/name: mattermost-postgres
        app.kubernetes.io/component: database
    spec:
      containers:
        - name: postgres
          image: ${MATTERCODEX_POSTGRES_IMAGE}
          imagePullPolicy: IfNotPresent
          ports:
            - name: postgres
              containerPort: 5432
          env:
            - name: POSTGRES_DB
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_POSTGRES_SECRET}
                  key: postgres-db
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_POSTGRES_SECRET}
                  key: postgres-user
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_POSTGRES_SECRET}
                  key: postgres-password
          readinessProbe:
            exec:
              command:
                - sh
                - -c
                - pg_isready -U "${MATTERCODEX_POSTGRES_USER}" -d "${MATTERCODEX_POSTGRES_DB}"
            initialDelaySeconds: 10
            periodSeconds: 10
          volumeMounts:
            - name: postgres-data
              mountPath: /var/lib/postgresql/data
  volumeClaimTemplates:
    - metadata:
        name: postgres-data
        labels:
          app.kubernetes.io/name: mattermost-postgres
          app.kubernetes.io/component: database
      spec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: ${MATTERCODEX_POSTGRES_STORAGE_SIZE}
