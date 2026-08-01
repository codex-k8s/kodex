def sha256: type == "string" and test("^[a-f0-9]{64}$");
def exact_digest:
  type == "object" and (keys == ["sha256"]) and (.sha256 | sha256);
def parameters:
  .predicate.buildDefinition.externalParameters.args;
def dependency_key:
  [.uri, .digest.sha256] | join("@");

type == "object" and
._type == "https://in-toto.io/Statement/v1" and
.predicateType == "https://slsa.dev/provenance/v1" and
(.subject | type == "array" and length == 1) and
(.subject[0] | type == "object" and .name == $subject and
  (.digest | exact_digest) and .digest.sha256 == $image) and
(.predicate | type == "object") and
.predicate.buildDefinition.buildType == $build_type and
.predicate.runDetails.builder.id == $builder_id and
(parameters | type == "object") and
parameters["label:mattercodex.dev/source-sha256"] == $source and
parameters["label:mattercodex.dev/build-tag"] == $build_tag and
parameters["label:mattercodex.dev/admission-tools-sha256"] == $tools_digest and
parameters["label:mattercodex.dev/admission-policy-revision"] == $policy_revision and
(.predicate.buildDefinition.resolvedDependencies | type == "array" and length > 0) and
all(.predicate.buildDefinition.resolvedDependencies[];
  type == "object" and (.uri | type == "string" and length > 0) and
  (.digest | exact_digest)) and
([.predicate.buildDefinition.resolvedDependencies[] | dependency_key] as $dependencies |
  ($dependencies | unique | length) == ($dependencies | length)) and
($source | sub("^sha256:"; "")) as $source_hex |
([.predicate.buildDefinition.resolvedDependencies[] |
  select(.uri == ("mattercodex:source/" + $build_tag) and
    .digest.sha256 == $source_hex)] | length == 1)
