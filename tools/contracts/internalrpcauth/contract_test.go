package internalrpcauth_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const (
	issuerSocket   = "/run/mattercodex/internal-rpc-authority/issuer.sock"
	verifierSocket = "/run/mattercodex/internal-rpc-authority/verifier.sock"

	issuerReadyMethod = "/internalrpcauthority.v1.AuthorizationIssuerService/CheckReadiness"
	verifierReady     = "/internalrpcauthority.v1.AuthorizationVerifierService/CheckReadiness"
)

func TestJSONSchemasAreClosed(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{
		"jws-protected-header.schema.json",
		"authority-proof.schema.json",
		"authorization-context.schema.json",
		"authority-snapshot.schema.json",
		"key-delivery-targets.schema.json",
		"readback-attestation.schema.json",
		"readback-credential.schema.json",
		"readback-credential-trust.schema.json",
		"readback-manifest-trust-root.schema.json",
		"readback-root-verification-material.schema.json",
		"readback-root-rotation.schema.json",
		"restore-fence-evidence.schema.json",
		"restore-role-trust.schema.json",
	} {
		path := filepath.Join(root, "contracts/authorization/v1", name)
		var schema map[string]any
		decodeJSONStrict(t, path, &schema)

		closed, ok := schema["additionalProperties"].(bool)
		if !ok || closed {
			t.Fatalf("%s must reject unknown top-level properties", name)
		}
	}
}

func TestExecutableReadbackMigrationУбираетDirectDML(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(
		root,
		"services/internal/internal-rpc-authority/cmd/cli/migrations",
		"20260730000600_internal_rpc_authority_owner_bound_readback.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{
		"REVOKE INSERT, UPDATE, DELETE",
		"issue_authority_readback_attestation_challenge(",
		"consume_authority_readback_attestation_challenge(",
		"SECURITY DEFINER",
		"SET search_path = pg_catalog, internal_rpc_authority, pg_temp",
		"OWNER TO internal_rpc_authority_readback_owner",
		"authority_readback_trust_watermarks",
		"activate_readback_trust(",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("owner-bound migration misses %q", required)
		}
	}
	for _, forbidden := range []string{
		"GRANT INSERT, UPDATE\n    ON internal_rpc_authority.authority_readback_attestation_challenges",
		"GRANT INSERT\n    ON internal_rpc_authority.authority_readback_attestation_receipts",
		"TO internal_rpc_authority_publisher;\nGRANT EXECUTE ON FUNCTION\ninternal_rpc_authority.consume_authority_readback",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("owner-bound migration retains unsafe privilege %q", forbidden)
		}
	}
}

func TestRestoreCoordinationSchemasAreClosed(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "contracts/authorization/v1/restore-coordination.schema.json")
	var schema map[string]any
	decodeJSONStrict(t, path, &schema)
	definitions := requiredMap(t, schema, "$defs")
	for _, name := range []string{
		"credentialIssuanceDirective",
		"credentialDeliveryReceipt",
		"roleCredential",
		"directive",
		"quiescenceAck",
	} {
		definition := requiredMap(t, definitions, name)
		requireEqual(t, definition, "additionalProperties", false)
	}
}

func TestRestoreAndNormalReadbackTrustAndAttestationSchemasAreBound(t *testing.T) {
	root := repositoryRoot(t)
	var trust map[string]any
	decodeJSONStrict(
		t,
		filepath.Join(
			root,
			"contracts/authorization/v1/restore-role-trust.schema.json",
		),
		&trust,
	)
	required := stringSet(trust["required"].([]any))
	for _, field := range []string{
		"source_revision",
		"key_set_revision",
		"trust_set_digest_sha256",
		"predecessor",
		"history",
		"manifest_signer_generation",
		"keys",
	} {
		if !required[field] {
			t.Fatalf("restore role trust does not require %s", field)
		}
	}
	properties := requiredMap(t, trust, "properties")
	keys := requiredMap(t, properties, "keys")
	if keys["minItems"] != float64(2) || keys["maxItems"] != float64(3) {
		t.Fatal("restore role trust does not bound CURRENT NEXT PREVIOUS")
	}

	var readbackTrust map[string]any
	decodeJSONStrict(
		t,
		filepath.Join(
			root,
			"contracts/authorization/v1/readback-credential-trust.schema.json",
		),
		&readbackTrust,
	)
	for _, field := range []string{
		"readback_manifest_root_id",
		"readback_manifest_root_fingerprint_sha256",
		"readback_manifest_bundle_revision",
		"readback_manifest_signer_kid",
		"source_revision",
		"key_set_revision",
		"trust_set_digest_sha256",
		"predecessor",
		"history",
		"manifest_signer_generation",
		"keys",
	} {
		if !stringSet(readbackTrust["required"].([]any))[field] {
			t.Fatalf("normal readback trust does not require %s", field)
		}
	}
	readbackTrustProperties := requiredMap(t, readbackTrust, "properties")
	requireEqual(
		t,
		requiredMap(t, readbackTrustProperties, "aud"),
		"const",
		"urn:mattercodex:internal-rpc-authority-readback-attestor",
	)
	readbackTrustKeys := requiredMap(
		t,
		readbackTrustProperties,
		"keys",
	)
	if readbackTrustKeys["minItems"] != float64(2) ||
		readbackTrustKeys["maxItems"] != float64(3) {
		t.Fatal("normal readback trust does not bound CURRENT NEXT PREVIOUS")
	}

	var credential map[string]any
	decodeJSONStrict(
		t,
		filepath.Join(
			root,
			"contracts/authorization/v1/readback-credential.schema.json",
		),
		&credential,
	)
	credentialRequired := stringSet(credential["required"].([]any))
	for _, field := range []string{
		"purpose",
		"intent_id",
		"intent_digest_sha256",
		"workload_spiffe_id",
		"role",
		"workload_generation",
		"credential_generation",
		"material_generation",
		"possession_key_kid",
		"possession_key_generation",
		"possession_public_jwk",
		"possession_key_thumbprint_sha256",
		"credential_signer_source_revision",
		"credential_signer_source_digest_sha256",
		"credential_signer_key_set_revision",
		"credential_signer_generation",
	} {
		if !credentialRequired[field] {
			t.Fatalf("normal readback credential does not bind %s", field)
		}
	}
	credentialProperties := requiredMap(t, credential, "properties")
	requireEqual(
		t,
		requiredMap(t, credentialProperties, "aud"),
		"const",
		"urn:mattercodex:internal-rpc-authority-readback-attestor",
	)
	var restoreCoordination map[string]any
	decodeJSONStrict(
		t,
		filepath.Join(
			root,
			"contracts/authorization/v1/restore-coordination.schema.json",
		),
		&restoreCoordination,
	)
	restoreCredential := requiredMap(
		t,
		requiredMap(t, restoreCoordination, "$defs"),
		"roleCredential",
	)
	restoreCredentialProperties := requiredMap(t, restoreCredential, "properties")
	requireEqual(
		t,
		requiredMap(t, restoreCredentialProperties, "aud"),
		"const",
		"urn:mattercodex:internal-rpc-authority-restore-controller",
	)
	if requiredString(
		t,
		requiredMap(t, credentialProperties, "aud"),
		"const",
	) == requiredString(
		t,
		requiredMap(t, restoreCredentialProperties, "aud"),
		"const",
	) {
		t.Fatal("restore and normal-readback credentials share an audience")
	}

	var attestation map[string]any
	decodeJSONStrict(
		t,
		filepath.Join(
			root,
			"contracts/authorization/v1/readback-attestation.schema.json",
		),
		&attestation,
	)
	attestationRequired := stringSet(attestation["required"].([]any))
	for _, field := range []string{
		"intent_id",
		"intent_revision",
		"workload_id",
		"role",
		"workload_generation",
		"credential_generation",
		"readback_credential_jti",
		"readback_credential_digest_sha256",
		"possession_key_kid",
		"possession_key_generation",
		"possession_key_thumbprint_sha256",
		"served_state_digest_sha256",
		"challenge_id",
		"challenge_jti",
		"challenge_nonce",
		"challenge_digest_sha256",
		"jti",
	} {
		if !attestationRequired[field] {
			t.Fatalf("readback attestation does not bind %s", field)
		}
	}
}

func TestRestoreEvidenceCarriesQuarantineBarrier(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "contracts/authorization/v1/restore-fence-evidence.schema.json")
	var schema map[string]any
	decodeJSONStrict(t, path, &schema)
	required := stringSet(schema["required"].([]any))
	for _, field := range []string{
		"anchor_revision",
		"restore_epoch",
		"predecessor",
		"workload_set_revision",
		"expected_workload_role_generations_sha256",
		"quiescence_ack_set_sha256",
		"expected_ack_count",
		"accepted_ack_count",
		"semantic_transition",
	} {
		if !required[field] {
			t.Fatalf("restore evidence does not require %s", field)
		}
	}
	properties := schema["properties"].(map[string]any)
	requireEqual(
		t,
		properties["semantic_transition"].(map[string]any),
		"const",
		"EXACT_INCREMENT_WITH_PREDECESSOR_DIGEST",
	)
}

func TestProtectedHeaderContract(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "contracts/authorization/v1/jws-protected-header.schema.json")
	var schema map[string]any
	decodeJSONStrict(t, path, &schema)

	properties := schema["properties"].(map[string]any)
	alg := properties["alg"].(map[string]any)
	if got := alg["const"]; got != "ES256" {
		t.Fatalf("protected alg = %v, want ES256", got)
	}
	mcxv := properties["mcxv"].(map[string]any)
	if got := mcxv["const"]; got != float64(1) {
		t.Fatalf("protected mcxv = %v, want 1", got)
	}
	wantTypes := []string{
		"mattercodex-internal-rpc-auth+jws",
		"mattercodex-internal-rpc-authority-proof+jws",
		"mattercodex-internal-rpc-readback-attestation+jws",
		"mattercodex-internal-rpc-readback-credential+jws",
		"mattercodex-internal-rpc-readback-credential-trust+jws",
		"mattercodex-internal-rpc-readback-manifest-root+jws",
		"mattercodex-internal-rpc-restore-ack+jws",
		"mattercodex-internal-rpc-restore-role-delivery-receipt+jws",
		"mattercodex-internal-rpc-restore-directive+jws",
		"mattercodex-internal-rpc-restore-evidence+jws",
		"mattercodex-internal-rpc-restore-role-issuance+jws",
		"mattercodex-internal-rpc-restore-role-credential+jws",
		"mattercodex-internal-rpc-restore-role-trust+jws",
		"mattercodex-internal-rpc-snapshot+jws",
	}
	typ := properties["typ"].(map[string]any)
	gotTypesAny := typ["enum"].([]any)
	gotTypes := make([]string, 0, len(gotTypesAny))
	for _, value := range gotTypesAny {
		gotTypes = append(gotTypes, value.(string))
	}
	if strings.Join(gotTypes, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("protected typ values are not closed")
	}
}

func TestProtectedHeaderRFC8785Golden(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(
		root,
		"contracts/authorization/v1/fixtures/protected-header-golden.json",
	)
	var fixtures struct {
		Version  int `json:"v"`
		Fixtures []struct {
			Name                    string `json:"name"`
			CanonicalUTF8           string `json:"canonical_utf8"`
			CanonicalBase64URL      string `json:"canonical_base64url"`
			RejectedLegacyUTF8      string `json:"rejected_legacy_utf8"`
			RejectedLegacyBase64URL string `json:"rejected_legacy_base64url"`
		} `json:"fixtures"`
	}
	decodeJSONStrict(t, path, &fixtures)
	if fixtures.Version != 1 || len(fixtures.Fixtures) != 2 {
		t.Fatalf("unexpected protected header fixtures: %+v", fixtures)
	}

	for _, fixture := range fixtures.Fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			canonical := canonicalProtectedHeader(t, fixture.CanonicalUTF8)
			if canonical != fixture.CanonicalUTF8 {
				t.Fatalf("golden UTF-8 is not canonical:\n got %s\nwant %s", canonical, fixture.CanonicalUTF8)
			}
			if got := base64.RawURLEncoding.EncodeToString([]byte(canonical)); got != fixture.CanonicalBase64URL {
				t.Fatalf("golden base64url = %s, want %s", got, fixture.CanonicalBase64URL)
			}
			if got := base64.RawURLEncoding.EncodeToString([]byte(fixture.RejectedLegacyUTF8)); got != fixture.RejectedLegacyBase64URL {
				t.Fatalf("legacy base64url fixture = %s, want %s", got, fixture.RejectedLegacyBase64URL)
			}
			if canonicalProtectedHeader(t, fixture.RejectedLegacyUTF8) == fixture.RejectedLegacyUTF8 {
				t.Fatal("legacy alg,typ,kid,crit,mcxv order was accepted as canonical")
			}
		})
	}
}

func TestSnapshotSchemaCriticalCardinality(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "contracts/authorization/v1/authority-snapshot.schema.json")
	var schema map[string]any
	decodeJSONStrict(t, path, &schema)

	definitions := schema["$defs"].(map[string]any)
	issuerKeySet := definitions["issuerKeySet"].(map[string]any)
	issuerProperties := issuerKeySet["properties"].(map[string]any)
	keys := issuerProperties["keys"].(map[string]any)
	if keys["minItems"] != float64(2) ||
		keys["maxItems"] != float64(3) ||
		keys["uniqueItems"] != true {
		t.Fatal("issuer keys must contain the closed current/next/previous set")
	}
	cardinalityRules := keys["allOf"].([]any)
	if len(cardinalityRules) != 3 {
		t.Fatal("issuer keys must contain one cardinality rule per key status")
	}
	for index, wantStatus := range []string{"CURRENT", "NEXT", "PREVIOUS"} {
		rule := cardinalityRules[index].(map[string]any)
		contains := rule["contains"].(map[string]any)
		containsProperties := contains["properties"].(map[string]any)
		status := containsProperties["status"].(map[string]any)
		if status["const"] != wantStatus ||
			rule["maxContains"] != float64(1) ||
			(wantStatus != "PREVIOUS" && rule["minContains"] != float64(1)) {
			t.Fatalf("unexpected %s key cardinality rule", wantStatus)
		}
	}

	properties := schema["properties"].(map[string]any)
	history := properties["history"].(map[string]any)
	if history["maxItems"] != float64(32) || history["uniqueItems"] != true {
		t.Fatal("snapshot history must be unique and bounded to 32 entries")
	}

	policy := definitions["policy"].(map[string]any)
	policyProperties := policy["properties"].(map[string]any)
	for key, want := range map[string]any{
		"default_decision":           "DENY",
		"token_ttl_seconds":          float64(30),
		"allowed_clock_skew_seconds": float64(5),
		"max_compact_jws_bytes":      float64(8192),
	} {
		value := policyProperties[key].(map[string]any)["const"]
		if value != want {
			t.Fatalf("policy %s = %v, want %v", key, value, want)
		}
	}

	operationBinding := definitions["operationBinding"].(map[string]any)
	required := stringSet(operationBinding["required"].([]any))
	for _, field := range []string{"authority_proof_producer_id"} {
		if !required[field] {
			t.Fatalf("operation binding does not require %s", field)
		}
	}
	producer := definitions["authorityProofProducer"].(map[string]any)
	producerRequired := stringSet(producer["required"].([]any))
	for _, field := range []string{
		"caller_workload_id",
		"caller_spiffe_id",
		"full_method",
		"application_credential",
		"application_credential_issuer",
		"application_credential_audience",
		"application_credential_trust_bundle_id",
		"authority_proof_issuer",
		"authority_proof_audience",
		"authority_proof_trust_bundle_id",
		"authority_proof_max_age_seconds",
		"allowed_operation_ids",
		"server_resolved_fields",
	} {
		if !producerRequired[field] {
			t.Fatalf("authority proof producer does not require %s", field)
		}
	}
	producerProperties := producer["properties"].(map[string]any)
	maxAge := producerProperties["authority_proof_max_age_seconds"].(map[string]any)
	if maxAge["const"] != float64(15) {
		t.Fatalf("authority proof max age = %v, want 15", maxAge["const"])
	}
	requireEqual(t, producerProperties["deadline_milliseconds"].(map[string]any), "const", float64(2000))
	requireEqual(t, producerProperties["max_attempts"].(map[string]any), "const", float64(2))
}

