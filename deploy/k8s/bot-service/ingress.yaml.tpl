apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: matter-codex-bot-service
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: bot-service-ingress
  annotations:
    cert-manager.io/cluster-issuer: ${MATTERCODEX_CLUSTER_ISSUER}
    kubernetes.io/ingress.class: ${MATTERCODEX_INGRESS_CLASS}
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
    traefik.ingress.kubernetes.io/router.tls: "true"
spec:
  ingressClassName: ${MATTERCODEX_INGRESS_CLASS}
  tls:
    - hosts:
        - ${MATTERCODEX_BOT_SERVICE_HOST}
      secretName: ${MATTERCODEX_BOT_SERVICE_TLS_SECRET}
  rules:
    - host: ${MATTERCODEX_BOT_SERVICE_HOST}
      http:
        paths:
          - path: /mattermost/slash/agents
            pathType: Exact
            backend:
              service:
                name: matter-codex-bot-service
                port:
                  name: http
          - path: /github/webhook
            pathType: Exact
            backend:
              service:
                name: matter-codex-bot-service
                port:
                  name: http
          - path: /control-center
            pathType: Exact
            backend:
              service:
                name: matter-codex-bot-service
                port:
                  name: http
          - path: /control-center/
            pathType: Prefix
            backend:
              service:
                name: matter-codex-bot-service
                port:
                  name: http
          - path: /api/control-center/v1/automation-runs
            pathType: Exact
            backend:
              service:
                name: matter-codex-bot-service
                port:
                  name: http
