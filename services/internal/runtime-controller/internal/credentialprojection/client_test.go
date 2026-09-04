package credentialprojection

import (
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestProjectionDescriptorBindsExactExecution(t *testing.T) {
	input := projectionTestInput()
	descriptor := projectionTestDescriptor(input)
	projection, err := projectionFromDescriptor(input, descriptor)
	if err != nil {
		t.Fatalf("projectionFromDescriptor() error = %v", err)
	}
	if projection.SecretName != descriptor.SecretName || projection.RuntimeSecretKeys["SERVICE_TOKEN"] != "SERVICE_TOKEN" {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestMaterializeRequestCarriesOnlyExactExecutionLocator(t *testing.T) {
	input := projectionTestInput()
	request := materializeRequest(input)
	if request.GetWorkloadInstance() != input.WorkloadInstance || request.GetLeaseRef() != input.LeaseRef ||
		request.GetFence() != input.LeaseFence || request.GetGeneration() != input.LeaseGeneration ||
		request.GetRuntimeRevisionRef() != input.RuntimeRevisionRef || request.GetRuntimeRevisionDigest() != input.RuntimeRevisionDigest ||
		request.GetSessionRef() != input.SessionRef || request.GetTurnRef() != input.TurnRef ||
		request.GetAttempt() != input.Attempt || request.GetInputDigest() != input.InputDigest {
		t.Fatalf("materialize request = %#v", request)
	}
}

func TestProjectionDescriptorRejectsEveryCrossExecutionBinding(t *testing.T) {
	input := projectionTestInput()
	tests := map[string]func(*secretbrokerv1.RuntimeCredentialProjectionDescriptor){
		"namespace":        func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) { value.Namespace = "other" },
		"secret name":      func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) { value.SecretName = "../secret" },
		"secret UID":       func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) { value.SecretUid = "" },
		"resource version": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) { value.SecretResourceVersion = "" },
		"content digest": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) {
			value.ContentSha256 = strings.Repeat("A", 64)
		},
		"provider key": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) { value.ProviderAuthKey = "auth.json" },
		"lease":        func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) { value.LeaseRef = "lease_other123" },
		"generation":   func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) { value.Generation++ },
		"revision ref": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) {
			value.RuntimeRevisionRef = "revision_other123"
		},
		"revision digest": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) {
			value.RuntimeRevisionDigest = strings.Repeat("d", 64)
		},
		"session": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) {
			value.SessionRef = "session_other123"
		},
		"turn":    func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) { value.TurnRef = "turn_other123" },
		"attempt": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) { value.Attempt++ },
		"input digest": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) {
			value.InputDigest = strings.Repeat("e", 64)
		},
		"expired": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) {
			value.ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
		},
		"missing key": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) { value.RuntimeSecretKeys = nil },
		"renamed key": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) {
			value.RuntimeSecretKeys[0].SecretKey = "OTHER_TOKEN"
		},
		"duplicate key": func(value *secretbrokerv1.RuntimeCredentialProjectionDescriptor) {
			value.RuntimeSecretKeys = append(value.RuntimeSecretKeys, proto.Clone(value.RuntimeSecretKeys[0]).(*secretbrokerv1.RuntimeCredentialProjectionKey))
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := proto.Clone(projectionTestDescriptor(input)).(*secretbrokerv1.RuntimeCredentialProjectionDescriptor)
			mutate(candidate)
			if _, err := projectionFromDescriptor(input, candidate); err == nil {
				t.Fatal("cross-execution credential descriptor was accepted")
			}
		})
	}
}

func projectionTestInput() runtimecontract.RunnerInput {
	return runtimecontract.RunnerInput{
		WorkloadInstance: "runtime-controller-pod", LeaseRef: "lease_abcdefgh", LeaseFence: "fence-3", LeaseGeneration: 3, RuntimeRevisionRef: "revision_abcdefgh",
		RuntimeRevisionDigest: strings.Repeat("a", 64), SessionRef: "session_abcdefgh", TurnRef: "turn_abcdefgh",
		Attempt: 2, InputDigest: strings.Repeat("b", 64),
		SecretProjections: []runtimecontract.RuntimeSecretProjection{{Name: "SERVICE_TOKEN"}},
	}
}

func projectionTestDescriptor(input runtimecontract.RunnerInput) *secretbrokerv1.RuntimeCredentialProjectionDescriptor {
	return &secretbrokerv1.RuntimeCredentialProjectionDescriptor{
		Namespace: "kodex-runtime", SecretName: "runtime-credentials-0123456789abcdef0123456789abcdef01234567",
		SecretUid: "40000000-0000-4000-8000-000000000001", SecretResourceVersion: "19",
		ContentSha256: strings.Repeat("c", 64), ProviderAuthKey: "provider-auth.json",
		RuntimeSecretKeys: []*secretbrokerv1.RuntimeCredentialProjectionKey{{Name: "SERVICE_TOKEN", SecretKey: "SERVICE_TOKEN"}},
		LeaseRef:          input.LeaseRef, Generation: input.LeaseGeneration, RuntimeRevisionRef: input.RuntimeRevisionRef,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, SessionRef: input.SessionRef, TurnRef: input.TurnRef,
		Attempt: input.Attempt, InputDigest: input.InputDigest, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}
}