func TestBootstrapPolicyIsDenyAll(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "contracts/authorization/v1/bootstrap-deny-all-policy.yaml")
	var policy struct {
		Version                 int    `json:"version"`
		TrustDomain             string `json:"trust_domain"`
		DefaultDecision         string `json:"default_decision"`
		TokenTTLSeconds         int    `json:"token_ttl_seconds"`
		AllowedClockSkewSeconds int    `json:"allowed_clock_skew_seconds"`
		MaxCompactJWSBytes      int    `json:"max_compact_jws_bytes"`
		AuthorityProofProducers []any  `json:"authority_proof_producers"`
		OperationBindings       []any  `json:"operation_bindings"`
	}
	decodeYAMLStrict(t, path, &policy)

	if policy.Version != 1 ||
		policy.TrustDomain != "mattercodex.local" ||
		policy.DefaultDecision != "DENY" ||
		policy.TokenTTLSeconds != 30 ||
		policy.AllowedClockSkewSeconds != 5 ||
		policy.MaxCompactJWSBytes != 8192 ||
		len(policy.AuthorityProofProducers) != 0 ||
		len(policy.OperationBindings) != 0 {
		t.Fatalf("bootstrap policy is not the exact deny-all contract: %+v", policy)
	}
}

func TestBootstrapKeyDeliveryTargetsAreEmpty(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(
		root,
		"contracts/authorization/v1/bootstrap-key-delivery-targets.yaml",
	)
	var targets struct {
		Version        int    `json:"version"`
		SourceRevision uint64 `json:"source_revision"`
		Targets        []any  `json:"targets"`
	}
	decodeYAMLStrict(t, path, &targets)
	if targets.Version != 1 ||
		targets.SourceRevision != 1 ||
		len(targets.Targets) != 0 {
		t.Fatalf("bootstrap key delivery targets are not deny-all: %+v", targets)
	}
}

func TestHumanAuthoredYAMLRejectsUnknownAndDuplicateFields(t *testing.T) {
	type bootstrap struct {
		Version int `json:"version"`
	}
	for name, document := range map[string]string{
		"unknown":   "version: 1\nunexpected: true\n",
		"duplicate": "version: 1\nversion: 2\n",
	} {
		t.Run(name, func(t *testing.T) {
			var decoded bootstrap
			if err := yaml.UnmarshalStrict([]byte(document), &decoded); err == nil {
				t.Fatal("strict YAML decoder accepted an unsafe document")
			}
		})
	}
}

