package httptransport

import (
	"context"
	"errors"
	"strings"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

const maximumWorkspaceSelectorScan = 1000

// resolveWorkspaceSelector разрешает только owner-visible stable selector. Raw
// resource ID и private locator никогда не принимаются browser contract.
func (server *Server) resolveWorkspaceSelector(
	ctx context.Context,
	kind controlplanev1.ResourceKind,
	selector string,
) (*controlplanev1.Resource, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" || len(selector) > 160 {
		return nil, errors.New("workspace selector is invalid")
	}
	if _, err := uuid.Parse(selector); err == nil {
		return nil, errors.New("workspace selector must not be a raw resource identifier")
	}
	pageToken := ""
	scanned := 0
	var match *controlplanev1.Resource
	for {
		page, err := server.control.ListResources(ctx, &controlplanev1.ListResourcesRequest{
			Kind: kind,
			States: []controlplanev1.LifecycleState{
				controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE,
				controlplanev1.LifecycleState_LIFECYCLE_STATE_PAUSED,
			},
			PageSize:  100,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, candidate := range page.GetResources() {
			scanned++
			if scanned > maximumWorkspaceSelectorScan {
				return nil, errors.New("workspace selector catalog is too large")
			}
			if workspaceStableSelector(candidate) != selector && candidate.GetName() != selector {
				continue
			}
			if match != nil && match.GetId() != candidate.GetId() {
				return nil, errors.New("workspace selector is ambiguous")
			}
			match = candidate
		}
		next := page.GetNextPageToken()
		if next == "" {
			break
		}
		if next == pageToken {
			return nil, errors.New("workspace selector pagination is invalid")
		}
		pageToken = next
	}
	if match == nil || match.GetKind() != kind {
		return nil, errors.New("workspace selector is unavailable")
	}
	return match, nil
}

func workspaceStableSelector(resource *controlplanev1.Resource) string {
	if resource == nil || resource.GetSpec() == nil {
		return ""
	}
	switch value := resource.GetSpec().GetValue().(type) {
	case *controlplanev1.ResourceSpec_Chat:
		return value.Chat.GetStableKey()
	case *controlplanev1.ResourceSpec_Team:
		return value.Team.GetStableKey()
	case *controlplanev1.ResourceSpec_Role:
		return value.Role.GetStableKey()
	case *controlplanev1.ResourceSpec_RoleDefinition:
		return value.RoleDefinition.GetStableKey()
	case *controlplanev1.ResourceSpec_Agent:
		return value.Agent.GetStableKey()
	case *controlplanev1.ResourceSpec_InstructionSet:
		return value.InstructionSet.GetStableKey()
	case *controlplanev1.ResourceSpec_ProviderConnectionReference:
		return value.ProviderConnectionReference.GetStableKey()
	case *controlplanev1.ResourceSpec_ProviderPool:
		return value.ProviderPool.GetStableKey()
	default:
		return resource.GetName()
	}
}

func (server *Server) resolveWorkspaceSelectors(
	ctx context.Context,
	kind controlplanev1.ResourceKind,
	selectors []string,
) ([]string, error) {
	result := make([]string, 0, len(selectors))
	seen := make(map[string]struct{}, len(selectors))
	for _, selector := range selectors {
		resource, err := server.resolveWorkspaceSelector(ctx, kind, selector)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[resource.GetId()]; duplicate {
			return nil, errors.New("workspace selector resolves to a duplicate")
		}
		seen[resource.GetId()] = struct{}{}
		result = append(result, resource.GetId())
	}
	return result, nil
}

func (server *Server) currentWorkspaceResource(
	ctx context.Context,
	resourceID string,
	kind controlplanev1.ResourceKind,
) (*controlplanev1.Resource, error) {
	if resourceID == "" {
		return nil, errors.New("workspace source selector is required")
	}
	response, err := server.control.GetResource(ctx, &controlplanev1.GetResourceRequest{
		ResourceId: resourceID, ExpectedKind: kind,
	})
	if err != nil {
		return nil, err
	}
	if response.GetResource().GetKind() != kind {
		return nil, errors.New("workspace current resource kind is invalid")
	}
	return response.GetResource(), nil
}

func (server *Server) currentOrSelectedWorkspaceResource(
	ctx context.Context,
	resourceID string,
	kind controlplanev1.ResourceKind,
	selector *string,
) (*controlplanev1.Resource, error) {
	if selector != nil && *selector != "" {
		return server.resolveWorkspaceSelector(ctx, kind, *selector)
	}
	return server.currentWorkspaceResource(ctx, resourceID, kind)
}

func (server *Server) resolveCredentialSelector(
	ctx context.Context,
	kind generated.CredentialBindingSourceKind,
	selector string,
) (*controlplanev1.CredentialBindingSpec, error) {
	if kind == generated.CredentialBindingSourceKindPROVIDERCONNECTIONREFERENCE {
		provider, err := server.resolveWorkspaceSelector(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_PROVIDER_CONNECTION_REFERENCE, selector)
		if err != nil {
			return nil, err
		}
		bindingID := provider.GetSpec().GetProviderConnectionReference().GetCredentialBindingId()
		if bindingID == "" {
			return nil, errors.New("provider credential selection is unavailable")
		}
		binding, getErr := server.currentWorkspaceResource(ctx, bindingID, controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING)
		if getErr != nil {
			return nil, getErr
		}
		if value := binding.GetSpec().GetCredentialBinding(); value != nil {
			return value, nil
		}
		return nil, errors.New("provider credential selection is unavailable")
	}
	if kind != generated.CredentialBindingSourceKindCREDENTIALBINDING {
		return nil, errors.New("credential source kind is invalid")
	}
	binding, err := server.resolveWorkspaceSelector(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING, selector)
	if err != nil || binding.GetSpec().GetCredentialBinding() == nil {
		return nil, errors.New("credential selector is unavailable")
	}
	return binding.GetSpec().GetCredentialBinding(), nil
}

func (server *Server) resolveCredentialIDs(ctx context.Context, selectors []string) ([]string, error) {
	result := make([]string, 0, len(selectors))
	seen := make(map[string]struct{}, len(selectors))
	for _, selector := range selectors {
		resource, err := server.resolveWorkspaceSelector(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING, selector)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[resource.GetId()]; duplicate {
			return nil, errors.New("credential selector resolves to a duplicate")
		}
		seen[resource.GetId()] = struct{}{}
		result = append(result, resource.GetId())
	}
	return result, nil
}

func (server *Server) mutableWorkspaceSpec(
	ctx context.Context,
	resourceID string,
	kind generated.MutableResourceKind,
	input generated.ResourceSpecInput,
) (controlplanev1.ResourceKind, *controlplanev1.ResourceSpec, error) {
	count := 0
	for _, present := range []bool{input.Chat != nil, input.CredentialBinding != nil, input.RepositoryWorkspace != nil, input.Integration != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return 0, nil, errors.New("resource spec cardinality is invalid")
	}
	switch kind {
	case generated.MutableResourceKindCHAT:
		if input.Chat == nil {
			break
		}
		room := map[generated.ChatRoomType]controlplanev1.RoomType{generated.USER: controlplanev1.RoomType_ROOM_TYPE_USER, generated.COORDINATION: controlplanev1.RoomType_ROOM_TYPE_COORDINATION, generated.WORKCONTROL: controlplanev1.RoomType_ROOM_TYPE_WORK_CONTROL, generated.RUNS: controlplanev1.RoomType_ROOM_TYPE_RUNS}[input.Chat.RoomType]
		if room == 0 {
			break
		}
		defaultAgentID := ""
		if input.Chat.DefaultAgentSelector != nil {
			agent, err := server.resolveWorkspaceSelector(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_AGENT, *input.Chat.DefaultAgentSelector)
			if err != nil {
				return 0, nil, err
			}
			defaultAgentID = agent.GetId()
		} else if resourceID != "" {
			current, err := server.currentWorkspaceResource(ctx, resourceID, controlplanev1.ResourceKind_RESOURCE_KIND_CHAT)
			if err != nil {
				return 0, nil, err
			}
			defaultAgentID = current.GetSpec().GetChat().GetDefaultAgentId()
		}
		externalChannelRef := ""
		if input.Chat.ChannelSelector != nil {
			channel, err := server.resolveWorkspaceSelector(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_CHAT, *input.Chat.ChannelSelector)
			if err != nil {
				return 0, nil, err
			}
			externalChannelRef = channel.GetSpec().GetChat().GetExternalChannelRef()
		} else if resourceID != "" {
			current, err := server.currentWorkspaceResource(ctx, resourceID, controlplanev1.ResourceKind_RESOURCE_KIND_CHAT)
			if err != nil {
				return 0, nil, err
			}
			externalChannelRef = current.GetSpec().GetChat().GetExternalChannelRef()
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_CHAT, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Chat{Chat: &controlplanev1.ChatSpec{StableKey: input.Chat.StableKey, RoomType: room, DefaultAgentId: defaultAgentID, ExternalChannelRef: externalChannelRef, WorkPolicy: input.Chat.WorkPolicy, Ownership: uiOwnership()}}}, nil
	case generated.MutableResourceKindCREDENTIALBINDING:
		if input.CredentialBinding == nil || input.CredentialBinding.Revision < 1 {
			break
		}
		var source *controlplanev1.CredentialBindingSpec
		var err error
		if input.CredentialBinding.SourceSelector != nil && input.CredentialBinding.SourceKind != nil {
			source, err = server.resolveCredentialSelector(ctx, *input.CredentialBinding.SourceKind, *input.CredentialBinding.SourceSelector)
		} else if input.CredentialBinding.SourceSelector != nil || input.CredentialBinding.SourceKind != nil {
			err = errors.New("credential source selector is incomplete")
		} else {
			current, currentErr := server.currentWorkspaceResource(ctx, resourceID, controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING)
			err = currentErr
			if current != nil {
				source = current.GetSpec().GetCredentialBinding()
			}
		}
		if err != nil || source == nil {
			return 0, nil, errors.New("credential source selector is unavailable")
		}
		binding := proto.Clone(source).(*controlplanev1.CredentialBindingSpec)
		binding.Purpose, binding.Revision, binding.Ownership = input.CredentialBinding.Purpose, uint64(input.CredentialBinding.Revision), uiOwnership()
		return controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_CredentialBinding{CredentialBinding: binding}}, nil
	case generated.MutableResourceKindREPOSITORYWORKSPACE:
		if input.RepositoryWorkspace == nil {
			break
		}
		base, err := server.currentOrSelectedWorkspaceResource(ctx, resourceID, controlplanev1.ResourceKind_RESOURCE_KIND_REPOSITORY_WORKSPACE, input.RepositoryWorkspace.RepositorySelector)
		if err != nil || base.GetSpec().GetRepositoryWorkspace() == nil {
			return 0, nil, errors.New("repository selector is unavailable")
		}
		repository := proto.Clone(base.GetSpec().GetRepositoryWorkspace()).(*controlplanev1.RepositoryWorkspaceSpec)
		repository.WorkspaceMode, repository.DefaultBranch, repository.Ownership = input.RepositoryWorkspace.WorkspaceMode, input.RepositoryWorkspace.DefaultBranch, uiOwnership()
		if input.RepositoryWorkspace.CredentialBindingSelector != nil {
			binding, bindingErr := server.resolveWorkspaceSelector(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING, *input.RepositoryWorkspace.CredentialBindingSelector)
			if bindingErr != nil {
				return 0, nil, bindingErr
			}
			repository.CredentialBindingId = binding.GetId()
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_REPOSITORY_WORKSPACE, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_RepositoryWorkspace{RepositoryWorkspace: repository}}, nil
	case generated.MutableResourceKindINTEGRATION:
		if input.Integration == nil || input.Integration.DefinitionVersion < 1 {
			break
		}
		base, err := server.currentOrSelectedWorkspaceResource(ctx, resourceID, controlplanev1.ResourceKind_RESOURCE_KIND_INTEGRATION, input.Integration.SourceSelector)
		if err != nil || base.GetSpec().GetIntegration() == nil {
			return 0, nil, errors.New("integration source selector is unavailable")
		}
		credentialIDs, err := server.resolveCredentialIDs(ctx, input.Integration.CredentialBindingSelectors)
		if err != nil {
			return 0, nil, err
		}
		integration := proto.Clone(base.GetSpec().GetIntegration()).(*controlplanev1.IntegrationSpec)
		integration.DefinitionRef, integration.DefinitionVersion = input.Integration.DefinitionRef, uint64(input.Integration.DefinitionVersion)
		integration.Capabilities, integration.CredentialBindingIds, integration.Ownership = append([]string(nil), input.Integration.Capabilities...), credentialIDs, uiOwnership()
		return controlplanev1.ResourceKind_RESOURCE_KIND_INTEGRATION, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Integration{Integration: integration}}, nil
	}
	return 0, nil, errors.New("resource kind and spec mismatch")
}

