apiVersion: v1
kind: ServiceAccount
metadata:
  name: mattermost-oauth2-proxy
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-oauth2-proxy
    app.kubernetes.io/component: oauth2-proxy
automountServiceAccountToken: false
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mattermost-oauth2-proxy
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-oauth2-proxy
    app.kubernetes.io/component: oauth2-proxy
data:
  authenticated-emails.txt: "${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_AUTHENTICATED_EMAILS}"
---
apiVersion: v1
kind: Service
metadata:
  name: mattermost-oauth2-proxy
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-oauth2-proxy
    app.kubernetes.io/component: oauth2-proxy
spec:
  selector:
    app.kubernetes.io/name: mattermost-oauth2-proxy
    app.kubernetes.io/component: oauth2-proxy
  ports:
    - name: http
      port: 4180
      targetPort: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mattermost-oauth2-proxy
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost-oauth2-proxy
    app.kubernetes.io/component: oauth2-proxy
spec:
  replicas: ${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_REPLICAS}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: mattermost-oauth2-proxy
      app.kubernetes.io/component: oauth2-proxy
  template:
    metadata:
      labels:
        app.kubernetes.io/name: mattermost-oauth2-proxy
        app.kubernetes.io/component: oauth2-proxy
    spec:
      serviceAccountName: mattermost-oauth2-proxy
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: oauth2-proxy
          image: ${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_IMAGE}
          imagePullPolicy: IfNotPresent
          args:
            - "--provider=google"
            - "--http-address=0.0.0.0:4180"
            - "--upstream=http://mattermost.${MATTERCODEX_NAMESPACE}.svc.cluster.local:8065/"
            - "--redirect-url=https://${MATTERCODEX_MATTERMOST_HOST}/oauth2/callback"
            - "--authenticated-emails-file=/etc/oauth2-proxy/authenticated-emails.txt"
            - "--scope=openid email profile"
            - "--cookie-name=_mattercodex_mattermost_oauth2"
            - "--cookie-domain=${MATTERCODEX_MATTERMOST_HOST}"
            - "--whitelist-domain=${MATTERCODEX_MATTERMOST_HOST}"
            - "--cookie-secure=true"
            - "--cookie-httponly=true"
            - "--cookie-samesite=lax"
            - "--cookie-expire=8h"
            - "--reverse-proxy=true"
            - "--set-xauthrequest=true"
            - "--pass-access-token=false"
            - "--pass-authorization-header=false"
            - "--session-cookie-minimal=true"
            - "--skip-provider-button=true"
            - "--standard-logging=false"
            - "--auth-logging=true"
            - "--request-logging=true"
            - "--silence-ping-logging=true"
          env:
            - name: OAUTH2_PROXY_CLIENT_ID
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET}
                  key: OAUTH_CLIENT_ID
            - name: OAUTH2_PROXY_CLIENT_SECRET
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET}
                  key: OAUTH_CLIENT_SECRET
            - name: OAUTH2_PROXY_COOKIE_SECRET
              valueFrom:
                secretKeyRef:
                  name: ${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET}
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
              cpu: ${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CPU_REQUEST}
              memory: ${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_MEMORY_REQUEST}
            limits:
              cpu: ${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CPU_LIMIT}
              memory: ${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_MEMORY_LIMIT}
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
            name: mattermost-oauth2-proxy
