package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
)

const (
	interactionCapabilityContextKey     = "capability"
	interactionCapabilityPostBindingKey = "capability_post_binding"
	interactionKindAction               = "action"
	interactionKindDialog               = "dialog"
	interactionDialogCallbackResult     = "agents_dialog_result"
	defaultInstallationScope            = "single-installation"
	defaultWorkspaceScope               = "installation-root"
	defaultInteractionCapabilityTTL     = 4 * time.Hour
)

var (
	ErrInteractionCapabilityMissing = errors.New("interaction capability is missing")
	ErrInteractionAuthentication    = errors.New("interaction callback authentication failed")
	ErrInteractionAdmissionDenied   = errors.New("interaction callback admission denied")
	ErrInteractionAdmissionUnknown  = errors.New("interaction callback admission indeterminate")
	ErrInteractionPreparation       = errors.New("interaction callback preparation failed")
)

type AuthenticatedActor struct {
	UserID   string
	UserName string
}

type InteractionScope struct {
	Installation string
	Workspace    string
	Session      string
}

type MattermostCardInteraction struct {
	Actor AuthenticatedActor
	Scope InteractionScope
}

type AdmissionStatus string

const (
	AdmissionAllowed       AdmissionStatus = "allowed"
	AdmissionDenied        AdmissionStatus = "denied"
	AdmissionIndeterminate AdmissionStatus = "indeterminate"
)

type InteractionAdmissionRequest struct {
	ActionKey    string
	Operation    string
	ResourceType string
	ResourceID   string
	Actor        AuthenticatedActor
	Scope        InteractionScope
	ChannelID    string
	PostID       string
}

type InteractionAdmissionDecision struct {
	Status AdmissionStatus
	Reason string
}

type InteractionAdmission interface {
	Admit(ctx context.Context, request InteractionAdmissionRequest) InteractionAdmissionDecision
}

type MattermostInteractionActorVerifier interface {
	VerifyInteractionActor(ctx context.Context, userID string, channelID string) (bool, error)
}

type serverSideInteractionAdmission struct {
	installationScope string
	actorVerifier     MattermostInteractionActorVerifier
	resources         securityrepo.InteractionResourceAdmissionRepository
}

func NewServerSideInteractionAdmission(installationScope string, actorVerifier MattermostInteractionActorVerifier, resources securityrepo.InteractionResourceAdmissionRepository) InteractionAdmission {
	installationScope = strings.TrimSpace(installationScope)
	if installationScope == "" {
		installationScope = defaultInstallationScope
	}
	return &serverSideInteractionAdmission{
		installationScope: installationScope,
		actorVerifier:     actorVerifier,
		resources:         resources,
	}
}

func (admission *serverSideInteractionAdmission) Admit(ctx context.Context, request InteractionAdmissionRequest) InteractionAdmissionDecision {
	if admission == nil || admission.actorVerifier == nil || admission.resources == nil {
		return InteractionAdmissionDecision{Status: AdmissionIndeterminate, Reason: "admission_backend_missing"}
	}
	if request.ActionKey != "mattermost.callback.action" && request.ActionKey != "mattermost.callback.dialog" {
		return InteractionAdmissionDecision{Status: AdmissionIndeterminate, Reason: "unknown_action"}
	}
	if strings.TrimSpace(request.Actor.UserID) == "" || strings.TrimSpace(request.ChannelID) == "" || strings.TrimSpace(request.PostID) == "" {
		return InteractionAdmissionDecision{Status: AdmissionDenied, Reason: "verified_subject_scope_missing"}
	}
	if request.Scope.Installation != admission.installationScope {
		return InteractionAdmissionDecision{Status: AdmissionDenied, Reason: "installation_scope_mismatch"}
	}
	if !validInteractionScope(request.Scope.Workspace) || (request.Scope.Session != "" && !validInteractionScope(request.Scope.Session)) {
		return InteractionAdmissionDecision{Status: AdmissionDenied, Reason: "scope_invalid"}
	}
	if !typedInteractionOperationAllowed(request) {
		return InteractionAdmissionDecision{Status: AdmissionDenied, Reason: "operation_resource_denied"}
	}
	verified, err := admission.actorVerifier.VerifyInteractionActor(ctx, request.Actor.UserID, request.ChannelID)
	if err != nil {
		return InteractionAdmissionDecision{Status: AdmissionIndeterminate, Reason: "subject_verification_failed"}
	}
	if !verified {
		return InteractionAdmissionDecision{Status: AdmissionDenied, Reason: "subject_not_channel_member"}
	}
	allowed, err := admission.resources.AdmitInteractionResource(ctx, securityrepo.InteractionResourceAdmissionInput{
		ActionKey: request.ActionKey, Operation: request.Operation,
		ResourceType: request.ResourceType, ResourceID: request.ResourceID,
		ActorUserID: request.Actor.UserID, ChannelID: request.ChannelID, PostID: request.PostID,
		Installation: request.Scope.Installation, Workspace: request.Scope.Workspace, Session: request.Scope.Session,
	})
	if err != nil {
		return InteractionAdmissionDecision{Status: AdmissionIndeterminate, Reason: "resource_verification_failed"}
	}
	if !allowed {
		return InteractionAdmissionDecision{Status: AdmissionDenied, Reason: "resource_scope_denied"}
	}
	return InteractionAdmissionDecision{Status: AdmissionAllowed, Reason: "verified_server_side_grant"}
}

