package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// mailboxCommand executes a typed mailbox command with the verified method principal and supplied
// mutation, requiring a mailbox result without adding an idempotency fallback.
// mailboxCommand выполняет типизированную команду ящика с проверенным principal метода и
// переданной mutation, требуя результат ящика без добавления fallback идемпотентности.
func (server *Server) mailboxCommand(ctx context.Context, method string, kind command.Kind, mutationInput *cp.MutationContext, input command.EmailMailboxInput) (*cp.EmailMailboxConfigurationView, error) {
	p, err := principal(ctx, method)
	if err != nil {
		return nil, err
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: kind, Principal: p, Mutation: mutation(mutationInput), Payload: input})
	if err != nil {
		return nil, transportError(err)
	}
	if result.EmailMailbox == nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	return castMailboxView(*result.EmailMailbox), nil
}

// CreateEmailMailboxDraft converts YAML or typed content before passing connection, configuration
// and name to the dedicated mailbox create command.
// CreateEmailMailboxDraft преобразует YAML либо типизированное содержимое до передачи подключения,
// конфигурации и имени отдельной команде создания черновика ящика.
func (server *Server) CreateEmailMailboxDraft(ctx context.Context, request *cp.CreateEmailMailboxDraftRequest) (*cp.CreateEmailMailboxDraftResponse, error) {
	format, content, err := mailboxContent(request.GetContent())
	if err != nil {
		return nil, transportError(err)
	}
	view, err := server.mailboxCommand(ctx, cp.PlatformCommandService_CreateEmailMailboxDraft_FullMethodName, command.CreateEmailMailboxDraft, request.GetMutation(),
		command.EmailMailboxInput{ConnectionRef: request.GetConnectionRef(), Managed: command.ManagedConfigurationInput{
			ConfigurationRef: request.GetConfigurationRef(), Name: request.GetName(), ContentFormat: format, Content: content}})
	if err != nil {
		return nil, err
	}
	return &cp.CreateEmailMailboxDraftResponse{Configuration: view}, nil
}

// SaveEmailMailboxDraft converts draft content and passes configuration and revision locators
// through the dedicated mailbox save command.
// SaveEmailMailboxDraft преобразует содержимое черновика и передаёт ссылки конфигурации и ревизии
// отдельной команде сохранения ящика.
func (server *Server) SaveEmailMailboxDraft(ctx context.Context, request *cp.SaveEmailMailboxDraftRequest) (*cp.SaveEmailMailboxDraftResponse, error) {
	format, content, err := mailboxContent(request.GetContent())
	if err != nil {
		return nil, transportError(err)
	}
	view, err := server.mailboxCommand(ctx, cp.PlatformCommandService_SaveEmailMailboxDraft_FullMethodName, command.SaveEmailMailboxDraft, request.GetMutation(),
		command.EmailMailboxInput{Managed: command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef(), ContentFormat: format, Content: content}})
	if err != nil {
		return nil, err
	}
	return &cp.SaveEmailMailboxDraftResponse{Configuration: view}, nil
}

// ValidateEmailMailboxDraft passes configuration and revision locators to the dedicated validation
// command and returns its mailbox view.
// ValidateEmailMailboxDraft передаёт ссылки конфигурации и ревизии отдельной команде проверки и
// возвращает её представление ящика.
func (server *Server) ValidateEmailMailboxDraft(ctx context.Context, request *cp.ValidateEmailMailboxDraftRequest) (*cp.ValidateEmailMailboxDraftResponse, error) {
	view, err := server.mailboxCommand(ctx, cp.PlatformCommandService_ValidateEmailMailboxDraft_FullMethodName, command.ValidateEmailMailboxDraft, request.GetMutation(),
		command.EmailMailboxInput{Managed: command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef()}})
	if err != nil {
		return nil, err
	}
	return &cp.ValidateEmailMailboxDraftResponse{Configuration: view}, nil
}

// PublishEmailMailboxDraft passes configuration and revision locators to the dedicated publication
// command without performing external publication in transport.
// PublishEmailMailboxDraft передаёт ссылки конфигурации и ревизии отдельной команде публикации без
// внешней публикации силами transport.
func (server *Server) PublishEmailMailboxDraft(ctx context.Context, request *cp.PublishEmailMailboxDraftRequest) (*cp.PublishEmailMailboxDraftResponse, error) {
	view, err := server.mailboxCommand(ctx, cp.PlatformCommandService_PublishEmailMailboxDraft_FullMethodName, command.PublishEmailMailboxDraft, request.GetMutation(),
		command.EmailMailboxInput{Managed: command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef()}})
	if err != nil {
		return nil, err
	}
	return &cp.PublishEmailMailboxDraftResponse{Configuration: view}, nil
}

