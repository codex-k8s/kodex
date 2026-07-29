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
		"restore-fence-evidence.schema.json",
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
		"mattercodex-internal-rpc-restore-evidence+jws",
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
	for _, field := range []string{
		"authority_proof_issuer",
		"authority_proof_audience",
		"authority_proof_trust_bundle_id",
		"authority_proof_max_age_seconds",
	} {
		if !required[field] {
			t.Fatalf("operation binding does not require %s", field)
		}
	}
	bindingProperties := operationBinding["properties"].(map[string]any)
	maxAge := bindingProperties["authority_proof_max_age_seconds"].(map[string]any)
	if maxAge["const"] != float64(15) {
		t.Fatalf("authority proof max age = %v, want 15", maxAge["const"])
	}
}

func TestBootstrapPolicyIsDenyAll(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "contracts/authorization/v1/bootstrap-deny-all-policy.json")
	var policy struct {
		TrustDomain             string `json:"trust_domain"`
		DefaultDecision         string `json:"default_decision"`
		TokenTTLSeconds         int    `json:"token_ttl_seconds"`
		AllowedClockSkewSeconds int    `json:"allowed_clock_skew_seconds"`
		MaxCompactJWSBytes      int    `json:"max_compact_jws_bytes"`
		OperationBindings       []any  `json:"operation_bindings"`
	}
	decodeJSONStrict(t, path, &policy)

	if policy.TrustDomain != "mattercodex.local" ||
		policy.DefaultDecision != "DENY" ||
		policy.TokenTTLSeconds != 30 ||
		policy.AllowedClockSkewSeconds != 5 ||
		policy.MaxCompactJWSBytes != 8192 ||
		len(policy.OperationBindings) != 0 {
		t.Fatalf("bootstrap policy is not the exact deny-all contract: %+v", policy)
	}
}