type InteractionSecurityConfig struct {
	Repository        securityrepo.Repository
	Admission         InteractionAdmission
	InstallationScope string
	CapabilityTTL     time.Duration
	Random            io.Reader
	Now               func() time.Time
}

type InteractionSecurityService struct {
	repository        securityrepo.Repository
	admission         InteractionAdmission
	installationScope string
	capabilityTTL     time.Duration
	random            io.Reader
	now               func() time.Time
}

type AuthenticatedInteraction struct {
	Actor          AuthenticatedActor
	Scope          InteractionScope
	Kind           string
	Operation      string
	ResourceType   string
	ResourceID     string
	ChannelID      string
	PostBinding    string
	CallbackPostID string
}

type ActionCallback struct {
	Context   map[string]any
	UserID    string
	ChannelID string
	PostID    string
}

type DialogCallback struct {
	CallbackID string
	State      string
	UserID     string
	ChannelID  string
}

func NewInteractionSecurityService(cfg InteractionSecurityConfig) *InteractionSecurityService {
	installationScope := strings.TrimSpace(cfg.InstallationScope)
	if installationScope == "" {
		installationScope = defaultInstallationScope
	}
	capabilityTTL := cfg.CapabilityTTL
	if capabilityTTL <= 0 {
		capabilityTTL = defaultInteractionCapabilityTTL
	}
	random := cfg.Random
	if random == nil {
		random = rand.Reader
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	admission := cfg.Admission
	if admission == nil {
		admission = denyAllInteractionAdmission{}
	}
	return &InteractionSecurityService{
		repository:        cfg.Repository,
		admission:         admission,
		installationScope: installationScope,
		capabilityTTL:     capabilityTTL,
		random:            random,
		now:               now,
	}
}

type denyAllInteractionAdmission struct{}

func (denyAllInteractionAdmission) Admit(context.Context, InteractionAdmissionRequest) InteractionAdmissionDecision {
	return InteractionAdmissionDecision{Status: AdmissionIndeterminate, Reason: "admission_not_configured"}
}

func (svc *InteractionSecurityService) SealCard(ctx context.Context, card *MattermostCard, actor AuthenticatedActor, scope InteractionScope) error {
	return svc.sealCard(ctx, card, actor, scope, securityrepo.CapabilityStateUnused)
}

func (svc *InteractionSecurityService) SealCardPending(ctx context.Context, card *MattermostCard, actor AuthenticatedActor, scope InteractionScope) error {
	return svc.sealCard(ctx, card, actor, scope, securityrepo.CapabilityStatePending)
}

func (svc *InteractionSecurityService) sealCard(ctx context.Context, card *MattermostCard, actor AuthenticatedActor, scope InteractionScope, state securityrepo.CapabilityState) error {
	if card == nil || len(card.Actions) == 0 {
		return nil
	}
	if svc == nil || svc.repository == nil || strings.TrimSpace(actor.UserID) == "" || strings.TrimSpace(card.ChannelID) == "" {
		card.Actions = nil
		return ErrInteractionAuthentication
	}
	scope = svc.normalizeScope(scope)
	postBinding := strings.TrimSpace(card.PostID)
	if postBinding == "" {
		card.Actions = nil
		return ErrInteractionAuthentication
	}
	sealed := make([]MattermostCardAction, 0, len(card.Actions))
	for _, action := range card.Actions {
		contextCopy := cloneInteractionContext(action.Context)
		contextCopy[interactionCapabilityPostBindingKey] = postBinding
		operation := actionCallbackOperation(contextCopy)
		resourceType, resourceID := interactionResource(contextCopy)
		token, err := svc.issue(ctx, securityrepo.IssueCapabilityInput{
			Kind:              interactionKindAction,
			Operation:         operation,
			ResourceType:      resourceType,
			ResourceID:        resourceID,
			ChannelID:         strings.TrimSpace(card.ChannelID),
			PostBinding:       postBinding,
			ActorUserID:       strings.TrimSpace(actor.UserID),
			ActorUserName:     strings.TrimSpace(actor.UserName),
			InstallationScope: scope.Installation,
			WorkspaceScope:    scope.Workspace,
			SessionScope:      scope.Session,
			State:             state,
		}, contextCopy)
		if err != nil {
			card.Actions = sealed
			revokeErr := svc.transitionCardCapabilities(ctx, *card, state, securityrepo.CapabilityStateRevoked)
			card.Actions = nil
			return errors.Join(err, revokeErr)
		}
		contextCopy[interactionCapabilityContextKey] = token
		action.Context = contextCopy
		sealed = append(sealed, action)
	}
	card.Actions = sealed
	return nil
}

func (svc *InteractionSecurityService) SealDialog(ctx context.Context, dialog *MattermostDialog, interaction AuthenticatedInteraction) error {
	if dialog == nil {
		return nil
	}
	state, err := interactionState(dialog.State)
	if err != nil {
		return fmt.Errorf("parse Mattermost dialog state: %w", err)
	}
	state[interactionCapabilityPostBindingKey] = interaction.PostBinding
	contextCopy := map[string]any{"callback_id": strings.TrimSpace(dialog.CallbackID), "state": state}
	resourceType, resourceID := interactionResource(state)
	token, err := svc.issue(ctx, securityrepo.IssueCapabilityInput{
		Kind:              interactionKindDialog,
		Operation:         dialogCallbackOperation(dialog.CallbackID),
		ResourceType:      resourceType,
		ResourceID:        resourceID,
		ChannelID:         interaction.ChannelID,
		PostBinding:       interaction.PostBinding,
		ActorUserID:       interaction.Actor.UserID,
		ActorUserName:     interaction.Actor.UserName,
		InstallationScope: interaction.Scope.Installation,
		WorkspaceScope:    interaction.Scope.Workspace,
		SessionScope:      interaction.Scope.Session,
	}, contextCopy)
	if err != nil {
		return err
	}
	state[interactionCapabilityContextKey] = token
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode Mattermost dialog capability state: %w", err)
	}
	dialog.State = string(encoded)
	return nil
}

