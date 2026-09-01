package grpc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Owner изолирует generated variadic grpc.CallOption от бизнесовой реализации.
type Owner interface {
	Check(context.Context) error
	Consume(context.Context, string) (*controlplanev1.ConsumeRuntimeSecretOperationResponse, error)
	Complete(context.Context, string, int64, *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RuntimeSecret, error)
	Fail(context.Context, string, int64, controlplanev1.RuntimeSecretFailureCode) error
}

type Recovery interface {
	Check(context.Context) error
}

type Store interface {
	Namespace() string
	Check(context.Context) error
	CreateImmutableForEffect(context.Context, kubernetesstore.MaterializationEffect, []byte) (kubernetesstore.Materialization, error)
	ResolveExact(context.Context, kubernetesstore.ExactDescriptor) (kubernetesstore.Materialization, error)
	ReadExactValue(context.Context, kubernetesstore.ExactDescriptor) (kubernetesstore.Materialization, []byte, error)
	DeleteExact(context.Context, kubernetesstore.Materialization) error
}

type Server struct {
	secretbrokerv1.UnimplementedSecretBrokerServiceServer
	controlplanev1.UnimplementedProviderCredentialMaterializerServiceServer
	owner               Owner
	store               Store
	recovery            Recovery
	providerCredentials ProviderCredentialMaterializer
	namespace           string
	maximumSize         int
}

type ProviderCredentialMaterializer interface {
	Check(context.Context) error
	StartDeviceAuthorization(context.Context, string, string) (providercredential.DeviceAuthorization, error)
	ObserveDeviceAuthorization(context.Context, string) (kubernetesstore.ProviderAuthorizationAttempt, error)
	MaterializeAPIKey(context.Context, string, string, []byte) (kubernetesstore.ProviderCredentialDescriptor, string, error)
	Discard(context.Context, providercredential.DiscardMaterialization) error
	CleanupProviderCredential(context.Context, string, string, int64, kubernetesstore.ProviderCredentialDescriptor) (string, error)
}

type Option func(*Server)

func WithProviderCredentialMaterializer(materializer ProviderCredentialMaterializer) Option {
	return func(server *Server) { server.providerCredentials = materializer }
}

func New(owner Owner, store Store, recovery Recovery, maximumSize int, options ...Option) (*Server, error) {
	if owner == nil || store == nil || recovery == nil || store.Namespace() != "kodex-runtime" || maximumSize < 1<<10 || maximumSize > 1<<20 {
		return nil, errors.New("secret broker server configuration is invalid")
	}
	server := &Server{owner: owner, store: store, recovery: recovery, namespace: store.Namespace(), maximumSize: maximumSize}
	for _, option := range options {
		if option != nil {
			option(server)
		}
	}
	return server, nil
}

func (server *Server) CreateSecret(ctx context.Context, request *secretbrokerv1.CreateSecretRequest) (*secretbrokerv1.CreateSecretResponse, error) {
	secret, err := server.mutate(ctx, request.GetOperationGrant(), request.GetValue(), controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE)
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.CreateSecretResponse{Secret: secret}, nil
}

func (server *Server) RotateSecret(ctx context.Context, request *secretbrokerv1.RotateSecretRequest) (*secretbrokerv1.RotateSecretResponse, error) {
	secret, err := server.mutate(ctx, request.GetOperationGrant(), request.GetValue(), controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE)
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.RotateSecretResponse{Secret: secret}, nil
}