func TestCapabilityRegistryCriticalBoundary(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(
		root,
		"deploy/k8s/base/internal-rpc-authority/capability-registry.yaml",
	)
	var registry map[string]any
	data := []byte(readFile(t, path))
	if err := yaml.UnmarshalStrict(data, &registry); err != nil {
		t.Fatalf("strictly decode capability registry: %v", err)
	}

	spec := requiredMap(t, registry, "spec")
	sockets := requiredMap(t, spec, "unixDomainSockets")
	identity := requiredMap(t, sockets, "identity")
	requireEqual(t, identity, "applicationUid", float64(10001))
	requireEqual(t, identity, "issuerUid", float64(29001))
	requireEqual(t, identity, "verifierUid", float64(29002))
	requireEqual(t, identity, "sharedFsGid", float64(29000))
	requireEqual(t, requiredMap(t, sockets, "issuer"), "path", issuerSocket)
	requireEqual(t, requiredMap(t, sockets, "verifier"), "path", verifierSocket)
	requireEqual(t, requiredMap(t, sockets, "peerBinding"), "mechanism", "LINUX_SO_PEERCRED")

	deployables := requiredMap(t, spec, "deployables")
	issuer := requiredMap(t, deployables, "issuer")
	requireEqual(t, issuer, "manifestTrustSecretName", "internal-rpc-authority-manifest-trust")
	requireEqual(t, issuer, "manifestTrustBundleId", "internal-rpc-authority-manifest-signers")
	requireEqual(t, issuer, "databaseLoginPrincipal", "FROM_EXACT_WORKLOAD_ROLE_REGISTRY")
	requireEqual(t, issuer, "databaseGroupRole", "internal_rpc_authority_issuer")
	issuerVerification := requiredMap(t, issuer, "snapshotVerification")
	for _, field := range []string{
		"requireIndependentManifestTrust",
		"requireCertificateValidity",
		"requireExactSignerGeneration",
		"requireCanonicalPayload",
		"requirePredecessorChain",
		"requirePersistentHighWatermark",
		"requireCryptographicReadback",
	} {
		requireEqual(t, issuerVerification, field, true)
	}

	resolver := requiredMap(t, deployables, "authority-proof-resolver")
	requireEqual(t, resolver, "owner", "control-plane")
	requireEqual(t, resolver, "implementationIssue", "187")
	requireEqual(
		t,
		resolver,
		"fullMethod",
		"/internalrpcauthority.v1.AuthorityProofResolverService/ResolveAuthorityProof",
	)
	requireEqual(t, resolver, "firstCallInternalAuthorizationContext", "FORBIDDEN")
	resolverCredential := requiredMap(t, resolver, "applicationCredential")
	requireEqual(t, resolverCredential, "type", "OIDC_BEARER")
	requireEqual(t, resolverCredential, "requestIdentityFieldsAllowed", false)
	resolution := requiredMap(t, resolver, "resolution")
	requireEqual(t, resolution, "crossTenantPolicy", "DENY_BEFORE_SIGN")

	publisher := requiredMap(t, deployables, "publisher")
	publisherRestoreInterface := requiredMap(t, publisher, "restoreRoleCredentialInterface")
	requireEqual(
		t,
		publisherRestoreInterface,
		"publishMethod",
		"/internalrpcauthority.v1.RestoreRoleCredentialPublisherService/PublishRoleCredential",
	)
	publisherDelivery := requiredMap(t, publisherRestoreInterface, "delivery")
	requireEqual(t, publisherDelivery, "generateDistinctEs256AckKeyPerExactRole", true)
	requireEqual(t, publisherDelivery, "publicAckJwkDestination", "SIGNED_ROLE_CREDENTIAL")
	requireEqual(t, publisherDelivery, "privateKeyInResponseOrLog", "FORBIDDEN")
	publisherDatabaseIdentity := requiredMap(t, publisher, "databaseIdentity")
	requireEqual(t, publisherDatabaseIdentity, "capabilityRole", "internal_rpc_authority_publisher")
	requireEqual(t, publisherDatabaseIdentity, "capabilityRoleLogin", false)
	requireEqual(
		t,
		publisherDatabaseIdentity,
		"roleActivation",
		"SET_ROLE_EXACT_CAPABILITY_AFTER_TLS_LOGIN",
	)
	publisherVaultDatabase := requiredMap(t, publisherDatabaseIdentity, "vaultDatabaseCredentials")
	requireEqual(t, publisherVaultDatabase, "mode", "STATIC_ROLE_CURRENT_NEXT_PASSWORD_ROTATION")
	requireEqual(t, publisherVaultDatabase, "boundServiceAccount", "internal-rpc-authority-publisher")
	requireEqual(t, publisherVaultDatabase, "postgresqlSslMode", "verify-full")
	requireEqual(
		t,
		publisherVaultDatabase,
		"issuance",
		"READ_EXACT_STATIC_CREDS_PATH_AFTER_KUBERNETES_AUTH",
	)
	requireEqual(
		t,
		publisherVaultDatabase,
		"revocation",
		"SERVER_SIDE_RETIRED_FENCE_THEN_NOLOGIN_MEMBERSHIP_REVOKE_VAULT_ROTATION_AND_BACKEND_DRAIN",
	)
	requireEqual(t, publisherVaultDatabase, "workloadTokenRenewalRequired", true)
	requireEqual(t, publisherVaultDatabase, "currentNextOverlapRequired", true)
	publisherDatabaseReadiness := requiredMap(t, publisherDatabaseIdentity, "databaseReadiness")
	requireEqual(t, publisherDatabaseReadiness, "requireSessionUserExactCurrentPrincipal", true)
	requireEqual(t, publisherDatabaseReadiness, "requireConsumerEvidenceWriteDenied", true)

	normalReadback := requiredMap(t, publisher, "normalReadbackCredential")
	requireEqual(t, normalReadback, "restoreUseAllowed", false)
	requireEqual(
		t,
		normalReadback,
		"credentialProtectedType",
		"mattercodex-internal-rpc-readback-credential+jws",
	)
	requireEqual(
		t,
		normalReadback,
		"credentialAudience",
		"urn:mattercodex:internal-rpc-authority-readback-attestor",
	)
	requireEqual(t, normalReadback, "possessionKeyReuseForRestoreAck", "FORBIDDEN")
	exactClientInputs := requiredMap(t, normalReadback, "exactClientInputs")
	requireStringSliceEqual(t, exactClientInputs, "issueChallenge", []string{
		"pinned_intent_id",
		"readback_credential_compact_jws",
		"idempotency_key",
		"correlation_id",
	})
	requireStringSliceEqual(t, exactClientInputs, "attestServedState", []string{
		"pinned_intent_id",
		"readback_credential_compact_jws",
		"served_state_attestation_compact_jws",
		"idempotency_key",
		"correlation_id",
		"challenge_id",
	})
	requireEqual(
		t,
		exactClientInputs,
		"callerProvidedWorkloadRoleGenerationAudienceOrTtlAllowed",
		false,
	)
	normalReadbackTrust := requiredMap(t, normalReadback, "trustSnapshot")
	requireEqual(t, normalReadbackTrust, "sameRevisionMutationOrRollback", "REJECT")
	requireEqual(t, normalReadbackTrust, "currentNextOverlapBeforeUseRequired", true)
	rotation := requiredMap(t, publisher, "rotationOwnership")
	requireEqual(t, rotation, "authKeyGenerationOwner", "internal-rpc-authority-publisher")
	requireEqual(t, rotation, "authPrivateKeyWriteOwner", "internal-rpc-authority-publisher")
	requireEqual(t, rotation, "authorityProofPrivateKeyWriteOwner", "internal-rpc-authority-publisher")
	requireEqual(t, rotation, "manifestTrustOverlapWriteOwner", "internal-rpc-authority-publisher")
	requireEqual(t, rotation, "authorityProofTrustOverlapWriteOwner", "internal-rpc-authority-publisher")
	requireEqual(t, rotation, "compareAndSwapRequired", true)
	requireEqual(t, rotation, "perWorkloadRoleFanOutRequired", true)
	requireEqual(t, rotation, "requiredRoleUnion", "CALLER_ISSUER_TARGET_VERIFIER_PROOF_RESOLVER")
	requireEqual(t, rotation, "readbackCardinality", "EXACTLY_ONE_PER_REQUIRED_WORKLOAD_ROLE")
	requireEqual(t, rotation, "wildcardVaultPathsAllowed", false)
	proofRotation := requiredMap(t, rotation, "authorityProofSignerRotation")
	requireEqual(t, proofRotation, "resolverPrivateToPublicReadbackRequired", true)
	requireEqual(t, proofRotation, "everyCallerIssuerTrustReadbackRequired", true)
	requireEqual(t, proofRotation, "promotionBeforeAllReadbacks", false)
	requireStringSliceEqual(t, rotation, "sequence", []string{
		"load-and-verify-forward-history-and-same-input-publication",
		"reconcile-current-next-bounded-previous-key-lifecycle",
		"vault-cas-deliver-exact-targets",
		"verify-independent-root-to-manifest-signer-chain",
		"build-full-auth-proof-manifest-policy-snapshot",
		"append-immutable-history-and-prepared-intent",
		"kubernetes-resource-version-cas-and-served-readback",
		"workload-role-independent-attestation-readbacks",
		"promote-after-exact-full-role-readback-set",
	})
	prepare := requiredMap(t, rotation, "prepareTransaction")
	requireEqual(
		t,
		prepare,
		"function",
		"internal_rpc_authority.publisher_append_snapshot_history(bigint,text,bigint,bigint,bigint,bigint,text,text,uuid,text,integer)",
	)
	requireEqual(t, prepare, "sameInputRetry", "RETURN_PERSISTED_COMPACT_JWS")
	promotion := requiredMap(t, rotation, "promotionTransaction")
	requireEqual(
		t,
		promotion,
		"function",
		"internal_rpc_authority.publisher_promote_snapshot(uuid,bigint,text,integer)",
	)
	requireEqual(t, promotion, "partialCommitAllowed", false)

	recoveryName := "internal-rpc-authority-recovery-job"
	pitrName := "internal-rpc-authority-restore-pitr-job"
	pitr := requiredMap(t, deployables, pitrName)
	for key, want := range map[string]any{
		"kind":           "CronJob",
		"canonicalPath":  "deploy/k8s/base/internal-rpc-authority-restore",
		"artifactBinary": "/usr/local/bin/internal-rpc-authority-restore-pitr",
		"serviceAccount": "internal-rpc-authority-restore-pitr",
		"failurePolicy":  "KEEP_TRAFFIC_QUARANTINED",
	} {
		requireEqual(t, pitr, key, want)
	}
	pitrExecution := requiredMap(t, pitr, "execution")
	requireEqual(t, pitrExecution, "provider", "CloudNativePG")
	requireEqual(
		t,
		pitrExecution,
		"recoveryMethod",
		"BARMAN_CLOUD_PLUGIN_PITR_NEW_CLUSTER",
	)
	pitrEvidence := requiredMap(t, pitr, "evidence")
	requireEqual(t, pitrEvidence, "operatorCanSignOrWrite", false)
	requireEqual(t, pitrEvidence, "servedReadbackRequired", true)

	recovery := requiredMap(t, deployables, recoveryName)
	for key, want := range map[string]any{
		"kind":           "CronJob",
		"canonicalPath":  "deploy/k8s/base/internal-rpc-authority-restore",
		"artifactBinary": "/usr/local/bin/internal-rpc-authority-restore-recovery",
		"serviceAccount": "internal-rpc-authority-restore-recovery",
		"databaseRole":   "internal_rpc_authority_recovery",
		"failurePolicy":  "KEEP_TRAFFIC_QUARANTINED",
	} {
		requireEqual(t, recovery, key, want)
	}
	recoveryInterface := requiredMap(t, recovery, "interface")
	requireEqual(t, recoveryInterface, "requireCompletedPhase", true)
	requireEqual(t, recoveryInterface, "requireExternalPredecessorHighWatermark", true)
	requireEqual(t, recoveryInterface, "databaseEffect", "ATOMIC_RESTORE_FENCE_CAS")
	recoveryReadiness := requiredMap(t, recovery, "readiness")
	requireEqual(t, recoveryReadiness, "requireExactExternalEpochAndDigestReadback", true)
	requireEqual(t, recoveryReadiness, "successBeforeIssuerVerifierStartup", true)

	restore := requiredMap(t, spec, "restore")
	requireEqual(t, restore, "fenceOwner", recoveryName)
	if _, ok := deployables[requiredString(t, restore, "fenceOwner")]; !ok {
		t.Fatal("restore fence owner does not resolve to a deployable")
	}
	requireEqual(t, restore, "externalAnchorOutsideRestoredDatabase", true)
	requireEqual(t, restore, "fenceDurationSeconds", float64(40))
	requireEqual(t, restore, "missingOrStaleExternalEvidencePolicy", "FAIL_CLOSED")
	requireEqual(t, restore, "watermarkResetAllowed", false)
	requireEqual(t, restore, "replayReservationResetAllowed", false)
	requireStringSliceEqual(t, restore, "startupOrder", []string{
		"controller-enters-quiescing",
		"every-current-workload-role-generation-stops-and-acks",
		"signed-external-anchor-prepared",
		"database-restore",
		"signed-external-anchor-completed",
		"recovery-job-validates-and-commits-fence",
		"database-safe-window-elapses",
		"every-workload-role-external-anchor-readback",
		"application-readiness",
	})

	anchor := requiredMap(t, deployables, "restore-evidence-anchor")
	requireEqual(t, anchor, "outsideRestoredPostgreSQL", true)
	requireEqual(t, anchor, "updateProtocol", "SEMANTIC_REVISION_PREDECESSOR_CAS")
	requireEqual(t, anchor, "resourceVersionUse", "LOST_UPDATE_GUARD_ONLY")
	requireEqual(t, anchor, "failurePolicy", "FAIL_CLOSED")
	admission := requiredMap(t, anchor, "admission")
	requireEqual(t, admission, "failurePolicy", "Fail")
	requireEqual(t, admission, "exactResourceName", "internal-rpc-authority-restore-evidence")

	controller := requiredMap(t, deployables, "internal-rpc-authority-restore-controller")
	requireEqual(t, controller, "kind", "Deployment")
	requireEqual(
		t,
		controller,
		"canonicalPath",
		"deploy/k8s/base/internal-rpc-authority-restore",
	)
	requireEqual(t, controller, "artifactBinary", "/usr/local/bin/internal-rpc-authority-restore-controller")
	requireEqual(t, controller, "serviceAccount", "internal-rpc-authority-restore-controller")
	controllerInterface := requiredMap(t, controller, "interface")
	requireEqual(
		t,
		controllerInterface,
		"prepareMethod",
		"/internalrpcauthority.v1.RestoreControllerService/PrepareRestore",
	)
	issuance := requiredMap(t, controllerInterface, "roleCredentialIssuance")
	requireEqual(
		t,
		issuance,
		"method",
		"/internalrpcauthority.v1.RestoreRoleCredentialPublisherService/PublishRoleCredential",
	)
	requireEqual(t, issuance, "persistDeliveryReceiptBeforeDirectivePublication", true)
	requireEqual(
		t,
		controllerInterface,
		"directiveMethod",
		"/internalrpcauthority.v1.RestoreControllerService/GetRestoreDirective",
	)
	coordinationPoll := requiredMap(t, controllerInterface, "coordinationPoll")
	requireEqual(t, coordinationPoll, "directiveBeforePreparedRequired", true)
	requireEqual(t, coordinationPoll, "observedVersionField", "observed_coordination_revision")
	acknowledgement := requiredMap(t, controllerInterface, "acknowledgement")
	requireEqual(t, acknowledgement, "directiveAndAckJtiOneTime", true)
	requireEqual(t, acknowledgement, "persistentReplayStore", "restore-coordination-state")
	requireEqual(t, acknowledgement, "publicKeySource", "SIGNED_ROLE_CREDENTIAL_ONLY")
	requireEqual(t, acknowledgement, "requireRfc7638ThumbprintMatch", true)
	idempotency := requiredMap(t, acknowledgement, "idempotency")
	requireEqual(t, idempotency, "sameKeyJtiAndDigest", "RETURN_SAVED_RECEIPT_AND_RESULT")
	requireEqual(t, idempotency, "sameKeyOrJtiDifferentDigest", "RESTORE_ACK_REPLAY_DETECTED")
	roleCredential := requiredMap(t, controllerInterface, "workloadRoleCredential")
	requireEqual(t, roleCredential, "readbackAttestorAudienceAccepted", false)
	requireEqual(t, roleCredential, "normalReadbackCredentialProtectedTypeAccepted", false)
	requireEqual(t, roleCredential, "multiAudienceAccepted", false)
	signerTrust := requiredMap(t, roleCredential, "signerTrust")
	requireEqual(t, signerTrust, "controllerOwnedServedReadback", true)
	requireEqual(t, signerTrust, "sameRevisionMutationOrRollback", "REJECT")
	barrier := requiredMap(t, controller, "quarantineBarrier")
	requireEqual(t, barrier, "requireEveryCurrentGenerationAck", true)
	requireEqual(t, barrier, "closeOnEveryIssuanceAndReservationPath", true)
	requireEqual(t, barrier, "publishPreparedBeforeCompleteAckSet", false)

	coordinationState := requiredMap(t, deployables, "restore-coordination-state")
	acceptedRecord := requiredMap(t, coordinationState, "acceptedAckRecord")
	fields := stringSet(acceptedRecord["exactFields"].([]any))
	for _, field := range []string{
		"workload_id",
		"role",
		"workload_generation",
		"credential_generation",
		"directive_digest_sha256",
		"ack_digest_sha256",
		"ack_jti",
		"idempotency_key",
		"semantic_request_digest_sha256",
		"receipt_id",
	} {
		if !fields[field] {
			t.Fatalf("coordination accepted ACK record lacks %s", field)
		}
	}
	coordinationRecovery := requiredMap(t, coordinationState, "leaderRecovery")
	requireEqual(t, coordinationRecovery, "reconstructExpectedAndAcceptedDistinctSets", true)
	requireEqual(t, coordinationRecovery, "partialSetRemainsQuiescing", true)
	requireEqual(t, coordinationRecovery, "preparedOnlyOnExactCurrentFullSet", true)

	restoreRoleTrust := requiredMap(t, deployables, "restore-role-credential-trust-snapshot")
	requireEqual(t, restoreRoleTrust, "resourceName", "internal-rpc-authority-restore-role-trust")
	requireEqual(
		t,
		restoreRoleTrust,
		"updateProtocol",
		"SIGNED_SOURCE_REVISION_KEY_SET_REVISION_PREDECESSOR_CAS",
	)
	requireEqual(t, restoreRoleTrust, "readinessRequiresActuallyServedSnapshotReadback", true)
	readbackTrust := requiredMap(t, deployables, "readback-credential-trust-snapshot")
	requireEqual(
		t,
		readbackTrust,
		"resourceName",
		"internal-rpc-authority-readback-credential-trust",
	)
	requireEqual(
		t,
		readbackTrust,
		"exactAudience",
		"urn:mattercodex:internal-rpc-authority-readback-attestor",
	)
	requireEqual(t, readbackTrust, "restoreControllerAudienceAllowed", false)
	requireEqual(t, readbackTrust, "readinessRequiresActuallyServedSnapshotReadback", true)
	requireEqual(t, readbackTrust, "coDeliveredPublisherSignerAccepted", false)
	requireEqual(
		t,
		readbackTrust,
		"manifestTrustRootResource",
		"internal-rpc-authority-readback-manifest-trust-root",
	)

	readbackRoot := requiredMap(t, deployables, "readback-manifest-trust-root")
	requireEqual(
		t,
		readbackRoot,
		"owner",
		"internal-rpc-authority-readback-trust-root-controller",
	)
	requireEqual(
		t,
		readbackRoot,
		"bootstrapPublicKeyFingerprintSource",
		"IMAGE_EMBEDDED_PUBLIC_JWK_THUMBPRINT_PIN_FROM_OWNER_CEREMONY",
	)
	requireEqual(
		t,
		readbackRoot,
		"bootstrapPublicJwkPath",
		"/usr/local/share/internal-rpc-authority/readback-root/bootstrap-public.jwk",
	)
	requireEqual(t, readbackRoot, "sameChannelVaultRootKeyAccepted", false)
	rootRotation := requiredMap(t, readbackRoot, "rootRotationCeremony")
	requireEqual(t, rootRotation, "requireOldAndNewRootCrossSignedOverlap", true)
	requireEqual(t, rootRotation, "verifyCurrentSignatureWithPersistedTrustedRoot", true)
	requireEqual(t, rootRotation, "verifyNextSignatureWithCandidateRoot", true)
	requireEqual(t, rootRotation, "sameRevisionMutationRollbackOrGap", "REJECT")
	requireEqual(
		t,
		requiredMap(t, readbackRoot, "publisherPermissions"),
		"readWriteOrMountAllowed",
		false,
	)
	requireEqual(t, readbackRoot, "readinessRequiresActuallyServedSnapshotReadback", true)

	credentialReconciler := requiredMap(
		t,
		deployables,
		"internal-rpc-authority-database-credential-reconciler",
	)
	requireEqual(
		t,
		credentialReconciler,
		"artifactBinary",
		"/usr/local/bin/internal-rpc-authority-database-credential-reconciler",
	)
	requireEqual(t, credentialReconciler, "leaderElection", "POSTGRESQL_LEASE_WITH_FENCING_TOKEN")
	reconcilerInterface := requiredMap(t, credentialReconciler, "interface")
	requireEqual(
		t,
		reconcilerInterface,
		"reconcileMethod",
		"/internalrpcauthority.v1.DatabaseCredentialLifecycleService/ReconcileDatabaseCredentials",
	)
	requireEqual(t, reconcilerInterface, "callerProvidedPrincipalGenerationOrStatusAllowed", false)
	requireEqual(
		t,
		requiredMap(t, credentialReconciler, "reconciliation"),
		"missedUpdateOrRejoin",
		"REREAD_DATABASE_LIFECYCLE_AND_ALL_EXACT_VAULT_STATIC_ROLES",
	)

	attestor := requiredMap(t, deployables, "internal-rpc-authority-readback-attestor")
	requireEqual(t, attestor, "artifactBinary", "/usr/local/bin/internal-rpc-authority-readback-attestor")
	attestorDatabaseIdentity := requiredMap(t, attestor, "databaseIdentity")
	requireEqual(
		t,
		attestorDatabaseIdentity,
		"capabilityRole",
		"internal_rpc_authority_readback_attestor",
	)
	requireEqual(t, attestorDatabaseIdentity, "capabilityRoleLogin", false)
	attestorVault := requiredMap(t, attestorDatabaseIdentity, "vaultDatabaseCredentials")
	requireEqual(t, attestorVault, "currentPrincipal", "ira_readback_attestor_g4")
	requireEqual(t, attestorVault, "nextPrincipal", "ira_readback_attestor_g5")
	requireEqual(t, attestorVault, "previousPrincipal", "ira_readback_attestor_g3")
	requireStringSliceEqual(
		t,
		attestorVault,
		"retiredPrincipals",
		[]string{"ira_readback_attestor_g1", "ira_readback_attestor_g2"},
	)
	requireEqual(
		t,
		attestorVault,
		"owner",
		"internal-rpc-authority-database-credential-reconciler",
	)
	requireEqual(
		t,
		attestorVault,
		"boundServiceAccount",
		"internal-rpc-authority-readback-attestor",
	)
	requireEqual(t, attestorVault, "postgresqlSslMode", "verify-full")
	requireEqual(
		t,
		requiredMap(t, attestorDatabaseIdentity, "runtimeSessionFence"),
		"retiredOpenSessionPolicy",
		"FAIL_CLOSED",
	)
	attestorInterface := requiredMap(t, attestor, "interface")
	requireEqual(
		t,
		attestorInterface,
		"challengeMethod",
		"/internalrpcauthority.v1.AuthorityReadbackAttestorService/IssueAttestationChallenge",
	)
	requireEqual(
		t,
		attestorInterface,
		"attestMethod",
		"/internalrpcauthority.v1.AuthorityReadbackAttestorService/AttestServedState",
	)
	requireEqual(
		t,
		attestorInterface,
		"applicationCredential",
		"PUBLISHER_SIGNED_NORMAL_READBACK_CREDENTIAL",
	)
	attestorRoot := requiredMap(t, attestor, "readbackManifestTrustRoot")
	requireEqual(t, attestorRoot, "publisherReadOrWriteAllowed", false)
	requireEqual(t, attestorRoot, "restoreTrustAccepted", false)
	requireEqual(
		t,
		attestorRoot,
		"bootstrapFingerprintConfig",
		"INTERNAL_RPC_AUTHORITY_READBACK_MANIFEST_ROOT_FINGERPRINT_SHA256",
	)
	applicationCredential := requiredMap(t, attestorInterface, "applicationCredentialContract")
	requireEqual(t, applicationCredential, "restoreCredentialProtectedTypeAccepted", false)
	requireEqual(t, applicationCredential, "restoreControllerAudienceAccepted", false)
	requireEqual(t, applicationCredential, "multiAudienceAccepted", false)
	requireEqual(t, applicationCredential, "permissiveFallbackAccepted", false)
	challenge := requiredMap(t, attestorInterface, "challenge")
	requireEqual(t, challenge, "table", "authority_readback_attestation_challenges")
	requireEqual(t, challenge, "ttlSeconds", float64(30))
	requireStringSliceEqual(t, challenge, "serverGeneratedFields", []string{
		"challenge_id",
		"challenge_jti",
		"challenge_nonce",
		"challenge_digest_sha256",
		"issued_at",
		"expires_at",
	})
	issuanceTransaction := requiredMap(t, challenge, "issuanceTransaction")
	requireEqual(t, issuanceTransaction, "persistBeforeResponse", true)
	requireEqual(
		t,
		issuanceTransaction,
		"sameKeyAndCanonicalRequestDigest",
		"RETURN_PERSISTED_CHALLENGE",
	)
	consumeTransaction := requiredMap(t, challenge, "consumeTransaction")
	requireEqual(t, consumeTransaction, "atomic", true)
	requireEqual(t, consumeTransaction, "crashBeforeCommit", "CHALLENGE_REMAINS_ISSUED")
	requireEqual(
		t,
		consumeTransaction,
		"crashAfterCommitBeforeResponse",
		"RETRY_RETURNS_PERSISTED_RECEIPT",
	)
	receipt := requiredMap(t, attestorInterface, "receipt")
	requireEqual(t, receipt, "publisherMayCreateOrUpdate", false)
	requireEqual(t, receipt, "consumersMayCreateOrUpdate", false)

	store := requiredMap(t, deployables, "replay-store")
	databaseIdentity := requiredMap(t, store, "workloadRoleDatabaseIdentity")
	requireEqual(
		t,
		databaseIdentity,
		"cardinality",
		"BOUNDED_ONE_CURRENT_ONE_NEXT_ONE_PREVIOUS_PER_WORKLOAD_ROLE_GENERATION",
	)
	requireEqual(t, databaseIdentity, "sharedCapabilityRoles", "NOLOGIN")
	rowLevelSecurity := requiredMap(t, databaseIdentity, "rowLevelSecurity")
	requireEqual(t, rowLevelSecurity, "forcedForTableOwner", true)
	requireEqual(t, rowLevelSecurity, "principalSource", "session_user")
	protectedReadbackTables := []string{
		"authority_readback_intents",
		"authority_readback_attestation_challenges",
		"authority_readback_attestation_receipts",
		"authority_key_delivery_readbacks",
		"authority_snapshot_readbacks",
	}
	requireStringSliceEqual(t, databaseIdentity, "protectedReadbackTables", protectedReadbackTables)
	readbackFunctions := requiredMap(t, databaseIdentity, "readbackFunctions")
	challengeIssueFunction := requiredMap(t, readbackFunctions, "challengeIssue")
	requireEqual(
		t,
		challengeIssueFunction,
		"exactSignature",
		"internal_rpc_authority.issue_authority_readback_attestation_challenge(uuid,uuid,uuid,text,text,uuid,text,uuid,text)",
	)
	requireEqual(t, challengeIssueFunction, "returns", "uuid")
	requireEqual(t, challengeIssueFunction, "directChallengeInsertGrantAllowed", false)
	requireEqual(t, challengeIssueFunction, "serverGeneratedChallengeFieldsOnly", true)
	requireEqual(
		t,
		challengeIssueFunction,
		"sameIdempotencyKeyAndCanonicalRequestDigest",
		"RETURN_PERSISTED_CHALLENGE",
	)
	challengeConsumeFunction := requiredMap(t, readbackFunctions, "challengeConsume")
	requireEqual(
		t,
		challengeConsumeFunction,
		"exactSignature",
		"internal_rpc_authority.consume_authority_readback_attestation_challenge(uuid,uuid,uuid,text,bigint,uuid,text)",
	)
	requireEqual(t, challengeConsumeFunction, "returns", "uuid")
	requireEqual(t, challengeConsumeFunction, "directReceiptInsertGrantAllowed", false)
	requireEqual(t, challengeConsumeFunction, "atomicChallengeConsumeAndReceiptInsert", true)
	requireEqual(
		t,
		challengeConsumeFunction,
		"sameIdempotencyKeyAndEvidenceDigest",
		"RETURN_PERSISTED_RECEIPT",
	)
	for functionKey, want := range map[string]struct {
		name  string
		table string
	}{
		"keyDelivery": {
			name:  "internal_rpc_authority.record_authority_key_delivery_readback",
			table: "authority_key_delivery_readbacks",
		},
		"snapshot": {
			name:  "internal_rpc_authority.record_authority_snapshot_readback",
			table: "authority_snapshot_readbacks",
		},
	} {
		readbackFunction := requiredMap(t, readbackFunctions, functionKey)
		requireEqual(t, readbackFunction, "name", want.name)
		requireEqual(t, readbackFunction, "targetTable", want.table)
		requireEqual(t, readbackFunction, "security", "SECURITY_DEFINER")
		requireEqual(t, readbackFunction, "callerProvidedWorkloadOrRoleAllowed", false)
		requireEqual(t, readbackFunction, "unsafeOverloadAllowed", false)
		requireEqual(t, readbackFunction, "publisherExecuteAllowed", false)
		requireEqual(t, readbackFunction, "callerProvidedRevisionDigestGenerationAllowed", false)
		requireEqual(t, readbackFunction, "identityArguments", "uuid")
	}
	minimumRights := requiredMap(t, store, "minimumDatabaseRights")
	publisherRights := requiredMap(t, minimumRights, "internal_rpc_authority_publisher")
	requireStringSliceEqual(t, publisherRights, "select", []string{
		"authority_snapshot_history",
		"authority_rotation_intents",
		"authority_key_delivery_readbacks",
		"authority_snapshot_readbacks",
	})
	requireStringSliceEqual(t, publisherRights, "execute", []string{
		"internal_rpc_authority.publisher_append_snapshot_history(bigint,text,bigint,bigint,bigint,bigint,text,text,uuid,text,integer)",
		"internal_rpc_authority.publisher_promote_snapshot(uuid,bigint,text,integer)",
		"internal_rpc_authority.publisher_record_rotation_intent(uuid,bigint,text,bigint,uuid)",
		"internal_rpc_authority.publisher_read_restore_fence()",
		"internal_rpc_authority.promote_authority_workload_database_identity(text,text,bigint,bigint,uuid,uuid)",
		"internal_rpc_authority.is_active_runtime_database_session(text)",
	})
	publisherWrites := map[string]bool{}
	if direct, ok := publisherRights["selectInsertUpdate"].([]any); ok {
		publisherWrites = stringSet(direct)
	}
	for _, table := range protectedReadbackTables {
		if publisherWrites[table] {
			t.Fatalf("publisher retains direct write authority on %s", table)
		}
	}
	for _, role := range []string{
		"internal_rpc_authority_issuer",
		"internal_rpc_authority_verifier",
		"internal_rpc_authority_proof_resolver",
	} {
		rights := requiredMap(t, minimumRights, role)
		requireStringSliceEqual(t, rights, "execute", []string{
			"internal_rpc_authority.record_authority_key_delivery_readback(uuid)",
			"internal_rpc_authority.record_authority_snapshot_readback(uuid)",
		})
		insertUpdate := stringSet(rights["insertUpdate"].([]any))
		for _, table := range protectedReadbackTables {
			if insertUpdate[table] {
				t.Fatalf("%s retains direct write authority on %s", role, table)
			}
		}
	}
	attestorRights := requiredMap(t, minimumRights, "internal_rpc_authority_readback_attestor")
	requireStringSliceEqual(t, attestorRights, "selectInsert", []string{
		"authority_readback_intents",
	})
	requireStringSliceEqual(t, attestorRights, "select", []string{
		"authority_readback_attestation_challenges",
		"authority_readback_attestation_receipts",
	})
	requireStringSliceEqual(t, attestorRights, "execute", []string{
		"internal_rpc_authority.issue_authority_readback_attestation_challenge(uuid,uuid,uuid,text,text,uuid,text,uuid,text)",
		"internal_rpc_authority.consume_authority_readback_attestation_challenge(uuid,uuid,uuid,text,bigint,uuid,text)",
		"internal_rpc_authority.is_active_runtime_database_session(text)",
	})
	runtimeIdentity := requiredMap(t, store, "runtimeCapabilityDatabaseIdentity")
	requireEqual(t, runtimeIdentity, "mappingTable", "authority_runtime_database_identities")
	requireEqual(t, runtimeIdentity, "callerKey", "session_user")
	requireEqual(t, runtimeIdentity, "retirementFenceAppliesToAlreadyOpenSession", true)
	requireEqual(
		t,
		requiredMap(t, runtimeIdentity, "retiredOpenSession"),
		"publisherPromotion",
		"DENIED",
	)

	for name, raw := range deployables {
		deployable, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("deployable %s is not an object", name)
		}
		requiredBefore, ok := deployable["requiredBefore"].([]any)
		if !ok {
			continue
		}
		for _, rawReference := range requiredBefore {
			reference, ok := rawReference.(string)
			if !ok {
				t.Fatalf("deployable %s has non-string requiredBefore", name)
			}
			if _, exists := deployables[reference]; !exists {
				t.Fatalf("deployable %s references missing critical capability %s", name, reference)
			}
		}
	}

	readiness := requiredMap(t, spec, "readiness")
	probe := requiredMap(t, readiness, "applicationProbe")
	requireEqual(t, probe, "issuerFullMethod", issuerReadyMethod)
	requireEqual(t, probe, "verifierFullMethod", verifierReady)

	network := requiredMap(t, spec, "network")
	coordinationNetwork := requiredMap(t, network, "restoreCoordinationNetworkPolicies")
	requireEqual(t, coordinationNetwork, "controllerPort", float64(8443))
	requireEqual(t, coordinationNetwork, "requireBothSourceEgressAndControllerIngress", true)
	if len(coordinationNetwork["exactClients"].([]any)) != 2 {
		t.Fatal("restore coordination network registry is incomplete")
	}
	readbackNetwork := requiredMap(t, network, "readbackAttestationNetworkPolicies")
	requireEqual(t, readbackNetwork, "sourceEgressPolicyPerWorkloadRoleRequired", true)
	requireEqual(t, readbackNetwork, "destinationIngressPolicyRequired", true)
	requireEqual(t, readbackNetwork, "missingEitherDirectionPolicy", "FAIL_CLOSED")
	requireStringSliceEqual(t, readbackNetwork, "exactAllowedMethods", []string{
		"/internalrpcauthority.v1.AuthorityReadbackAttestorService/IssueAttestationChallenge",
		"/internalrpcauthority.v1.AuthorityReadbackAttestorService/AttestServedState",
	})
	publisherDependencyNetwork := requiredMap(
		t,
		network,
		"publisherDatabaseCredentialNetworkPolicies",
	)
	requireEqual(
		t,
		publisherDependencyNetwork,
		"exactSourceServiceAccount",
		"internal-rpc-authority-publisher",
	)
	requireEqual(t, publisherDependencyNetwork, "sourceEgressPolicyRequired", true)
	requireEqual(t, publisherDependencyNetwork, "destinationIngressPolicyRequired", true)
	requireEqual(t, publisherDependencyNetwork, "wildcardDestinationAllowed", false)
	requireEqual(t, publisherDependencyNetwork, "missingEitherDirectionPolicy", "FAIL_CLOSED")
	attestorDependencyNetwork := requiredMap(
		t,
		network,
		"attestorDatabaseCredentialNetworkPolicies",
	)
	requireEqual(
		t,
		attestorDependencyNetwork,
		"exactSourceServiceAccount",
		"internal-rpc-authority-readback-attestor",
	)
	requireEqual(t, attestorDependencyNetwork, "sourceEgressPolicyRequired", true)
	requireEqual(t, attestorDependencyNetwork, "destinationIngressPolicyRequired", true)
	requireEqual(t, attestorDependencyNetwork, "wildcardDestinationAllowed", false)
	requireEqual(t, attestorDependencyNetwork, "missingEitherDirectionPolicy", "FAIL_CLOSED")
	configuration := requiredMap(t, spec, "configuration")
	environmentKeys := requiredMap(t, configuration, "environmentKeys")
	requireStringSliceEqual(t, environmentKeys, "readbackAttestor", []string{
		"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_LISTEN_ADDRESS",
		"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_TLS_CERT_FILE",
		"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_TLS_KEY_FILE",
		"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_CLIENT_CA_FILE",
		"INTERNAL_RPC_AUTHORITY_READBACK_CREDENTIAL_TRUST_FILE",
		"INTERNAL_RPC_AUTHORITY_READBACK_MANIFEST_ROOT_FILE",
		"INTERNAL_RPC_AUTHORITY_READBACK_MANIFEST_ROOT_FINGERPRINT_SHA256",
		"INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE",
		"INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME",
		"INTERNAL_RPC_AUTHORITY_POSTGRES_CA_FILE",
		"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_DATABASE_USERNAME_FILE",
		"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_DATABASE_PASSWORD_FILE",
		"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_VAULT_ROLE",
		"INTERNAL_RPC_AUTHORITY_VAULT_ADDRESS",
		"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_VAULT_AUTH_FILE",
		"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_VAULT_TLS_SERVER_NAME",
		"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_VAULT_CA_FILE",
	})
	mtls := requiredMap(t, network, "mtlsDownstreamRpc")
	requireEqual(t, mtls, "exactServerName", "REQUIRED_PER_OPERATION_BINDING")
	requireEqual(t, mtls, "exactTrustBundleId", "REQUIRED_PER_OPERATION_BINDING")
	requireEqual(t, mtls, "plaintextFallback", "FORBIDDEN")
}