func (svc *InteractionSecurityService) AuthenticateAction(ctx context.Context, callback ActionCallback) (AuthenticatedInteraction, error) {
	contextCopy := cloneInteractionContext(callback.Context)
	token := strings.TrimSpace(contextStringValue(contextCopy, interactionCapabilityContextKey))
	delete(contextCopy, interactionCapabilityContextKey)
	postBinding := strings.TrimSpace(contextStringValue(contextCopy, interactionCapabilityPostBindingKey))
	if token == "" || postBinding == "" {
		return AuthenticatedInteraction{}, ErrInteractionCapabilityMissing
	}
	callbackPostID := strings.TrimSpace(callback.PostID)
	if callbackPostID == "" || callbackPostID != postBinding {
		return AuthenticatedInteraction{}, ErrInteractionAuthentication
	}
	interaction, err := svc.consumeAndAdmit(ctx, token, securityrepo.ConsumeCapabilityInput{
		Kind:         interactionKindAction,
		Operation:    actionCallbackOperation(contextCopy),
		ResourceType: resourceValue(contextCopy, "resource_type"),
		ResourceID:   resourceValue(contextCopy, "resource_id"),
		ChannelID:    strings.TrimSpace(callback.ChannelID),
		PostBinding:  postBinding,
		ActorUserID:  strings.TrimSpace(callback.UserID),
	}, contextCopy, "mattermost.callback.action", callbackPostID, nil)
	if err != nil {
		return AuthenticatedInteraction{}, err
	}
	interaction.CallbackPostID = callbackPostID
	return interaction, nil
}

