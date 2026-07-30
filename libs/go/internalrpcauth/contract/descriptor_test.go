package contract_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxJSONSafeInteger = uint64(9007199254740991)

func TestIssuerRequestCompiledDescriptor(t *testing.T) {
	message := requiredMessage(
		t,
		internalrpcauthorityv1.File_internalrpcauthority_v1_authority_proto,
		"IssueAuthorizationContextRequest",
	)
	if err := validateIssuerRequestDescriptor(message); err != nil {
		t.Fatal(err)
	}
	if !message.ReservedRanges().Has(2) {
		t.Fatal("IssueAuthorizationContextRequest does not reserve field number 2")
	}
	if !message.ReservedNames().Has("authority") {
		t.Fatal("IssueAuthorizationContextRequest does not reserve field name authority")
	}
}

func TestIssuerRequestForbiddenFieldMutations(t *testing.T) {
	forbidden := []string{
		"actor",
		"tenant",
		"project",
		"provenance",
		"audience",
		"full_method",
		"permission",
		"expires_at",
		"source_revision",
		"key_set_revision",
		"policy_revision",
		"signer_generation",
		"kid",
	}
	for index, fieldName := range forbidden {
		fieldName := fieldName
		t.Run(fieldName, func(t *testing.T) {
			fileProto := proto.Clone(
				protodesc.ToFileDescriptorProto(
					internalrpcauthorityv1.File_internalrpcauthority_v1_authority_proto,
				),
			).(*descriptorpb.FileDescriptorProto)
			messageProto := requiredMessageProto(t, fileProto, "IssueAuthorizationContextRequest")
			number := int32(100 + index)
			label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
			fieldType := descriptorpb.FieldDescriptorProto_TYPE_STRING
			messageProto.Field = append(messageProto.Field, &descriptorpb.FieldDescriptorProto{
				Name:   proto.String(fieldName),
				Number: &number,
				Label:  &label,
				Type:   &fieldType,
			})
			mutatedFile, err := protodesc.NewFile(fileProto, protoregistry.GlobalFiles)
			if err != nil {
				return
			}
			mutatedMessage := requiredMessage(t, mutatedFile, "IssueAuthorizationContextRequest")
			if err := validateIssuerRequestDescriptor(mutatedMessage); err == nil {
				t.Fatal("forbidden caller-controlled field mutation was accepted")
			}
		})
	}
}

func TestResolverRequestCompiledDescriptor(t *testing.T) {
	message := requiredMessage(
		t,
		internalrpcauthorityv1.File_internalrpcauthority_v1_authority_proto,
		"ResolveAuthorityProofRequest",
	)
	want := map[protoreflect.FieldNumber]protoreflect.Name{
		1: "operation_id",
		2: "resource_reference",
		3: "idempotency_key",
		4: "correlation_id",
	}
	if message.Fields().Len() != len(want) {
		t.Fatalf("resolver request field cardinality = %d, want %d", message.Fields().Len(), len(want))
	}
	for number, name := range want {
		field := message.Fields().ByNumber(number)
		if field == nil || field.Name() != name || field.Kind() != protoreflect.StringKind {
			t.Fatalf("resolver request field %d = %v, want string %s", number, field, name)
		}
	}
	for _, forbidden := range []protoreflect.Name{
		"actor",
		"tenant",
		"project",
		"ownership",
		"caller_spiffe_id",
		"audience",
		"permission",
		"application_credential",
		"internal_authorization_context",
	} {
		if message.Fields().ByName(forbidden) != nil {
			t.Fatalf("resolver request contains caller-authoritative field %s", forbidden)
		}
	}
}