func TestRegistryOwnership(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "contracts/registry.yaml")
	data := []byte(readFile(t, path))
	var registry struct {
		Version  int `json:"version"`
		Packages []struct {
			ID        string            `json:"id"`
			Format    string            `json:"format"`
			Owner     string            `json:"owner"`
			Source    string            `json:"source"`
			Generated map[string]string `json:"generated"`
			Consumers []string          `json:"consumers"`
		} `json:"packages"`
	}
	if err := yaml.UnmarshalStrict(data, &registry); err != nil {
		t.Fatalf("strictly decode contract registry: %v", err)
	}

	entries := make(map[string]struct {
		Format    string
		Owner     string
		Source    string
		Generated map[string]string
		Consumers []string
	}, len(registry.Packages))
	for _, entry := range registry.Packages {
		if _, exists := entries[entry.ID]; exists {
			t.Fatalf("duplicate contract registry id %s", entry.ID)
		}
		entries[entry.ID] = struct {
			Format    string
			Owner     string
			Source    string
			Generated map[string]string
			Consumers []string
		}{
			Format:    entry.Format,
			Owner:     entry.Owner,
			Source:    entry.Source,
			Generated: entry.Generated,
			Consumers: entry.Consumers,
		}
	}

	protoEntry, ok := entries["internal-rpc-authority-v1"]
	if !ok {
		t.Fatal("internal-rpc-authority-v1 registry entry is missing")
	}
	if protoEntry.Format != "proto" ||
		protoEntry.Owner != "internal-rpc-authority" ||
		protoEntry.Source != "contracts/proto/internalrpcauthority/v1" ||
		protoEntry.Generated["go"] != "libs/go/internalrpcauth/gen/internalrpcauthority/v1" ||
		len(protoEntry.Consumers) < 2 {
		t.Fatalf("unexpected authority registry entry: %+v", protoEntry)
	}

	for id, source := range map[string]string{
		"internal-rpc-authority-proof-v1":                               "contracts/authorization/v1/authority-proof.schema.json",
		"internal-rpc-authority-restore-evidence-v1":                    "contracts/authorization/v1/restore-fence-evidence.schema.json",
		"internal-rpc-authority-restore-coordination-v1":                "contracts/authorization/v1/restore-coordination.schema.json",
		"internal-rpc-authority-restore-role-trust-v1":                  "contracts/authorization/v1/restore-role-trust.schema.json",
		"internal-rpc-authority-readback-postgresql-v1":                 "contracts/authorization/v1/postgresql-readback-boundary.sql",
		"internal-rpc-authority-readback-attestation-v1":                "contracts/authorization/v1/readback-attestation.schema.json",
		"internal-rpc-authority-readback-credential-v1":                 "contracts/authorization/v1/readback-credential.schema.json",
		"internal-rpc-authority-readback-credential-trust-v1":           "contracts/authorization/v1/readback-credential-trust.schema.json",
		"internal-rpc-authority-readback-manifest-trust-root-v1":        "contracts/authorization/v1/readback-manifest-trust-root.schema.json",
		"internal-rpc-authority-readback-root-verification-material-v1": "contracts/authorization/v1/readback-root-verification-material.schema.json",
		"internal-rpc-authority-readback-root-rotation-v1":              "contracts/authorization/v1/readback-root-rotation.schema.json",
		"internal-rpc-authority-key-delivery-v1":                        "contracts/authorization/v1/key-delivery-targets.schema.json",
		"internal-rpc-authority-error-matrix-v1":                        "contracts/authorization/v1/authorization-error-matrix.json",
	} {
		entry, exists := entries[id]
		if !exists || entry.Source != source || len(entry.Consumers) == 0 {
			t.Fatalf("registry entry %s is missing or inconsistent: %+v", id, entry)
		}
	}
	restoreEntry := entries["internal-rpc-authority-restore-evidence-v1"]
	if restoreEntry.Owner != "internal-rpc-authority-restore-controller" {
		t.Fatalf("restore evidence has unresolved owner: %+v", restoreEntry)
	}
}

func TestAuthorityProofNegativeFixtures(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(
		root,
		"contracts/authorization/v1/fixtures/authority-proof-negative.json",
	)
	var fixtures struct {
		Version int `json:"v"`
		Cases   []struct {
			Name     string `json:"name"`
			Mutation string `json:"mutation"`
			GRPCCode string `json:"grpc_code"`
			Reason   string `json:"reason"`
		} `json:"cases"`
	}
	decodeJSONStrict(t, path, &fixtures)

	expected := map[string]string{
		"caller-controlled-syntactic-authority-tuple": "AUTHORITY_PROOF_REQUIRED",
		"untrusted-proof-signer":                      "AUTHORITY_PROOF_INVALID",
		"trusted-signer-cross-tenant":                 "AUTHORITY_SCOPE_MISMATCH",
		"wrong-caller-workload":                       "AUTHORITY_PROOF_BINDING_MISMATCH",
		"wrong-operation":                             "AUTHORITY_PROOF_BINDING_MISMATCH",
		"wrong-downstream-audience":                   "AUTHORITY_PROOF_BINDING_MISMATCH",
		"expired-proof":                               "AUTHORITY_PROOF_EXPIRED",
		"lower-proof-revision":                        "AUTHORITY_PROOF_REVISION_REJECTED",
		"same-revision-proof-mutation":                "AUTHORITY_PROOF_REVISION_REJECTED",
		"replayed-proof-jti":                          "AUTHORITY_PROOF_REPLAY_DETECTED",
	}
	if fixtures.Version != 1 || len(fixtures.Cases) != len(expected) {
		t.Fatalf("unexpected authority proof fixture cardinality: %+v", fixtures)
	}
	seen := make(map[string]bool, len(fixtures.Cases))
	for _, fixture := range fixtures.Cases {
		if fixture.Mutation == "" || fixture.GRPCCode == "" {
			t.Fatalf("authority proof fixture is incomplete: %+v", fixture)
		}
		wantReason, ok := expected[fixture.Name]
		if !ok || fixture.Reason != wantReason || seen[fixture.Name] {
			t.Fatalf("unexpected authority proof fixture: %+v", fixture)
		}
		seen[fixture.Name] = true
	}
}