func (svc *InteractionSecurityService) AuthenticateDialog(ctx context.Context, callback DialogCallback) (AuthenticatedInteraction, string, error) {
	return svc.authenticateDialog(ctx, callback, nil)
}

func (svc *InteractionSecurityService) AuthenticateDialogPrepared(ctx context.Context, callback DialogCallback, beforeConsume func(AuthenticatedInteraction) error) (AuthenticatedInteraction, string, error) {
	return svc.authenticateDialog(ctx, callback, beforeConsume)
}

func (svc *InteractionSecurityService) authenticateDialog(ctx context.Context, callback DialogCallback, beforeConsume func(AuthenticatedInteraction) error) (AuthenticatedInteraction, string, error) {
	state, err := interactionState(callback.State)
	if err != nil {
		return AuthenticatedInteraction{}, "", ErrInteractionAuthentication
	}
	token := strings.TrimSpace(contextStringValue(state, interactionCapabilityContextKey))
	delete(state, interactionCapabilityContextKey)
	postBinding := strings.TrimSpace(contextStringValue(state, interactionCapabilityPostBindingKey))
	if token == "" || postBinding == "" {
		return AuthenticatedInteraction{}, "", ErrInteractionCapabilityMissing
	}
	contextCopy := map[string]any{"callback_id": strings.TrimSpace(callback.CallbackID), "state": state}
	resourceType, resourceID := interactionResource(state)
	interaction, err := svc.consumeAndAdmit(ctx, token, securityrepo.ConsumeCapabilityInput{
		Kind:         interactionKindDialog,
		Operation:    dialogCallbackOperation(callback.CallbackID),
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ChannelID:    strings.TrimSpace(callback.ChannelID),
		PostBinding:  postBinding,
		ActorUserID:  strings.TrimSpace(callback.UserID),
	}, contextCopy, "mattermost.callback.dialog", postBinding, beforeConsume)
	if err != nil {
		return AuthenticatedInteraction{}, "", err
	}
	delete(state, interactionCapabilityPostBindingKey)
	cleanState, err := json.Marshal(state)
	if err != nil {
		return AuthenticatedInteraction{}, "", ErrInteractionAuthentication
	}
	return interaction, string(cleanState), nil
}

func (svc *InteractionSecurityService) issue(ctx context.Context, input securityrepo.IssueCapabilityInput, safeContext map[string]any) (string, error) {
	token, err := svc.randomValue(32)
	if err != nil {
		return "", err
	}
	tokenHash := sha256.Sum256([]byte(token))
	contextHash, err := interactionContextHash(safeContext)
	if err != nil {
		return "", err
	}
	now := svc.now().UTC()
	input.TokenHash = tokenHash[:]
	input.ContextHash = contextHash
	input.IssuedAt = now
	input.ExpiresAt = now.Add(svc.capabilityTTL)
	if input.State == "" {
		input.State = securityrepo.CapabilityStateUnused
	}
	if err := svc.repository.IssueInteractionCapability(ctx, input); err != nil {
		return "", fmt.Errorf("store interaction capability: %w", err)
	}
	return token, nil
}

