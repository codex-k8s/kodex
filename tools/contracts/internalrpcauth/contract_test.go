package internalrpcauth_test

import (
	"bytes"
	"encoding/json"
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

	issuerIssueMethod = "/internalrpcauthority.v1.AuthorizationIssuerService/IssueAuthorizationContext"
	issuerReadyMethod = "/internalrpcauthority.v1.AuthorizationIssuerService/CheckReadiness"
	verifierMethod    = "/internalrpcauthority.v1.AuthorizationVerifierService/VerifyAuthorizationContext"
	verifierReady     = "/internalrpcauthority.v1.AuthorizationVerifierService/CheckReadiness"
)

func TestProtoBoundary(t *testing.T) {
	root := repositoryRoot(t)
	proto := readFile(t, filepath.Join(root, "contracts/proto/internalrpcauthority/v1/authority.proto"))

	for _, want := range []string{
		"package internalrpcauthority.v1;",
		"service AuthorizationIssuerService",
		"service AuthorizationVerifierService",
		"rpc IssueAuthorizationContext",
		"rpc VerifyAuthorizationContext",
		"message AuthorizationErrorDetail",
	} {
		if !strings.Contains(proto, want) {
			t.Fatalf("Proto contract does not contain %q", want)
		}
	}

	issueBody := messageBody(t, proto, "IssueAuthorizationContextRequest")
	for _, forbidden := range []string{
		"audience",
		"full_method",
		"permission",
		"expires_at",
		"source_revision",
		"key_set_revision",
		"policy_revision",
		"signer_generation",
		"kid",
	} {
		if strings.Contains(issueBody, " "+forbidden+" =") {
			t.Fatalf("issuer request contains server-derived field %q", forbidden)
		}
	}
}

func TestGeneratedFullMethods(t *testing.T) {
	root := repositoryRoot(t)
	generated := readFile(t, filepath.Join(
		root,
		"libs/go/internalrpcauth/gen/internalrpcauthority/v1/authority_grpc.pb.go",
	))

	for _, method := range []string{
		issuerIssueMethod,
		issuerReadyMethod,
		verifierMethod,
		verifierReady,
	} {
		if !strings.Contains(generated, method) {
			t.Fatalf("generated contract does not contain full method %q", method)
		}
	}
}

func TestJSONSchemasAreClosed(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{
		"jws-protected-header.schema.json",
		"authorization-context.schema.json",
		"authority-snapshot.schema.json",
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

	document := string(data)
	for _, want := range []string{
		issuerSocket,
		verifierSocket,
		issuerReadyMethod,
		verifierReady,
		"applicationUid: 10001",
		"issuerUid: 29001",
		"verifierUid: 29002",
		"sharedFsGid: 29000",
		`mode: "1770"`,
		"sticky: true",
		`mode: "0660"`,
		"mechanism: LINUX_SO_PEERCRED",
		"exactServerName: REQUIRED_PER_OPERATION_BINDING",
		"exactTrustBundleId: REQUIRED_PER_OPERATION_BINDING",
		"pitrBehavior: QUARANTINE",
		"fenceDurationSeconds: 40",
		"watermarkResetAllowed: false",
		"replayReservationResetAllowed: false",
	} {
		if !strings.Contains(document, want) {
			t.Fatalf("capability registry does not contain %q", want)
		}
	}

	for _, forbidden := range []string{
		"skipTLSVerify: true",
		"plaintextFallback: true",
		"inMemoryReplayStore: true",
		"emptyDirReplayStore: true",
	} {
		if strings.Contains(document, forbidden) {
			t.Fatalf("capability registry enables forbidden setting %q", forbidden)
		}
	}
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

	for _, entry := range registry.Packages {
		if entry.ID != "internal-rpc-authority-v1" {
			continue
		}
		if entry.Format != "proto" ||
			entry.Owner != "internal-rpc-authority" ||
			entry.Source != "contracts/proto/internalrpcauthority/v1" ||
			entry.Generated["go"] != "libs/go/internalrpcauth/gen/internalrpcauthority/v1" {
			t.Fatalf("unexpected authority registry entry: %+v", entry)
		}
		if len(entry.Consumers) < 2 {
			t.Fatalf("authority contract must have multiple registered consumers")
		}
		return
	}
	t.Fatal("internal-rpc-authority-v1 registry entry is missing")
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
		"READBACK_MISMATCH",
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

func messageBody(t *testing.T, proto string, name string) string {
	t.Helper()
	startMarker := "message " + name + " {"
	start := strings.Index(proto, startMarker)
	if start < 0 {
		t.Fatalf("message %s is missing", name)
	}
	start += len(startMarker)
	end := strings.Index(proto[start:], "\n}")
	if end < 0 {
		t.Fatalf("message %s is not closed", name)
	}
	return proto[start : start+end]
}