func TestAuthorityProofFirstCallIsNonCyclic(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(
		root,
		"contracts/authorization/v1/fixtures/authority-proof-first-call.json",
	)
	var fixture struct {
		Version  int    `json:"v"`
		Scenario string `json:"scenario"`
		Steps    []struct {
			Order                        int      `json:"order"`
			Caller                       string   `json:"caller"`
			Target                       string   `json:"target"`
			FullMethod                   string   `json:"full_method"`
			Transport                    string   `json:"transport"`
			ApplicationCredential        string   `json:"application_credential"`
			InternalAuthorizationContext string   `json:"internal_authorization_context"`
			RequestFields                []string `json:"request_fields"`
			ServerResolves               []string `json:"server_resolves"`
		} `json:"steps"`
		TerminalResult string `json:"terminal_result"`
	}
	decodeJSONStrict(t, path, &fixture)
	if fixture.Version != 1 || len(fixture.Steps) != 3 {
		t.Fatalf("unexpected first-call fixture: %+v", fixture)
	}
	first := fixture.Steps[0]
	if first.Caller != "control-api-gateway" ||
		first.Target != "control-plane" ||
		first.FullMethod != "/internalrpcauthority.v1.AuthorityProofResolverService/ResolveAuthorityProof" ||
		first.Transport != "MTLS" ||
		first.ApplicationCredential != "OIDC_BEARER_METADATA" ||
		first.InternalAuthorizationContext != "FORBIDDEN" ||
		containsString(first.RequestFields, "actor") ||
		containsString(first.RequestFields, "tenant") ||
		containsString(first.RequestFields, "project") ||
		strings.Join(first.ServerResolves, ",") != "actor,tenant,project,ownership,provenance" {
		t.Fatalf("first call is cyclic or caller-authoritative: %+v", first)
	}
}

func TestAuthorityResolutionNotFoundIsIndistinguishable(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "contracts/authorization/v1/fixtures/authority-resolution-negative.json")
	var fixture struct {
		Version int `json:"v"`
		Cases   []struct {
			Name      string `json:"name"`
			Mutation  string `json:"mutation"`
			GRPCCode  string `json:"grpc_code"`
			Reason    string `json:"reason"`
			Stage     string `json:"stage"`
			Retryable bool   `json:"retryable"`
			Message   string `json:"message"`
		} `json:"cases"`
	}
	decodeJSONStrict(t, path, &fixture)
	if fixture.Version != 1 || len(fixture.Cases) != 2 {
		t.Fatalf("unexpected authority resolution fixture: %+v", fixture)
	}
	for _, item := range fixture.Cases {
		if item.Mutation == "" ||
			item.GRPCCode != "NOT_FOUND" ||
			item.Reason != "AUTHORITY_RESOURCE_NOT_FOUND" ||
			item.Stage != "AUTHORITY_RESOLUTION" ||
			item.Retryable ||
			item.Message != "authority resource not found" {
			t.Fatalf("hidden and missing outcomes are distinguishable: %+v", item)
		}
	}
}

func TestRestoreCoordinationSameWorkloadTwoRoles(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "contracts/authorization/v1/fixtures/restore-coordination.json")
	var fixture struct {
		Version  int `json:"v"`
		Positive struct {
			Name             string `json:"name"`
			WorkloadID       string `json:"workload_id"`
			WorkloadSPIFFEID string `json:"workload_spiffe_id"`
			Roles            []struct {
				Role                 string `json:"role"`
				WorkloadGeneration   int    `json:"workload_generation"`
				CredentialGeneration int    `json:"credential_generation"`
				CredentialID         string `json:"role_credential_id"`
				SignerGeneration     int    `json:"credential_signer_generation"`
				SignerKeyID          string `json:"credential_signer_kid"`
				AckKeyID             string `json:"ack_key_id"`
				AckKeyGeneration     int    `json:"ack_key_generation"`
				AckKeyThumbprint     string `json:"ack_key_thumbprint_sha256"`
				NetworkPolicy        string `json:"network_policy"`
			} `json:"roles"`
			ExpectedAckCount                 int  `json:"expected_ack_count"`
			PreparedAfterDistinctOneTimeAcks bool `json:"prepared_after_distinct_one_time_acks"`
			ControllerVerifiesTwoSignatures  bool `json:"controller_verifies_publisher_signature_then_ack_signature"`
		} `json:"positive"`
		Negative []struct {
			Name     string `json:"name"`
			Mutation string `json:"mutation"`
			Reason   string `json:"reason"`
		} `json:"negative"`
	}
	decodeJSONStrict(t, path, &fixture)
	if fixture.Version != 1 ||
		fixture.Positive.WorkloadID != "control-plane" ||
		len(fixture.Positive.Roles) != 2 ||
		fixture.Positive.ExpectedAckCount != 2 ||
		!fixture.Positive.PreparedAfterDistinctOneTimeAcks ||
		!fixture.Positive.ControllerVerifiesTwoSignatures {
		t.Fatalf("same-workload two-role fixture is incomplete: %+v", fixture.Positive)
	}
	first, second := fixture.Positive.Roles[0], fixture.Positive.Roles[1]
	if first.Role == second.Role ||
		first.CredentialID == second.CredentialID ||
		first.AckKeyID == second.AckKeyID ||
		first.AckKeyGeneration == second.AckKeyGeneration ||
		first.AckKeyThumbprint == second.AckKeyThumbprint ||
		first.WorkloadGeneration != second.WorkloadGeneration ||
		first.NetworkPolicy == "" ||
		second.NetworkPolicy == "" {
		t.Fatal("shared SPIFFE roles do not have distinct application identities")
	}
	expected := map[string]string{
		"duplicate-spiffe-opposite-role-credential": "RESTORE_ROLE_CREDENTIAL_REJECTED",
		"missing-quiescing-directive":               "RESTORE_DIRECTIVE_REJECTED",
		"stale-role-generation":                     "RESTORE_ROLE_CREDENTIAL_REJECTED",
		"replayed-directive-or-ack-jti":             "RESTORE_ACK_REPLAY_DETECTED",
		"unknown-or-expired-credential-signer":      "RESTORE_ROLE_CREDENTIAL_REJECTED",
		"substituted-ack-public-jwk":                "RESTORE_ROLE_CREDENTIAL_REJECTED",
		"missing-controller-trust-readback":         "RESTORE_COORDINATION_UNAVAILABLE",
		"missing-client-egress-network-policy":      "RESTORE_COORDINATION_UNAVAILABLE",
		"missing-controller-ingress-network-policy": "RESTORE_COORDINATION_UNAVAILABLE",
	}
	if len(fixture.Negative) != len(expected) {
		t.Fatal("restore coordination negative coverage is incomplete")
	}
	for _, item := range fixture.Negative {
		if item.Mutation == "" || expected[item.Name] != item.Reason {
			t.Fatalf("unexpected restore coordination fixture: %+v", item)
		}
	}
}

func TestRestoreTrustSemanticRetryAndCrashRecovery(t *testing.T) {
	root := repositoryRoot(t)
	var fixture map[string]any
	decodeJSONStrict(
		t,
		filepath.Join(
			root,
			"contracts/authorization/v1/fixtures/restore-ack-state.json",
		),
		&fixture,
	)
	trust := requiredMap(t, fixture, "credential_signer_trust")
	requireEqual(t, trust, "controller_owned_readback_required", true)
	keys := trust["keys"].([]any)
	if len(keys) != 3 {
		t.Fatalf("restore role trust key cardinality = %d, want 3", len(keys))
	}
	statuses := make(map[string]bool, 3)
	for _, raw := range keys {
		key := raw.(map[string]any)
		statuses[requiredString(t, key, "status")] = true
		if requiredString(t, key, "kid") == "" ||
			len(requiredString(t, key, "jwk_thumbprint_sha256")) != 64 {
			t.Fatal("restore role trust key lacks kid or thumbprint")
		}
	}
	for _, status := range []string{"CURRENT", "NEXT", "PREVIOUS"} {
		if !statuses[status] {
			t.Fatalf("restore role trust does not contain %s", status)
		}
	}
	issuance := requiredMap(t, fixture, "issuance_delivery")
	requireEqual(
		t,
		issuance,
		"method",
		"/internalrpcauthority.v1.RestoreRoleCredentialPublisherService/PublishRoleCredential",
	)
	requireEqual(t, issuance, "private_ack_key_destination", "EXACT_ROLE_VAULT_PATH_FROM_TARGET_REGISTRY")
	requireEqual(t, issuance, "public_ack_jwk_destination", "SIGNED_ROLE_CREDENTIAL")
	requireEqual(t, issuance, "controller_persists_delivery_receipt_before_directive", true)

	restore := requiredMap(t, fixture, "restore")
	expectedRoles := restore["expected_roles"].([]any)
	if len(expectedRoles) != 2 {
		t.Fatalf("expected restore role cardinality = %d, want 2", len(expectedRoles))
	}
	expectedRoleSet := make(map[string]bool, len(expectedRoles))
	for _, raw := range expectedRoles {
		role := raw.(map[string]any)
		key := fmt.Sprintf(
			"%s|%s|%.0f|%.0f",
			requiredString(t, role, "workload_id"),
			requiredString(t, role, "role"),
			role["workload_generation"].(float64),
			role["credential_generation"].(float64),
		)
		if expectedRoleSet[key] {
			t.Fatal("expected restore set contains duplicate role tuple")
		}
		expectedRoleSet[key] = true
	}
	subsets := restore["partial_subsets"].([]any)
	wantPhases := []string{"QUIESCING", "QUIESCING", "PREPARED"}
	if len(subsets) != len(wantPhases) {
		t.Fatal("partial ACK crash subsets are incomplete")
	}
	for index, raw := range subsets {
		subset := raw.(map[string]any)
		acceptedRoles := subset["accepted_roles"].([]any)
		phase := requiredString(t, subset, "resulting_phase")
		if phase != wantPhases[index] {
			t.Fatalf("partial subset %d has unsafe phase", index)
		}
		if (phase == "PREPARED") != (len(acceptedRoles) == len(expectedRoles)) {
			t.Fatalf("partial subset %d violates exact full-set PREPARED rule", index)
		}
		seen := make(map[string]bool, len(acceptedRoles))
		for _, rawRole := range acceptedRoles {
			role := rawRole.(string)
			if seen[role] {
				t.Fatalf("partial subset %d accepts duplicate role %s", index, role)
			}
			seen[role] = true
		}
	}
	persistedFields := stringSet(restore["persisted_ack_record_fields"].([]any))
	for _, field := range []string{
		"restore_id",
		"coordination_revision",
		"workload_id",
		"role",
		"workload_generation",
		"credential_generation",
		"directive_digest_sha256",
		"ack_digest_sha256",
		"ack_jti",
		"idempotency_key",
		"semantic_request_digest_sha256",
		"receipt_id",
		"accepted_resulting_phase",
	} {
		if !persistedFields[field] {
			t.Fatalf("durable ACK record does not contain %s", field)
		}
	}
	retry := requiredMap(t, fixture, "semantic_retry")
	for _, field := range []string{
		"lost_response_retry_returns_same_receipt",
		"concurrent_duplicate_returns_same_receipt",
		"different_digest_is_replay",
	} {
		requireEqual(t, retry, field, true)
	}
	type acceptedReceipt struct {
		digest  string
		jti     string
		receipt string
	}
	receiptsByKey := map[string]acceptedReceipt{}
	accept := func(key, jti, digest, receipt string) (string, error) {
		if accepted, exists := receiptsByKey[key]; exists {
			if accepted.jti == jti && accepted.digest == digest {
				return accepted.receipt, nil
			}
			return "", fmt.Errorf("RESTORE_ACK_REPLAY_DETECTED")
		}
		for _, accepted := range receiptsByKey {
			if accepted.jti == jti {
				return "", fmt.Errorf("RESTORE_ACK_REPLAY_DETECTED")
			}
		}
		receiptsByKey[key] = acceptedReceipt{
			digest:  digest,
			jti:     jti,
			receipt: receipt,
		}
		return receipt, nil
	}
	key := requiredString(t, retry, "idempotency_key")
	jti := requiredString(t, retry, "ack_jti")
	digest := requiredString(t, retry, "semantic_request_digest_sha256")
	receiptID := requiredString(t, retry, "receipt_id")
	first, err := accept(key, jti, digest, receiptID)
	if err != nil {
		t.Fatalf("first semantic ACK rejected: %v", err)
	}
	repeated, err := accept(key, jti, digest, "ignored-new-receipt")
	if err != nil || repeated != first {
		t.Fatalf("lost-response retry did not return saved receipt: %q/%v", repeated, err)
	}
	if _, err := accept(key, jti, strings.Repeat("f", 64), receiptID); err == nil {
		t.Fatal("same ACK idempotency key and JTI with a different digest was accepted")
	}
}

func TestReadbackChallengeAtomicConsumeRetryAndCrashContract(t *testing.T) {
	type challenge struct {
		ID            string
		RequestDigest string
		Status        string
		Receipt       string
		Evidence      string
	}
	challengesByKey := map[string]*challenge{}
	issue := func(key, digest, id string) (*challenge, error) {
		if saved := challengesByKey[key]; saved != nil {
			if saved.RequestDigest != digest {
				return nil, fmt.Errorf("IDEMPOTENCY_CONFLICT")
			}
			return saved, nil
		}
		saved := &challenge{ID: id, RequestDigest: digest, Status: "ISSUED"}
		challengesByKey[key] = saved
		return saved, nil
	}
	consume := func(saved *challenge, evidenceDigest, receipt string) (string, error) {
		if saved.Status == "CONSUMED" {
			if saved.Evidence == evidenceDigest {
				return saved.Receipt, nil
			}
			return "", fmt.Errorf("READBACK_CHALLENGE_REPLAY_DETECTED")
		}
		if saved.Status != "ISSUED" {
			return "", fmt.Errorf("READBACK_CHALLENGE_REJECTED")
		}
		saved.Status = "CONSUMED"
		saved.Evidence = evidenceDigest
		saved.Receipt = receipt
		return receipt, nil
	}

	saved, err := issue("challenge-key", strings.Repeat("1", 64), "challenge-1")
	if err != nil {
		t.Fatalf("first challenge issuance failed: %v", err)
	}
	retried, err := issue("challenge-key", strings.Repeat("1", 64), "ignored")
	if err != nil || retried != saved {
		t.Fatalf("lost challenge response retry did not return persisted challenge: %v", err)
	}
	if _, err := issue("challenge-key", strings.Repeat("2", 64), "ignored"); err == nil {
		t.Fatal("same challenge key with a different request digest was accepted")
	}

	// Crash до commit не меняет ISSUED; следующая replica может завершить ту
	// же transaction. После commit exact retry возвращает persisted receipt.
	if saved.Status != "ISSUED" {
		t.Fatal("challenge did not remain issued before consume commit")
	}
	receipt, err := consume(saved, strings.Repeat("a", 64), "receipt-1")
	if err != nil || receipt != "receipt-1" {
		t.Fatalf("challenge consume failed: %q/%v", receipt, err)
	}
	retriedReceipt, err := consume(saved, strings.Repeat("a", 64), "ignored")
	if err != nil || retriedReceipt != receipt {
		t.Fatalf("lost attestation response retry did not return persisted receipt: %v", err)
	}
	if _, err := consume(saved, strings.Repeat("b", 64), "ignored"); err == nil {
		t.Fatal("consumed challenge accepted a different evidence digest")
	}
}

