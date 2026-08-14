def approved_control_center_ingress:
  .metadata.name == "control-center-public" and
  .metadata.namespace == "mattercodex-system" and
  (.metadata.deletionTimestamp // null) == null and
  .metadata.labels["app.kubernetes.io/name"] == "control-center-public-bridge" and
  .metadata.labels["app.kubernetes.io/component"] == "public-entrypoint" and
  .metadata.labels["mattercodex.dev/release-managed"] != "true" and
  .metadata.annotations["cert-manager.io/cluster-issuer"] == "letsencrypt-prod" and
  .metadata.annotations["kubernetes.io/ingress.class"] == "kodex-public" and
  .metadata.annotations["traefik.ingress.kubernetes.io/router.entrypoints"] == "websecure" and
  .metadata.annotations["traefik.ingress.kubernetes.io/router.tls"] == "true" and
  .spec.ingressClassName == "kodex-public" and
  (.spec.defaultBackend // null) == null and
  .spec.tls == [{"hosts":["control.kodex.works"],"secretName":"control-center-public-tls"}] and
  .spec.rules == [{
    "host":"control.kodex.works",
    "http":{"paths":[{
      "backend":{"service":{"name":"control-center-public-bridge","port":{"name":"http"}}},
      "path":"/",
      "pathType":"Prefix"
    }]}
  }];

(.items | length) == 0 or
((.items | length) == 1 and (.items[0] | approved_control_center_ingress))
