package grpc

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// sshProjectionOwner observes exact worker bindings and supplies synthetic owner responses or failures.
// sshProjectionOwner фиксирует точные worker bindings и возвращает синтетические owner ответы или ошибки.
type sshProjectionOwner struct {
	fakeOwner
	events   *[]string
	request  *cp.ResolveSSHComposeCredentialProjectionRequest
	response *cp.ResolveSSHComposeCredentialProjectionResponse
	err      error
}

// ResolveSSHComposeCredentialProjection records the authorization stage without accessing a real owner.
// ResolveSSHComposeCredentialProjection фиксирует этап авторизации без обращения к настоящему owner.
func (owner *sshProjectionOwner) ResolveSSHComposeCredentialProjection(_ context.Context, request *cp.ResolveSSHComposeCredentialProjectionRequest) (*cp.ResolveSSHComposeCredentialProjectionResponse, error) {
	*owner.events = append(*owner.events, "owner")
	owner.request = proto.Clone(request).(*cp.ResolveSSHComposeCredentialProjectionRequest)
	return owner.response, owner.err
}

// sshProjectionStore captures exact materialization pins and retains the synthetic buffer to observe clearing.
// sshProjectionStore фиксирует точные materialization pins и сохраняет синтетический буфер для проверки очистки.
type sshProjectionStore struct {
	fakeStore
	events     *[]string
	descriptor kubernetesstore.ExactDescriptor
	raw        []byte
	err        error
}

// ReadExactValue records one exact read and returns controlled bytes, never a Kubernetes credential.
// ReadExactValue фиксирует одно точное чтение и возвращает заданные байты, а не Kubernetes credential.
func (store *sshProjectionStore) ReadExactValue(_ context.Context, descriptor kubernetesstore.ExactDescriptor) (kubernetesstore.Materialization, []byte, error) {
	*store.events = append(*store.events, "store")
	store.descriptor = descriptor
	return kubernetesstore.Materialization{}, store.raw, store.err
}