func (server *Server) mutate(ctx context.Context, grant string, value []byte, expected controlplanev1.RuntimeSecretOperationKind) (*secretbrokerv1.RuntimeSecretMetadata, error) {
	if len(value) == 0 || len(value) > server.maximumSize {
		return nil, status.Error(codes.InvalidArgument, "secret value size is invalid")
	}
	operation, err := server.owner.Consume(ctx, grant)
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	if err := validateOperation(operation, expected, server.namespace); err != nil {
		return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID, err)
	}
	if !contentDigestMatches(operation.GetExpectedContentSha256(), value) {
		return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID,
			status.Error(codes.InvalidArgument, "secret value does not match authorized content digest"))
	}
	if err := validateValue(operation.GetValueType(), value); err != nil {
		return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID, err)
	}
	materialized, err := server.store.CreateImmutableForEffect(ctx, kubernetesstore.MaterializationEffect{
		OperationRef: operation.GetOperationRef(), ClaimGeneration: operation.GetClaimGeneration(),
		SecretRef: operation.GetSecretRef(), Key: operation.GetSecretKey(), Revision: operation.GetTargetRevision(),
		ContentSHA256: operation.GetExpectedContentSha256(),
	}, value)
	if err != nil {
		if errors.Is(err, kubernetesstore.ErrMaterializationConflict) {
			return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_CONFLICT,
				status.Error(codes.FailedPrecondition, "secret materialization conflicts with authorized effect"))
		}
		if errors.Is(err, kubernetesstore.ErrMaterializationInvalid) {
			return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID,
				status.Error(codes.FailedPrecondition, "secret materialization is invalid"))
		}
		return nil, status.Error(codes.Unavailable, "secret storage is unavailable")
	}
	materialization := castMaterialization(materialized)
	if hint := displayHint(operation.GetValueType(), value); hint != nil {
		materialization.DisplayHint = hint
	}
	secret, err := server.completeEffect(ctx, operation.GetOperationRef(), operation.GetClaimGeneration(), materialization)
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	return castSecret(secret), nil
}

func (server *Server) RevealSecret(ctx context.Context, request *secretbrokerv1.RevealSecretRequest) (*secretbrokerv1.RevealSecretResponse, error) {
	operation, err := server.owner.Consume(ctx, request.GetOperationGrant())
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	if err := validateOperation(operation, controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL, server.namespace); err != nil {
		return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID, err)
	}
	descriptor, err := exactDescriptor(operation, operation.GetRevisionDescriptors()[0])
	if err != nil {
		return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID, err)
	}
	_, value, err := server.store.ReadExactValue(ctx, descriptor)
	if err != nil {
		if errors.Is(err, kubernetesstore.ErrMaterializationConflict) || errors.Is(err, kubernetesstore.ErrMaterializationNotFound) ||
			errors.Is(err, kubernetesstore.ErrExactDeletePreconditionsRequired) {
			return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_CONFLICT,
				status.Error(codes.FailedPrecondition, "secret materialization does not match active revision"))
		}
		return nil, status.Error(codes.Unavailable, "secret storage is unavailable")
	}
	defer clear(value)
	if _, err := server.completeEffect(ctx, operation.GetOperationRef(), operation.GetClaimGeneration(), nil); err != nil {
		return nil, preserveOwnerError(err)
	}
	return &secretbrokerv1.RevealSecretResponse{Value: append([]byte(nil), value...), ValueType: castValueType(operation.GetValueType())}, nil
}

func (server *Server) RevokeSecret(ctx context.Context, request *secretbrokerv1.RevokeSecretRequest) (*secretbrokerv1.RevokeSecretResponse, error) {
	operation, err := server.owner.Consume(ctx, request.GetOperationGrant())
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	if err := validateOperation(operation, controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE, server.namespace); err != nil {
		return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID, err)
	}
	descriptors := make([]kubernetesstore.ExactDescriptor, 0, len(operation.GetRevisionDescriptors()))
	for _, revision := range operation.GetRevisionDescriptors() {
		descriptor, descriptorErr := exactDescriptor(operation, revision)
		if descriptorErr != nil {
			return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID, descriptorErr)
		}
		descriptors = append(descriptors, descriptor)
	}
	secret, err := server.completeEffect(ctx, operation.GetOperationRef(), operation.GetClaimGeneration(), nil)
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	server.cleanupRevoked(ctx, descriptors)
	return &secretbrokerv1.RevokeSecretResponse{Secret: castSecret(secret)}, nil
}

func (server *Server) completeEffect(ctx context.Context, operationRef string, claimGeneration int64, materialization *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RuntimeSecret, error) {
	completion, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	delays := [...]time.Duration{0, 100 * time.Millisecond, 300 * time.Millisecond}
	var err error
	for attempt, delay := range delays {
		if attempt > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-completion.Done():
				timer.Stop()
				return nil, completion.Err()
			case <-timer.C:
			}
		}
		var secret *controlplanev1.RuntimeSecret
		secret, err = server.owner.Complete(completion, operationRef, claimGeneration, materialization)
		if err == nil {
			return secret, nil
		}
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted:
			continue
		default:
			return nil, err
		}
	}
	return nil, err
}

