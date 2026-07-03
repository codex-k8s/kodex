apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${MATTERCODEX_IMAGE_REGISTRY_NAME}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: ${MATTERCODEX_IMAGE_REGISTRY_NAME}
    app.kubernetes.io/component: image-registry
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: ${MATTERCODEX_IMAGE_REGISTRY_STORAGE_SIZE}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${MATTERCODEX_IMAGE_REGISTRY_NAME}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: ${MATTERCODEX_IMAGE_REGISTRY_NAME}
    app.kubernetes.io/component: image-registry
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: ${MATTERCODEX_IMAGE_REGISTRY_NAME}
      app.kubernetes.io/component: image-registry
  template:
    metadata:
      labels:
        app.kubernetes.io/name: ${MATTERCODEX_IMAGE_REGISTRY_NAME}
        app.kubernetes.io/component: image-registry
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      containers:
        - name: registry
          image: ${MATTERCODEX_IMAGE_REGISTRY_IMAGE}
          imagePullPolicy: IfNotPresent
          env:
            - name: REGISTRY_HTTP_ADDR
              value: 0.0.0.0:${MATTERCODEX_IMAGE_REGISTRY_HOST_PORT}
          ports:
            - name: registry
              containerPort: ${MATTERCODEX_IMAGE_REGISTRY_HOST_PORT}
              hostPort: ${MATTERCODEX_IMAGE_REGISTRY_HOST_PORT}
          volumeMounts:
            - name: data
              mountPath: /var/lib/registry
          readinessProbe:
            httpGet:
              path: /v2/
              port: registry
            initialDelaySeconds: 3
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /v2/
              port: registry
            initialDelaySeconds: 10
            periodSeconds: 20
          resources:
            requests:
              cpu: 50m
              memory: 128Mi
            limits:
              cpu: 1000m
              memory: 512Mi
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: ${MATTERCODEX_IMAGE_REGISTRY_NAME}
---
apiVersion: v1
kind: Service
metadata:
  name: ${MATTERCODEX_IMAGE_REGISTRY_NAME}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: ${MATTERCODEX_IMAGE_REGISTRY_NAME}
    app.kubernetes.io/component: image-registry
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: ${MATTERCODEX_IMAGE_REGISTRY_NAME}
    app.kubernetes.io/component: image-registry
  ports:
    - name: registry
      port: 5000
      targetPort: registry
