apiVersion: v1
kind: ServiceAccount
metadata:
  name: web-console-public-oauth2-proxy
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: web-console-public-oauth2-proxy
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web
---
apiVersion: v1
kind: Service
metadata:
  name: web-console-public-oauth2-proxy
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: web-console-public-oauth2-proxy
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web
spec:
  selector:
    app.kubernetes.io/name: web-console-public-oauth2-proxy
    app.kubernetes.io/part-of: kodex
  ports:
    - name: http
      port: 4180
      targetPort: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-console-public-oauth2-proxy
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: web-console-public-oauth2-proxy
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web
spec:
  replicas: {{ envOr "KODEX_OAUTH2_PROXY_REPLICAS" "2" }}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: web-console-public-oauth2-proxy
      app.kubernetes.io/part-of: kodex
  template:
    metadata:
      labels:
        app.kubernetes.io/name: web-console-public-oauth2-proxy
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: public-web
    spec:
      serviceAccountName: web-console-public-oauth2-proxy
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: oauth2-proxy
          image: {{ imageOr "oauth2-proxy" "KODEX_OAUTH2_PROXY_IMAGE" }}
          imagePullPolicy: IfNotPresent
          args:
            - "--provider=github"
            - "--http-address=0.0.0.0:4180"
            - "--upstream=http://web-console:8080/"
            - "--redirect-url=https://platform.kodex.works/oauth2/callback"
            - "--authenticated-emails-file=/etc/oauth2-proxy/authenticated-emails.txt"
            - "--scope=user:email read:org"
            - "--cookie-domain=platform.kodex.works"
            - "--cookie-secure=true"
            - "--cookie-httponly=true"
            - "--cookie-samesite=lax"
            - "--cookie-expire=8h"
            - "--reverse-proxy=true"
            - "--set-xauthrequest=true"
            - "--pass-user-headers=true"
            - "--pass-access-token=false"
            - "--pass-authorization-header=false"
            - "--session-cookie-minimal=true"
            - "--skip-provider-button=true"
            - "--standard-logging=false"
            - "--auth-logging=true"
            - "--request-logging=true"
            - "--auth-logging-format=[{{`{{.Timestamp}}`}}] [auth] {{`{{.Status}}`}} {{`{{.Message}}`}}"
            - "--request-logging-format=[{{`{{.Timestamp}}`}}] [request] {{`{{.StatusCode}}`}} {{`{{.RequestMethod}}`}} {{`{{.Host}}`}} {{`{{.Upstream}}`}} {{`{{.RequestDuration}}`}}"
            - "--silence-ping-logging=true"
            - "--trusted-proxy-ip=10.0.0.0/8"
            - "--trusted-proxy-ip=172.16.0.0/12"
            - "--trusted-proxy-ip=192.168.0.0/16"
          env:
            - name: OAUTH2_PROXY_CLIENT_ID
              valueFrom:
                secretKeyRef:
                  name: kodex-web-oauth2-proxy
                  key: KODEX_GITHUB_OAUTH_CLIENT_ID
            - name: OAUTH2_PROXY_CLIENT_SECRET
              valueFrom:
                secretKeyRef:
                  name: kodex-web-oauth2-proxy
                  key: KODEX_GITHUB_OAUTH_CLIENT_SECRET
            - name: OAUTH2_PROXY_COOKIE_SECRET
              valueFrom:
                secretKeyRef:
                  name: kodex-web-oauth2-proxy
                  key: KODEX_OAUTH2_PROXY_COOKIE_SECRET
          ports:
            - name: http
              containerPort: 4180
          volumeMounts:
            - name: authenticated-emails
              mountPath: /etc/oauth2-proxy/authenticated-emails.txt
              subPath: authenticated-emails.txt
              readOnly: true
          readinessProbe:
            httpGet:
              path: /ping
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
            timeoutSeconds: 2
            failureThreshold: 6
          livenessProbe:
            httpGet:
              path: /ping
              port: http
            initialDelaySeconds: 10
            periodSeconds: 20
            timeoutSeconds: 2
            failureThreshold: 3
          resources:
            requests:
              cpu: "{{ envOr "KODEX_OAUTH2_PROXY_CPU_REQUEST" "50m" }}"
              memory: "{{ envOr "KODEX_OAUTH2_PROXY_MEMORY_REQUEST" "64Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_OAUTH2_PROXY_CPU_LIMIT" "500m" }}"
              memory: "{{ envOr "KODEX_OAUTH2_PROXY_MEMORY_LIMIT" "128Mi" }}"
          securityContext:
            runAsNonRoot: true
            runAsUser: 2000
            runAsGroup: 2000
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
      volumes:
        - name: authenticated-emails
          configMap:
            name: web-console-public-oauth2-proxy