func TestBootstrapKeyDeliveryTargetsAreEmpty(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(
		root,
		"contracts/authorization/v1/bootstrap-key-delivery-targets.json",
	)
	var targets struct {
		Version int   `json:"v"`
		Targets []any `json:"targets"`
	}
	decodeJSONStrict(t, path, &targets)
	if targets.Version != 1 || len(targets.Targets) != 0 {
		t.Fatalf("bootstrap key delivery targets are not deny-all: %+v", targets)
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
	requireEqual(t, issuer, "snapshotDatabaseRole", "internal_rpc_authority_issuer")
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

	publisher := requiredMap(t, deployables, "publisher")
	rotation := requiredMap(t, publisher, "rotationOwnership")
	requireEqual(t, rotation, "authKeyGenerationOwner", "internal-rpc-authority-publisher")
	requireEqual(t, rotation, "authPrivateKeyWriteOwner", "internal-rpc-authority-publisher")
	requireEqual(t, rotation, "manifestTrustOverlapWriteOwner", "internal-rpc-authority-publisher")
	requireEqual(t, rotation, "compareAndSwapRequired", true)
	requireEqual(t, rotation, "perWorkloadFanOutRequired", true)
	requireEqual(t, rotation, "wildcardVaultPathsAllowed", false)
	requireStringSliceEqual(t, rotation, "sequence", []string{
		"persist-rotation-intent",
		"generate-key-or-certificate",
		"vault-cas-deliver-exact-targets",
		"issuer-verifier-cryptographic-readback",
		"publish-snapshot-cas",
		"publisher-cryptographic-readback",
		"promote-after-overlap-window",
	})

	recoveryName := "internal-rpc-authority-recovery-job"
	recovery := requiredMap(t, deployables, recoveryName)
	for key, want := range map[string]any{
		"kind":           "Job",
		"artifactBinary": "/usr/local/bin/internal-rpc-authority-recovery",
		"serviceAccount": "internal-rpc-authority-recovery",
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
		"consuming-workloads-readiness-false-and-quiesced",
		"signed-external-anchor-prepared",
		"database-restore",
		"signed-external-anchor-completed",
		"recovery-job-validates-and-commits-fence",
		"database-safe-window-elapses",
		"issuer-verifier-external-anchor-readback",
		"application-readiness",
	})

	anchor := requiredMap(t, deployables, "restore-evidence-anchor")
	requireEqual(t, anchor, "outsideRestoredPostgreSQL", true)
	requireEqual(t, anchor, "updateProtocol", "RESOURCE_VERSION_CAS")
	requireEqual(t, anchor, "failurePolicy", "FAIL_CLOSED")

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
		"internal-rpc-authority-proof-v1":            "contracts/authorization/v1/authority-proof.schema.json",
		"internal-rpc-authority-restore-evidence-v1": "contracts/authorization/v1/restore-fence-evidence.schema.json",
		"internal-rpc-authority-key-delivery-v1":     "contracts/authorization/v1/key-delivery-targets.schema.json",
		"internal-rpc-authority-error-matrix-v1":     "contracts/authorization/v1/authorization-error-matrix.json",
	} {
		entry, exists := entries[id]
		if !exists || entry.Source != source || len(entry.Consumers) == 0 {
			t.Fatalf("registry entry %s is missing or inconsistent: %+v", id, entry)
		}
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
		"missing-key-delivery": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets = nil
			return mutated
		},
		"duplicate-key-delivery": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets = append(mutated.KeyDeliveryTargets, mutated.KeyDeliveryTargets[0])
			return mutated
		},
		"wildcard-vault-path": func(mutated operationBindingFixture) operationBindingFixture {
			mutated.KeyDeliveryTargets[0].AuthPrivateKey.VaultPath += "/*"
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

type operationBindingFixture struct {
	Version            int                 `json:"v"`
	OperationBindings  []operationBinding  `json:"operation_bindings"`
	KeyDeliveryTargets []keyDeliveryTarget `json:"key_delivery_targets"`
}

type operationBinding struct {
	OperationID                 string   `json:"operation_id"`
	CallerWorkloadID            string   `json:"caller_workload_id"`
	CallerSPIFFEID              string   `json:"caller_spiffe_id"`
	Issuer                      string   `json:"issuer"`
	TargetWorkloadID            string   `json:"target_workload_id"`
	TargetSPIFFEID              string   `json:"target_spiffe_id"`
	Audience                    string   `json:"audience"`
	FullMethod                  string   `json:"full_method"`
	TargetTLSServerName         string   `json:"target_tls_server_name"`
	TargetTrustBundleID         string   `json:"target_trust_bundle_id"`
	Permission                  string   `json:"permission"`
	AuthorityProofIssuer        string   `json:"authority_proof_issuer"`
	AuthorityProofAudience      string   `json:"authority_proof_audience"`
	AuthorityProofTrustBundleID string   `json:"authority_proof_trust_bundle_id"`
	AuthorityProofMaxAgeSeconds int      `json:"authority_proof_max_age_seconds"`
	AuthoritySources            []string `json:"authority_sources"`
	ProjectRequired             bool     `json:"project_required"`
}

type keyDeliveryTarget struct {
	WorkloadID         string   `json:"workload_id"`
	IssuerSPIFFEID     string   `json:"issuer_spiffe_id"`
	Namespace          string   `json:"namespace"`
	ServiceAccount     string   `json:"service_account"`
	AuthPrivateKey     delivery `json:"auth_private_key"`
	ManifestTrust      delivery `json:"manifest_trust"`
	IssuerReadbackID   string   `json:"issuer_readback_id"`
	VerifierReadbackID string   `json:"verifier_readback_id"`
}

type delivery struct {
	VaultPath  string `json:"vault_path"`
	SecretName string `json:"secret_name"`
	MountPath  string `json:"mount_path"`
}

func validateOperationBindingFixture(fixture operationBindingFixture) error {
	if fixture.Version != 1 || len(fixture.OperationBindings) == 0 {
		return fmt.Errorf("invalid fixture version or empty bindings")
	}

	operationIDs := make(map[string]bool, len(fixture.OperationBindings))
	methodPermissions := make(map[string]string, len(fixture.OperationBindings))
	bindingsPerCaller := make(map[string][]operationBinding)
	for _, binding := range fixture.OperationBindings {
		if binding.OperationID == "" || operationIDs[binding.OperationID] {
			return fmt.Errorf("duplicate or empty operation_id")
		}
		operationIDs[binding.OperationID] = true
		if permission, exists := methodPermissions[binding.FullMethod]; exists && permission != binding.Permission {
			return fmt.Errorf("ambiguous permission for full method")
		}
		methodPermissions[binding.FullMethod] = binding.Permission
		if binding.AuthorityProofIssuer == "" ||
			binding.AuthorityProofAudience == "" ||
			binding.AuthorityProofTrustBundleID == "" ||
			binding.AuthorityProofMaxAgeSeconds != 15 ||
			len(binding.AuthoritySources) == 0 {
			return fmt.Errorf("incomplete authority proof binding")
		}
		bindingsPerCaller[binding.CallerWorkloadID] = append(
			bindingsPerCaller[binding.CallerWorkloadID],
			binding,
		)
	}

	targets := make(map[string]keyDeliveryTarget, len(fixture.KeyDeliveryTargets))
	vaultPaths := make(map[string]bool, len(fixture.KeyDeliveryTargets)*2)
	for _, target := range fixture.KeyDeliveryTargets {
		if target.WorkloadID == "" {
			return fmt.Errorf("empty key delivery workload")
		}
		if _, exists := targets[target.WorkloadID]; exists {
			return fmt.Errorf("duplicate key delivery target")
		}
		for _, item := range []delivery{target.AuthPrivateKey, target.ManifestTrust} {
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
		if target.IssuerReadbackID == "" ||
			target.VerifierReadbackID == "" ||
			target.IssuerReadbackID == target.VerifierReadbackID {
			return fmt.Errorf("invalid cryptographic readback ids")
		}
		targets[target.WorkloadID] = target
	}

	for caller, bindings := range bindingsPerCaller {
		target, exists := targets[caller]
		if !exists {
			return fmt.Errorf("missing key delivery target for caller %s", caller)
		}
		for _, binding := range bindings {
			if target.IssuerSPIFFEID != binding.Issuer {
				return fmt.Errorf("key delivery issuer mismatch for caller %s", caller)
			}
		}
		delete(targets, caller)
	}
	if len(targets) != 0 {
		return fmt.Errorf("unreferenced key delivery targets")
	}
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
