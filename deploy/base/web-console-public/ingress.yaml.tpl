apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: web-console-public
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: web-console-public
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: public-web
  annotations:
    kubernetes.io/ingress.class: {{ envOr "KODEX_PUBLIC_INGRESS_CLASS" "kodex-public" }}
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
    traefik.ingress.kubernetes.io/router.tls: "true"
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
          - path: /
            pathType: Prefix
            backend:
              service:
                name: web-console-public-oauth2-proxy
                port:
                  number: 4180