func TestReadbackAttestationAndCredentialLifecycleAreExecutable(t *testing.T) {
	root := repositoryRoot(t)
	boundary := readFile(t, filepath.Join(
		root,
		"contracts/authorization/v1/postgresql-readback-boundary.sql",
	))
	for _, want := range []string{
		"authority_readback_intents",
		"authority_readback_attestation_challenges",
		"authority_readback_attestation_receipts",
		"challenge_status = 'ISSUED'",
		"authority_readback_challenge_consume",
		"consume_authority_readback_attestation_challenge",
		"readback_credential_jti",
		"ES256_NORMAL_READBACK_POSSESSION_CHALLENGE_V1",
		"p_attestation_receipt_id uuid",
		"credential_status IN ('CURRENT', 'NEXT')",
		"credential_status = 'PREVIOUS'",
		"attestation.consumed_at IS NULL",
		"attestation.served_state_digest_sha256 = pinned.digest_sha256",
		"promote_authority_workload_database_identity",
		"current_setting('transaction_isolation') <> 'serializable'",
	} {
		if !strings.Contains(boundary, want) {
			t.Fatalf("readback boundary does not contain %q", want)
		}
	}
	for _, forbidden := range []string{
		"p_served_proof_sha256 text",
		"UNIQUE (workload_id, role, workload_generation, active)",
	} {
		if strings.Contains(boundary, forbidden) {
			t.Fatalf("readback boundary still trusts or blocks lifecycle via %q", forbidden)
		}
	}
	behavior := readFile(t, filepath.Join(
		root,
		"contracts/authorization/v1/postgresql-readback-behavior.sql",
	))
	for _, want := range []string{
		"'CURRENT'",
		"'NEXT'",
		"record_authority_key_delivery_readback",
		"record_authority_snapshot_readback",
		"BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE",
		"'PREVIOUS'",
		"'RETIRED'",
		"reused attestation receipt was accepted",
		"retired principal was accepted",
		"attestor direct challenge write was accepted",
		"attestor direct receipt write was accepted",
		"publisher direct key delivery write was accepted",
		"publisher direct snapshot update was accepted",
		"publisher consumer readback function was accepted",
		"\\connect :verifier_g1_dsn",
		"\\connect :publisher_dsn",
		"\\connect :attestor_g2_dsn",
		"SET LOCAL ROLE internal_rpc_authority_publisher",
		"SET LOCAL ROLE internal_rpc_authority_readback_attestor",
	} {
		if !strings.Contains(behavior, want) {
			t.Fatalf("PostgreSQL behavior contour does not contain %q", want)
		}
	}
	if strings.Contains(behavior, "SESSION AUTHORIZATION") {
		t.Fatal("PostgreSQL behavior contour still depends on superuser session impersonation")
	}
}

func TestPostgreSQLReadbackBoundaryIsExact(t *testing.T) {
	root := repositoryRoot(t)
	sql := readFile(t, filepath.Join(
		root,
		"contracts/authorization/v1/postgresql-readback-boundary.sql",
	))
	for _, want := range []string{
		"CREATE ROLE internal_rpc_authority_readback_owner",
		"CREATE ROLE ira_publisher_g1",
		"CREATE ROLE ira_publisher_g2",
		"CREATE ROLE ira_readback_attestor_g1",
		"CREATE ROLE ira_readback_attestor_g2",
		"GRANT internal_rpc_authority_publisher TO ira_publisher_g1, ira_publisher_g2",
		"CREATE TABLE internal_rpc_authority.authority_runtime_database_identities",
		"CREATE FUNCTION internal_rpc_authority.is_active_runtime_database_session(",
		"session_login = session_user",
		"FOR SHARE",
		"publisher runtime database identity rejected",
		"readback attestor runtime database identity rejected",
		"NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS",
		"LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS",
		"CREATE SCHEMA internal_rpc_authority",
		"FORCE ROW LEVEL SECURITY",
		"SET search_path = pg_catalog, internal_rpc_authority, pg_temp",
		"internal_rpc_authority.record_authority_key_delivery_readback(\n  p_attestation_receipt_id uuid\n)",
		"internal_rpc_authority.record_authority_snapshot_readback(\n  p_attestation_receipt_id uuid\n)",
		"CREATE UNIQUE INDEX authority_identity_one_current",
		"CREATE UNIQUE INDEX authority_identity_one_next",
		"CREATE UNIQUE INDEX authority_identity_one_previous",
		"workload_spiffe_id text NOT NULL",
		"readback_credential_jti uuid NOT NULL UNIQUE",
		"possession_key_thumbprint_sha256 text NOT NULL",
		"ES256_NORMAL_READBACK_POSSESSION_CHALLENGE_V1",
		"CREATE TABLE internal_rpc_authority.authority_readback_attestation_challenges",
		"CREATE TRIGGER authority_readback_challenge_consume",
		"internal_rpc_authority.issue_authority_readback_attestation_challenge(\n  p_intent_id uuid,\n  p_challenge_id uuid,\n  p_challenge_jti uuid,\n  p_challenge_nonce text,\n  p_challenge_digest_sha256 text,\n  p_readback_credential_jti uuid,\n  p_readback_credential_digest_sha256 text,\n  p_idempotency_key uuid,\n  p_semantic_request_digest_sha256 text\n)",
		"internal_rpc_authority.consume_authority_readback_attestation_challenge(\n  p_challenge_id uuid,\n  p_receipt_id uuid,\n  p_evidence_jti uuid,\n  p_evidence_digest_sha256 text,\n  p_verifier_generation bigint,\n  p_idempotency_key uuid,\n  p_semantic_request_digest_sha256 text\n)",
		"TO internal_rpc_authority_publisher;",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("PostgreSQL readback contract does not contain %q", want)
		}
	}
	if strings.Contains(sql, "GRANT EXECUTE ON FUNCTION\n  internal_rpc_authority.record_authority_snapshot_readback(uuid)\n  TO internal_rpc_authority_publisher") ||
		strings.Contains(sql, "GRANT INSERT") ||
		strings.Contains(sql, "GRANT UPDATE") {
		t.Fatal("publisher or runtime retained direct readback write authority")
	}
	if strings.Contains(
		sql,
		"internal_rpc_authority.authority_readback_attestation_receipts\nTO ira_readback_attestor_g1",
	) && strings.Contains(
		sql,
		"GRANT SELECT, INSERT ON\n  internal_rpc_authority.authority_readback_attestation_receipts",
	) {
		t.Fatal("attestor retained direct receipt insert instead of atomic consume function")
	}
	if strings.Contains(
		sql,
		"GRANT SELECT, INSERT ON\n  internal_rpc_authority.authority_readback_attestation_challenges",
	) {
		t.Fatal("attestor retained direct challenge insert instead of issue function")
	}
	if count := strings.Count(sql, "CREATE FUNCTION internal_rpc_authority.record_authority_"); count != 2 {
		t.Fatalf("unsafe function overload count = %d", count)
	}
	if count := strings.Count(
		sql,
		"CREATE FUNCTION internal_rpc_authority.issue_authority_readback_attestation_challenge(",
	); count != 1 {
		t.Fatalf("unsafe challenge issue function overload count = %d", count)
	}
	if count := strings.Count(
		sql,
		"CREATE FUNCTION internal_rpc_authority.consume_authority_readback_attestation_challenge(",
	); count != 1 {
		t.Fatalf("unsafe challenge consume function overload count = %d", count)
	}
	for _, liveContract := range []struct {
		path string
		want []string
	}{
		{
			path: "postgresql-publisher-live-session-retirement.sql",
			want: []string{
				"SET ROLE internal_rpc_authority_publisher",
				"publisher runtime database identity rejected",
				"retired open publisher session retained evidence read",
				"retired publisher retained direct snapshot read",
				"retired publisher retained direct rotation insert",
				"retired publisher retained direct restore fence read",
			},
		},
		{
			path: "postgresql-attestor-live-session-retirement.sql",
			want: []string{
				"SET ROLE internal_rpc_authority_readback_attestor",
				"readback attestor runtime database identity rejected",
				"retired open attestor session retained protected reads",
			},
		},
	} {
		contract := readFile(t, filepath.Join(
			root,
			"contracts/authorization/v1",
			liveContract.path,
		))
		for _, want := range liveContract.want {
			if !strings.Contains(contract, want) {
				t.Fatalf("%s does not contain %q", liveContract.path, want)
			}
		}
		if strings.Contains(contract, "SESSION AUTHORIZATION") {
			t.Fatalf("%s depends on superuser session impersonation", liveContract.path)
		}
	}

	postgresTest := readFile(t, filepath.Join(
		root,
		"scripts/test-internal-rpc-authority-postgres-contract.sh",
	))
	for _, want := range []string{
		"INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_PUBLISHER_G2_DSN",
		"REVOKE internal_rpc_authority_publisher FROM ira_publisher_g1",
		"REVOKE internal_rpc_authority_readback_attestor FROM ira_readback_attestor_g1",
		"next publisher runtime database identity was rejected",
		"next attestor runtime database identity was rejected",
	} {
		if !strings.Contains(postgresTest, want) {
			t.Fatalf("PostgreSQL integration contour does not contain %q", want)
		}
	}
}

func TestApprovedGuideCoversIndependentTrustAndLiveSessionFence(t *testing.T) {
	guide := readFile(t, filepath.Join(
		repositoryRoot(t),
		"docs/guides/distributed-security.md",
	))
	for _, want := range []string{
		"Отдельный владелец\nдоставляет точный открытый JWK/сертификат корня",
		"Fingerprint без открытого ключа не позволяет проверить\nподпись",
		"старый корень подписывает точный\nновый открытый ключ",
		"`NOLOGIN`, отзыв членства и смена пароля не прекращают уже открытую",
		"неизменяемый `session_user`",
		"удерживают строку идентичности",
		"Аварийное завершение действия освобождает\nблокировку",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("GUIDE-DOC-003 does not contain %q", want)
		}
	}
}

func TestRuntimeSessionFenceRetirementOrderAndCrashRetry(t *testing.T) {
	type runtimeFence struct {
		status string
		locked bool
	}
	beginAction := func(fence *runtimeFence) error {
		if fence.locked {
			return fmt.Errorf("SERIALIZATION_RETRY")
		}
		if fence.status != "CURRENT" && fence.status != "NEXT" {
			return fmt.Errorf("RUNTIME_DATABASE_IDENTITY_REJECTED")
		}
		fence.locked = true
		return nil
	}
	finishAction := func(fence *runtimeFence) {
		fence.locked = false
	}
	retire := func(fence *runtimeFence) error {
		if fence.locked {
			return fmt.Errorf("SERIALIZATION_RETRY")
		}
		fence.status = "RETIRED"
		return nil
	}

	actionFirst := &runtimeFence{status: "CURRENT"}
	if err := beginAction(actionFirst); err != nil {
		t.Fatalf("active action did not acquire the runtime fence: %v", err)
	}
	if err := retire(actionFirst); err == nil {
		t.Fatal("retirement bypassed an in-flight runtime fence")
	}
	finishAction(actionFirst)
	if err := retire(actionFirst); err != nil {
		t.Fatalf("retirement did not proceed after action commit: %v", err)
	}
	if err := beginAction(actionFirst); err == nil {
		t.Fatal("action retry after committed retirement was accepted")
	}

	crashedAction := &runtimeFence{status: "NEXT"}
	if err := beginAction(crashedAction); err != nil {
		t.Fatalf("NEXT action did not acquire the runtime fence: %v", err)
	}
	finishAction(crashedAction)
	if err := retire(crashedAction); err != nil {
		t.Fatalf("retirement did not proceed after action crash: %v", err)
	}
	if err := beginAction(crashedAction); err == nil {
		t.Fatal("crashed action retry after retirement was accepted")
	}

	retirementFirst := &runtimeFence{status: "CURRENT"}
	if err := retire(retirementFirst); err != nil {
		t.Fatalf("retirement-first transition failed: %v", err)
	}
	if err := beginAction(retirementFirst); err == nil {
		t.Fatal("action started after committed retirement")
	}
}

func TestRoundTwoNegativeFixtureCoverage(t *testing.T) {
	root := repositoryRoot(t)
	fixtures := map[string]map[string]string{
		"delivery-negative.json": {
			"missing-target-verifier": "MISSING_REQUIRED_ROLE",
			"missing-target-readback": "MISSING_CRYPTOGRAPHIC_READBACK",
			"target-private-auth-key": "OPPOSITE_ROLE_MATERIAL",
			"duplicate-target-role":   "AMBIGUOUS_WORKLOAD_ROLE",
			"wildcard-vault-path":     "NON_EXACT_DELIVERY_PATH",
		},
		"readback-authority-negative.json": {
			"cross-target-write":                                  "DATABASE_IDENTITY_MISMATCH",
			"opposite-role-write":                                 "DATABASE_ROLE_MISMATCH",
			"caller-supplied-workload":                            "CALLER_IDENTITY_FIELD_FORBIDDEN",
			"stale-credential-generation":                         "DATABASE_CREDENTIAL_GENERATION_REJECTED",
			"shared-group-login":                                  "DATABASE_LOGIN_PRINCIPAL_REQUIRED",
			"direct-key-delivery-table-write":                     "DIRECT_READBACK_WRITE_FORBIDDEN",
			"direct-snapshot-table-write":                         "DIRECT_READBACK_WRITE_FORBIDDEN",
			"publisher-direct-key-delivery-write":                 "DIRECT_READBACK_WRITE_FORBIDDEN",
			"publisher-direct-snapshot-write":                     "DIRECT_READBACK_WRITE_FORBIDDEN",
			"publisher-key-delivery-function-execute":             "DATABASE_FUNCTION_EXECUTE_FORBIDDEN",
			"publisher-snapshot-function-execute":                 "DATABASE_FUNCTION_EXECUTE_FORBIDDEN",
			"caller-supplied-revision-digest-generation":          "CALLER_STATE_FIELD_FORBIDDEN",
			"arbitrary-proof-hash":                                "ATTESTATION_RECEIPT_REQUIRED",
			"stale-pinned-intent":                                 "PINNED_INTENT_REJECTED",
			"opposite-role-attestation":                           "ATTESTATION_ROLE_BINDING_REJECTED",
			"missing-served-state":                                "SERVED_STATE_ATTESTATION_REJECTED",
			"reused-attestation-receipt":                          "ATTESTATION_RECEIPT_REPLAY",
			"concurrent-promotion":                                "SERIALIZATION_RETRY",
			"missing-server-challenge":                            "READBACK_CHALLENGE_REJECTED",
			"attestor-direct-challenge-write":                     "DIRECT_READBACK_WRITE_FORBIDDEN",
			"attestor-direct-receipt-write":                       "DIRECT_READBACK_WRITE_FORBIDDEN",
			"expired-server-challenge":                            "READBACK_CHALLENGE_REJECTED",
			"replayed-server-challenge":                           "READBACK_CHALLENGE_REPLAY_DETECTED",
			"lost-response-exact-retry":                           "RETURN_PERSISTED_RECEIPT",
			"restore-credential-at-readback-attestor":             "READBACK_CREDENTIAL_REJECTED",
			"readback-credential-at-restore-controller":           "RESTORE_ROLE_CREDENTIAL_REJECTED",
			"multi-audience-readback-credential":                  "READBACK_CREDENTIAL_REJECTED",
			"restore-ack-key-for-normal-readback":                 "ATTESTATION_ROLE_BINDING_REJECTED",
			"missing-readback-network-policy":                     "READBACK_CHALLENGE_UNAVAILABLE",
			"co-delivered-readback-manifest-signer":               "READBACK_CREDENTIAL_REJECTED",
			"readback-manifest-root-fingerprint-mismatch":         "READBACK_CREDENTIAL_REJECTED",
			"missing-root-public-verification-material":           "READBACK_CREDENTIAL_REJECTED",
			"same-channel-root-public-key-substitution":           "READBACK_CREDENTIAL_REJECTED",
			"root-kid-public-key-mismatch":                        "READBACK_CREDENTIAL_REJECTED",
			"root-rotation-missing-current-cross-signature":       "READBACK_CREDENTIAL_REJECTED",
			"root-rotation-missing-next-proof-of-possession":      "READBACK_CREDENTIAL_REJECTED",
			"root-rotation-rollback-or-gap":                       "SNAPSHOT_ROLLBACK",
			"readback-manifest-bundle-same-revision-mutation":     "SNAPSHOT_MUTATION",
			"readback-manifest-bundle-rollback-or-gap":            "SNAPSHOT_ROLLBACK",
			"readback-manifest-root-expired":                      "READBACK_CREDENTIAL_REJECTED",
			"restore-trust-as-readback-manifest-root":             "READBACK_CREDENTIAL_REJECTED",
			"missing-attestor-database-credential-delivery":       "DATABASE_LOGIN_PRINCIPAL_REQUIRED",
			"missing-attestor-vault-or-postgresql-network-policy": "READBACK_CHALLENGE_UNAVAILABLE",
			"retired-open-attestor-database-session":              "DATABASE_CREDENTIAL_GENERATION_REJECTED",
			"retired-open-publisher-database-session":             "DATABASE_CREDENTIAL_GENERATION_REJECTED",
		},
		"restore-negative.json": {
			"lower-anchor-revision":        "RESTORE_ANCHOR_REJECTED",
			"same-revision-mutation":       "RESTORE_ANCHOR_REJECTED",
			"missing-quiescence-ack":       "RESTORE_BARRIER_INCOMPLETE",
			"stale-workload-generation":    "RESTORE_BARRIER_INCOMPLETE",
			"controller-signature-failure": "RESTORE_ANCHOR_REJECTED",
			"controller-unavailable":       "RESTORE_CONTROLLER_UNAVAILABLE",
			"restore-without-prepared":     "RESTORE_BARRIER_INCOMPLETE",
			"missing-role-bound-directive": "RESTORE_DIRECTIVE_REJECTED",
			"replayed-quiescence-ack":      "RESTORE_ACK_REPLAY_DETECTED",
		},
	}
	for name, expected := range fixtures {
		name, expected := name, expected
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, "contracts/authorization/v1/fixtures", name)
			var fixture struct {
				Version int `json:"v"`
				Cases   []struct {
					Name     string `json:"name"`
					Mutation string `json:"mutation"`
					Reason   string `json:"reason"`
				} `json:"cases"`
			}
			decodeJSONStrict(t, path, &fixture)
			if fixture.Version != 1 || len(fixture.Cases) != len(expected) {
				t.Fatalf("unexpected fixture cardinality: %+v", fixture)
			}
			for _, item := range fixture.Cases {
				want, ok := expected[item.Name]
				if !ok || want != item.Reason || item.Mutation == "" {
					t.Fatalf("unexpected negative fixture: %+v", item)
				}
			}
		})
	}
}

