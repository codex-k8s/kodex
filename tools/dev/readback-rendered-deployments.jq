([.items[] |
  select(.metadata.name as $name |
    $expected_deployments | index($name) != null)]) as $rendered |
($rendered | length) == ($expected_deployments | length) and
($rendered | length) > 0 and
all($rendered[];
  .spec.template.metadata.annotations[
    "kodex.dev/runtime-admission-policy-sha256"] == $policy.policySHA256 and
  all(((.spec.template.spec.initContainers // []) +
      (.spec.template.spec.containers // []))[];
    all((.env // [])[];
      .valueFrom.configMapKeyRef.name !=
        "kodex-image-admission-policy")))
