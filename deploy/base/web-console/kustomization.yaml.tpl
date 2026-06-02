apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
generatorOptions:
  disableNameSuffixHash: true
configMapGenerator:
  - name: web-console-nginx
    files:
      - nginx.conf=nginx.conf
resources:
  - web-console.yaml