func TestOperationBindingsAndKeyDeliveryAreOneToOne(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(
		root,
		"contracts/authorization/v1/fixtures/operation-binding.json",
	)
	var fixture operationBindingFixture
	decodeJSONStrict(t, path, &fixture)
	if err := validateOperationBindingFixture(fixture); err != nil {
		t.Fatalf("valid operation binding fixture rejected: %v", err)
	}

	mutations := map[string]func(operationBindingFixture) operationBindingFixture{
		"duplicate-operation-id": func(mutated operationBindingFixture) operationBindingFixture {
			duplicate := mutated.OperationBindings[0]
			duplicate.FullMethod = "/controlplane.v1.ProjectService/ListProjects"
			mutated.OperationBindings = append(mutated.OperationBindings, duplicate)
			return mutated
		},
		"ambiguous-full-method": func(mutated operationBindingFixture) operationBindingFixture {
			duplicate := mutated.OperationBindings[0]
			duplicate.OperationID = "control.project.list"
			duplicate.Permission = "control.project.list"
			mutated.OperationBindings = append(mutated.OperationBindings, duplicate)
			return mutated
		},
		"missing-target-verifier": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets = append(
				mutated.KeyDeliveryTargets[:1],
				mutated.KeyDeliveryTargets[2:]...,
			)
			return mutated
		},
		"missing-target-readback": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets[1].Readback = readback{}
			return mutated
		},
		"missing-proof-resolver": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets = mutated.KeyDeliveryTargets[:2]
			return mutated
		},
		"extra-opposite-role": func(mutated operationBindingFixture) operationBindingFixture {
			extra := mutated.KeyDeliveryTargets[1]
			extra.Role = "AUTHORIZATION_ISSUER"
			extra.Readback.ExpectedRole = extra.Role
			extra.Readback.ReadbackID = "control-plane-extra-issuer"
			extra.DatabaseIdentity.LoginPrincipal = "ira_control_plane_issuer_g1"
			mutated.KeyDeliveryTargets = append(mutated.KeyDeliveryTargets, extra)
			return mutated
		},
		"duplicate-ambiguous-target": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets = append(mutated.KeyDeliveryTargets, mutated.KeyDeliveryTargets[1])
			return mutated
		},
		"duplicate-spiffe-opposite-role-credential": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets[2].RestoreCoordination.RoleCredentialID =
				mutated.KeyDeliveryTargets[1].RestoreCoordination.RoleCredentialID
			return mutated
		},
		"duplicate-normal-readback-credential": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets[2].Readback.CredentialID =
				mutated.KeyDeliveryTargets[1].Readback.CredentialID
			return mutated
		},
		"restore-credential-reused-for-normal-readback": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets[0].Readback.CredentialID =
				mutated.KeyDeliveryTargets[0].RestoreCoordination.RoleCredentialID
			return mutated
		},
		"missing-normal-readback-network-policy": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets[0].Readback.NetworkPolicy = ""
			return mutated
		},
		"wildcard-vault-path": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets[0].AuthPrivateKey.VaultPath += "/*"
			return mutated
		},
		"unknown-proof-producer": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.OperationBindings[0].AuthorityProofProducerID = "missing.producer"
			return mutated
		},
		"producer-operation-gap": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.AuthorityProofProducers[0].AllowedOperationIDs = []string{"control.project.list"}
			return mutated
		},
	}
	for name, mutate := range mutations {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			if err := validateOperationBindingFixture(mutate(cloneOperationBindingFixture(t, fixture))); err == nil {
				t.Fatal("mutation was accepted")
			}
		})
	}
}

func TestBufRemotePluginRevisionsAreExact(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "buf.gen.yaml")
	var config struct {
		Version string `json:"version"`
		Plugins []struct {
			Remote   string   `json:"remote"`
			Revision int      `json:"revision"`
			Out      string   `json:"out"`
			Opt      []string `json:"opt"`
		} `json:"plugins"`
		Inputs []struct {
			Directory string `json:"directory"`
		} `json:"inputs"`
	}
	data := []byte(readFile(t, path))
	if err := yaml.UnmarshalStrict(data, &config); err != nil {
		t.Fatalf("strictly decode buf.gen.yaml: %v", err)
	}
	expected := map[string]int{
		"buf.build/protocolbuffers/go:v1.36.11": 1,
		"buf.build/grpc/go:v1.6.2":              1,
	}
	if config.Version != "v2" || len(config.Plugins) != len(expected) {
		t.Fatalf("unexpected Buf plugin contract: %+v", config)
	}
	for _, plugin := range config.Plugins {
		revision, ok := expected[plugin.Remote]
		if !ok || plugin.Revision != revision {
			t.Fatalf("remote plugin is not exactly pinned: %+v", plugin)
		}
		delete(expected, plugin.Remote)
	}
	if len(expected) != 0 {
		t.Fatalf("missing exact Buf plugins: %+v", expected)
	}
}