func (server *Server) failPermanent(ctx context.Context, operation *controlplanev1.ConsumeRuntimeSecretOperationResponse, failureCode controlplanev1.RuntimeSecretFailureCode, cause error) error {
	if operation == nil || operation.GetOperationRef() == "" || operation.GetClaimGeneration() < 1 {
		return cause
	}
	failure, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	delays := [...]time.Duration{0, 100 * time.Millisecond, 300 * time.Millisecond}
	var err error
	for attempt, delay := range delays {
		if attempt > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-failure.Done():
				timer.Stop()
				return status.Error(codes.Unavailable, "runtime secret failure receipt is unavailable")
			case <-timer.C:
			}
		}
		err = server.owner.Fail(failure, operation.GetOperationRef(), operation.GetClaimGeneration(), failureCode)
		if err == nil {
			return cause
		}
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted:
			continue
		default:
			return preserveOwnerError(err)
		}
	}
	return preserveOwnerError(err)
}

func (server *Server) cleanupRevoked(ctx context.Context, descriptors []kubernetesstore.ExactDescriptor) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	for _, descriptor := range descriptors {
		materialized, err := server.store.ResolveExact(cleanup, descriptor)
		if errors.Is(err, kubernetesstore.ErrMaterializationNotFound) {
			continue
		}
		if err != nil {
			return
		}
		if err := server.store.DeleteExact(cleanup, materialized); err != nil {
			return
		}
	}
}

func (server *Server) CheckReadiness(ctx context.Context, _ *secretbrokerv1.CheckReadinessRequest) (*secretbrokerv1.CheckReadinessResponse, error) {
	if err := errors.Join(server.owner.Check(ctx), server.store.Check(ctx), server.recovery.Check(ctx)); err != nil {
		return &secretbrokerv1.CheckReadinessResponse{Ready: false}, status.Error(codes.Unavailable, "secret broker dependencies are unavailable")
	}
	return &secretbrokerv1.CheckReadinessResponse{Ready: true}, nil
}

func validateOperation(operation *controlplanev1.ConsumeRuntimeSecretOperationResponse, expected controlplanev1.RuntimeSecretOperationKind, namespace string) error {
	if operation == nil || operation.GetOperationRef() == "" || operation.GetKind() != expected ||
		operation.GetNamespace() != namespace || operation.GetSecretRef() == "" || operation.GetSecretKey() == "" ||
		operation.GetTargetRevision() < 1 || operation.GetClaimGeneration() < 1 || operation.GetLeaseDeadline() == nil {
		return status.Error(codes.FailedPrecondition, "secret operation does not match request")
	}
	if err := operation.GetLeaseDeadline().CheckValid(); err != nil {
		return status.Error(codes.FailedPrecondition, "secret operation lease is invalid")
	}
	switch expected {
	case controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE,
		controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE:
		if !validDigest(operation.GetExpectedContentSha256()) || len(operation.GetRevisionDescriptors()) != 1 {
			return status.Error(codes.FailedPrecondition, "secret mutation intent is invalid")
		}
		descriptor := operation.GetRevisionDescriptors()[0]
		if descriptor.GetRevision() != operation.GetTargetRevision() || descriptor.GetNamespace() != namespace ||
			descriptor.GetSecretName() == "" || descriptor.GetSecretKey() != operation.GetSecretKey() ||
			descriptor.GetContentSha256() != operation.GetExpectedContentSha256() || descriptor.GetSecretUid() != "" || descriptor.GetSecretResourceVersion() != "" {
			return status.Error(codes.FailedPrecondition, "secret mutation descriptor is invalid")
		}
	case controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL:
		if operation.GetExpectedContentSha256() != "" || len(operation.GetRevisionDescriptors()) != 1 ||
			operation.GetRevisionDescriptors()[0].GetRevision() != operation.GetTargetRevision() {
			return status.Error(codes.FailedPrecondition, "secret reveal descriptor is invalid")
		}
	case controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE:
		if operation.GetExpectedContentSha256() != "" || len(operation.GetRevisionDescriptors()) == 0 {
			return status.Error(codes.FailedPrecondition, "secret revoke descriptors are invalid")
		}
	default:
		return status.Error(codes.FailedPrecondition, "secret operation kind is unsupported")
	}
	return nil
}