func TestCompiledServiceMethods(t *testing.T) {
	file := internalrpcauthorityv1.File_internalrpcauthority_v1_authority_proto
	want := map[string]bool{
		"/internalrpcauthority.v1.AuthorizationIssuerService/IssueAuthorizationContext":        true,
		"/internalrpcauthority.v1.AuthorizationIssuerService/CheckReadiness":                   true,
		"/internalrpcauthority.v1.AuthorizationVerifierService/VerifyAuthorizationContext":     true,
		"/internalrpcauthority.v1.AuthorizationVerifierService/CheckReadiness":                 true,
		"/internalrpcauthority.v1.AuthorityProofResolverService/ResolveAuthorityProof":         true,
		"/internalrpcauthority.v1.AuthorityProofResolverService/CheckReadiness":                true,
		"/internalrpcauthority.v1.RestoreControllerService/PrepareRestore":                     true,
		"/internalrpcauthority.v1.RestoreControllerService/GetRestoreDirective":                true,
		"/internalrpcauthority.v1.RestoreControllerService/AcknowledgeQuiescence":              true,
		"/internalrpcauthority.v1.RestoreControllerService/CompleteRestore":                    true,
		"/internalrpcauthority.v1.RestoreControllerService/CheckReadiness":                     true,
		"/internalrpcauthority.v1.AuthorityReadbackAttestorService/IssueAttestationChallenge":  true,
		"/internalrpcauthority.v1.AuthorityReadbackAttestorService/AttestServedState":          true,
		"/internalrpcauthority.v1.AuthorityReadbackAttestorService/CheckReadiness":             true,
		"/internalrpcauthority.v1.RestoreRoleCredentialPublisherService/PublishRoleCredential": true,
		"/internalrpcauthority.v1.RestoreRoleCredentialPublisherService/CheckReadiness":        true,
	}
	got := make(map[string]bool)
	services := file.Services()
	for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
		service := services.Get(serviceIndex)
		methods := service.Methods()
		for methodIndex := 0; methodIndex < methods.Len(); methodIndex++ {
			method := methods.Get(methodIndex)
			fullMethod := "/" + string(service.FullName()) + "/" + string(method.Name())
			got[fullMethod] = true
		}
	}
	if len(got) != len(want) {
		t.Fatalf("compiled method cardinality = %d, want %d: %+v", len(got), len(want), got)
	}
	for method := range want {
		if !got[method] {
			t.Fatalf("compiled descriptor does not contain %s", method)
		}
	}
}

func TestReadbackChallengeCompiledDescriptor(t *testing.T) {
	file := internalrpcauthorityv1.File_internalrpcauthority_v1_authority_proto
	request := requiredMessage(t, file, "IssueAttestationChallengeRequest")
	for number, name := range map[protoreflect.FieldNumber]protoreflect.Name{
		1: "pinned_intent_id",
		2: "readback_credential_compact_jws",
		3: "idempotency_key",
		4: "correlation_id",
	} {
		field := request.Fields().ByNumber(number)
		if field == nil || field.Name() != name {
			t.Fatalf("readback challenge request field %d = %v, want %s", number, field, name)
		}
	}
	for _, forbidden := range []protoreflect.Name{
		"workload_id",
		"workload_spiffe_id",
		"role",
		"workload_generation",
		"credential_generation",
		"possession_key_generation",
		"audience",
		"ttl_seconds",
		"challenge_nonce",
	} {
		if request.Fields().ByName(forbidden) != nil {
			t.Fatalf("challenge request contains caller-authoritative field %s", forbidden)
		}
	}

	response := requiredMessage(t, file, "IssueAttestationChallengeResponse")
	for number, name := range map[protoreflect.FieldNumber]protoreflect.Name{
		1:  "challenge_id",
		2:  "challenge_jti",
		3:  "challenge_nonce",
		4:  "challenge_digest_sha256",
		5:  "issued_at",
		6:  "expires_at",
		7:  "kind",
		8:  "pinned_intent_revision",
		9:  "pinned_intent_digest_sha256",
		10: "workload_generation",
		11: "credential_generation",
		12: "possession_key_generation",
		13: "readback_credential_digest_sha256",
	} {
		field := response.Fields().ByNumber(number)
		if field == nil || field.Name() != name {
			t.Fatalf("readback challenge response field %d = %v, want %s", number, field, name)
		}
	}

	attest := requiredMessage(t, file, "AttestServedStateRequest")
	if field := attest.Fields().ByNumber(2); field == nil ||
		field.Name() != "readback_credential_compact_jws" {
		t.Fatal("served-state request does not use separate normal-readback credential")
	}
	if field := attest.Fields().ByNumber(6); field == nil ||
		field.Name() != "challenge_id" {
		t.Fatal("served-state request does not bind durable challenge id")
	}
}