func (svc *InteractionSecurityService) consumeAndAdmit(ctx context.Context, token string, input securityrepo.ConsumeCapabilityInput, safeContext map[string]any, actionKey string, callbackPostID string, beforeConsume func(AuthenticatedInteraction) error) (AuthenticatedInteraction, error) {
	if svc == nil || svc.repository == nil {
		return AuthenticatedInteraction{}, ErrInteractionAuthentication
	}
	tokenHash := sha256.Sum256([]byte(token))
	contextHash, err := interactionContextHash(safeContext)
	if err != nil {
		return AuthenticatedInteraction{}, ErrInteractionAuthentication
	}
	input.TokenHash = tokenHash[:]
	input.ContextHash = contextHash
	input.Now = svc.now().UTC()
	capability, err := svc.repository.CheckInteractionCapability(ctx, input)
	if err != nil {
		return AuthenticatedInteraction{}, fmt.Errorf("%w: %v", ErrInteractionAuthentication, err)
	}
	interaction := AuthenticatedInteraction{
		Actor: AuthenticatedActor{UserID: capability.ActorUserID, UserName: capability.ActorUserName},
		Scope: InteractionScope{
			Installation: capability.InstallationScope,
			Workspace:    capability.WorkspaceScope,
			Session:      capability.SessionScope,
		},
		Kind:         capability.Kind,
		Operation:    capability.Operation,
		ResourceType: capability.ResourceType,
		ResourceID:   capability.ResourceID,
		ChannelID:    capability.ChannelID,
		PostBinding:  capability.PostBinding,
	}
	decision := svc.admission.Admit(ctx, InteractionAdmissionRequest{
		ActionKey:    actionKey,
		Operation:    interaction.Operation,
		ResourceType: interaction.ResourceType,
		ResourceID:   interaction.ResourceID,
		Actor:        interaction.Actor,
		Scope:        interaction.Scope,
		ChannelID:    interaction.ChannelID,
		PostID:       callbackPostID,
	})
	switch decision.Status {
	case AdmissionAllowed:
		if beforeConsume != nil {
			if err := beforeConsume(interaction); err != nil {
				return AuthenticatedInteraction{}, fmt.Errorf("%w: %w", ErrInteractionPreparation, err)
			}
		}
		consumed, err := svc.repository.ConsumeInteractionCapability(ctx, input)
		if err != nil {
			return AuthenticatedInteraction{}, fmt.Errorf("%w: %v", ErrInteractionAuthentication, err)
		}
		interaction.Actor = AuthenticatedActor{UserID: consumed.ActorUserID, UserName: consumed.ActorUserName}
		return interaction, nil
	case AdmissionDenied:
		return AuthenticatedInteraction{}, fmt.Errorf("%w: %s", ErrInteractionAdmissionDenied, decision.Reason)
	default:
		return AuthenticatedInteraction{}, fmt.Errorf("%w: %s", ErrInteractionAdmissionUnknown, decision.Reason)
	}
}

func (svc *InteractionSecurityService) ActivateCard(ctx context.Context, card MattermostCard) error {
	return svc.transitionCardCapabilities(ctx, card, securityrepo.CapabilityStatePending, securityrepo.CapabilityStateUnused)
}

func (svc *InteractionSecurityService) RevokeCard(ctx context.Context, card MattermostCard) error {
	pendingErr := svc.transitionCardCapabilities(ctx, card, securityrepo.CapabilityStatePending, securityrepo.CapabilityStateRevoked)
	if pendingErr == nil {
		return nil
	}
	unusedErr := svc.transitionCardCapabilities(ctx, card, securityrepo.CapabilityStateUnused, securityrepo.CapabilityStateRevoked)
	if unusedErr == nil {
		return nil
	}
	return errors.Join(pendingErr, unusedErr)
}

