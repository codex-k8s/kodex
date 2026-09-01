def sha256: type == "string" and test("^[a-f0-9]{64}$");
def exact_digest:
  type == "object" and (keys == ["sha256"]) and (.sha256 | sha256);
def dependency_key:
  [.uri, .digest.sha256] | join("@");

type == "object" and
._type == "https://in-toto.io/Statement/v0.1" and
.predicateType == "https://slsa.dev/provenance/v1" and
(.subject | type == "array" and length == 1) and
(.subject[0] | type == "object" and (.name | type == "string" and length > 0) and
  (.digest | exact_digest) and .digest.sha256 == $image) and
(.predicate | type == "object") and
.predicate.buildDefinition.buildType == $build_type and
.predicate.runDetails.builder.id == $builder_id and
(.predicate.buildDefinition.resolvedDependencies | type == "array" and length > 0) and
all(.predicate.buildDefinition.resolvedDependencies[];
  type == "object" and (.uri | type == "string" and length > 0) and
  (.digest | exact_digest)) and
([.predicate.buildDefinition.resolvedDependencies[] | dependency_key] as $dependencies |
  ($dependencies | unique | length) == ($dependencies | length)) and
any(.predicate.buildDefinition.resolvedDependencies[]; .digest.sha256 == $base) and
any(.predicate.buildDefinition.resolvedDependencies[]; .digest.sha256 == $frontend)