func TestRestoreAckIdempotencyCompiledDescriptor(t *testing.T) {
	file := internalrpcauthorityv1.File_internalrpcauthority_v1_authority_proto
	request := requiredMessage(t, file, "AcknowledgeQuiescenceRequest")
	idempotency := request.Fields().ByNumber(8)
	if idempotency == nil ||
		idempotency.Name() != "idempotency_key" ||
		idempotency.Kind() != protoreflect.StringKind {
		t.Fatal("restore ACK request lacks exact idempotency key field 8")
	}
	response := requiredMessage(t, file, "AcknowledgeQuiescenceResponse")
	receipt := response.Fields().ByNumber(2)
	if receipt == nil ||
		receipt.Name() != "receipt" ||
		receipt.Kind() != protoreflect.MessageKind ||
		receipt.Message().Name() != "QuiescenceAckReceipt" {
		t.Fatal("restore ACK response lacks saved receipt field 2")
	}
	receiptMessage := requiredMessage(t, file, "QuiescenceAckReceipt")
	for number, name := range map[protoreflect.FieldNumber]protoreflect.Name{
		1: "receipt_id",
		2: "idempotency_key",
		3: "ack_jti",
		4: "semantic_request_digest_sha256",
		5: "accepted_ack_digest_sha256",
		6: "coordination_revision",
		7: "resulting_phase",
		8: "accepted_at",
	} {
		field := receiptMessage.Fields().ByNumber(number)
		if field == nil || field.Name() != name {
			t.Fatalf("restore ACK receipt field %d = %v, want %s", number, field, name)
		}
	}
}

func TestEnumCanonicalMapping(t *testing.T) {
	actorCases := map[internalrpcauthorityv1.ActorKind]string{
		internalrpcauthorityv1.ActorKind_ACTOR_KIND_HUMAN:      "HUMAN",
		internalrpcauthorityv1.ActorKind_ACTOR_KIND_AGENT:      "AGENT",
		internalrpcauthorityv1.ActorKind_ACTOR_KIND_SERVICE:    "SERVICE",
		internalrpcauthorityv1.ActorKind_ACTOR_KIND_AUTOMATION: "AUTOMATION",
	}
	for value, want := range actorCases {
		got, err := canonicalActorKind(value)
		if err != nil || got != want {
			t.Fatalf("actor kind %s maps to %q/%v, want %q", value, got, err, want)
		}
		if got == value.String() {
			t.Fatalf("actor kind %s used prefixed enum.String()", value)
		}
	}
	if _, err := canonicalActorKind(internalrpcauthorityv1.ActorKind_ACTOR_KIND_UNSPECIFIED); err == nil {
		t.Fatal("unspecified actor kind was accepted")
	}

	sourceCases := map[internalrpcauthorityv1.AuthoritySource]string{
		internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION:          "OIDC_SESSION",
		internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_MATTERMOST_EVENT:      "MATTERMOST_EVENT",
		internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE:          "DOMAIN_STATE",
		internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_AGENT_SESSION:         "AGENT_SESSION",
		internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_PROCESS_RUN:           "PROCESS_RUN",
		internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_AUTOMATION_OCCURRENCE: "AUTOMATION_OCCURRENCE",
	}
	for value, want := range sourceCases {
		got, err := canonicalAuthoritySource(value)
		if err != nil || got != want {
			t.Fatalf("authority source %s maps to %q/%v, want %q", value, got, err, want)
		}
		if got == value.String() {
			t.Fatalf("authority source %s used prefixed enum.String()", value)
		}
	}
	if _, err := canonicalAuthoritySource(
		internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_UNSPECIFIED,
	); err == nil {
		t.Fatal("unspecified authority source was accepted")
	}
}