// TestSSHComposeCredentialProjection checks admission, lease forwarding, descriptor bounds and byte ownership.
// All values are synthetic; no network, Kubernetes lookup, SSH connection or external effect occurs.
// TestSSHComposeCredentialProjection проверяет допуск, передачу lease, границы descriptor и владение байтами.
// Все значения синтетические; сеть, Kubernetes lookup, SSH и внешние эффекты не используются.
func TestSSHComposeCredentialProjection(t *testing.T) {
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway"
	const operation = "platform.ssh-compose.credential.project"
	method := sb.RuntimeCredentialProjectionService_ProjectSSHComposeCredential_FullMethodName
	for _, scenario := range []string{"valid", "raw-metadata", "wrong-caller", "wrong-operation", "owner-denied", "owner-failure", "missing-descriptor", "invalid-revision", "missing-target", "missing-permit", "large-permit", "missing-expiry", "expired", "distant-expiry", "store-conflict", "store-failure", "empty-key", "large-key"} {
		t.Run(scenario, func(t *testing.T) {
			events := []string{}
			resolved := &cp.ResolveSSHComposeCredentialProjectionResponse{
				SecretRef: "sec_synthetic", TargetRef: "int_synthetic", Permit: []byte("synthetic-signed-permit"), ExpiresAt: timestamppb.New(time.Now().Add(10 * time.Second)),
				SecretRevision: &cp.RuntimeSecretRevisionDescriptor{Revision: 3, Namespace: runtimeNamespace, SecretName: "synthetic-key", SecretKey: "private_key", SecretUid: "synthetic-uid", SecretResourceVersion: "7", ContentSha256: strings.Repeat("b", 64)},
			}
			owner := &sshProjectionOwner{events: &events, response: resolved}
			store := &sshProjectionStore{events: &events, raw: []byte("synthetic-private-key")}
			request := &sb.ProjectSSHComposeCredentialRequest{WorkloadInstance: "worker_synthetic", InvocationRef: "iiv_synthetic", LeaseRef: "lea_synthetic", Fence: "synthetic-fence", Generation: 4}
			wantCode, wantEvents := codes.FailedPrecondition, "owner"
			binding := serviceidentity.Binding{CallerSPIFFEID: caller, FullMethod: method, OperationID: operation, Permission: operation, ActorMode: serviceidentity.ServiceActor}
			md := metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, caller)
			switch scenario {
			case "valid":
				wantCode, wantEvents = codes.OK, "owner,store"
			case "raw-metadata", "wrong-caller", "wrong-operation":
				wantCode, wantEvents = codes.PermissionDenied, ""
				if scenario == "wrong-caller" {
					md.Set(serviceidentity.CallerMetadataKey, runtimeControllerSPIFFEID)
				}
				if scenario == "wrong-operation" {
					binding.OperationID, binding.Permission = "platform.other.read", "platform.other.read"
				}
			case "owner-denied":
				owner.err, wantCode = status.Error(codes.NotFound, "synthetic hidden source"), codes.NotFound
			case "owner-failure":
				owner.err, wantCode = errors.New("synthetic-private-owner-diagnostic"), codes.Unavailable
			case "missing-descriptor":
				resolved.SecretRevision = nil
			case "invalid-revision":
				resolved.SecretRevision.Revision = 0
			case "missing-target":
				resolved.TargetRef = ""
			case "missing-permit":
				resolved.Permit = nil
			case "large-permit":
				resolved.Permit = make([]byte, 16385)
			case "missing-expiry":
				resolved.ExpiresAt = nil
			case "expired":
				resolved.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			case "distant-expiry":
				resolved.ExpiresAt = timestamppb.New(time.Now().Add(time.Minute))
			case "store-conflict":
				store.err, wantEvents = kubernetesstore.ErrMaterializationConflict, "owner,store"
			case "store-failure":
				store.err, wantCode, wantEvents = errors.New("synthetic-private-store-diagnostic"), codes.Unavailable, "owner,store"
			case "empty-key":
				store.raw, wantEvents = nil, "owner,store"
			case "large-key":
				store.raw, wantEvents = make([]byte, 16385), "owner,store"
			}
			server := &Server{owner: owner, store: store}
			ctx := metadata.NewIncomingContext(t.Context(), md)
			var response *sb.ProjectSSHComposeCredentialResponse
			var err error
			if scenario == "raw-metadata" {
				response, err = server.ProjectSSHComposeCredential(ctx, request)
			} else {
				authorizer, createErr := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, secretBrokerSPIFFEID, []serviceidentity.Binding{binding})
				if createErr != nil {
					t.Fatal(createErr)
				}
				_, err = authorizer.UnaryServerInterceptor()(ctx, request, &grpcgo.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
					response, err = server.ProjectSSHComposeCredential(ctx, request)
					return response, err
				})
			}
			if status.Code(err) != wantCode || strings.Join(events, ",") != wantEvents {
				t.Fatalf("projection boundary mismatch: code=%s events=%v", status.Code(err), events)
			}
			if wantEvents != "" && !proto.Equal(owner.request.GetBinding(), &cp.SSHComposeLeaseBinding{WorkloadInstance: request.WorkloadInstance, InvocationRef: request.InvocationRef, ConnectionTestRef: request.ConnectionTestRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation}) {
				t.Fatal("worker lease tuple changed before owner resolution")
			}
			if wantEvents == "owner,store" {
				want := kubernetesstore.ExactDescriptor{Namespace: runtimeNamespace, Name: "synthetic-key", Key: "private_key", SecretRef: "sec_synthetic", Revision: 3, UID: "synthetic-uid", ResourceVersion: "7", ContentSHA256: strings.Repeat("b", 64)}
				if store.descriptor != want {
					t.Fatal("materialization did not use the exact owner descriptor")
				}
				if store.err == nil && !bytes.Equal(store.raw, make([]byte, len(store.raw))) {
					t.Fatal("temporary credential buffer was not cleared")
				}
			}
			if wantCode == codes.OK {
				if response == nil || string(response.PrivateKey) != "synthetic-private-key" || response.TargetRef != resolved.TargetRef || !bytes.Equal(response.Permit, resolved.Permit) || !proto.Equal(response.ExpiresAt, resolved.ExpiresAt) {
					t.Fatal("successful projection lost its value copy or owner pins")
				}
				clear(response.PrivateKey)
			} else if response != nil {
				t.Fatal("rejected projection returned credential bytes")
			}
			if err != nil && (strings.Contains(err.Error(), "synthetic-private-owner-diagnostic") || strings.Contains(err.Error(), "synthetic-private-store-diagnostic")) {
				t.Fatal("private dependency diagnostics escaped the closed boundary")
			}
		})
	}
}