func (svc *InteractionSecurityService) transitionCardCapabilities(ctx context.Context, card MattermostCard, from securityrepo.CapabilityState, to securityrepo.CapabilityState) error {
	if svc == nil || svc.repository == nil {
		return ErrInteractionAuthentication
	}
	hashes := make([][]byte, 0, len(card.Actions))
	seen := make(map[[sha256.Size]byte]struct{}, len(card.Actions))
	for _, action := range card.Actions {
		token := strings.TrimSpace(contextStringValue(action.Context, interactionCapabilityContextKey))
		if token == "" {
			return ErrInteractionCapabilityMissing
		}
		hash := sha256.Sum256([]byte(token))
		if _, exists := seen[hash]; exists {
			return ErrInteractionAuthentication
		}
		seen[hash] = struct{}{}
		hashCopy := make([]byte, len(hash))
		copy(hashCopy, hash[:])
		hashes = append(hashes, hashCopy)
	}
	if len(hashes) == 0 {
		return nil
	}
	return svc.repository.TransitionInteractionCapabilities(ctx, securityrepo.TransitionCapabilitiesInput{
		TokenHashes: hashes,
		From:        from,
		To:          to,
	})
}

func (svc *InteractionSecurityService) normalizeScope(scope InteractionScope) InteractionScope {
	scope.Installation = strings.TrimSpace(scope.Installation)
	if scope.Installation == "" {
		scope.Installation = svc.installationScope
	}
	scope.Workspace = strings.TrimSpace(scope.Workspace)
	if scope.Workspace == "" {
		scope.Workspace = defaultWorkspaceScope
	}
	scope.Session = strings.TrimSpace(scope.Session)
	return scope
}