func TestVerifiedAuthorizationContextBinaryRoundTrip(t *testing.T) {
	issuedAt := time.Unix(1785348000, 0).UTC()
	context := &internalrpcauthorityv1.VerifiedAuthorizationContext{
		ContractVersion:  1,
		Issuer:           "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
		Audience:         "urn:mattercodex:internal-rpc:control-plane",
		Subject:          "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
		CallerWorkloadId: "control-api-gateway",
		CallerSpiffeId:   "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
		TargetWorkloadId: "control-plane",
		TargetSpiffeId:   "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
		FullMethod:       "/controlplane.v1.ProjectService/GetProject",
		OperationId:      "control.project.get",
		Authority: &internalrpcauthorityv1.CallerAuthority{
			ActorKind: internalrpcauthorityv1.ActorKind_ACTOR_KIND_HUMAN,
			Actor:     identity("10000000-0000-4000-8000-000000000001", 1),
			Tenant:    identity("20000000-0000-4000-8000-000000000002", 2),
			Project:   identity("30000000-0000-4000-8000-000000000003", 3),
		},
		Permission:         "control.project.get",
		Jti:                "40000000-0000-4000-8000-000000000004",
		IssuedAt:           timestamppb.New(issuedAt),
		NotBefore:          timestamppb.New(issuedAt),
		ExpiresAt:          timestamppb.New(issuedAt.Add(30 * time.Second)),
		SourceRevision:     maxJSONSafeInteger,
		SourceDigestSha256: strings.Repeat("a", 64),
		KeySetRevision:     maxJSONSafeInteger,
		PolicyRevision:     maxJSONSafeInteger,
		SignerGeneration:   maxJSONSafeInteger,
	}
	if err := validateVerifiedContext(context, true); err != nil {
		t.Fatalf("valid verified context rejected: %v", err)
	}

	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(context)
	if err != nil {
		t.Fatalf("marshal verified context: %v", err)
	}
	var decoded internalrpcauthorityv1.VerifiedAuthorizationContext
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("unmarshal verified context: %v", err)
	}
	if !proto.Equal(context, &decoded) {
		t.Fatalf("verified context changed after binary round-trip:\n got %v\nwant %v", &decoded, context)
	}
	if decoded.GetAuthority().GetProject() == nil ||
		decoded.GetIssuedAt().GetNanos() != 0 ||
		decoded.GetSourceRevision() != maxJSONSafeInteger {
		t.Fatal("presence, timestamp precision or safe revision was lost")
	}
}

func TestVerifiedAuthorizationContextNegativeCasterMatrix(t *testing.T) {
	base := validVerifiedContext(t)
	tests := map[string]func(*internalrpcauthorityv1.VerifiedAuthorizationContext){
		"safe-integer-overflow": func(context *internalrpcauthorityv1.VerifiedAuthorizationContext) {
			context.SourceRevision = maxJSONSafeInteger + 1
		},
		"project-presence": func(context *internalrpcauthorityv1.VerifiedAuthorizationContext) {
			context.Authority.Project = nil
		},
		"actor-unspecified": func(context *internalrpcauthorityv1.VerifiedAuthorizationContext) {
			context.Authority.ActorKind = internalrpcauthorityv1.ActorKind_ACTOR_KIND_UNSPECIFIED
		},
		"source-unspecified": func(context *internalrpcauthorityv1.VerifiedAuthorizationContext) {
			context.Authority.Actor.Provenance.Source =
				internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_UNSPECIFIED
		},
		"timestamp-nanos": func(context *internalrpcauthorityv1.VerifiedAuthorizationContext) {
			context.IssuedAt.Nanos = 1
		},
		"ttl": func(context *internalrpcauthorityv1.VerifiedAuthorizationContext) {
			context.ExpiresAt.Seconds++
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			context := proto.Clone(base).(*internalrpcauthorityv1.VerifiedAuthorizationContext)
			mutate(context)
			if err := validateVerifiedContext(context, true); err == nil {
				t.Fatal("negative caster fixture was accepted")
			}
		})
	}
}

