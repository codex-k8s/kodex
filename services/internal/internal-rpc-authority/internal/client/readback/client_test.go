package readback

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNewRequestUUIDReturnsDistinctVersionFourIdentifiers(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 32)
	for range 32 {
		value, err := newRequestUUID()
		if err != nil {
			t.Fatalf("new request UUID: %v", err)
		}
		if len(value) != 36 || value[8] != '-' || value[13] != '-' ||
			value[14] != '4' || value[18] != '-' || value[23] != '-' ||
			(value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b') {
			t.Fatalf("request UUID has invalid RFC 4122 version 4 shape: %q", value)
		}
		if _, exists := seen[value]; exists {
			t.Fatalf("request UUID repeated: %q", value)
		}
		seen[value] = struct{}{}
	}
}

func TestSecretAttestorReusesTransportAcrossRefreshes(t *testing.T) {
	t.Parallel()

	key, err := internalrpcauth.GenerateES256Key("readback-test-key")
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	privateJWK, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		t.Fatalf("marshal test key: %v", err)
	}
	reader := &secretReaderStub{materials: map[string]repository.SecretMaterial{
		"credential": {Data: map[string]string{
			"pinned_intent_id":                "intent-1",
			"readback_credential_compact_jws": "credential-1",
			"readback_credential_jti":         "credential-jti-1",
		}},
		"possession": {Data: map[string]string{
			"possession_private_jwk": string(privateJWK),
		}},
	}}
	api := &attestorAPIStub{}
	var opened, closed int
	attestor, err := newSecretAttestor(testSecretConfig(reader), func(
		string,
		*tls.Config,
		grpc.UnaryClientInterceptor,
	) (*transport, error) {
		opened++
		return &transport{
			api: api,
			close: func() error {
				closed++
				return nil
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("construct Secret attestor: %v", err)
	}

	state := repository.SnapshotState{
		SourceRevision:     7,
		SourceDigestSHA256: "snapshot-digest",
	}
	for attempt := range 2 {
		receipt, attestErr := attestor.Attest(context.Background(), state)
		if attestErr != nil {
			t.Fatalf("attest refresh %d: %v", attempt+1, attestErr)
		}
		if receipt.ReceiptID == "" {
			t.Fatalf("attest refresh %d returned an empty receipt", attempt+1)
		}
	}
	if opened != 1 {
		t.Fatalf("transport opened %d times, want 1", opened)
	}
	if api.challengeCalls != 2 || api.attestationCalls != 2 {
		t.Fatalf(
			"unexpected RPC calls: challenge=%d attestation=%d",
			api.challengeCalls,
			api.attestationCalls,
		)
	}
	if reader.calls != 4 {
		t.Fatalf("Secret material read %d times, want 4", reader.calls)
	}
	if err := attestor.Close(); err != nil {
		t.Fatalf("close Secret attestor: %v", err)
	}
	if closed != 1 {
		t.Fatalf("transport closed %d times, want 1", closed)
	}
}

func testSecretConfig(reader SecretReader) SecretConfig {
	return SecretConfig{
		Address:                 "dns:///readback-attestor.test:8443",
		TLS:                     &tls.Config{ServerName: "readback-attestor.test", MinVersion: tls.VersionTLS13},
		CredentialPath:          "credential",
		PossessionPath:          "possession",
		Delivery:                reader,
		WorkloadID:              "workload-1",
		WorkloadSPIFFEID:        "spiffe://kodex.test/ns/kodex-system/sa/workload-1",
		Role:                    "AUTHORIZATION_VERIFIER",
		WorkloadGeneration:      1,
		CredentialGeneration:    1,
		PossessionKeyGeneration: 1,
		UnaryInterceptor: func(
			ctx context.Context,
			method string,
			req any,
			reply any,
			cc *grpc.ClientConn,
			invoker grpc.UnaryInvoker,
			opts ...grpc.CallOption,
		) error {
			return invoker(ctx, method, req, reply, cc, opts...)
		},
	}
}

type secretReaderStub struct {
	materials map[string]repository.SecretMaterial
	calls     int
}

func (reader *secretReaderStub) ReadVersioned(
	_ context.Context,
	path string,
) (repository.SecretMaterial, bool, error) {
	reader.calls++
	material, found := reader.materials[path]
	return material, found, nil
}

type attestorAPIStub struct {
	challengeCalls   int
	attestationCalls int
}

func (api *attestorAPIStub) IssueAttestationChallenge(
	_ context.Context,
	request *internalrpcauthorityv1.IssueAttestationChallengeRequest,
	_ ...grpc.CallOption,
) (*internalrpcauthorityv1.IssueAttestationChallengeResponse, error) {
	api.challengeCalls++
	issuedAt := time.Now().UTC().Truncate(time.Second)
	digest := sha256.Sum256([]byte(request.GetReadbackCredentialCompactJws()))
	return &internalrpcauthorityv1.IssueAttestationChallengeResponse{
		ChallengeId:                    request.GetIdempotencyKey(),
		ChallengeJti:                   "challenge-jti",
		ChallengeNonce:                 "challenge-nonce",
		ChallengeDigestSha256:          "challenge-digest",
		IssuedAt:                       timestamppb.New(issuedAt),
		ExpiresAt:                      timestamppb.New(issuedAt.Add(time.Minute)),
		Kind:                           internalrpcauthorityv1.ReadbackAttestationKind_READBACK_ATTESTATION_KIND_SNAPSHOT,
		PinnedIntentRevision:           1,
		WorkloadGeneration:             1,
		CredentialGeneration:           1,
		PossessionKeyGeneration:        1,
		ReadbackCredentialDigestSha256: hex.EncodeToString(digest[:]),
	}, nil
}

func (api *attestorAPIStub) AttestServedState(
	_ context.Context,
	request *internalrpcauthorityv1.AttestServedStateRequest,
	_ ...grpc.CallOption,
) (*internalrpcauthorityv1.AttestServedStateResponse, error) {
	api.attestationCalls++
	return &internalrpcauthorityv1.AttestServedStateResponse{
		AttestationReceiptId: "receipt-" + request.GetChallengeId(),
		Kind:                 internalrpcauthorityv1.ReadbackAttestationKind_READBACK_ATTESTATION_KIND_SNAPSHOT,
		ExpiresAt:            timestamppb.New(time.Now().Add(time.Minute)),
		EvidenceDigestSha256: "evidence-digest",
		VerifierGeneration:   1,
	}, nil
}
