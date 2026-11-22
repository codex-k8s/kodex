package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// TestSSHIdentityRPCRequiresMethodAuthority checks every entry point rejects a caller lacking verified exact-method context before touching an absent owner service.
// TestSSHIdentityRPCRequiresMethodAuthority проверяет отказ всех entry point без verified exact-method context до обращения к отсутствующему owner service.
func TestSSHIdentityRPCRequiresMethodAuthority(t *testing.T) {
	server := &Server{}
	calls := []func() error{
		func() error {
			_, err := server.BeginSSHIdentitySetup(context.Background(), &cp.BeginSSHIdentitySetupRequest{})
			return err
		},
		func() error {
			_, err := server.GetSSHIdentitySetup(context.Background(), &cp.GetSSHIdentitySetupRequest{})
			return err
		},
		func() error {
			_, err := server.CancelSSHIdentitySetup(context.Background(), &cp.CancelSSHIdentitySetupRequest{})
			return err
		},
		func() error {
			_, err := server.ResolveSSHIdentityUpload(context.Background(), &cp.ResolveSSHIdentityUploadRequest{})
			return err
		},
		func() error {
			_, err := server.ClaimSSHIdentityUpload(context.Background(), &cp.ClaimSSHIdentityUploadRequest{})
			return err
		},
		func() error {
			_, err := server.CompleteSSHIdentityUpload(context.Background(), &cp.CompleteSSHIdentityUploadRequest{})
			return err
		},
	}
	for i, call := range calls {
		if status.Code(call()) != codes.Unauthenticated {
			t.Fatalf("route %d admitted unverified caller", i)
		}
	}
}

// TestSSHIdentityUploadOriginRejectsAlternateRouting verifies the response cannot redirect a browser-held key to userinfo, an alternate SNI, path, query, malformed port or ambiguous default-port origin.
// TestSSHIdentityUploadOriginRejectsAlternateRouting проверяет запрет перенаправления browser-held key через userinfo, alternate SNI, path, query, malformed port либо ambiguous default-port origin.
func TestSSHIdentityUploadOriginRejectsAlternateRouting(t *testing.T) {
	for _, endpoint := range []entity.SSHIdentityUploadEndpoint{{Origin: "https://broker.example.invalid", TLSServerName: "broker.example.invalid"}, {Origin: "https://broker.example.invalid:8443", TLSServerName: "broker.example.invalid"}, {Origin: "https://[2001:db8::1]:8443", TLSServerName: "2001:db8::1"}} {
		if !validSSHSetupUploadEndpoint(endpoint) {
			t.Fatal("canonical upload origin rejected")
		}
	}
	for _, origin := range []string{"http://broker.example.invalid", "https://user@broker.example.invalid", "https://broker.example.invalid/", "https://broker.example.invalid?", "https://broker.example.invalid?token=synthetic", "https://broker.example.invalid/#fragment", "https://broker.example.invalid:0", "https://broker.example.invalid:65536", "https://broker.example.invalid:08443", "https://broker.example.invalid:443", "https://:8443", "https://foreign.example.invalid", "https://broker.example.invalid."} {
		if validSSHSetupUploadEndpoint(entity.SSHIdentityUploadEndpoint{Origin: origin, TLSServerName: "broker.example.invalid"}) {
			t.Fatal("alternate upload route admitted")
		}
	}
	for _, host := range []string{"*.example.invalid", "broker_example.invalid", "BROKER.example.invalid", "брокер.example.invalid", "-broker.example.invalid"} {
		if validSSHSetupUploadEndpoint(entity.SSHIdentityUploadEndpoint{Origin: "https://" + host, TLSServerName: host}) {
			t.Fatal("noncanonical TLS name admitted")
		}
	}
}

