apiVersion: v1
kind: ServiceAccount
metadata:
  name: kodex-public-ingress
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: kodex-public-ingress
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web-foundation
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kodex-public-ingress
  labels:
    app.kubernetes.io/name: kodex-public-ingress
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web-foundation
rules:
  - apiGroups:
      - ""
    resources:
      - services
      - endpoints
      - secrets
      - nodes
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - discovery.k8s.io
    resources:
      - endpointslices
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - networking.k8s.io
    resources:
      - ingresses
      - ingressclasses
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - networking.k8s.io
    resources:
      - ingresses/status
    verbs:
      - update
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kodex-public-ingress
  labels:
    app.kubernetes.io/name: kodex-public-ingress
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web-foundation
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kodex-public-ingress
subjects:
  - kind: ServiceAccount
    name: kodex-public-ingress
    namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
---
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  name: {{ envOr "KODEX_PUBLIC_INGRESS_CLASS" "kodex-public" }}
  labels:
    app.kubernetes.io/name: kodex-public-ingress
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web-foundation
  annotations:
    ingressclass.kubernetes.io/is-default-class: "false"
spec:
  controller: traefik.io/ingress-controller
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kodex-public-ingress
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: kodex-public-ingress
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web-foundation
spec:
  replicas: 1
  revisionHistoryLimit: 3
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app.kubernetes.io/name: kodex-public-ingress
      app.kubernetes.io/part-of: kodex
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kodex-public-ingress
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: public-web-foundation
    spec:
      serviceAccountName: kodex-public-ingress
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: false
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: traefik
          image: {{ imageOr "traefik" "KODEX_TRAEFIK_IMAGE" }}
          imagePullPolicy: IfNotPresent
          args:
            - "--global.checknewversion=false"
            - "--global.sendanonymoususage=false"
            - "--log.level={{ envOr "KODEX_PUBLIC_INGRESS_LOG_LEVEL" "INFO" }}"
            - "--accesslog=false"
            - "--entrypoints.traefik.address=:9000"
            - "--entrypoints.web.address=:80"
            - "--entrypoints.websecure.address=:443"
            - "--providers.kubernetesingress=true"
            - "--providers.kubernetesingress.allowemptyservices=false"
            - "--ping=true"
            - "--ping.entrypoint=traefik"
          ports:
            - name: web
              containerPort: 80
              protocol: TCP
            - name: websecure
              containerPort: 443
              protocol: TCP
            - name: traefik
              containerPort: 9000
              protocol: TCP
          readinessProbe:
            httpGet:
              path: /ping
              port: traefik
            initialDelaySeconds: 5
            periodSeconds: 10
            timeoutSeconds: 2
            failureThreshold: 6
          livenessProbe:
            httpGet:
              path: /ping
              port: traefik
            initialDelaySeconds: 10
            periodSeconds: 20
            timeoutSeconds: 2
            failureThreshold: 3
          resources:
            requests:
              cpu: "{{ envOr "KODEX_PUBLIC_INGRESS_CPU_REQUEST" "100m" }}"
              memory: "{{ envOr "KODEX_PUBLIC_INGRESS_MEMORY_REQUEST" "128Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_PUBLIC_INGRESS_CPU_LIMIT" "1" }}"
              memory: "{{ envOr "KODEX_PUBLIC_INGRESS_MEMORY_LIMIT" "512Mi" }}"
          securityContext:
            runAsNonRoot: false
            runAsUser: 0
            runAsGroup: 0
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
              add:
                - NET_BIND_SERVICE
