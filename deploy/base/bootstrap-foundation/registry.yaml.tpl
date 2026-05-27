apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: kodex-registry-data
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: kodex-registry
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: bootstrap-foundation
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: {{ envOr "KODEX_INTERNAL_REGISTRY_STORAGE_SIZE" "20Gi" }}
---
apiVersion: v1
kind: Service
metadata:
  name: kodex-registry
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: kodex-registry
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: bootstrap-foundation
spec:
  selector:
    app.kubernetes.io/name: kodex-registry
    app.kubernetes.io/part-of: kodex
  ports:
    - name: http
      port: 5000
      targetPort: registry
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kodex-registry
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: kodex-registry
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: bootstrap-foundation
spec:
  replicas: 1
  revisionHistoryLimit: 2
  selector:
    matchLabels:
      app.kubernetes.io/name: kodex-registry
      app.kubernetes.io/part-of: kodex
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kodex-registry
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: bootstrap-foundation
    spec:
      terminationGracePeriodSeconds: 30
      securityContext:
        fsGroup: 1000
      containers:
        - name: registry
          image: {{ envOr "KODEX_REGISTRY_IMAGE" "registry:2" }}
          imagePullPolicy: IfNotPresent
          ports:
            - name: registry
              containerPort: 5000
              hostPort: {{ envOr "KODEX_INTERNAL_REGISTRY_PORT" "5000" }}
              hostIP: 127.0.0.1
          env:
            - name: REGISTRY_HTTP_ADDR
              value: "0.0.0.0:5000"
            - name: REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY
              value: /var/lib/registry
            - name: REGISTRY_STORAGE_DELETE_ENABLED
              value: "true"
          readinessProbe:
            httpGet:
              path: /v2/
              port: registry
            initialDelaySeconds: 5
            periodSeconds: 10
            timeoutSeconds: 2
            failureThreshold: 6
          livenessProbe:
            httpGet:
              path: /v2/
              port: registry
            initialDelaySeconds: 15
            periodSeconds: 20
            timeoutSeconds: 2
            failureThreshold: 3
          resources:
            requests:
              cpu: "{{ envOr "KODEX_INTERNAL_REGISTRY_CPU_REQUEST" "100m" }}"
              memory: "{{ envOr "KODEX_INTERNAL_REGISTRY_MEMORY_REQUEST" "128Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_INTERNAL_REGISTRY_CPU_LIMIT" "1" }}"
              memory: "{{ envOr "KODEX_INTERNAL_REGISTRY_MEMORY_LIMIT" "512Mi" }}"
          securityContext:
            runAsNonRoot: true
            runAsUser: 1000
            runAsGroup: 1000
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
          volumeMounts:
            - name: data
              mountPath: /var/lib/registry
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: kodex-registry-data