// DiscardEmailMailboxDraft executes the dedicated draft discard command for the supplied
// configuration and revision and returns its view.
// DiscardEmailMailboxDraft выполняет отдельную команду отбрасывания черновика заданных
// конфигурации и ревизии и возвращает её представление.
func (server *Server) DiscardEmailMailboxDraft(ctx context.Context, request *cp.DiscardEmailMailboxDraftRequest) (*cp.DiscardEmailMailboxDraftResponse, error) {
	view, err := server.mailboxCommand(ctx, cp.PlatformCommandService_DiscardEmailMailboxDraft_FullMethodName, command.DiscardEmailMailboxDraft, request.GetMutation(),
		command.EmailMailboxInput{Managed: command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef()}})
	if err != nil {
		return nil, err
	}
	return &cp.DiscardEmailMailboxDraftResponse{Configuration: view}, nil
}

// BindEmailMailboxConfiguration passes exact connection version and configuration/revision
// locators through the dedicated binding command.
// BindEmailMailboxConfiguration передаёт точную версию подключения и ссылки конфигурации и ревизии
// отдельной команде привязки.
func (server *Server) BindEmailMailboxConfiguration(ctx context.Context, request *cp.BindEmailMailboxConfigurationRequest) (*cp.BindEmailMailboxConfigurationResponse, error) {
	view, err := server.mailboxCommand(ctx, cp.PlatformCommandService_BindEmailMailboxConfiguration_FullMethodName, command.BindEmailMailboxConfiguration, request.GetMutation(),
		command.EmailMailboxInput{ConnectionRef: request.GetConnectionRef(), ExpectedConnectionVersion: request.GetExpectedConnectionVersion(),
			Managed: command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef()}})
	if err != nil {
		return nil, err
	}
	return &cp.BindEmailMailboxConfigurationResponse{Configuration: view}, nil
}

// UnbindEmailMailboxConfiguration uses the verified unbind principal and supplied mutation,
// requires publication metadata and returns the resulting connection version.
// UnbindEmailMailboxConfiguration использует проверенный principal отвязки и переданную mutation,
// требует метаданные публикации и возвращает полученную версию подключения.
func (server *Server) UnbindEmailMailboxConfiguration(ctx context.Context, request *cp.UnbindEmailMailboxConfigurationRequest) (*cp.UnbindEmailMailboxConfigurationResponse, error) {
	p, err := principal(ctx, cp.PlatformCommandService_UnbindEmailMailboxConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: command.UnbindEmailMailboxConfiguration, Principal: p, Mutation: mutation(request.GetMutation()), Payload: command.EmailMailboxInput{ConnectionRef: request.GetConnectionRef()}})
	if err != nil {
		return nil, transportError(err)
	}
	if result.EmailPublication == nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &cp.UnbindEmailMailboxConfigurationResponse{Publication: castMailboxView(entity.EmailMailboxConfigurationView{Publication: result.EmailPublication}).Publication, ConnectionVersion: result.EmailConnectionVersion}, nil
}

// ReportEmailConfigurationReadback passes the worker revision and digest to the authorized domain
// readback report and acknowledges only successful acceptance.
// ReportEmailConfigurationReadback передаёт ревизию и digest worker авторизованному доменному
// отчёту о readback и подтверждает только успешное принятие.
func (server *Server) ReportEmailConfigurationReadback(ctx context.Context, request *cp.ReportEmailConfigurationReadbackRequest) (*cp.ReportEmailConfigurationReadbackResponse, error) {
	p, err := principal(ctx, cp.RuntimeWorkService_ReportEmailConfigurationReadback_FullMethodName)
	if err != nil {
		return nil, err
	}
	if err := server.service.ReportEmailConfigurationReadback(ctx, p, request.GetRevision(), request.GetDigest()); err != nil {
		return nil, transportError(err)
	}
	return &cp.ReportEmailConfigurationReadbackResponse{Accepted: true}, nil
}
