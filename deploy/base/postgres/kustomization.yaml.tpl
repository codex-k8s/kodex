apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
resources:
  - secrets.yaml
  - postgres.yaml
  - ../platform-event-log/migrations.yaml
configMapGenerator:
  - name: kodex-postgres-bootstrap
    files:
      - bootstrap-databases.sh
generatorOptions:
  disableNameSuffixHash: true
