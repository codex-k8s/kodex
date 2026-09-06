def workload($kind; $name): .[] | select(.kind == $kind and .metadata.name == $name);
def named($name): .[] | select(.name == $name);
def one_handler:
  [keys[] | select(. == "exec" or . == "httpGet" or . == "tcpSocket" or . == "grpc")] | length == 1;

([workload("Deployment"; "stt-tts-service")] | length == 1) and
any(workload("Deployment"; "stt-tts-service") | .spec.template.spec;
  .serviceAccountName == "stt-tts-service" and
  ([.containers[].name] | sort) == ["internal-rpc-authority-issuer", "internal-rpc-authority-verifier", "stt-tts-service"] and
  all(.containers[];
    .command == ["/workspace/tools/dev/run-go-hot-reload.sh"] and
    .securityContext.runAsNonRoot == true and
    .securityContext.allowPrivilegeEscalation == false and
    .securityContext.capabilities.drop == ["ALL"] and
    all((.startupProbe, .readinessProbe, .livenessProbe) | select(. != null); one_handler) and
    all(["dev-source", "dev-go-mod", "dev-go-sumdb", "dev-go-tools"][] as $name |
      [.volumeMounts[] | select(.name == $name)]; length == 1 and .[0].readOnly == true)) and
  any(.containers | named("stt-tts-service");
    .image == $image and
    .args == ["services/internal/stt-tts-service", "./cmd/stt-tts-service", "stt-tts-service"] and
    .securityContext.readOnlyRootFilesystem == true and
    .readinessProbe.httpGet == {path:"/readyz", port:"metrics"} and
    any(.volumeMounts[]; .name == "stt-spool" and .mountPath == "/var/lib/kodex/stt-spool") and
    any(.volumeMounts[]; .name == "workload-tls" and .readOnly == true)) and
  any(.containers | named("internal-rpc-authority-issuer");
    .args[1] == "./cmd/internal-rpc-authority-issuer") and
  any(.containers | named("internal-rpc-authority-verifier");
    .args[1] == "./cmd/internal-rpc-authority-verifier") and
  any(.initContainers[]; .name == "internal-rpc-authority-socket-init" and
    .command == ["/workspace/tools/dev/run-authority-socket-init.sh"]) and
  any(.volumes[]; .name == "stt-spool" and .emptyDir == {medium:"Memory", sizeLimit:"64Mi"}) and
  any(.volumes[]; .name == "dev-stt-tmp" and .emptyDir.sizeLimit == "128Mi")) and
any(workload("Service"; "stt-tts-service");
  .spec.selector["app.kubernetes.io/name"] == "stt-tts-service" and
  any(.spec.ports[]; .port == 8443)) and
($targets | map(.role) | sort) == ["AUTHORIZATION_ISSUER", "AUTHORIZATION_VERIFIER"] and
all($targets[]; .startup_readback_required == true and
  .namespace == "kodex-system" and .service_account == "stt-tts-service") and
any(workload("NetworkPolicy"; "stt-tts-service-default-deny");
  .spec.policyTypes == ["Ingress", "Egress"] and
  (.spec.ingress // []) == [] and (.spec.egress // []) == []) and
any(workload("NetworkPolicy"; "stt-tts-service-exact-runtime-paths");
  any(.spec.egress[]; .ports == [{protocol:"TCP", port:8081}] and
    any(.to[]; .namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == "kodex-system" and
      .podSelector.matchLabels["app.kubernetes.io/name"] == "egress-gateway")))