// TestSSHIdentitySetupSafeProjectionAndExactMutation checks actual ProtoJSON contains only safe metadata and the transport does not trim durable idempotency coordinates or infer missing OCC.
// TestSSHIdentitySetupSafeProjectionAndExactMutation проверяет safe metadata в actual ProtoJSON и отсутствие trimming durable idempotency coordinates либо inference missing OCC.
func TestSSHIdentitySetupSafeProjectionAndExactMutation(t *testing.T) {
	input := entity.SSHIdentitySetupView{Ref: "ssu_fixture_setup", ConnectionRef: "int_fixture_connection", IdentityRef: "bir_fixture_identity", TargetRef: "target_fixture", Version: 1, ConnectionVersion: 2, IdentityRevision: 1,
		Policy: entity.IntegrationCredentialInitiatorRequired, State: enum.SSHIdentitySetupPrepared, ExpiresAt: time.Now().UTC().Add(time.Minute)}
	view, err := castSSHSetupView(input)
	if err != nil {
		t.Fatal("valid setup projection rejected")
	}
	raw, err := protojson.Marshal(view)
	if err != nil {
		t.Fatal("setup JSON encoding failed")
	}
	for _, field := range []string{"namespace", "secretName", "secretUid", "permit", "fence", "contentSha256", "authenticationSha256"} {
		if strings.Contains(string(raw), field) {
			t.Fatal("safe response exposed private coordinates")
		}
	}
	for _, mutate := range []func(*entity.SSHIdentitySetupView){func(v *entity.SSHIdentitySetupView) { v.State = "FUTURE" }, func(v *entity.SSHIdentitySetupView) { v.Policy = "FUTURE" }, func(v *entity.SSHIdentitySetupView) { v.ExpiresAt = time.Time{} }, func(v *entity.SSHIdentitySetupView) { v.IdentityRevision = 0 }} {
		invalid := input
		mutate(&invalid)
		if _, err := castSSHSetupView(invalid); status.Code(err) != codes.Unavailable {
			t.Fatal("invalid owner projection accepted")
		}
	}
	zero := int64(0)
	mutation := sshSetupMutation(&cp.MutationContext{IdempotencyKey: " setup-exact ", ExpectedVersion: &zero})
	if mutation.IdempotencyKey != " setup-exact " || mutation.ExpectedVersion == nil || *mutation.ExpectedVersion != 0 || sshSetupMutation(nil).ExpectedVersion != nil {
		t.Fatal("mutation coordinates were normalized")
	}
}

// TestSSHIdentityCompletionWirePreservesExplicitOutcomes distinguishes an explicit UNKNOWN message from unreported or nil oneof bodies, and keeps every private locator pin unchanged through protobuf wire encoding.
// TestSSHIdentityCompletionWirePreservesExplicitOutcomes отличает explicit UNKNOWN message от unreported либо nil oneof bodies и сохраняет все private locator pins через protobuf wire encoding.
func TestSSHIdentityCompletionWirePreservesExplicitOutcomes(t *testing.T) {
	for _, request := range []*cp.CompleteSSHIdentityUploadRequest{nil, {}, {Outcome: &cp.CompleteSSHIdentityUploadRequest_Unknown{}}, {Outcome: &cp.CompleteSSHIdentityUploadRequest_Identity{}}, {Outcome: &cp.CompleteSSHIdentityUploadRequest_RejectedAcknowledgement{}}, {Outcome: (*cp.CompleteSSHIdentityUploadRequest_Unknown)(nil)}} {
		if _, err := castSSHUploadCompletion(request); status.Code(err) != codes.InvalidArgument {
			t.Fatal("missing outcome classified as reported")
		}
	}
	unknown := &cp.CompleteSSHIdentityUploadRequest{Outcome: &cp.CompleteSSHIdentityUploadRequest_Unknown{Unknown: &cp.ManagedSSHIdentityUnknownOutcome{}}}
	bytes, err := proto.Marshal(unknown)
	if err != nil {
		t.Fatal("UNKNOWN encoding failed")
	}
	decoded := &cp.CompleteSSHIdentityUploadRequest{}
	if err := proto.Unmarshal(bytes, decoded); err != nil {
		t.Fatal("UNKNOWN decoding failed")
	}
	result, err := castSSHUploadCompletion(decoded)
	if err != nil || !result.Unknown || result.Identity != nil || result.Rejected != nil {
		t.Fatal("explicit UNKNOWN lost presence")
	}
	ref := "broker_identity_ssh_fixture"
	known := &cp.CompleteSSHIdentityUploadRequest{Outcome: &cp.CompleteSSHIdentityUploadRequest_Identity{Identity: &cp.ManagedSSHIdentityLocator{Preparation: &cp.ManagedSSHIdentityPreparation{
		IdentityRevisionRef: ref, TargetRef: "target_fixture", Revision: 1, Namespace: "kodex-system", ContentSha256: strings.Repeat("a", 64)}, AuthenticationSha256: strings.Repeat("b", 64),
		SecretName: managedssh.IdentitySecretName(ref, 1), SecretUid: "opaque-uid", SecretResourceVersion: "opaque-resource-version"}}}
	bytes, err = proto.Marshal(known)
	if err != nil {
		t.Fatal("known outcome encoding failed")
	}
	decoded = &cp.CompleteSSHIdentityUploadRequest{}
	if err := proto.Unmarshal(bytes, decoded); err != nil {
		t.Fatal("known outcome decoding failed")
	}
	result, err = castSSHUploadCompletion(decoded)
	if err != nil || result.Identity == nil || !result.Identity.Valid() || result.Identity.Purpose != managedssh.IdentityPurpose || result.Identity.SecretUID != "opaque-uid" || result.Identity.SecretResourceVersion != "opaque-resource-version" {
		t.Fatal("known private pins changed on wire")
	}
}
