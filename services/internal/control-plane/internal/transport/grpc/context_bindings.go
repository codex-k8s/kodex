package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// castContextBinding projects the domain binding reference, version, agent, resource, revision and
// digest without performing binding validation.
// castContextBinding преобразует ссылку, версию, агента, ресурс, ревизию и digest доменной
// привязки без проверки её допустимости.
func castContextBinding(input entity.AgentContextBinding) *controlplanev1.AgentContextBinding {
	return &controlplanev1.AgentContextBinding{Ref: input.Ref, Version: input.Version, AgentRef: input.AgentRef, ResourceRef: input.ResourceRef, RevisionRef: input.RevisionRef, Digest: input.Digest}
}

// BindAgentSkillBundle passes exact agent, bundle, revision and expected binding version through
// the dedicated bind command and requires a valid returned binding.
// BindAgentSkillBundle передаёт точные агента, bundle, ревизию и ожидаемую версию связи отдельной
// команде привязки и требует допустимую связь в результате.
func (server *Server) BindAgentSkillBundle(ctx context.Context, request *controlplanev1.BindAgentSkillBundleRequest) (*controlplanev1.BindAgentSkillBundleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_BindAgentSkillBundle_FullMethodName, command.BindAgentSkillBundle, request.GetMutation(), command.AgentContextBindingInput{AgentRef: request.GetAgentRef(), ResourceRef: request.GetBundleRef(), RevisionRef: request.GetRevisionRef(), ExpectedBindingVersion: request.GetExpectedBindingVersion()})
	if err != nil {
		return nil, err
	}
	binding, err := castContextBindingResult(result.ContextBinding)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.BindAgentSkillBundleResponse{Binding: binding}, nil
}

// UnbindAgentSkillBundle passes skill-bundle selection pins through the dedicated unbind command
// and rejects incomplete owner binding results.
// UnbindAgentSkillBundle передаёт привязки выбора skill bundle отдельной команде отвязки и
// отклоняет неполный результат связи владельца.
func (server *Server) UnbindAgentSkillBundle(ctx context.Context, request *controlplanev1.UnbindAgentSkillBundleRequest) (*controlplanev1.UnbindAgentSkillBundleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_UnbindAgentSkillBundle_FullMethodName, command.UnbindAgentSkillBundle, request.GetMutation(), command.AgentContextBindingInput{AgentRef: request.GetAgentRef(), ResourceRef: request.GetBundleRef(), RevisionRef: request.GetRevisionRef(), ExpectedBindingVersion: request.GetExpectedBindingVersion()})
	if err != nil {
		return nil, err
	}
	binding, err := castContextBindingResult(result.ContextBinding)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UnbindAgentSkillBundleResponse{Binding: binding}, nil
}

// BindAgentMemoryRecord passes exact agent, memory record, revision and expected binding version
// through the dedicated bind command and validates its returned binding.
// BindAgentMemoryRecord передаёт точные агента, запись памяти, ревизию и ожидаемую версию связи
// отдельной команде привязки и проверяет возвращённую связь.
func (server *Server) BindAgentMemoryRecord(ctx context.Context, request *controlplanev1.BindAgentMemoryRecordRequest) (*controlplanev1.BindAgentMemoryRecordResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_BindAgentMemoryRecord_FullMethodName, command.BindAgentMemoryRecord, request.GetMutation(), command.AgentContextBindingInput{AgentRef: request.GetAgentRef(), ResourceRef: request.GetRecordRef(), RevisionRef: request.GetRevisionRef(), ExpectedBindingVersion: request.GetExpectedBindingVersion()})
	if err != nil {
		return nil, err
	}
	binding, err := castContextBindingResult(result.ContextBinding)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.BindAgentMemoryRecordResponse{Binding: binding}, nil
}

// UnbindAgentMemoryRecord executes the dedicated memory unbind command with version pins and
// requires a nonempty positive-version owner result.
// UnbindAgentMemoryRecord выполняет отдельную команду отвязки памяти с привязками версий и требует
// непустой результат владельца с положительной версией.
func (server *Server) UnbindAgentMemoryRecord(ctx context.Context, request *controlplanev1.UnbindAgentMemoryRecordRequest) (*controlplanev1.UnbindAgentMemoryRecordResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_UnbindAgentMemoryRecord_FullMethodName, command.UnbindAgentMemoryRecord, request.GetMutation(), command.AgentContextBindingInput{AgentRef: request.GetAgentRef(), ResourceRef: request.GetRecordRef(), RevisionRef: request.GetRevisionRef(), ExpectedBindingVersion: request.GetExpectedBindingVersion()})
	if err != nil {
		return nil, err
	}
	binding, err := castContextBindingResult(result.ContextBinding)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UnbindAgentMemoryRecordResponse{Binding: binding}, nil
}

// castContextBindingResult requires the owner's binding identity and positive version before
// applying the ordinary binding projection; malformed results retain the existing Internal status.
// castContextBindingResult требует identity связи владельца и положительную версию до обычного
// преобразования связи; неверный результат сохраняет прежний status Internal.
func castContextBindingResult(input *entity.AgentContextBinding) (*controlplanev1.AgentContextBinding, error) {
	if input == nil || input.Ref == "" || input.Version < 1 {
		return nil, status.Error(codes.Internal, "context binding result is incomplete")
	}
	return castContextBinding(*input), nil
}
