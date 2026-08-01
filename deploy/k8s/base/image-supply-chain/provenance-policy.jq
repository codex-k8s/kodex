type == "object" and
._type == "https://in-toto.io/Statement/v1" and
.predicateType == "https://slsa.dev/provenance/v1" and
([.subject[]? | select(.digest.sha256 == $image)] | length == 1) and
(
  .predicate.buildDefinition.externalParameters.sourceDigest == $source or
  .predicate.invocation.parameters.sourceDigest == $source or
  .predicate.invocation.parameters.frontendAttrs["label:mattercodex.dev/source-sha256"] == $source
)