func validInteractionScope(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 200 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == ':' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func (svc *InteractionSecurityService) randomValue(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := io.ReadFull(svc.random, buffer); err != nil {
		return "", fmt.Errorf("generate interaction capability: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func interactionContextHash(value map[string]any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode interaction capability context: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func cloneInteractionContext(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+2)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func interactionState(raw string) (map[string]any, error) {
	state := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return state, nil
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, err
	}
	return state, nil
}

func actionCallbackOperation(context map[string]any) string {
	parts := []string{interactionKindAction}
	for _, key := range []string{"kind", "action", "dialog", "command", "view"} {
		if value := strings.TrimSpace(contextStringValue(context, key)); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	return strings.Join(parts, ";")
}

func dialogCallbackOperation(callbackID string) string {
	return interactionKindDialog + ";callback_id=" + strings.TrimSpace(callbackID)
}

func interactionResource(context map[string]any) (string, string) {
	return resourceValue(context, "resource_type"), resourceValue(context, "resource_id")
}

func resourceValue(context map[string]any, key string) string {
	return strings.TrimSpace(contextStringValue(context, key))
}

func contextStringValue(context map[string]any, key string) string {
	value, ok := context[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func typedInteractionOperationAllowed(request InteractionAdmissionRequest) bool {
	parts := strings.Split(strings.TrimSpace(request.Operation), ";")
	if len(parts) < 2 || (parts[0] != interactionKindAction && parts[0] != interactionKindDialog) {
		return false
	}
	values := make(map[string]string, len(parts)-1)
	for _, part := range parts[1:] {
		key, value, ok := strings.Cut(part, "=")
		if !ok || key == "" || value == "" || values[key] != "" {
			return false
		}
		values[key] = value
	}
	resourceType := strings.TrimSpace(request.ResourceType)
	resourceID := strings.TrimSpace(request.ResourceID)
	if len(resourceType) > 80 || len(resourceID) > 4096 || strings.ContainsAny(resourceType+resourceID, "\x00\r\n") {
		return false
	}
	if parts[0] == interactionKindDialog {
		if request.ActionKey != "mattermost.callback.dialog" || len(values) != 1 {
			return false
		}
		return dialogCallbackResourceAllowed(values["callback_id"], resourceType, resourceID)
	}
	if request.ActionKey != "mattermost.callback.action" {
		return false
	}
	if values["kind"] == "agent_turn" {
		if len(values) != 2 || request.Scope.Session == "" || resourceType != "agent_session_turn" || resourceID == "" {
			return false
		}
		return values["action"] == "stop_turn" || values["action"] == "retry_turn"
	}
	if values["kind"] != "agents_menu" || !allowedInteractionMenuView(values["view"]) {
		return false
	}
	if len(values) == 2 {
		return resourceType == "" && resourceID == ""
	}
	if len(values) != 3 {
		return false
	}
	if action := values["action"]; action != "" {
		return menuActionResourceAllowed(action, resourceType, resourceID)
	}
	if dialog := values["dialog"]; dialog != "" {
		return menuDialogResourceAllowed(dialog, resourceType, resourceID)
	}
	return false
}

func allowedInteractionMenuView(view string) bool {
	switch view {
	case menuViewMain, menuViewRepositories, menuViewAccounts, menuViewOpenAI, menuViewGitHub,
		menuViewProfiles, menuViewPrompts, menuViewRuntime, menuViewSystem, menuViewHelp,
		menuViewProjects, menuViewRoles, menuViewChats, menuViewAdvanced:
		return true
	default:
		return false
	}
}

func menuActionResourceAllowed(action string, resourceType string, resourceID string) bool {
	allowed := func(types ...string) bool {
		for _, candidate := range types {
			if resourceType == candidate {
				return resourceID != "" || action == menuActionList || action == menuActionRepositoryOnboard
			}
		}
		return false
	}
	switch action {
	case menuActionList:
		return allowed(menuResourceProject, menuResourceRepository, menuResourceAgentRole, menuResourceChat,
			menuResourceRuntimeVar, menuResourceOpenAIAccount, menuResourceGitHubAccount, menuResourceProfile,
			menuResourcePromptTemplate, menuResourceRun)
	case menuActionShow:
		return allowed(menuResourceProject, menuResourceRepository, menuResourceAgentRole, menuResourceChat,
			menuResourceRuntimeVar, menuResourceOpenAIAccount, menuResourceGitHubAccount, menuResourceProfile,
			menuResourcePromptTemplate, menuResourceRun)
	case menuActionConfirmDelete, menuActionDelete:
		return allowed(menuResourceRepository, menuResourceRuntimeVar, menuResourceOpenAIAccount, menuResourceGitHubAccount)
	case menuActionCancel:
		return resourceType == "" && resourceID == ""
	case menuActionRepositoryOnboard:
		return allowed(menuResourceRepository, menuResourceProject)
	case menuActionRepositoryRepos:
		return allowed(menuResourceGitHubAccount)
	case menuActionRepositoryBranches, menuActionRepositoryConnect, menuActionRepositoryCheck, menuActionRepositoryWebhook:
		return allowed(menuResourceRepository)
	case menuActionOpenAIAuth, menuActionOpenAIStatus, menuActionOpenAICleanup:
		return allowed(menuResourceOpenAIAccount)
	case menuActionSystemStatus, menuActionTokenCheck, menuActionLocaleGet, menuActionLocaleSetRU, menuActionLocaleSetEN:
		return resourceType == menuResourceSystem && resourceID == ""
	case menuActionRuntimeSmoke, menuActionRuntimePruneDry, menuActionRuntimePruneApply:
		return resourceType == menuResourceRuntime && resourceID == ""
	case menuActionRuntimeCleanup:
		return allowed(menuResourceRun)
	case menuActionPromptHelp, menuActionPromptRender:
		return allowed(menuResourcePromptTemplate)
	case menuActionProfileEnable, menuActionProfileDisable:
		return allowed(menuResourceProfile)
	case menuActionProjectDashboard:
		return allowed(menuResourceProject)
	case menuActionProjectBindRepo:
		return allowed(menuResourceProject)
	case menuActionThreadRepositorySelect:
		return allowed(menuResourceThreadContext)
	default:
		return false
	}
}

func menuDialogResourceAllowed(dialog string, resourceType string, resourceID string) bool {
	allowed := func(allowEmpty bool, types ...string) bool {
		if resourceType == "" {
			return allowEmpty && resourceID == ""
		}
		for _, candidate := range types {
			if resourceType == candidate {
				return resourceID != ""
			}
		}
		return false
	}
	switch dialog {
	case menuDialogRepositoryAdd, menuDialogRepositorySearch:
		return allowed(true, menuResourceProject, menuResourceGitHubAccount)
	case menuDialogRepositoryEdit, menuDialogRepositoryDelete:
		return allowed(false, menuResourceRepository)
	case menuDialogOpenAIAuth, menuDialogOpenAIStatus, menuDialogOpenAICleanup, menuDialogOpenAIDelete:
		return allowed(true, menuResourceOpenAIAccount)
	case menuDialogGitHubAccountAdd:
		return allowed(true)
	case menuDialogGitHubAccountEdit, menuDialogGitHubAccountDelete:
		return allowed(false, menuResourceGitHubAccount)
	case menuDialogProfileUpsert:
		return allowed(true, menuResourceProfile)
	case menuDialogPromptEdit:
		return allowed(false, menuResourcePromptTemplate)
	case menuDialogRuntimePruneApply:
		return allowed(true, menuResourceRuntime)
	case menuDialogProjectUpsert:
		return allowed(true, menuResourceProject)
	case menuDialogProjectRepositoryBind, menuDialogChatCreate:
		return allowed(false, menuResourceProject)
	case menuDialogProjectRuntimeVar:
		return allowed(false, menuResourceProject, menuResourceRuntimeVar)
	case menuDialogRoleRuntimeVarAttach:
		return allowed(false, menuResourceProject, menuResourceAgentRole, menuResourceRuntimeVar)
	case menuDialogRoleRuntimeVarDetach:
		return allowed(false, menuResourceAgentRole)
	case menuDialogAgentRoleUpsert:
		return allowed(false, menuResourceProject, menuResourceAgentRole)
	default:
		return false
	}
}

func dialogCallbackResourceAllowed(callbackID string, resourceType string, resourceID string) bool {
	allowed := func(allowEmpty bool, types ...string) bool {
		if resourceType == "" {
			return allowEmpty && resourceID == ""
		}
		for _, candidate := range types {
			if resourceType == candidate {
				return resourceID != ""
			}
		}
		return false
	}
	switch callbackID {
	case interactionDialogCallbackResult:
		return allowed(true)
	case dialogCallbackRepositoryAdd:
		return allowed(true, menuResourceProject, menuResourceGitHubAccount)
	case dialogCallbackRepositoryEdit, dialogCallbackRepositoryDelete:
		return allowed(false, menuResourceRepository)
	case dialogCallbackRepositorySearch, dialogCallbackRepositorySearchPick, dialogCallbackRepositorySearchBranch:
		return allowed(true, menuResourceProject, menuResourceGitHubAccount, menuResourceRepository)
	case dialogCallbackOpenAIAuth, dialogCallbackOpenAIStatus, dialogCallbackOpenAICleanup, dialogCallbackOpenAIDelete:
		return allowed(true, menuResourceOpenAIAccount)
	case dialogCallbackGitHubAccountAdd:
		return allowed(true)
	case dialogCallbackGitHubAccountEdit, dialogCallbackGitHubAccountDelete:
		return allowed(false, menuResourceGitHubAccount)
	case dialogCallbackProfileUpsert:
		return allowed(true, menuResourceProfile)
	case dialogCallbackPromptEdit:
		return allowed(false, menuResourcePromptTemplate)
	case dialogCallbackRuntimePruneApply:
		return allowed(true, menuResourceRuntime)
	case dialogCallbackProjectUpsert:
		return allowed(true, menuResourceProject)
	case dialogCallbackProjectRepositoryBind, dialogCallbackChatCreate:
		return allowed(false, menuResourceProject)
	case dialogCallbackProjectRuntimeVar:
		return allowed(false, menuResourceProject, menuResourceRuntimeVar)
	case dialogCallbackRoleRuntimeVarAttach:
		return allowed(false, menuResourceProject, menuResourceAgentRole, menuResourceRuntimeVar)
	case dialogCallbackRoleRuntimeVarDetach:
		return allowed(false, menuResourceAgentRole)
	case dialogCallbackAgentRoleUpsert:
		return allowed(false, menuResourceProject, menuResourceAgentRole)
	default:
		return false
	}
}
