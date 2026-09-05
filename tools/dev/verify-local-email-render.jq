def resource($kind; $name): .[] | select(.kind == $kind and .metadata.name == $name);
def named($name): .[] | select(.name == $name);
def policy_spec: .spec | .ingress //= [] | .egress //= [];
def mount($name; $path; $readonly):
  [.volumeMounts[] | select(.name == $name and .mountPath == $path and (.readOnly // false) == $readonly)] | length == 1;
def one_handler:
  [keys[] | select(. == "exec" or . == "httpGet" or . == "tcpSocket" or . == "grpc")] | length == 1;
def go_cache:
  .image == "docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83" and
  .securityContext.readOnlyRootFilesystem == true and
  .securityContext.allowPrivilegeEscalation == false and
  .securityContext.capabilities.drop == ["ALL"] and
  mount("dev-source"; "/workspace"; true) and
  mount("dev-go-mod"; "/go/pkg/mod"; true) and
  mount("dev-go-sumdb"; "/go/pkg/sumdb"; true) and
  mount("dev-go-tools"; "/go/tools"; true) and
  mount(("dev-build-" + .name); "/go/build-cache"; false) and
  any(.env[]; .name == "GOTOOLCHAIN" and .value == "local") and
  any(.env[]; .name == "GOWORK" and .value == "off");

. as $all |
([resource("Deployment"; "email-bridge")] | length == 1) and
any(resource("Deployment"; "email-bridge");
  .spec.replicas == 1 and
  .spec.template.metadata.annotations["kodex.dev/source-revision"] ==
    ($all | resource("ConfigMap"; "kodex-dev-source-provenance") | .data.sourceRevision) and
  (.spec.template.spec |
    .serviceAccountName == "email-bridge" and .automountServiceAccountToken == false and
    .securityContext.runAsNonRoot == true and .securityContext.runAsUser == 10001 and
    ([.containers[].name] | sort) == ["email-bridge", "internal-rpc-authority-issuer", "platform-worker-grant-agent"] and
    all(.containers[];
      go_cache and .command == ["/workspace/tools/dev/run-go-hot-reload.sh"] and
      all((.startupProbe, .readinessProbe, .livenessProbe); one_handler) and
      mount(("dev-email-tmp-" + .name); "/tmp"; false)) and
    any(.containers | named("email-bridge");
      .args == ["services/internal/email-bridge", "./cmd/email-bridge", "email-bridge"] and
      .readinessProbe.httpGet == {path:"/readyz", port:"metrics"} and
      mount("tls"; "/var/run/email/tls"; true) and
      mount("database"; "/var/run/email/database"; true) and
      mount("mail"; "/var/run/email/mail"; true) and
      all(.volumeMounts[]; (.subPath // "") == "" and (.subPathExpr // "") == "") and
      mount("application-grant"; "/var/run/secrets/kodex/email-bridge/application-grant"; true)) and
    any(.containers | named("internal-rpc-authority-issuer"); .args[1] == "./cmd/internal-rpc-authority-issuer" and .securityContext.runAsUser == 29001) and
    any(.containers | named("platform-worker-grant-agent");
      .args[1] == "./cmd/internal-rpc-authority-platform-worker-grant-agent" and .securityContext.runAsUser == 29004 and
      any(.env[]; .name == "PLATFORM_WORKER_GRANT_WORKLOAD_ID" and .value == "email-bridge") and
      mount("application-grant"; "/var/run/secrets/kodex/email-bridge/application-grant"; false)) and
    any(.initContainers[]; .name == "internal-rpc-authority-socket-init" and
      go_cache and .command == ["/workspace/tools/dev/run-authority-socket-init.sh"] and
      .securityContext.runAsNonRoot == true and .securityContext.runAsUser == 29000 and
      .securityContext.runAsGroup == 29000 and (.securityContext.capabilities.add // []) == [] and
      mount("dev-email-init-bin"; "/usr/local/bin"; false) and
      mount("dev-email-init-tmp"; "/tmp"; false)) and
    any(.volumes[]; .name == "database" and .secret.secretName == "email-bridge-runtime-database") and
    any(.volumes[]; .name == "mail" and .secret.secretName == "email-bridge-mailbox-projection" and
      (.secret.optional // false) == false and (.secret | has("items") | not)) and
    all(.volumes[]; .name != "configuration") and
    any(.volumes[]; .name == "tls" and .secret.secretName == "email-bridge-tls") and
    any(.volumes[]; .name == "platform-worker-grant-signer" and .secret.secretName == "email-bridge-platform-worker-grant-signer") and
    all(.volumes[] | select(.name | startswith("dev-email-tmp-")); .emptyDir == {sizeLimit:"128Mi"}))) and
any(resource("Job"; "email-bridge-migration") | .spec.template.spec;
  .serviceAccountName == "email-bridge-migration" and .automountServiceAccountToken == false and
  .securityContext.runAsNonRoot == true and .securityContext.runAsUser == 10001 and
  any(.containers | named("migration"); go_cache and
    .command == ["/workspace/tools/dev/run-go-command.sh"] and
    .args == ["services/internal/email-bridge", "./cmd/cli", "up"] and
    mount("database"; "/var/run/email/database"; true) and mount("tls"; "/var/run/email/tls"; true)) and
  any(.volumes[]; .name == "database" and .secret.secretName == "email-bridge-migration-database")) and
any(resource("StatefulSet"; "email-bridge-postgresql") | .spec.template.spec;
  .serviceAccountName == "email-bridge-postgresql" and .automountServiceAccountToken == false and
  .securityContext.runAsNonRoot == true and .securityContext.runAsUser == 70 and
  any(.containers | named("postgresql"); (.args | index("ssl=on")) != null)) and
any(resource("ConfigMap"; "email-bridge-runtime");
  .data.EMAIL_BRIDGE_AUTHORITY_TARGET == "control-plane.kodex-system.svc.cluster.local:8443" and
  .data.EMAIL_BRIDGE_EGRESS_ADDRESS == "egress-gateway.kodex-system.svc:8082" and
  .data.EMAIL_BRIDGE_SECRETS_ROOT == "/var/run/email/mail" and
  (.data | has("EMAIL_BRIDGE_CONFIGURATION_FILE") | not)) and
any(resource("Secret"; "email-bridge-mailbox-projection");
  .metadata.labels["app.kubernetes.io/managed-by"] == "control-plane" and
  .type == "Opaque" and .immutable != true and
  (.stringData["mailboxes.json"] | fromjson | .version == "email-bridge/v1")) and
any(resource("Role"; "control-plane-email-projection-writer");
  .rules == [{apiGroups:[""],resources:["secrets"],resourceNames:["email-bridge-mailbox-projection"],verbs:["get","update"]}]) and
any(resource("RoleBinding"; "control-plane-email-projection-writer");
  .roleRef == {apiGroup:"rbac.authorization.k8s.io",kind:"Role",name:"control-plane-email-projection-writer"} and
  .subjects == [{kind:"ServiceAccount",name:"control-plane",namespace:"kodex-system"}]) and
any(resource("Service"; "email-bridge");
  .spec.selector["app.kubernetes.io/name"] == "email-bridge" and
  any(.spec.ports[]; .name == "https" and .port == 443 and .targetPort == "https")) and
($targets | length == 1) and
all($targets[]; .role == "AUTHORIZATION_ISSUER" and .startup_readback_required == true and
  .namespace == "kodex-system" and .service_account == "email-bridge" and
  .database_identity.login_principal == "ira_email_bridge_issuer_g1") and
all($policies[]; . as $expected |
  any($all[]; .kind == "NetworkPolicy" and .metadata.name == $expected.metadata.name and
    policy_spec == ($expected | policy_spec))) and
all($all[] | select(.kind == "Ingress");
  all(.spec.rules[]?.http.paths[]?; .backend.service.name != "email-bridge"))