func TestApprovedContractCoversFailureBoundary(t *testing.T) {
	root := repositoryRoot(t)
	document := readFile(t, filepath.Join(root, "contracts/authorization/README.md"))
	for _, want := range []string{
		"status: approved",
		"SO_PEERCRED",
		"REPLAY_DETECTED",
		"SNAPSHOT_ROLLBACK",
		"SNAPSHOT_MUTATION",
		"SNAPSHOT_HISTORY_GAP",
		"MTLS_REQUIRED",
		"AUTHORIZATION_CONTEXT_REQUIRED",
		"AUTHORITY_PROOF_REQUIRED",
		"AUTHORITY_PROOF_REPLAY_DETECTED",
		"AUTHORITY_SCOPE_MISMATCH",
		"AuthorityProofResolverService",
		"internal-rpc-authority-restore-controller",
		"ValidatingAdmissionPolicy",
		"session_user",
		"SECURITY DEFINER",
		"READBACK_MISMATCH",
		"authority_key_delivery_readbacks",
		"внешний монотонный restore evidence anchor",
		"Context7",
		"Monthly quota exceeded",
	} {
		if !strings.Contains(document, want) {
			t.Fatalf("approved contract does not cover %q", want)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func decodeJSONStrict(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("strictly decode %s: %v", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("%s contains more than one JSON value or trailing data: %v", path, err)
	}
}

func decodeYAMLStrict(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := yaml.UnmarshalStrict(data, target); err != nil {
		t.Fatalf("strictly decode %s: %v", path, err)
	}
}

type operationBindingFixture struct {
	Version                 int                      `json:"v"`
	AuthorityProofProducers []authorityProofProducer `json:"authority_proof_producers"`
	OperationBindings       []operationBinding       `json:"operation_bindings"`
	KeyDeliveryTargets      []keyDeliveryTarget      `json:"key_delivery_targets"`
}

type authorityProofProducer struct {
	ProducerID                    string   `json:"producer_id"`
	CallerWorkloadID              string   `json:"caller_workload_id"`
	CallerSPIFFEID                string   `json:"caller_spiffe_id"`
	OwnerWorkloadID               string   `json:"owner_workload_id"`
	OwnerSPIFFEID                 string   `json:"owner_spiffe_id"`
	FullMethod                    string   `json:"full_method"`
	TLSServerName                 string   `json:"tls_server_name"`
	TransportTrustBundleID        string   `json:"transport_trust_bundle_id"`
	ApplicationCredential         string   `json:"application_credential"`
	ApplicationCredentialMetadata string   `json:"application_credential_metadata"`
	ApplicationCredentialIssuer   string   `json:"application_credential_issuer"`
	ApplicationCredentialAudience string   `json:"application_credential_audience"`
	ApplicationCredentialTrustID  string   `json:"application_credential_trust_bundle_id"`
	AuthorityProofIssuer          string   `json:"authority_proof_issuer"`
	AuthorityProofAudience        string   `json:"authority_proof_audience"`
	AuthorityProofTrustBundleID   string   `json:"authority_proof_trust_bundle_id"`
	AuthorityProofMaxAgeSeconds   int      `json:"authority_proof_max_age_seconds"`
	DeadlineMilliseconds          int      `json:"deadline_milliseconds"`
	MaxAttempts                   int      `json:"max_attempts"`
	RetryableGRPCCodes            []string `json:"retryable_grpc_codes"`
	IdempotencyScope              string   `json:"idempotency_scope"`
	AuthoritySources              []string `json:"authority_sources"`
	AllowedOperationIDs           []string `json:"allowed_operation_ids"`
	ServerResolvedFields          []string `json:"server_resolved_fields"`
}

type operationBinding struct {
	OperationID              string    `json:"operation_id"`
	CallerWorkloadID         string    `json:"caller_workload_id"`
	CallerSPIFFEID           string    `json:"caller_spiffe_id"`
	Issuer                   string    `json:"issuer"`
	TargetWorkloadID         string    `json:"target_workload_id"`
	TargetSPIFFEID           string    `json:"target_spiffe_id"`
	Audience                 string    `json:"audience"`
	FullMethod               string    `json:"full_method"`
	TargetTLSServerName      string    `json:"target_tls_server_name"`
	TargetTrustBundleID      string    `json:"target_trust_bundle_id"`
	Permission               string    `json:"permission"`
	AuthorityProofProducerID string    `json:"authority_proof_producer_id"`
	AuthoritySources         []string  `json:"authority_sources"`
	ProjectRequired          bool      `json:"project_required"`
	LocalCaller              localPeer `json:"local_caller"`
	LocalTarget              localPeer `json:"local_target"`
}

type localPeer struct {
	UID         int `json:"uid"`
	PrimaryGID  int `json:"primary_gid"`
	SharedFSGID int `json:"shared_fs_gid"`
}

type keyDeliveryTarget struct {
	WorkloadID               string              `json:"workload_id"`
	Role                     string              `json:"role"`
	SPIFFEID                 string              `json:"spiffe_id"`
	Namespace                string              `json:"namespace"`
	ServiceAccount           string              `json:"service_account"`
	WorkloadGeneration       int                 `json:"workload_generation"`
	AuthoritySnapshot        projection          `json:"authority_snapshot"`
	AuthPrivateKey           *delivery           `json:"auth_private_key,omitempty"`
	AuthorityProofPrivateKey *delivery           `json:"authority_proof_private_key,omitempty"`
	ManifestTrust            delivery            `json:"manifest_trust"`
	AuthorityProofTrust      *delivery           `json:"authority_proof_trust,omitempty"`
	DatabaseIdentity         databaseIdentity    `json:"database_identity"`
	RestoreCoordination      restoreCoordination `json:"restore_coordination"`
	Readback                 readback            `json:"readback"`
}

type delivery struct {
	VaultPath  string `json:"vault_path"`
	SecretName string `json:"secret_name"`
	MountPath  string `json:"mount_path"`
}

type projection struct {
	SecretName string `json:"secret_name"`
	MountPath  string `json:"mount_path"`
}

type databaseIdentity struct {
	LoginPrincipal       string `json:"login_principal"`
	VaultDatabaseRole    string `json:"vault_database_role"`
	DSNMountPath         string `json:"dsn_mount_path"`
	CredentialGeneration int    `json:"credential_generation"`
}

type restoreCoordination struct {
	RoleCredentialID        string `json:"role_credential_id"`
	RoleCredentialVaultPath string `json:"role_credential_vault_path"`
	RoleCredentialMountPath string `json:"role_credential_mount_path"`
	AckKeyID                string `json:"ack_key_id"`
	AckKeyGeneration        int    `json:"ack_key_generation"`
	AckKeyVaultPath         string `json:"ack_key_vault_path"`
	AckKeyMountPath         string `json:"ack_key_mount_path"`
	AckPublicJWKSource      string `json:"ack_public_jwk_source"`
	ControllerAddress       string `json:"controller_address"`
	ControllerTLSServerName string `json:"controller_tls_server_name"`
	ControllerTrustBundleID string `json:"controller_trust_bundle_id"`
	ControllerCAMountPath   string `json:"controller_ca_mount_path"`
	ControllerAudience      string `json:"controller_audience"`
	ControllerFullMethod    string `json:"controller_full_method"`
	NetworkPolicy           string `json:"network_policy"`
}

type readback struct {
	ReadbackID                string `json:"readback_id"`
	CredentialID              string `json:"credential_id"`
	CredentialVaultPath       string `json:"credential_vault_path"`
	CredentialMountPath       string `json:"credential_mount_path"`
	CredentialProtectedType   string `json:"credential_protected_type"`
	CredentialSchema          string `json:"credential_schema"`
	PossessionKeyID           string `json:"possession_key_id"`
	PossessionKeyGeneration   int    `json:"possession_key_generation"`
	PossessionKeyVaultPath    string `json:"possession_key_vault_path"`
	PossessionKeyMountPath    string `json:"possession_key_mount_path"`
	PossessionPublicJWKSource string `json:"possession_public_jwk_source"`
	DatabaseFunction          string `json:"database_function"`
	SnapshotDatabaseFunction  string `json:"snapshot_database_function"`
	AttestorAddress           string `json:"attestor_address"`
	AttestorTLSServerName     string `json:"attestor_tls_server_name"`
	AttestorTrustBundleID     string `json:"attestor_trust_bundle_id"`
	AttestorCAMountPath       string `json:"attestor_ca_mount_path"`
	AttestorAudience          string `json:"attestor_audience"`
	AttestorChallengeMethod   string `json:"attestor_challenge_full_method"`
	AttestorFullMethod        string `json:"attestor_full_method"`
	ExpectedRole              string `json:"expected_role"`
	NetworkPolicy             string `json:"network_policy"`
}

func validateOperationBindingFixture(fixture operationBindingFixture) error {
	if fixture.Version != 1 ||
		len(fixture.AuthorityProofProducers) == 0 ||
		len(fixture.OperationBindings) == 0 {
		return fmt.Errorf("invalid fixture version, producers or bindings")
	}

	producers := make(map[string]authorityProofProducer, len(fixture.AuthorityProofProducers))
	producerOperations := make(map[string]map[string]bool, len(fixture.AuthorityProofProducers))
	boundProducerOperations := make(map[string]map[string]bool, len(fixture.AuthorityProofProducers))
	requiredRoles := make(map[string]string)
	for _, producer := range fixture.AuthorityProofProducers {
		if producer.ProducerID == "" || producers[producer.ProducerID].ProducerID != "" {
			return fmt.Errorf("duplicate or empty authority proof producer")
		}
		if producer.CallerWorkloadID == "" ||
			producer.CallerSPIFFEID == "" ||
			producer.OwnerWorkloadID == "" ||
			producer.OwnerSPIFFEID == "" ||
			producer.FullMethod != "/internalrpcauthority.v1.AuthorityProofResolverService/ResolveAuthorityProof" ||
			producer.ApplicationCredentialMetadata != "authorization" ||
			!strings.HasPrefix(producer.ApplicationCredentialIssuer, "https://") ||
			producer.ApplicationCredentialAudience == "" ||
			producer.ApplicationCredentialTrustID == "" ||
			producer.AuthorityProofIssuer != producer.OwnerSPIFFEID ||
			producer.AuthorityProofMaxAgeSeconds != 15 ||
			producer.DeadlineMilliseconds != 2000 ||
			producer.MaxAttempts != 2 ||
			strings.Join(producer.RetryableGRPCCodes, ",") != "UNAVAILABLE,DEADLINE_EXCEEDED" ||
			len(producer.AuthoritySources) == 0 ||
			len(producer.AllowedOperationIDs) == 0 ||
			strings.Join(producer.ServerResolvedFields, ",") !=
				"actor,tenant,project,ownership,provenance" {
			return fmt.Errorf("incomplete authority proof producer")
		}
		producers[producer.ProducerID] = producer
		producerOperations[producer.ProducerID] = make(map[string]bool, len(producer.AllowedOperationIDs))
		for _, operationID := range producer.AllowedOperationIDs {
			if operationID == "" || producerOperations[producer.ProducerID][operationID] {
				return fmt.Errorf("duplicate or empty producer operation")
			}
			producerOperations[producer.ProducerID][operationID] = true
		}
		boundProducerOperations[producer.ProducerID] = make(map[string]bool)
		if err := addRequiredRole(
			requiredRoles,
			producer.OwnerWorkloadID,
			"AUTHORITY_PROOF_RESOLVER",
			producer.OwnerSPIFFEID,
		); err != nil {
			return err
		}
	}

	operationIDs := make(map[string]bool, len(fixture.OperationBindings))
	methodPermissions := make(map[string]string, len(fixture.OperationBindings))
	for _, binding := range fixture.OperationBindings {
		if binding.OperationID == "" || operationIDs[binding.OperationID] {
			return fmt.Errorf("duplicate or empty operation_id")
		}
		operationIDs[binding.OperationID] = true
		if permission, exists := methodPermissions[binding.FullMethod]; exists && permission != binding.Permission {
			return fmt.Errorf("ambiguous permission for full method")
		}
		methodPermissions[binding.FullMethod] = binding.Permission
		producer, exists := producers[binding.AuthorityProofProducerID]
		if !exists ||
			producer.CallerWorkloadID != binding.CallerWorkloadID ||
			producer.CallerSPIFFEID != binding.CallerSPIFFEID ||
			!containsString(producer.AllowedOperationIDs, binding.OperationID) ||
			strings.Join(producer.AuthoritySources, ",") != strings.Join(binding.AuthoritySources, ",") ||
			binding.LocalCaller != (localPeer{UID: 10001, PrimaryGID: 10001, SharedFSGID: 29000}) ||
			binding.LocalTarget != (localPeer{UID: 10001, PrimaryGID: 10001, SharedFSGID: 29000}) {
			return fmt.Errorf("incomplete authority proof binding")
		}
		if err := addRequiredRole(
			requiredRoles,
			binding.CallerWorkloadID,
			"AUTHORIZATION_ISSUER",
			binding.CallerSPIFFEID,
		); err != nil {
			return err
		}
		if err := addRequiredRole(
			requiredRoles,
			binding.TargetWorkloadID,
			"AUTHORIZATION_VERIFIER",
			binding.TargetSPIFFEID,
		); err != nil {
			return err
		}
		boundProducerOperations[binding.AuthorityProofProducerID][binding.OperationID] = true
	}
	for producerID, allowed := range producerOperations {
		bound := boundProducerOperations[producerID]
		if len(allowed) != len(bound) {
			return fmt.Errorf("unused or unlisted producer operation")
		}
		for operationID := range allowed {
			if !bound[operationID] {
				return fmt.Errorf("producer operation coverage gap")
			}
		}
	}

	targets := make(map[string]keyDeliveryTarget, len(fixture.KeyDeliveryTargets))
	vaultPaths := make(map[string]bool, len(fixture.KeyDeliveryTargets)*4)
	databasePrincipals := make(map[string]bool, len(fixture.KeyDeliveryTargets))
	restoreCredentialIDs := make(map[string]bool, len(fixture.KeyDeliveryTargets))
	restoreAckKeyIDs := make(map[string]bool, len(fixture.KeyDeliveryTargets))
	readbackCredentialIDs := make(map[string]bool, len(fixture.KeyDeliveryTargets))
	readbackPossessionKeyIDs := make(map[string]bool, len(fixture.KeyDeliveryTargets))
	for _, target := range fixture.KeyDeliveryTargets {
		key := target.WorkloadID + "|" + target.Role
		if target.WorkloadID == "" || target.Role == "" || target.WorkloadGeneration < 1 {
			return fmt.Errorf("empty key delivery workload role")
		}
		if _, exists := targets[key]; exists {
			return fmt.Errorf("duplicate workload role target")
		}
		items := []*delivery{&target.ManifestTrust}
		for _, item := range []*delivery{
			target.AuthPrivateKey,
			target.AuthorityProofPrivateKey,
			target.AuthorityProofTrust,
		} {
			if item != nil {
				items = append(items, item)
			}
		}
		for _, item := range items {
			if item.VaultPath == "" ||
				!strings.HasPrefix(item.VaultPath, "kv/data/mattercodex/") ||
				strings.ContainsAny(item.VaultPath, "*?[]") ||
				strings.Contains(item.VaultPath, "..") ||
				vaultPaths[item.VaultPath] ||
				item.SecretName == "" ||
				!strings.HasPrefix(item.MountPath, "/") {
				return fmt.Errorf("non-exact or duplicate key delivery resource")
			}
			vaultPaths[item.VaultPath] = true
		}
		if target.Readback.ReadbackID == "" ||
			target.Readback.CredentialID == "" ||
			readbackCredentialIDs[target.Readback.CredentialID] ||
			restoreCredentialIDs[target.Readback.CredentialID] ||
			target.Readback.PossessionKeyID == "" ||
			readbackPossessionKeyIDs[target.Readback.PossessionKeyID] ||
			restoreAckKeyIDs[target.Readback.PossessionKeyID] ||
			target.Readback.CredentialID == target.Readback.PossessionKeyID ||
			!strings.HasPrefix(target.Readback.CredentialVaultPath, "kv/data/mattercodex/") ||
			!strings.HasPrefix(target.Readback.PossessionKeyVaultPath, "kv/data/mattercodex/") ||
			vaultPaths[target.Readback.CredentialVaultPath] ||
			vaultPaths[target.Readback.PossessionKeyVaultPath] ||
			!strings.HasPrefix(target.Readback.CredentialMountPath, "/") ||
			!strings.HasPrefix(target.Readback.PossessionKeyMountPath, "/") ||
			target.Readback.CredentialProtectedType != "mattercodex-internal-rpc-readback-credential+jws" ||
			target.Readback.CredentialSchema != "contracts/authorization/v1/readback-credential.schema.json" ||
			target.Readback.PossessionKeyGeneration < 1 ||
			target.Readback.PossessionPublicJWKSource != "SIGNED_NORMAL_READBACK_CREDENTIAL" ||
			target.Readback.DatabaseFunction != "internal_rpc_authority.record_authority_key_delivery_readback(uuid)" ||
			target.Readback.SnapshotDatabaseFunction != "internal_rpc_authority.record_authority_snapshot_readback(uuid)" ||
			target.Readback.AttestorAddress != "internal-rpc-authority-readback-attestor.mattercodex-system.svc:8443" ||
			target.Readback.AttestorTLSServerName != "internal-rpc-authority-readback-attestor.mattercodex-system.svc" ||
			target.Readback.AttestorTrustBundleID != "internal-rpc-authority-readback-attestor-ca" ||
			!strings.HasPrefix(target.Readback.AttestorCAMountPath, "/") ||
			target.Readback.AttestorAudience != "urn:mattercodex:internal-rpc-authority-readback-attestor" ||
			target.Readback.AttestorChallengeMethod != "/internalrpcauthority.v1.AuthorityReadbackAttestorService/IssueAttestationChallenge" ||
			target.Readback.AttestorFullMethod != "/internalrpcauthority.v1.AuthorityReadbackAttestorService/AttestServedState" ||
			target.Readback.NetworkPolicy == "" ||
			target.Readback.ExpectedRole != target.Role ||
			target.DatabaseIdentity.LoginPrincipal == "" ||
			databasePrincipals[target.DatabaseIdentity.LoginPrincipal] ||
			target.DatabaseIdentity.CredentialGeneration < 1 {
			return fmt.Errorf("invalid workload role database readback identity")
		}
		readbackCredentialIDs[target.Readback.CredentialID] = true
		readbackPossessionKeyIDs[target.Readback.PossessionKeyID] = true
		vaultPaths[target.Readback.CredentialVaultPath] = true
		vaultPaths[target.Readback.PossessionKeyVaultPath] = true
		coordination := target.RestoreCoordination
		if coordination.RoleCredentialID == "" ||
			coordination.AckKeyID == "" ||
			coordination.RoleCredentialID == coordination.AckKeyID ||
			coordination.RoleCredentialID == target.Readback.CredentialID ||
			coordination.AckKeyID == target.Readback.PossessionKeyID ||
			coordination.RoleCredentialVaultPath == target.Readback.CredentialVaultPath ||
			coordination.AckKeyVaultPath == target.Readback.PossessionKeyVaultPath ||
			restoreCredentialIDs[coordination.RoleCredentialID] ||
			restoreAckKeyIDs[coordination.AckKeyID] ||
			readbackCredentialIDs[coordination.RoleCredentialID] ||
			readbackPossessionKeyIDs[coordination.AckKeyID] ||
			!strings.HasPrefix(coordination.RoleCredentialVaultPath, "kv/data/mattercodex/") ||
			!strings.HasPrefix(coordination.AckKeyVaultPath, "kv/data/mattercodex/") ||
			!strings.HasPrefix(coordination.RoleCredentialMountPath, "/") ||
			!strings.HasPrefix(coordination.AckKeyMountPath, "/") ||
			coordination.AckKeyGeneration < 1 ||
			coordination.AckPublicJWKSource != "SIGNED_ROLE_CREDENTIAL" ||
			coordination.ControllerAddress != "internal-rpc-authority-restore-controller.mattercodex-system.svc:8443" ||
			coordination.ControllerTLSServerName != "internal-rpc-authority-restore-controller.mattercodex-system.svc" ||
			coordination.ControllerTrustBundleID != "internal-rpc-authority-restore-controller-ca" ||
			!strings.HasPrefix(coordination.ControllerCAMountPath, "/") ||
			coordination.ControllerAudience != "urn:mattercodex:internal-rpc-authority-restore-controller" ||
			coordination.ControllerFullMethod != "/internalrpcauthority.v1.RestoreControllerService/GetRestoreDirective" ||
			coordination.NetworkPolicy == "" {
			return fmt.Errorf("invalid workload role restore coordination identity")
		}
		restoreCredentialIDs[coordination.RoleCredentialID] = true
		restoreAckKeyIDs[coordination.AckKeyID] = true
		if target.AuthoritySnapshot.SecretName != "internal-rpc-authority-snapshot" ||
			!strings.HasPrefix(target.AuthoritySnapshot.MountPath, "/") {
			return fmt.Errorf("invalid authority snapshot projection")
		}
		databasePrincipals[target.DatabaseIdentity.LoginPrincipal] = true
		switch target.Role {
		case "AUTHORIZATION_ISSUER":
			if target.AuthPrivateKey == nil ||
				target.AuthorityProofTrust == nil ||
				target.AuthorityProofPrivateKey != nil {
				return fmt.Errorf("invalid issuer role material")
			}
		case "AUTHORIZATION_VERIFIER":
			if target.AuthPrivateKey != nil ||
				target.AuthorityProofPrivateKey != nil ||
				target.AuthorityProofTrust != nil {
				return fmt.Errorf("invalid verifier role material")
			}
		case "AUTHORITY_PROOF_RESOLVER":
			if target.AuthPrivateKey != nil ||
				target.AuthorityProofPrivateKey == nil ||
				target.AuthorityProofTrust == nil {
				return fmt.Errorf("invalid resolver role material")
			}
		default:
			return fmt.Errorf("unknown target role")
		}
		wantSPIFFEID, required := requiredRoles[key]
		if !required || wantSPIFFEID != target.SPIFFEID {
			return fmt.Errorf("extra or mismatched workload role target")
		}
		targets[key] = target
		delete(requiredRoles, key)
	}
	if len(requiredRoles) != 0 {
		return fmt.Errorf("missing workload role target")
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func addRequiredRole(required map[string]string, workloadID, role, spiffeID string) error {
	key := workloadID + "|" + role
	if existing, ok := required[key]; ok && existing != spiffeID {
		return fmt.Errorf("ambiguous workload role identity")
	}
	required[key] = spiffeID
	return nil
}

func cloneOperationBindingFixture(t *testing.T, fixture operationBindingFixture) operationBindingFixture {
	t.Helper()
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal operation binding fixture: %v", err)
	}
	var cloned operationBindingFixture
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatalf("clone operation binding fixture: %v", err)
	}
	return cloned
}

func canonicalProtectedHeader(t *testing.T, raw string) string {
	t.Helper()
	var header struct {
		Alg  string   `json:"alg"`
		Crit []string `json:"crit"`
		Kid  string   `json:"kid"`
		MCXV int      `json:"mcxv"`
		Typ  string   `json:"typ"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&header); err != nil {
		t.Fatalf("decode protected header: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("protected header contains trailing data: %v", err)
	}
	if header.Alg != "ES256" ||
		len(header.Crit) != 1 ||
		header.Crit[0] != "mcxv" ||
		header.Kid == "" ||
		header.MCXV != 1 ||
		header.Typ == "" {
		t.Fatalf("invalid protected header semantic values: %+v", header)
	}
	return fmt.Sprintf(
		`{"alg":%s,"crit":[%s],"kid":%s,"mcxv":1,"typ":%s}`,
		marshalJSONString(t, header.Alg),
		marshalJSONString(t, header.Crit[0]),
		marshalJSONString(t, header.Kid),
		marshalJSONString(t, header.Typ),
	)
}

func marshalJSONString(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON string: %v", err)
	}
	return string(data)
}

func stringSet(values []any) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, raw := range values {
		if value, ok := raw.(string); ok {
			result[value] = true
		}
	}
	return result
}

func requiredMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, exists := parent[key]
	if !exists {
		t.Fatalf("required object %s is missing", key)
	}
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object: %T", key, value)
	}
	return result
}

func requiredString(t *testing.T, parent map[string]any, key string) string {
	t.Helper()
	value, exists := parent[key]
	if !exists {
		t.Fatalf("required string %s is missing", key)
	}
	result, ok := value.(string)
	if !ok {
		t.Fatalf("%s is not a string: %T", key, value)
	}
	return result
}

func requireEqual(t *testing.T, parent map[string]any, key string, want any) {
	t.Helper()
	got, exists := parent[key]
	if !exists || got != want {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
	}
}

func requireStringSliceEqual(t *testing.T, parent map[string]any, key string, want []string) {
	t.Helper()
	raw, exists := parent[key]
	if !exists {
		t.Fatalf("required list %s is missing", key)
	}
	values, ok := raw.([]any)
	if !ok || len(values) != len(want) {
		t.Fatalf("%s = %#v, want %#v", key, raw, want)
	}
	for index, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok || value != want[index] {
			t.Fatalf("%s[%d] = %#v, want %q", key, index, rawValue, want[index])
		}
	}
}
