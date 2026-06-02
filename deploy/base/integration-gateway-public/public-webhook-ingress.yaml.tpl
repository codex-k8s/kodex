apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: integration-gateway-public-webhook
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: integration-gateway-public-webhook
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-provider-webhook
  annotations:
    kubernetes.io/ingress.class: {{ envOr "KODEX_PUBLIC_INGRESS_CLASS" "kodex-public" }}
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
    traefik.ingress.kubernetes.io/router.tls: "true"
    traefik.ingress.kubernetes.io/router.priority: "100"
spec:
  ingressClassName: {{ envOr "KODEX_PUBLIC_INGRESS_CLASS" "kodex-public" }}
  tls:
    - hosts:
        - {{ envOr "KODEX_PRODUCTION_DOMAIN" "platform.kodex.works" }}
      secretName: web-console-public-tls
  rules:
    - host: {{ envOr "KODEX_PRODUCTION_DOMAIN" "platform.kodex.works" }}
      http:
        paths:
          - path: /v1/provider-webhooks/github
            pathType: Exact
            backend:
              service:
                name: integration-gateway
                port:
                  number: 8080