func TestAuthorizationErrorMatrixCoversCompiledEnums(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(
		root,
		"contracts/authorization/v1/authorization-error-matrix.json",
	)
	var matrix struct {
		Version int `json:"v"`
		Errors  []struct {
			Reason    string `json:"reason"`
			GRPCCode  string `json:"grpc_code"`
			Stage     string `json:"stage"`
			Retryable bool   `json:"retryable"`
			Message   string `json:"message"`
		} `json:"errors"`
	}
	decodeJSONStrict(t, path, &matrix)
	if matrix.Version != 1 {
		t.Fatalf("error matrix version = %d, want 1", matrix.Version)
	}

	file := internalrpcauthorityv1.File_internalrpcauthority_v1_authority_proto
	reasonEnum := file.Enums().ByName("AuthorizationErrorReason")
	stageEnum := file.Enums().ByName("AuthorizationFailureStage")
	if reasonEnum == nil || stageEnum == nil {
		t.Fatal("compiled error enums are missing")
	}
	allowedCodes := map[string]bool{
		"INVALID_ARGUMENT":    true,
		"UNAUTHENTICATED":     true,
		"PERMISSION_DENIED":   true,
		"FAILED_PRECONDITION": true,
		"ALREADY_EXISTS":      true,
		"NOT_FOUND":           true,
		"UNAVAILABLE":         true,
		"INTERNAL":            true,
	}
	seen := make(map[string]bool, len(matrix.Errors))
	for _, entry := range matrix.Errors {
		if seen[entry.Reason] {
			t.Fatalf("duplicate error matrix reason %s", entry.Reason)
		}
		seen[entry.Reason] = true
		if !allowedCodes[entry.GRPCCode] {
			t.Fatalf("unknown gRPC code %s for %s", entry.GRPCCode, entry.Reason)
		}
		if stageEnum.Values().ByName(
			protoreflect.Name("AUTHORIZATION_FAILURE_STAGE_"+entry.Stage),
		) == nil {
			t.Fatalf("unknown compiled stage %s for %s", entry.Stage, entry.Reason)
		}
		if entry.Message == "" || strings.ContainsAny(entry.Message, "\r\n") {
			t.Fatalf("unsafe or unstable message for %s: %q", entry.Reason, entry.Message)
		}
	}
	for index := 1; index < reasonEnum.Values().Len(); index++ {
		name := string(reasonEnum.Values().Get(index).Name())
		reason := strings.TrimPrefix(name, "AUTHORIZATION_ERROR_REASON_")
		if !seen[reason] {
			t.Fatalf("compiled reason %s is missing from error matrix", reason)
		}
		delete(seen, reason)
	}
	if len(seen) != 0 || len(matrix.Errors) != reasonEnum.Values().Len()-1 {
		t.Fatalf("error matrix contains entries outside compiled enum: %+v", seen)
	}
}

func validateIssuerRequestDescriptor(message protoreflect.MessageDescriptor) error {
	type expectedField struct {
		number protoreflect.FieldNumber
		kind   protoreflect.Kind
	}
	expected := map[protoreflect.Name]expectedField{
		"operation_id": {
			number: 1,
			kind:   protoreflect.StringKind,
		},
		"correlation_id": {
			number: 3,
			kind:   protoreflect.StringKind,
		},
		"authority_proof_compact_jws": {
			number: 4,
			kind:   protoreflect.StringKind,
		},
	}
	fields := message.Fields()
	if fields.Len() != len(expected) {
		return fmt.Errorf("issuer request has %d fields, want %d", fields.Len(), len(expected))
	}
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		want, ok := expected[field.Name()]
		if !ok {
			return fmt.Errorf("issuer request contains forbidden field %s", field.Name())
		}
		if field.Number() != want.number ||
			field.Kind() != want.kind ||
			field.Cardinality() != protoreflect.Optional ||
			field.IsList() ||
			field.IsMap() {
			return fmt.Errorf("issuer request field %s has incompatible descriptor", field.Name())
		}
		delete(expected, field.Name())
	}
	if len(expected) != 0 {
		return fmt.Errorf("issuer request is missing fields: %+v", expected)
	}
	return nil
}

func validateVerifiedContext(
	context *internalrpcauthorityv1.VerifiedAuthorizationContext,
	projectRequired bool,
) error {
	if context.GetContractVersion() != 1 || context.GetAuthority() == nil {
		return fmt.Errorf("missing contract version or authority")
	}
	authority := context.GetAuthority()
	if authority.GetActor() == nil || authority.GetTenant() == nil {
		return fmt.Errorf("actor and tenant must be present")
	}
	if projectRequired && authority.GetProject() == nil {
		return fmt.Errorf("project must be present")
	}
	if _, err := canonicalActorKind(authority.GetActorKind()); err != nil {
		return err
	}
	for _, value := range []*internalrpcauthorityv1.AuthorityIdentity{
		authority.GetActor(),
		authority.GetTenant(),
		authority.GetProject(),
	} {
		if value == nil {
			continue
		}
		provenance := value.GetProvenance()
		if provenance == nil ||
			provenance.GetRevision() == 0 ||
			provenance.GetRevision() > maxJSONSafeInteger {
			return fmt.Errorf("invalid authority provenance")
		}
		if _, err := canonicalAuthoritySource(provenance.GetSource()); err != nil {
			return err
		}
	}
	for _, revision := range []uint64{
		context.GetSourceRevision(),
		context.GetKeySetRevision(),
		context.GetPolicyRevision(),
		context.GetSignerGeneration(),
	} {
		if revision == 0 || revision > maxJSONSafeInteger {
			return fmt.Errorf("revision is outside JSON safe integer range")
		}
	}
	for _, timestamp := range []*timestamppb.Timestamp{
		context.GetIssuedAt(),
		context.GetNotBefore(),
		context.GetExpiresAt(),
	} {
		if timestamp == nil || timestamp.CheckValid() != nil || timestamp.GetNanos() != 0 {
			return fmt.Errorf("invalid timestamp")
		}
	}
	if context.GetNotBefore().GetSeconds() != context.GetIssuedAt().GetSeconds() ||
		context.GetExpiresAt().GetSeconds()-context.GetIssuedAt().GetSeconds() != 30 {
		return fmt.Errorf("invalid authorization context lifetime")
	}
	return nil
}

