# Только disposable renderer: workload-wide watermark требует одного grant writer.
def grant_agent:
  ((.name // "") | endswith("platform-worker-grant-agent")) or
  any(((.command // []) + (.args // []))[];
    test("(^|/)internal-rpc-authority-platform-worker-grant-agent$"));
def grant_deployment:
  .kind == "Deployment" and
  any(.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?; grant_agent);
def known_worker:
  .metadata.name as $name |
  ["control-plane", "secret-broker", "email-bridge", "runtime-controller",
   "integration-gateway", "interaction-gateway", "automation-scheduler",
   "session-archive", "role-image-builder"] | index($name) != null;
map(if grant_deployment then
  if known_worker then
    .spec.replicas = 1 | .spec.strategy = {type:"Recreate"}
  else error("unregistered local worker grant Deployment") end
else . end)
