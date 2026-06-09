apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mattermost
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: mattermost
    app.kubernetes.io/component: ingress
  annotations:
    cert-manager.io/cluster-issuer: ${MATTERCODEX_CLUSTER_ISSUER}
spec:
  ingressClassName: ${MATTERCODEX_INGRESS_CLASS}
  tls:
    - hosts:
        - ${MATTERCODEX_MATTERMOST_HOST}
      secretName: ${MATTERCODEX_TLS_SECRET}
  rules:
    - host: ${MATTERCODEX_MATTERMOST_HOST}
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: mattermost
                port:
                  name: http