func exactDescriptor(operation *controlplanev1.ConsumeRuntimeSecretOperationResponse, revision *controlplanev1.RuntimeSecretRevisionDescriptor) (kubernetesstore.ExactDescriptor, error) {
	if operation == nil || revision == nil || revision.GetNamespace() != operation.GetNamespace() ||
		revision.GetSecretName() == "" || revision.GetSecretKey() != operation.GetSecretKey() || revision.GetRevision() < 1 ||
		revision.GetSecretUid() == "" || revision.GetSecretResourceVersion() == "" || !validDigest(revision.GetContentSha256()) {
		return kubernetesstore.ExactDescriptor{}, status.Error(codes.FailedPrecondition, "secret revision descriptor is invalid")
	}
	return kubernetesstore.ExactDescriptor{
		Namespace: revision.GetNamespace(), Name: revision.GetSecretName(), SecretRef: operation.GetSecretRef(),
		Key: revision.GetSecretKey(), Revision: revision.GetRevision(), UID: revision.GetSecretUid(),
		ResourceVersion: revision.GetSecretResourceVersion(), ContentSHA256: revision.GetContentSha256(),
	}, nil
}

func castMaterialization(value kubernetesstore.Materialization) *controlplanev1.RuntimeSecretMaterialization {
	return &controlplanev1.RuntimeSecretMaterialization{
		Namespace: value.Namespace, SecretName: value.Name, SecretKey: value.Key,
		SecretUid: value.UID, SecretResourceVersion: value.ResourceVersion, ContentSha256: value.ContentSHA256,
	}
}

func contentDigestMatches(expected string, value []byte) bool {
	if !validDigest(expected) {
		return false
	}
	digest := sha256.Sum256(value)
	actual := hex.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}

func validateValue(kind controlplanev1.RuntimeSecretValueType, value []byte) error {
	switch kind {
	case controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING:
		if !utf8.Valid(value) {
			return status.Error(codes.InvalidArgument, "string secret must contain valid UTF-8")
		}
	case controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_BINARY:
		return nil
	case controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_JSON:
		if !json.Valid(value) {
			return status.Error(codes.InvalidArgument, "JSON secret value is invalid")
		}
	default:
		return status.Error(codes.FailedPrecondition, "secret value type is unsupported")
	}
	return nil
}

func displayHint(kind controlplanev1.RuntimeSecretValueType, value []byte) *controlplanev1.RuntimeSecretDisplayHint {
	if kind != controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING || !utf8.Valid(value) {
		return nil
	}
	runes := []rune(string(value))
	budget := len(runes) * 15 / 100
	if budget > 12 {
		budget = 12
	}
	if budget < 2 {
		return nil
	}
	prefixCount := (budget + 1) / 2
	suffixCount := budget - prefixCount
	return &controlplanev1.RuntimeSecretDisplayHint{Prefix: string(runes[:prefixCount]), Suffix: string(runes[len(runes)-suffixCount:])}
}

func castSecret(value *controlplanev1.RuntimeSecret) *secretbrokerv1.RuntimeSecretMetadata {
	if value == nil {
		return nil
	}
	result := &secretbrokerv1.RuntimeSecretMetadata{
		SecretRef: value.GetRef(), ProjectRef: value.GetProjectRef(), Name: value.GetName(),
		ValueType: castValueType(value.GetValueType()), Status: castStatus(value.GetState()), Revision: uint64(value.GetCurrentRevision()),
		CreatedAt: cloneTimestamp(value.GetCreatedAt()), UpdatedAt: cloneTimestamp(value.GetUpdatedAt()),
		Description: value.GetDescription(), Version: value.GetVersion(),
	}
	if hint := value.GetDisplayHint(); hint != nil {
		result.DisplayHint = &secretbrokerv1.RuntimeSecretDisplayHint{Prefix: hint.GetPrefix(), Suffix: hint.GetSuffix()}
	}
	return result
}

func cloneTimestamp(value *timestamppb.Timestamp) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return &timestamppb.Timestamp{Seconds: value.Seconds, Nanos: value.Nanos}
}

func castValueType(value controlplanev1.RuntimeSecretValueType) secretbrokerv1.RuntimeSecretValueType {
	return secretbrokerv1.RuntimeSecretValueType(value)
}

func castStatus(value string) secretbrokerv1.RuntimeSecretStatus {
	if value == "ACTIVE" {
		return secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_ACTIVE
	}
	if value == "REVOKED" {
		return secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_REVOKED
	}
	return secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_UNSPECIFIED
}

func preserveOwnerError(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	return status.Error(codes.Unavailable, "runtime secret owner is unavailable")
}
