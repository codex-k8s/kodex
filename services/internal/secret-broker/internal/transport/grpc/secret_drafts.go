package grpc

import (
	"context"
	"errors"

	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/transport/grpc/caster"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DraftCommands is the domain command/readiness port used by draft RPC wrappers; concrete services
// own grant consumption, storage and lifecycle, not the response-conversion helper.
// DraftCommands — domain command/readiness port draft RPC wrappers; конкретные сервисы владеют
// grant consumption, storage и lifecycle, не helper преобразования ответа.
type DraftCommands interface {
	// Check reports the concrete service's current readiness without consuming an operation grant.
	// Check сообщает текущую готовность конкретного сервиса без потребления operation grant.
	Check(context.Context) error
	// Execute handles the exact named operation and opaque grant with plaintext only where permitted;
	// implementation-side authority and effect validation must precede domain result publication.
	// Execute выполняет точную operation и opaque grant с plaintext лишь в допустимых случаях;
	// authority и effect validation реализации должны предшествовать выдаче domain result.
	Execute(context.Context, value.DraftOperation, string, []byte) (value.DraftResult, error)
}

// WithDraftCommands installs the supplied port, including nil, as a composition-time option;
// it does not check readiness or provide synchronization for mutation after serving starts.
// WithDraftCommands устанавливает переданный port, включая nil, как composition-time option;
// готовность не проверяется и синхронизация изменения после начала serving не обеспечивается.
func WithDraftCommands(commands DraftCommands) Option {
	return func(server *Server) { server.drafts = commands }
}

// SaveSecretDraft forwards SAVE with the same grant/value and returns only converted metadata.
// Its defer clears the request value's backing bytes on every return, including command/caster failure;
// this also clears aliases and does not roll back a domain effect completed before casting failed.
// SaveSecretDraft передаёт SAVE с теми же grant/value и возвращает лишь преобразованные metadata.
// Defer обнуляет backing bytes request value при любом возврате, включая command/caster failure;
// обнуляются и aliases, но завершённый до ошибки casting доменный эффект не откатывается.
func (server *Server) SaveSecretDraft(ctx context.Context, request *secretbrokerv1.SaveSecretDraftRequest) (*secretbrokerv1.SaveSecretDraftResponse, error) {
	defer clear(request.GetValue())
	draft, err := castDraftResult(server.executeDraft(ctx, value.DraftSave, request.GetOperationGrant(), request.GetValue()))
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.SaveSecretDraftResponse{Draft: draft}, nil
}

// ValidateSecretDraft forwards VALIDATE without plaintext, preserving mapped execution failures
// and converting successful metadata without itself publishing the staged secret.
// ValidateSecretDraft передаёт VALIDATE без plaintext, сохраняя mapped execution failures
// и преобразуя успешные metadata без самостоятельной публикации staged secret.
func (server *Server) ValidateSecretDraft(ctx context.Context, request *secretbrokerv1.ValidateSecretDraftRequest) (*secretbrokerv1.ValidateSecretDraftResponse, error) {
	draft, err := castDraftResult(server.executeDraft(ctx, value.DraftValidate, request.GetOperationGrant(), nil))
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.ValidateSecretDraftResponse{Draft: draft}, nil
}

// PublishSecretDraft forwards PUBLISH without plaintext, converts draft metadata before published
// metadata and returns neither on error. A late conversion failure is not a rollback of completed work.
// PublishSecretDraft передаёт PUBLISH без plaintext, переводит draft metadata перед published
// metadata и не возвращает их при ошибке. Поздний отказ преобразования не откатывает завершённый work.
func (server *Server) PublishSecretDraft(ctx context.Context, request *secretbrokerv1.PublishSecretDraftRequest) (*secretbrokerv1.PublishSecretDraftResponse, error) {
	result, err := server.executeDraft(ctx, value.DraftPublish, request.GetOperationGrant(), nil)
	draft, err := castDraftResult(result, err)
	if err != nil {
		return nil, err
	}
	secret, err := caster.PublishedSecret(result.Secret)
	if err != nil {
		return nil, draftBoundaryError(err)
	}
	return &secretbrokerv1.PublishSecretDraftResponse{Draft: draft, Secret: secret}, nil
}

// DiscardSecretDraft forwards DISCARD without plaintext and maps only the successful metadata;
// exact cleanup eligibility belongs to the domain service, not this RPC response wrapper.
// DiscardSecretDraft передаёт DISCARD без plaintext и отображает лишь успешные metadata;
// допустимость точной очистки принадлежит domain service, не этому RPC response wrapper.
func (server *Server) DiscardSecretDraft(ctx context.Context, request *secretbrokerv1.DiscardSecretDraftRequest) (*secretbrokerv1.DiscardSecretDraftResponse, error) {
	draft, err := castDraftResult(server.executeDraft(ctx, value.DraftDiscard, request.GetOperationGrant(), nil))
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.DiscardSecretDraftResponse{Draft: draft}, nil
}

// CheckSecretDraftReadiness requires the configured port and a successful current Check,
// returning a fixed mapped failure otherwise; it never consumes a draft operation.
// CheckSecretDraftReadiness требует настроенный port и успешный текущий Check,
// иначе возвращая фиксированную mapped failure; draft operation она не потребляет.
func (server *Server) CheckSecretDraftReadiness(ctx context.Context, _ *secretbrokerv1.CheckSecretDraftReadinessRequest) (*secretbrokerv1.CheckSecretDraftReadinessResponse, error) {
	if server.drafts == nil {
		return nil, draftBoundaryError(secretdrafts.ErrUnavailable)
	}
	if err := server.drafts.Check(ctx); err != nil {
		return nil, draftBoundaryError(err)
	}
	return &secretbrokerv1.CheckSecretDraftReadinessResponse{Ready: true}, nil
}

// castDraftResult leaves an already-mapped execution error unchanged; only a successful result
// reaches the metadata caster, whose failure receives the existing closed transport mapping.
// It neither executes an operation nor rolls back effects completed before response conversion.
// castDraftResult сохраняет уже отображённую execution error; лишь успешный результат идёт
// в metadata caster, ошибка которого получает прежнее закрытое transport mapping.
// Она не исполняет операцию и не откатывает эффекты, завершённые до преобразования ответа.
func castDraftResult(result value.DraftResult, executionErr error) (*secretbrokerv1.RuntimeSecretDraftMetadata, error) {
	if executionErr != nil {
		return nil, executionErr
	}
	draft, err := caster.SecretDraft(result.Draft)
	if err != nil {
		return nil, draftBoundaryError(err)
	}
	return draft, nil
}

// executeDraft invokes the configured port once with unchanged context/arguments and no retry,
// discarding partial results on a mapped failure; plaintext clearing belongs to the SAVE wrapper.
// executeDraft единожды вызывает настроенный port с неизменными context/arguments без retry,
// отбрасывая частичный результат при mapped failure; plaintext clearing принадлежит SAVE wrapper.
func (server *Server) executeDraft(ctx context.Context, operation value.DraftOperation, grant string, plaintext []byte) (value.DraftResult, error) {
	if server.drafts == nil {
		return value.DraftResult{}, draftBoundaryError(secretdrafts.ErrUnavailable)
	}
	result, err := server.drafts.Execute(ctx, operation, grant, plaintext)
	if err != nil {
		return value.DraftResult{}, draftBoundaryError(err)
	}
	return result, nil
}

// draftBoundaryError maps cancellation/deadline first, then known repository and gRPC classes;
// unknown failures become Unavailable. Every output uses fixed text rather than upstream diagnostics.
// draftBoundaryError сначала отображает cancellation/deadline, затем известные repository/gRPC classes;
// неизвестный отказ становится Unavailable. Все ответы содержат фиксированный текст, не upstream diagnostics.
func draftBoundaryError(err error) error {
	code := codes.Unavailable
	switch {
	case errors.Is(err, context.Canceled):
		code = codes.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		code = codes.DeadlineExceeded
	case errors.Is(err, secretdrafts.ErrInvalid):
		code = codes.InvalidArgument
	case errors.Is(err, secretdrafts.ErrConflict):
		code = codes.FailedPrecondition
	case errors.Is(err, secretdrafts.ErrNotFound):
		code = codes.NotFound
	default:
		switch status.Code(err) {
		case codes.Unauthenticated, codes.PermissionDenied, codes.NotFound, codes.AlreadyExists, codes.Aborted,
			codes.InvalidArgument, codes.FailedPrecondition, codes.ResourceExhausted, codes.Canceled, codes.DeadlineExceeded:
			code = status.Code(err)
		}
	}
	// Ни upstream diagnostics, ни исходный protobuf не покидают boundary.
	return status.Error(code, "secret draft operation failed")
}
