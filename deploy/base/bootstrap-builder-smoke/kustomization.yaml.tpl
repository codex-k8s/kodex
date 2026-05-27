apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
resources:
  - registry-mirror-smoke.yaml
  - kaniko-smoke.yaml
configMapGenerator:
  - name: kodex-kaniko-smoke-context
    files:
      - Dockerfile=kaniko-smoke.Dockerfile
      - marker=kaniko-smoke.marker
generatorOptions:
  disableNameSuffixHash: true