func canonicalActorKind(value internalrpcauthorityv1.ActorKind) (string, error) {
	switch value {
	case internalrpcauthorityv1.ActorKind_ACTOR_KIND_HUMAN:
		return "HUMAN", nil
	case internalrpcauthorityv1.ActorKind_ACTOR_KIND_AGENT:
		return "AGENT", nil
	case internalrpcauthorityv1.ActorKind_ACTOR_KIND_SERVICE:
		return "SERVICE", nil
	case internalrpcauthorityv1.ActorKind_ACTOR_KIND_AUTOMATION:
		return "AUTOMATION", nil
	default:
		return "", fmt.Errorf("unknown actor kind %d", value)
	}
}

func canonicalAuthoritySource(value internalrpcauthorityv1.AuthoritySource) (string, error) {
	switch value {
	case internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION:
		return "OIDC_SESSION", nil
	case internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_MATTERMOST_EVENT:
		return "MATTERMOST_EVENT", nil
	case internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE:
		return "DOMAIN_STATE", nil
	case internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_AGENT_SESSION:
		return "AGENT_SESSION", nil
	case internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_PROCESS_RUN:
		return "PROCESS_RUN", nil
	case internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_AUTOMATION_OCCURRENCE:
		return "AUTOMATION_OCCURRENCE", nil
	default:
		return "", fmt.Errorf("unknown authority source %d", value)
	}
}

func identity(id string, revision uint64) *internalrpcauthorityv1.AuthorityIdentity {
	return &internalrpcauthorityv1.AuthorityIdentity{
		Id: id,
		Provenance: &internalrpcauthorityv1.AuthorityProvenance{
			Source:       internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE,
			Reference:    "50000000-0000-4000-8000-000000000005",
			Revision:     revision,
			DigestSha256: strings.Repeat("b", 64),
		},
	}
}

func validVerifiedContext(t *testing.T) *internalrpcauthorityv1.VerifiedAuthorizationContext {
	t.Helper()
	issuedAt := time.Unix(1785348000, 0).UTC()
	context := &internalrpcauthorityv1.VerifiedAuthorizationContext{
		ContractVersion: 1,
		Authority: &internalrpcauthorityv1.CallerAuthority{
			ActorKind: internalrpcauthorityv1.ActorKind_ACTOR_KIND_HUMAN,
			Actor:     identity("10000000-0000-4000-8000-000000000001", 1),
			Tenant:    identity("20000000-0000-4000-8000-000000000002", 2),
			Project:   identity("30000000-0000-4000-8000-000000000003", 3),
		},
		IssuedAt:         timestamppb.New(issuedAt),
		NotBefore:        timestamppb.New(issuedAt),
		ExpiresAt:        timestamppb.New(issuedAt.Add(30 * time.Second)),
		SourceRevision:   1,
		KeySetRevision:   1,
		PolicyRevision:   1,
		SignerGeneration: 1,
	}
	if err := validateVerifiedContext(context, true); err != nil {
		t.Fatalf("build valid verified context: %v", err)
	}
	return context
}

func requiredMessage(
	t *testing.T,
	file protoreflect.FileDescriptor,
	name protoreflect.Name,
) protoreflect.MessageDescriptor {
	t.Helper()
	message := file.Messages().ByName(name)
	if message == nil {
		t.Fatalf("compiled message %s is missing", name)
	}
	return message
}

func requiredMessageProto(
	t *testing.T,
	file *descriptorpb.FileDescriptorProto,
	name string,
) *descriptorpb.DescriptorProto {
	t.Helper()
	for _, message := range file.GetMessageType() {
		if message.GetName() == name {
			return message
		}
	}
	t.Fatalf("message proto %s is missing", name)
	return nil
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
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
		t.Fatalf("%s contains trailing data: %v", path, err)
	}
}
