apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
generatorOptions:
  disableNameSuffixHash: true
configMapGenerator:
  - name: web-console-public-oauth2-proxy
    files:
      - authenticated-emails.txt=authenticated-emails.txt
resources:
  - oauth2-proxy.yaml
  - certificate.yaml
  - ingress.yaml