func (server *Server) accessWorkspaceSpec(
	ctx context.Context,
	resourceID string,
	kind generated.AccessResourceKind,
	input generated.AccessSpecInput,
) (controlplanev1.ResourceKind, *controlplanev1.ResourceSpec, error) {
	count := 0
	for _, present := range []bool{input.Team != nil, input.Role != nil, input.PromptProfile != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return 0, nil, errors.New("access spec cardinality is invalid")
	}
	switch kind {
	case generated.AccessResourceKindTEAM:
		if input.Team == nil {
			break
		}
		externalTeamRef := ""
		if input.Team.SourceSelector != nil {
			source, err := server.resolveWorkspaceSelector(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_TEAM, *input.Team.SourceSelector)
			if err != nil {
				return 0, nil, err
			}
			externalTeamRef = source.GetSpec().GetTeam().GetExternalTeamRef()
		} else if resourceID != "" {
			current, err := server.currentWorkspaceResource(ctx, resourceID, controlplanev1.ResourceKind_RESOURCE_KIND_TEAM)
			if err != nil {
				return 0, nil, err
			}
			externalTeamRef = current.GetSpec().GetTeam().GetExternalTeamRef()
		}
		members, err := server.resolveWorkspaceSelectors(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_AGENT, input.Team.MemberActorSelectors)
		if err != nil {
			return 0, nil, err
		}
		roles, err := server.resolveWorkspaceSelectors(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_ROLE, input.Team.RoleSelectors)
		if err != nil {
			return 0, nil, err
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_TEAM, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Team{Team: &controlplanev1.TeamSpec{StableKey: input.Team.StableKey, ExternalTeamRef: externalTeamRef, MemberActorIds: members, RoleIds: roles, Ownership: uiOwnership()}}}, nil
	case generated.AccessResourceKindROLE:
		if input.Role == nil {
			break
		}
		allowed, err := server.resolveWorkspaceSelectors(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_ROLE, input.Role.AllowedTargetRoleSelectors)
		if err != nil {
			return 0, nil, err
		}
		promptID := ""
		if input.Role.PromptProfileSelector != nil {
			prompt, promptErr := server.resolveWorkspaceSelector(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_PROMPT_PROFILE, *input.Role.PromptProfileSelector)
			if promptErr != nil {
				return 0, nil, promptErr
			}
			promptID = prompt.GetId()
		} else if resourceID != "" {
			current, currentErr := server.currentWorkspaceResource(ctx, resourceID, controlplanev1.ResourceKind_RESOURCE_KIND_ROLE)
			if currentErr != nil {
				return 0, nil, currentErr
			}
			promptID = current.GetSpec().GetRole().GetPromptProfileId()
		}
		recipe, err := server.resolveWorkspaceSelector(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_ROLE_IMAGE_RECIPE, input.Role.RoleImageRecipeSelector)
		if err != nil {
			return 0, nil, err
		}
		credentials, err := server.resolveCredentialIDs(ctx, input.Role.ProviderCredentialBindingSelectors)
		if err != nil {
			return 0, nil, err
		}
		repositories, err := server.resolveWorkspaceSelectors(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_REPOSITORY_WORKSPACE, input.Role.RepositoryWorkspaceSelectors)
		if err != nil {
			return 0, nil, err
		}
		integrations, err := server.resolveWorkspaceSelectors(ctx, controlplanev1.ResourceKind_RESOURCE_KIND_INTEGRATION, input.Role.IntegrationSelectors)
		if err != nil {
			return 0, nil, err
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_ROLE, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Role{Role: &controlplanev1.RoleSpec{StableKey: input.Role.StableKey, Capabilities: append([]string(nil), input.Role.Capabilities...), AllowedTargetRoleIds: allowed, PromptProfileId: promptID, RoleImageRecipeId: recipe.GetId(), ProviderCredentialBindingIds: credentials, RepositoryWorkspaceIds: repositories, IntegrationIds: integrations, Ownership: uiOwnership()}}}, nil
	case generated.AccessResourceKindPROMPTPROFILE:
		if input.PromptProfile == nil || input.PromptProfile.Revision < 1 {
			break
		}
		base, err := server.currentOrSelectedWorkspaceResource(ctx, resourceID, controlplanev1.ResourceKind_RESOURCE_KIND_PROMPT_PROFILE, input.PromptProfile.SourceSelector)
		if err != nil || base.GetSpec().GetPromptProfile() == nil {
			return 0, nil, errors.New("prompt source selector is unavailable")
		}
		prompt := proto.Clone(base.GetSpec().GetPromptProfile()).(*controlplanev1.PromptProfileSpec)
		prompt.Revision, prompt.ContentSha256, prompt.Locale, prompt.Ownership = uint64(input.PromptProfile.Revision), input.PromptProfile.ContentSha256, input.PromptProfile.Locale, uiOwnership()
		return controlplanev1.ResourceKind_RESOURCE_KIND_PROMPT_PROFILE, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_PromptProfile{PromptProfile: prompt}}, nil
	}
	return 0, nil, errors.New("access kind and spec mismatch")
}
