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

type DraftCommands interface {
	Check(context.Context) error
	Execute(context.Context, value.DraftOperation, string, []byte) (value.DraftResult, error)
}

func WithDraftCommands(commands DraftCommands) Option {
	return func(server *Server) { server.drafts = commands }
}

func (server *Server) SaveSecretDraft(ctx context.Context, request *secretbrokerv1.SaveSecretDraftRequest) (*secretbrokerv1.SaveSecretDraftResponse, error) {
	defer clear(request.GetValue())
	result, err := server.executeDraft(ctx, value.DraftSave, request.GetOperationGrant(), request.GetValue())
	if err != nil {
		return nil, err
	}
	draft, err := caster.SecretDraft(result.Draft)
	if err != nil {
		return nil, draftBoundaryError(err)
	}
	return &secretbrokerv1.SaveSecretDraftResponse{Draft: draft}, nil
}

func (server *Server) ValidateSecretDraft(ctx context.Context, request *secretbrokerv1.ValidateSecretDraftRequest) (*secretbrokerv1.ValidateSecretDraftResponse, error) {
	result, err := server.executeDraft(ctx, value.DraftValidate, request.GetOperationGrant(), nil)
	if err != nil {
		return nil, err
	}
	draft, err := caster.SecretDraft(result.Draft)
	if err != nil {
		return nil, draftBoundaryError(err)
	}
	return &secretbrokerv1.ValidateSecretDraftResponse{Draft: draft}, nil
}

func (server *Server) PublishSecretDraft(ctx context.Context, request *secretbrokerv1.PublishSecretDraftRequest) (*secretbrokerv1.PublishSecretDraftResponse, error) {
	result, err := server.executeDraft(ctx, value.DraftPublish, request.GetOperationGrant(), nil)
	if err != nil {
		return nil, err
	}
	draft, err := caster.SecretDraft(result.Draft)
	if err != nil {
		return nil, draftBoundaryError(err)
	}
	secret, err := caster.PublishedSecret(result.Secret)
	if err != nil {
		return nil, draftBoundaryError(err)
	}
	return &secretbrokerv1.PublishSecretDraftResponse{Draft: draft, Secret: secret}, nil
}

func (server *Server) DiscardSecretDraft(ctx context.Context, request *secretbrokerv1.DiscardSecretDraftRequest) (*secretbrokerv1.DiscardSecretDraftResponse, error) {
	result, err := server.executeDraft(ctx, value.DraftDiscard, request.GetOperationGrant(), nil)
	if err != nil {
		return nil, err
	}
	draft, err := caster.SecretDraft(result.Draft)
	if err != nil {
		return nil, draftBoundaryError(err)
	}
	return &secretbrokerv1.DiscardSecretDraftResponse{Draft: draft}, nil
}

func (server *Server) CheckSecretDraftReadiness(ctx context.Context, _ *secretbrokerv1.CheckSecretDraftReadinessRequest) (*secretbrokerv1.CheckSecretDraftReadinessResponse, error) {
	if server.drafts == nil {
		return nil, draftBoundaryError(secretdrafts.ErrUnavailable)
	}
	if err := server.drafts.Check(ctx); err != nil {
		return nil, draftBoundaryError(err)
	}
	return &secretbrokerv1.CheckSecretDraftReadinessResponse{Ready: true}, nil
}

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
