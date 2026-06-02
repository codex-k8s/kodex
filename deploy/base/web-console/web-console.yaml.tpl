apiVersion: v1
kind: ServiceAccount
metadata:
  name: web-console
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: web-console
    app.kubernetes.io/part-of: kodex
---
apiVersion: v1
kind: Service
metadata:
  name: web-console
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: web-console
    app.kubernetes.io/part-of: kodex
spec:
  selector:
    app.kubernetes.io/name: web-console
    app.kubernetes.io/part-of: kodex
  ports:
    - name: http
      port: 8080
      targetPort: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-console
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
  labels:
    app.kubernetes.io/name: web-console
    app.kubernetes.io/part-of: kodex
spec:
  replicas: {{ envOr "KODEX_WEB_CONSOLE_REPLICAS" "2" }}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: web-console
      app.kubernetes.io/part-of: kodex
  template:
    metadata:
      labels:
        app.kubernetes.io/name: web-console
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: staff-frontend
    spec:
      serviceAccountName: web-console
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
      initContainers:
        - name: wait-staff-gateway
          image: {{ imageOr "busybox" "KODEX_BUSYBOX_IMAGE" }}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-ec"]
          args:
            - |
              until wget -q -T 2 -O - http://staff-gateway:8080/health/readyz >/dev/null 2>&1; do
                sleep 2
              done
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
      containers:
        - name: web-console
          image: {{ image "web-console" }}
          imagePullPolicy: Always
          ports:
            - name: http
              containerPort: 8080
          volumeMounts:
            - name: nginx-config
              mountPath: /etc/nginx/conf.d/default.conf
              subPath: nginx.conf
              readOnly: true
            - name: nginx-cache
              mountPath: /var/cache/nginx
            - name: nginx-run
              mountPath: /var/run
            - name: tmp
              mountPath: /tmp
          readinessProbe:
            httpGet:
              path: /health/readyz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
            timeoutSeconds: 2
            failureThreshold: 6
          livenessProbe:
            httpGet:
              path: /health/livez
              port: http
            initialDelaySeconds: 10
            periodSeconds: 20
            timeoutSeconds: 2
            failureThreshold: 3
          resources:
            requests:
              cpu: "{{ envOr "KODEX_WEB_CONSOLE_CPU_REQUEST" "50m" }}"
              memory: "{{ envOr "KODEX_WEB_CONSOLE_MEMORY_REQUEST" "64Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_WEB_CONSOLE_CPU_LIMIT" "500m" }}"
              memory: "{{ envOr "KODEX_WEB_CONSOLE_MEMORY_LIMIT" "128Mi" }}"
          securityContext:
            runAsNonRoot: true
            runAsUser: 101
            runAsGroup: 101
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
      volumes:
        - name: nginx-config
          configMap:
            name: web-console-nginx
        - name: nginx-cache
          emptyDir: {}
        - name: nginx-run
          emptyDir: {}
        - name: tmp
          emptyDir: {}
