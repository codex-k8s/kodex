apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
resources:
  - postgres
  - platform-event-log/migrations.yaml
  - access-manager/migrations.yaml
  - access-manager/access-manager.yaml
