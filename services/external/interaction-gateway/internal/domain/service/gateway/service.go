// Package gateway реализует transport/idempotency orchestration без владения
// control-plane aggregates.
package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/matter-codex/libs/go/i18n"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	domainbot "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/botservice"
	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
	domainmattermost "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/mattermost"
	domainobjectstore "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/objectstore"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

//go:embed messages/*.json
var messageFS embed.FS

type Authority interface {
	Sign(entity.InboundEvent) (string, error)
	CallbackToken(entity.Delivery, string) (string, error)
	VerifyCallback(entity.Delivery, string, string) bool
}

type Observer interface {
	ObserveInbound(string, string)
	ObserveDelivery(string, string)
	ObserveExternalEffect(string, string)
}

type MappingGuard interface {
	RequireBoundTeam(context.Context, entity.TeamPrincipal, string) (entity.WorkspaceMattermostMapping, error)
}

type Config struct {
	ActionCallbackURL          string
	DialogCallbackURL          string
	RetentionRef               string
	MaximumPromptBytes         int
	MaximumFiles               int
	MaximumAttempts            uint32
	MaximumMattermostFileBytes int64
	ArtifactDownloadBaseURL    string
	ArtifactDownloadTTL        time.Duration
	InboundLease               time.Duration
	DeliveryLease              time.Duration
	ScanPollInterval           time.Duration
	RetryBase                  time.Duration
}

type Service struct {
	repository domainrepo.Repository
	mattermost domainmattermost.Client
	objects    domainobjectstore.Client
	control    domaincontrol.Client
	bot        domainbot.Client
	authority  Authority
	mapping    MappingGuard
	observer   Observer
	config     Config
	localizers map[string]*i18n.Localizer
	now        func() time.Time
}

type Result struct {
	Message string
	Replay  bool
	Ignored bool
}

func New(repository domainrepo.Repository, mattermost domainmattermost.Client, objects domainobjectstore.Client,
	control domaincontrol.Client, bot domainbot.Client, authority Authority, mapping MappingGuard,
	observer Observer, config Config,
) (*Service, error) {
	callback, err := url.Parse(config.ActionCallbackURL)
	dialogCallback, dialogErr := url.Parse(config.DialogCallbackURL)
	downloadBase, downloadErr := url.Parse(config.ArtifactDownloadBaseURL)
	if repository == nil || mattermost == nil || objects == nil || control == nil || bot == nil || authority == nil ||
		mapping == nil || observer == nil ||
		err != nil || callback.Scheme != "https" || callback.Host == "" || callback.User != nil ||
		callback.RawQuery != "" || callback.Fragment != "" || config.RetentionRef == "" ||
		dialogErr != nil || dialogCallback.Scheme != "https" || dialogCallback.Host == "" || dialogCallback.User != nil ||
		dialogCallback.RawQuery != "" || dialogCallback.Fragment != "" ||
		downloadErr != nil || downloadBase.Scheme != "https" || downloadBase.Host == "" || downloadBase.User != nil ||
		downloadBase.RawQuery != "" || downloadBase.Fragment != "" || downloadBase.Path != "" ||
		config.ArtifactDownloadTTL < time.Minute || config.ArtifactDownloadTTL > 15*time.Minute ||
		config.MaximumPromptBytes < 1024 || config.MaximumPromptBytes > 1<<20 ||
		config.MaximumFiles < 1 || config.MaximumFiles > 32 || config.MaximumAttempts < 1 || config.MaximumAttempts > 32 ||
		config.MaximumMattermostFileBytes < 1<<20 || config.MaximumMattermostFileBytes > 256<<20 ||
		config.InboundLease < 5*time.Second || config.InboundLease > 5*time.Minute ||
		config.DeliveryLease < 5*time.Second || config.DeliveryLease > 5*time.Minute ||
		config.ScanPollInterval < time.Second || config.ScanPollInterval > time.Minute ||
		config.RetryBase < time.Second || config.RetryBase > time.Minute {
		return nil, errors.New("interaction service configuration is invalid")
	}
	localizers := make(map[string]*i18n.Localizer, 2)
	for _, locale := range i18n.SupportedLocales() {
		localizer, localizeErr := i18n.New(i18n.Config{
			Locale: locale, MessageFS: messageFS,
			MessageFiles: []string{"messages/en.json", "messages/ru.json"},
		})
		if localizeErr != nil {
			return nil, errors.New("load interaction locale messages")
		}
		localizers[locale] = localizer
	}
	return &Service{
		repository: repository, mattermost: mattermost, objects: objects,
		control: control, bot: bot, authority: authority, mapping: mapping, observer: observer,
		config: config, localizers: localizers, now: time.Now,
	}, nil
}

func (service *Service) HandleRaw(ctx context.Context, raw domainmattermost.RawEvent) (result Result, resultErr error) {
	defer func() { service.observeInbound(raw.Kind, result, resultErr) }()
	boundary, verified, err := service.mattermost.ResolveInbound(ctx, raw)
	if err != nil {
		if verified.Verified && verified.Kind == "POST" && verified.Cursor > 0 &&
			boundary.OrganizationID != "" && boundary.ProjectID != "" {
			if cursorErr := service.repository.AdvanceCursor(ctx, boundary, verified.ChannelID, verified.Cursor); cursorErr != nil {
				return Result{}, cursorErr
			}
		}
		return Result{}, domainerrs.ErrUnauthorized
	}
	if verified.Kind == "THREAD_RESTORE_CANDIDATE" {
		sessionID, resolveErr := service.repository.ResolveThreadSession(ctx, boundary.OrganizationID,
			boundary.ProjectID, verified.ChannelID, verified.RootPostID)
		if resolveErr == nil {
			pending, pendingErr := service.repository.HasDeletionPending(ctx, boundary.OrganizationID,
				boundary.ProjectID, boundary.ChatID, sessionID)
			if pendingErr != nil {
				return Result{}, pendingErr
			}
			if pending {
				verified.Kind = "THREAD_RESTORE"
				verified.ProviderEventID = fmt.Sprintf("thread_restore:%s:%d", verified.RootPostID, verified.Revision)
			}
		}
		if verified.Kind == "THREAD_RESTORE_CANDIDATE" {
			verified.Kind = "POST"
			boundary, verified, err = service.mattermost.ResolveInbound(ctx, verified)
			if err != nil {
				return Result{}, domainerrs.ErrUnauthorized
			}
		}
	}
	if err := service.requireBoundTeam(ctx, boundary); err != nil {
		return Result{}, domainerrs.ErrUnauthorized
	}
	if isLifecycleKind(verified.Kind) {
		return service.handleConversationLifecycle(ctx, verified, boundary)
	}
	if boundary.IgnoredBot || hasNoTrigger(verified.Text) {
		if verified.Kind == "POST" && verified.ChannelID != "" && verified.Cursor > 0 {
			if err := service.repository.AdvanceCursor(ctx, boundary, verified.ChannelID, verified.Cursor); err != nil {
				return Result{}, err
			}
		}
		return Result{Ignored: true}, domainerrs.ErrIgnored
	}
	sessionID := boundary.SessionID
	threadRoot := verified.RootPostID
	if threadRoot == "" {
		threadRoot = verified.PostID
	}
	if sessionID == "" && threadRoot != "" {
		sessionID, _ = service.repository.ResolveThreadSession(ctx, boundary.OrganizationID, boundary.ProjectID, verified.ChannelID, threadRoot)
	}
	pending, err := service.repository.HasDeletionPending(ctx, boundary.OrganizationID, boundary.ProjectID, boundary.ChatID, sessionID)
	if err != nil || pending {
		return Result{}, domainerrs.ErrConflict
	}
	inbound, err := buildInbound(verified, boundary)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	if inbound.Kind == enum.InboundReaction {
		return service.handleReaction(ctx, inbound)
	}
	stored, disposition, err := service.repository.ClaimInbound(ctx, inbound, service.config.InboundLease)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	if inbound.Kind == enum.InboundPost && verified.Cursor > 0 {
		if err := service.repository.AdvanceCursor(ctx, boundary, inbound.ChannelID, verified.Cursor); err != nil {
			return Result{}, err
		}
	}
	switch disposition {
	case domainrepo.InboundReplay:
		return service.replayInbound(stored)
	case domainrepo.InboundBusy:
		return Result{}, domainerrs.ErrBusy
	}
	return service.processPrompt(ctx, stored)
}

func (service *Service) HandleDecision(ctx context.Context, raw domainmattermost.RawEvent,
	deliveryID, callbackToken string, decision enum.OwnerDecision, reason string,
) (result Result, resultErr error) {
	defer func() { service.observeInbound(raw.Kind, result, resultErr) }()
	if !decision.Valid() || uuid.Validate(deliveryID) != nil || len(reason) > 2048 {
		return Result{}, domainerrs.ErrConflict
	}
	boundary, verified, err := service.mattermost.ResolveInbound(ctx, raw)
	if err != nil || boundary.IgnoredBot {
		return Result{}, domainerrs.ErrUnauthorized
	}
	if err := service.requireBoundTeam(ctx, boundary); err != nil {
		return Result{}, domainerrs.ErrUnauthorized
	}
	inbound, err := buildInbound(verified, boundary)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	inbound.Kind, inbound.DeliveryID, inbound.CallbackToken = enum.InboundAction, deliveryID, callbackToken
	inbound.Action, inbound.ActionReason = decision, strings.TrimSpace(reason)
	if inbound.ActionReason == "" {
		inbound.ActionReason = "Mattermost owner decision"
	}
	inbound.DigestSHA256, err = eventDigest(inbound)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	stored, disposition, err := service.repository.ClaimInbound(ctx, inbound, service.config.InboundLease)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	if disposition == domainrepo.InboundReplay {
		return service.replayInbound(stored)
	}
	if disposition == domainrepo.InboundBusy {
		return Result{}, domainerrs.ErrBusy
	}
	return service.resumeOwnerCallback(ctx, stored)
}

func (service *Service) HandleRuntimeAction(ctx context.Context, raw domainmattermost.RawEvent,
	deliveryID, callbackToken, action string,
) (result Result, resultErr error) {
	defer func() { service.observeInbound(raw.Kind, result, resultErr) }()
	if (action != "STOP" && action != "RETRY") || uuid.Validate(deliveryID) != nil {
		return Result{}, domainerrs.ErrConflict
	}
	boundary, verified, err := service.mattermost.ResolveInbound(ctx, raw)
	if err != nil || boundary.IgnoredBot {
		return Result{}, domainerrs.ErrUnauthorized
	}
	if err := service.requireBoundTeam(ctx, boundary); err != nil {
		return Result{}, domainerrs.ErrUnauthorized
	}
	inbound, err := buildInbound(verified, boundary)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	inbound.Kind, inbound.DeliveryID, inbound.CallbackToken = enum.InboundAction, deliveryID, callbackToken
	inbound.Action, inbound.ActionReason = enum.OwnerDecision(action), "Mattermost runtime action"
	inbound.DigestSHA256, err = eventDigest(inbound)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	stored, disposition, err := service.repository.ClaimInbound(ctx, inbound, service.config.InboundLease)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	if disposition == domainrepo.InboundReplay {
		return service.replayInbound(stored)
	}
	if disposition == domainrepo.InboundBusy {
		return Result{}, domainerrs.ErrBusy
	}
	return service.resumeOwnerCallback(ctx, stored)
}

func (service *Service) OpenDecisionDialog(ctx context.Context, raw domainmattermost.RawEvent,
	deliveryID, callbackToken, triggerID string,
) (Result, error) {
	boundary, verified, err := service.mattermost.ResolveInbound(ctx, raw)
	if err != nil || boundary.IgnoredBot || triggerID == "" {
		return Result{}, domainerrs.ErrUnauthorized
	}
	if err := service.requireBoundTeam(ctx, boundary); err != nil {
		return Result{}, domainerrs.ErrUnauthorized
	}
	inbound, err := buildInbound(verified, boundary)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	inbound.Kind, inbound.DeliveryID, inbound.CallbackToken, inbound.TriggerID = enum.InboundAction, deliveryID, callbackToken, triggerID
	inbound.DigestSHA256, err = eventDigest(inbound)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	stored, disposition, err := service.repository.ClaimInbound(ctx, inbound, service.config.InboundLease)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	if disposition == domainrepo.InboundReplay {
		return service.replayInbound(stored)
	}
	if disposition == domainrepo.InboundBusy {
		return Result{}, domainerrs.ErrBusy
	}
	return service.resumeOwnerCallback(ctx, stored)
}

func (service *Service) resumeOwnerCallback(ctx context.Context, inbound entity.InboundEvent) (Result, error) {
	if err := service.requireCurrentInboundBot(ctx, inbound); err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "MATTERMOST_BOT_IDENTITY_NOT_CURRENT")
	}
	delivery, err := service.repository.GetDelivery(ctx, inbound.DeliveryID)
	if err != nil || delivery.ProviderPostID != inbound.PostID ||
		delivery.ChannelID != inbound.ChannelID || delivery.TeamID != inbound.TeamID ||
		!service.authority.VerifyCallback(delivery, inbound.ActorID, inbound.CallbackToken) {
		return Result{}, service.failInbound(ctx, inbound, "CALLBACK_LINEAGE_MISMATCH")
	}
	if inbound.Action == enum.DecisionStop || inbound.Action == enum.DecisionRetry {
		if delivery.Kind != enum.DeliveryRun || delivery.SessionID == "" || delivery.TurnID == "" {
			return Result{}, service.failInbound(ctx, inbound, "CALLBACK_LINEAGE_MISMATCH")
		}
		return service.resolveRuntimeAction(ctx, inbound, delivery)
	}
	if delivery.OwnerGate == nil || delivery.OwnerGate.RecipientActorID != inbound.ActorID {
		return Result{}, service.failInbound(ctx, inbound, "CALLBACK_LINEAGE_MISMATCH")
	}
	pending, pendingErr := service.repository.HasDeletionPending(ctx, inbound.OrganizationID, inbound.ProjectID,
		inbound.ChatID, delivery.SessionID)
	if pendingErr != nil {
		return Result{}, pendingErr
	}
	if pending {
		return Result{}, service.failInbound(ctx, inbound, "CONVERSATION_DELETION_PENDING")
	}
	if inbound.Action.Valid() {
		return service.resolveDecision(ctx, inbound, delivery)
	}
	if inbound.TriggerID == "" {
		return Result{}, service.failInbound(ctx, inbound, "DIALOG_TRIGGER_UNAVAILABLE")
	}
	stateRaw, err := internalrpcauth.CanonicalJSON(map[string]string{
		"delivery_id": delivery.ID, "callback_token": inbound.CallbackToken, "post_id": delivery.ProviderPostID,
	})
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	state := base64.RawURLEncoding.EncodeToString(stateRaw)
	if err := service.mattermost.OpenDecisionDialog(ctx, delivery, inbound.TriggerID,
		service.config.DialogCallbackURL, state, delivery.Locale); err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "MATTERMOST_DIALOG_FAILED")
	}
	message := service.text(delivery.Locale, "decision.dialog_opened", nil)
	if err := service.repository.CompleteInbound(ctx, inbound, delivery.SessionID, delivery.TurnID, message); err != nil {
		return Result{}, err
	}
	return Result{Message: message}, nil
}

func (service *Service) resolveRuntimeAction(ctx context.Context, inbound entity.InboundEvent,
	delivery entity.Delivery,
) (Result, error) {
	grant, err := service.authority.Sign(inbound)
	if err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "AUTHORITY_SIGN_FAILED")
	}
	action := string(inbound.Action)
	if action == "RETRY" {
		current, readErr := service.control.GetTurn(ctx, grant, delivery.TurnID)
		if readErr != nil || current.ID != delivery.TurnID || current.SessionID != delivery.SessionID ||
			current.Attempt == 0 || current.Attempt == ^uint32(0) {
			return Result{}, service.failInbound(ctx, inbound, "RUNTIME_ACTION_CONFLICT")
		}
		nextAttempt := current.Attempt + 1
		executionID := stableID(current.ID, fmt.Sprintf("runtime-execution:%d", nextAttempt))
		binding, bindingErr := service.bot.EnsureRuntimeMCPBinding(ctx, domainbot.BindingRequest{
			ControlSessionID: delivery.SessionID, ChannelID: delivery.ChannelID, RootPostID: delivery.RootPostID,
			BotStableKey: delivery.BotStableKey, ExecutionID: executionID, TurnID: current.ID, Attempt: nextAttempt,
		})
		if bindingErr != nil {
			return Result{}, service.retryInbound(ctx, inbound, "MCP_BINDING_CREATE_FAILED")
		}
		_, bindingErr = service.control.BindSessionMCP(ctx, grant, domaincontrol.SessionMCPBindingInput{
			IdempotencyKey: stableID(inbound.ID, fmt.Sprintf("session-mcp-binding:%d:%s:%s",
				binding.AgentSessionVersion, binding.ProviderContentVersion, binding.ContentSHA256)),
			SessionID: delivery.SessionID, AgentSessionKey: binding.AgentSessionKey,
			AgentSessionID: binding.AgentSessionID, AgentSessionVersion: binding.AgentSessionVersion,
			AgentSessionBindingSHA256: binding.AgentSessionBindingSHA256,
			ImmutableSecretRef:        binding.ImmutableSecretRef, ProviderContentVersion: binding.ProviderContentVersion,
			ContentSHA256: binding.ContentSHA256,
		})
		if bindingErr != nil {
			return Result{}, service.retryInbound(ctx, inbound, "MCP_BINDING_REGISTER_FAILED")
		}
	}
	turn, err := service.control.ManageRuntimeAction(ctx, grant, domaincontrol.RuntimeActionInput{
		IdempotencyKey: stableID(inbound.ID, "runtime-action:"+strings.ToLower(action)),
		SessionID:      delivery.SessionID, TurnID: delivery.TurnID, Action: action,
	})
	if err != nil {
		if errors.Is(err, domaincontrol.ErrConflict) {
			return Result{}, service.failInbound(ctx, inbound, "RUNTIME_ACTION_CONFLICT")
		}
		return Result{}, service.retryInbound(ctx, inbound, "RUNTIME_ACTION_FAILED")
	}
	messageID := "card.run.stopped"
	if action == "RETRY" {
		messageID = "card.run.retried"
	}
	message := service.text(inbound.Locale, messageID, map[string]any{"Attempt": turn.Attempt})
	if err := service.repository.CompleteInbound(ctx, inbound, delivery.SessionID, delivery.TurnID, message); err != nil {
		return Result{}, err
	}
	if err := service.enqueueRuntimeActionCard(ctx, inbound, delivery, turn, action == "RETRY", message); err != nil {
		return Result{}, err
	}
	return Result{Message: message}, nil
}

func (service *Service) ProcessWaiting(ctx context.Context) (bool, error) {
	inbound, ok, err := service.repository.ClaimWaitingInbound(ctx, service.config.InboundLease)
	if err != nil || !ok {
		return ok, err
	}
	boundary, boundaryErr := service.mattermost.ResolveMappedChannel(ctx, inbound.TeamID, inbound.ChannelID)
	if boundaryErr != nil || boundary.OrganizationID != inbound.OrganizationID || boundary.ProjectID != inbound.ProjectID {
		return true, service.retryInbound(ctx, inbound, "MATTERMOST_MAPPING_NOT_CURRENT")
	}
	if boundaryErr = service.requireBoundTeam(ctx, boundary); boundaryErr != nil {
		return true, service.retryInbound(ctx, inbound, "MATTERMOST_MAPPING_NOT_CURRENT")
	}
	if inbound.Kind == enum.InboundChannelDelete || inbound.Kind == enum.InboundThreadDelete {
		_, err = service.finalizeConversationCleanup(ctx, inbound)
	} else if (inbound.Kind == enum.InboundAction || inbound.Kind == enum.InboundDialog) && inbound.DeliveryID != "" {
		_, err = service.resumeOwnerCallback(ctx, inbound)
	} else {
		_, err = service.processPrompt(ctx, inbound)
	}
	return true, err
}

func (service *Service) handleConversationLifecycle(ctx context.Context, raw domainmattermost.RawEvent,
	boundary entity.Boundary,
) (Result, error) {
	inbound, err := buildInbound(raw, boundary)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	kind, action, resourceID := "CHANNEL", "DELETE", boundary.ChatID
	if inbound.Kind == enum.InboundChannelRestore || inbound.Kind == enum.InboundThreadRestore {
		action = "RESTORE"
	}
	if inbound.Kind == enum.InboundThreadDelete || inbound.Kind == enum.InboundThreadRestore {
		kind = "THREAD"
		inbound.SessionID, err = service.repository.ResolveThreadSession(ctx, boundary.OrganizationID, boundary.ProjectID,
			raw.ChannelID, raw.RootPostID)
		if err != nil {
			return Result{}, domainerrs.ErrNotFound
		}
		resourceID = inbound.SessionID
	}
	inbound.LifecycleResourceID = resourceID
	inbound.DigestSHA256, err = eventDigest(inbound)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	stored, disposition, err := service.repository.ClaimInbound(ctx, inbound, service.config.InboundLease)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	if disposition == domainrepo.InboundReplay {
		return service.replayInbound(stored)
	}
	if disposition == domainrepo.InboundBusy {
		return Result{}, domainerrs.ErrBusy
	}
	grant, err := service.authority.Sign(stored)
	if err != nil {
		return Result{}, service.retryInbound(ctx, stored, "AUTHORITY_SIGN_FAILED")
	}
	if err := service.control.ManageConversationLifecycle(ctx, grant,
		stableID(stored.ID, "conversation-lifecycle:"+action), kind, resourceID, action); err != nil {
		return Result{}, service.retryInbound(ctx, stored, "CONVERSATION_LIFECYCLE_FAILED")
	}
	messageID := "conversation.deletion_pending"
	if action == "DELETE" {
		if err := service.repository.RevokeDownloadGrants(ctx, stored.OrganizationID, stored.ProjectID,
			stored.ChannelID, mapLifecycleScope(kind, stored.SessionID)); err != nil {
			return Result{}, err
		}
		stored.State, stored.NextAttemptAt = enum.InboundWaitingCleanup, service.now().Add(24*time.Hour)
		stored.SemanticOutcome = "SUCCESS"
		stored.ResponseMessage = service.text(stored.Locale, messageID, nil)
		stored.NextAction = "RESTORE_BEFORE_CLEANUP"
		if err := service.repository.SaveInboundProgress(ctx, stored); err != nil {
			return Result{}, err
		}
		return Result{Message: stored.ResponseMessage}, nil
	}
	messageID = "conversation.restored"
	if err := service.repository.CancelDeletion(ctx, stored.OrganizationID, stored.ProjectID,
		mapLifecycleScope(kind, stored.ChatID), mapLifecycleScope(kind, stored.SessionID),
		service.text(stored.Locale, "conversation.cleanup_cancelled", nil)); err != nil {
		return Result{}, err
	}
	message := service.text(stored.Locale, messageID, nil)
	if err := service.repository.CompleteInbound(ctx, stored, stored.SessionID, "", message); err != nil {
		return Result{}, err
	}
	return Result{Message: message}, nil
}

func (service *Service) finalizeConversationCleanup(ctx context.Context, inbound entity.InboundEvent) (Result, error) {
	kind := "CHANNEL"
	if inbound.Kind == enum.InboundThreadDelete {
		kind = "THREAD"
	}
	grant, err := service.authority.Sign(inbound)
	if err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "AUTHORITY_SIGN_FAILED")
	}
	if err := service.control.ManageConversationLifecycle(ctx, grant,
		stableID(inbound.ID, "conversation-lifecycle:finalize"), kind, inbound.LifecycleResourceID, "FINALIZE"); err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "CONVERSATION_CLEANUP_FAILED")
	}
	message := service.text(inbound.Locale, "conversation.deleted", nil)
	if err := service.repository.CompleteInbound(ctx, inbound, inbound.SessionID, "", message); err != nil {
		return Result{}, err
	}
	return Result{Message: message}, nil
}

func isLifecycleKind(kind string) bool {
	switch enum.InboundKind(kind) {
	case enum.InboundChannelDelete, enum.InboundChannelRestore, enum.InboundThreadDelete, enum.InboundThreadRestore:
		return true
	default:
		return false
	}
}

func mapLifecycleScope(kind, value string) string {
	if (kind == "CHANNEL" && value != "") || (kind == "THREAD" && value != "") {
		return value
	}
	return ""
}

func (service *Service) ProcessDelivery(ctx context.Context, instanceID string) (bool, error) {
	leaseToken, err := randomToken()
	if err != nil {
		return false, err
	}
	delivery, ok, err := service.repository.ClaimDelivery(ctx, instanceID, leaseToken, service.config.DeliveryLease)
	if err != nil || !ok {
		return ok, err
	}
	outcome := "success"
	defer func() { service.observer.ObserveDelivery(string(delivery.Kind), outcome) }()
	if delivery.State == enum.DeliveryProviderAccepted {
		err = service.acknowledgeDelivery(ctx, delivery)
		if err != nil {
			outcome = "retry"
			if delivery.AckAttempts >= service.config.MaximumAttempts {
				outcome = "dead_letter"
			}
			return true, service.retryDelivery(ctx, delivery, "DOMAIN_ACKNOWLEDGEMENT_FAILED")
		}
		return true, nil
	}
	boundary, boundaryErr := service.mattermost.ResolveMappedChannel(ctx, delivery.TeamID, delivery.ChannelID)
	if boundaryErr != nil || boundary.OrganizationID != delivery.OrganizationID || boundary.ProjectID != delivery.ProjectID {
		outcome = "retry"
		if delivery.Attempts >= service.config.MaximumAttempts {
			outcome = "dead_letter"
		}
		return true, service.retryDelivery(ctx, delivery, "MATTERMOST_MAPPING_NOT_CURRENT")
	}
	if boundaryErr = service.requireBoundTeam(ctx, boundary); boundaryErr != nil {
		outcome = "retry"
		if delivery.Attempts >= service.config.MaximumAttempts {
			outcome = "dead_letter"
		}
		return true, service.retryDelivery(ctx, delivery, "MATTERMOST_MAPPING_NOT_CURRENT")
	}
	// Mattermost UploadFile не имеет документированного readback, который
	// восстанавливает file_id после потери финального ответа. Такой effect не
	// повторяется: все исходящие artifacts заранее превращаются в одноразовые
	// gateway-mediated links, а private S3 object никогда не покидает boundary.
	if len(delivery.Attachments) != 0 {
		outcome = "dead_letter"
		return true, service.failDelivery(ctx, delivery, "AMBIGUOUS_UPLOAD_REQUIRES_REPAIR")
	}
	published, err := service.mattermost.Publish(ctx, delivery, nil)
	effect := "create_post"
	if delivery.UpdatePostID != "" {
		effect = "update_post"
	}
	if err != nil {
		service.observer.ObserveExternalEffect(effect, "failure")
		if errors.Is(err, domainmattermost.ErrAmbiguousEffect) {
			outcome = "dead_letter"
			return true, service.failDelivery(ctx, delivery, "AMBIGUOUS_PROVIDER_EFFECT_REQUIRES_RECONCILIATION")
		}
		outcome = "retry"
		if delivery.Attempts >= service.config.MaximumAttempts {
			outcome = "dead_letter"
		}
		return true, service.retryDelivery(ctx, delivery, "MATTERMOST_DELIVERY_FAILED")
	}
	service.observer.ObserveExternalEffect(effect, "success")
	if err := service.repository.MarkProviderAccepted(ctx, delivery,
		published.PostID, published.ReceiptSHA256, published.RootPostID); err != nil {
		outcome = "failure"
		return true, err
	}
	delivery.State = enum.DeliveryProviderAccepted
	delivery.AckAttempts++
	delivery.ProviderPostID, delivery.ProviderReceiptSHA256, delivery.RootPostID = published.PostID, published.ReceiptSHA256, published.RootPostID
	delivery.State = enum.DeliveryProviderAccepted
	err = service.acknowledgeDelivery(ctx, delivery)
	if err != nil {
		outcome = "retry"
		if delivery.AckAttempts >= service.config.MaximumAttempts {
			outcome = "dead_letter"
		}
		return true, service.retryDelivery(ctx, delivery, "DOMAIN_ACKNOWLEDGEMENT_FAILED")
	}
	return true, nil
}

func (service *Service) ClaimOwnerGate(ctx context.Context) (bool, error) {
	key, ok, err := service.repository.ClaimOwnerGateRequest(ctx)
	if err != nil || !ok {
		return ok, err
	}
	claim, err := service.control.ClaimOwnerGate(ctx, key)
	if err != nil {
		return true, err
	}
	if claim.GateID == "" {
		return false, service.repository.CompleteOwnerGateClaim(ctx, key)
	}
	if uuid.Validate(claim.DeliveryID) != nil || uuid.Validate(claim.GateID) != nil ||
		uuid.Validate(claim.ProjectID) != nil || uuid.Validate(claim.ProcessRunID) != nil ||
		uuid.Validate(claim.SessionID) != nil || uuid.Validate(claim.TurnID) != nil ||
		uuid.Validate(claim.RecipientActorID) != nil || claim.GateVersion == 0 ||
		claim.Attempt == 0 || claim.Attempt > 100 || claim.ClaimFence == 0 ||
		len(claim.ClaimToken) < 32 || len(claim.ClaimToken) > 512 || claim.ResultRef == "" ||
		!claim.ClaimExpiresAt.After(service.now()) || !validSHA256Text(claim.ImmutableInputSHA256) ||
		!validSHA256Text(claim.DeliveryPayloadSHA256) || !validSHA256Text(claim.Summary) {
		return true, errors.New("control-plane owner gate claim boundary is invalid")
	}
	var boundary entity.Boundary
	if claim.ScheduleID != "" {
		if uuid.Validate(claim.ScheduleID) != nil || uuid.Validate(claim.NotificationRoomID) != nil {
			return true, errors.New("control-plane owner gate room route is invalid")
		}
		boundary, err = service.mattermost.ResolveRoomDelivery(ctx,
			claim.ProjectID, claim.NotificationRoomID, claim.RecipientActorID,
		)
	} else {
		if claim.NotificationRoomID != "" {
			return true, errors.New("control-plane owner gate room route is invalid")
		}
		boundary, err = service.mattermost.ResolveDelivery(ctx, claim.ProjectID, claim.RecipientActorID)
	}
	if err != nil {
		return true, err
	}
	if err := service.requireBoundTeam(ctx, boundary); err != nil {
		return true, errors.New("owner gate Mattermost mapping is not current")
	}
	delivery := entity.Delivery{
		ID:   claim.DeliveryID,
		Kind: enum.DeliveryOwnerDecision, State: enum.DeliveryPending,
		OrganizationID: boundary.OrganizationID, ProjectID: claim.ProjectID,
		SessionID: claim.SessionID, TurnID: claim.TurnID, Attempt: claim.Attempt,
		ImmutableInputSHA256: claim.ImmutableInputSHA256, TeamID: boundary.TeamID,
		ChannelID: boundary.ChannelID, BotStableKey: boundary.BotStableKey,
		BotProviderUserID: boundary.BotProviderUserID, BotProviderGeneration: boundary.BotProviderGeneration,
		Locale: boundary.Locale,
		OwnerGate: &entity.OwnerGateBinding{
			GateID: claim.GateID, GateVersion: claim.GateVersion, ProcessRunID: claim.ProcessRunID,
			ProcessVersion: claim.ProcessVersion, ClaimToken: claim.ClaimToken, ClaimFence: claim.ClaimFence,
			ClaimExpiresAt: claim.ClaimExpiresAt, RecipientActorID: claim.RecipientActorID,
			DeliveryPayloadSHA256: claim.DeliveryPayloadSHA256,
		},
		CreatedAt: service.now().UTC(), UpdatedAt: service.now().UTC(),
	}
	resultObject, publishResult, err := service.objects.Inspect(ctx, claim.ProjectID, claim.ResultRef, claim.Summary)
	if err != nil {
		return true, err
	}
	protectedURL := ""
	if publishResult {
		binding := entity.ArtifactBinding{
			ArtifactID: stableID(claim.GateID, "result-delivery"), Version: claim.GateVersion,
			Name: safeName(resultObject.Name, 0), Path: "results/" + safeName(resultObject.Name, 0),
			StorageRef: resultObject.Reference, SizeBytes: resultObject.Size, MediaType: resultObject.MediaType,
			SHA256: resultObject.SHA256, Provenance: "control-plane-owner-gate:" + claim.GateID, ScanState: "CLEAN",
		}
		protectedURL, err = service.issueArtifactDownload(ctx, delivery, boundary, binding)
		if err != nil {
			return true, err
		}
	}
	token, err := service.authority.CallbackToken(delivery, claim.RecipientActorID)
	if err != nil {
		return true, err
	}
	payload := service.ownerGateCard(delivery, claim, token)
	if protectedURL != "" {
		payload["message"] = fmt.Sprintf("%s\n\n%s", payload["message"], service.text(delivery.Locale,
			"card.artifact.protected_link", map[string]any{
				"URL": protectedURL, "Name": resultObject.Name,
				"Size": resultObject.Size, "SHA256": resultObject.SHA256,
			}))
	}
	delivery.Payload, delivery.PayloadSHA256, err = encodeDeliveryPayload(delivery.ID, payload)
	if err != nil {
		return true, err
	}
	if err := service.repository.SaveOwnerGateClaim(ctx, key, delivery); err != nil {
		return true, err
	}
	return true, nil
}

func (service *Service) ClaimInteractionDelivery(ctx context.Context) (bool, error) {
	work, err := service.control.ClaimInteractionDelivery(ctx, uuid.NewString())
	if err != nil || work.DeliveryID == "" {
		return work.DeliveryID != "", err
	}
	if uuid.Validate(work.DeliveryID) != nil || uuid.Validate(work.OrganizationID) != nil ||
		uuid.Validate(work.ProjectID) != nil || uuid.Validate(work.ActorID) != nil ||
		uuid.Validate(work.SessionID) != nil || uuid.Validate(work.TurnID) != nil ||
		uuid.Validate(work.RuntimeRevisionID) != nil || work.SessionVersion == 0 || work.TurnVersion == 0 ||
		work.RuntimeRevisionVersion == 0 || work.Attempt == 0 || work.Fence == 0 || len(work.LeaseToken) != 64 ||
		!work.LeaseExpiresAt.After(service.now()) || !validSHA256Text(work.ImmutableInputSHA256) {
		return true, errors.New("control-plane interaction delivery lineage is invalid")
	}
	var boundary entity.Boundary
	if work.NotificationRoomID != "" {
		if uuid.Validate(work.NotificationRoomID) != nil ||
			(work.ScheduledOutcome != "ACTION_TAKEN" && work.ScheduledOutcome != "FAILED") ||
			work.NotificationPolicy == "AUDIT_ONLY" || work.NotificationPolicy == "UNSPECIFIED" {
			return true, errors.New("control-plane scheduled delivery route is invalid")
		}
		boundary, err = service.mattermost.ResolveRoomDelivery(ctx,
			work.ProjectID, work.NotificationRoomID, work.ActorID,
		)
	} else {
		if work.NotificationPolicy != "UNSPECIFIED" || work.ScheduledOutcome != "UNSPECIFIED" {
			return true, errors.New("control-plane scheduled delivery route is invalid")
		}
		boundary, err = service.mattermost.ResolveDelivery(ctx, work.ProjectID, work.ActorID)
	}
	if err != nil || boundary.OrganizationID != work.OrganizationID {
		return true, errors.New("control-plane interaction delivery route is invalid")
	}
	if err := service.requireBoundTeam(ctx, boundary); err != nil {
		return true, errors.New("interaction delivery Mattermost mapping is not current")
	}
	deliveryKind := enum.DeliveryStatus
	if work.Kind == "RUN_CARD" || work.Kind == "FINAL_MARKDOWN" {
		deliveryKind = enum.DeliveryRun
	} else if work.Kind == "INCIDENT" {
		deliveryKind = enum.DeliveryIncident
	} else if work.Kind == "PUBLISH_ARTIFACT" {
		deliveryKind = enum.DeliveryArtifact
	}
	delivery := entity.Delivery{
		ID: work.DeliveryID, Kind: deliveryKind, State: enum.DeliveryPending,
		OrganizationID: work.OrganizationID, ProjectID: work.ProjectID, SessionID: work.SessionID,
		TurnID: work.TurnID, Attempt: work.Attempt, ImmutableInputSHA256: work.ImmutableInputSHA256,
		TeamID: boundary.TeamID, ChannelID: boundary.ChannelID, BotStableKey: boundary.BotStableKey,
		BotProviderUserID: boundary.BotProviderUserID, BotProviderGeneration: boundary.BotProviderGeneration,
		Locale: boundary.Locale, CreatedAt: service.now().UTC(), UpdatedAt: service.now().UTC(),
		OwnerDelivery: &entity.OwnerDeliveryBinding{
			Fence: work.Fence, LeaseToken: work.LeaseToken,
			LeaseExpiresAt: work.LeaseExpiresAt, TurnVersion: work.TurnVersion,
			RuntimeRevisionID: work.RuntimeRevisionID, RuntimeRevisionVersion: work.RuntimeRevisionVersion,
		},
	}
	if original, originalErr := service.repository.GetRunDeliveryByTurn(ctx, work.OrganizationID,
		work.ProjectID, work.SessionID, work.TurnID); originalErr == nil {
		delivery.UpdatePostID = original.ProviderPostID
	} else if !errors.Is(originalErr, domainrepo.ErrNotFound) {
		return true, originalErr
	}
	message := service.text(delivery.Locale, "card.run.progress", map[string]any{
		"State":   work.LifecycleState,
		"Outcome": work.Outcome, "SessionID": work.SessionID, "TurnID": work.TurnID,
		"Attempt": work.Attempt, "InputSHA256": work.ImmutableInputSHA256,
	})
	if work.LifecycleState == "SUCCEEDED" || work.LifecycleState == "FAILED" ||
		work.LifecycleState == "CANCELLED" || work.LifecycleState == "BLOCKED" {
		message = service.text(delivery.Locale, "card.run.terminal", map[string]any{
			"State":   work.LifecycleState,
			"Outcome": work.Outcome, "SessionID": work.SessionID, "TurnID": work.TurnID,
			"Attempt": work.Attempt, "InputSHA256": work.ImmutableInputSHA256,
		})
	}
	if work.ArtifactID != "" {
		if uuid.Validate(work.ArtifactID) != nil || work.ArtifactVersion == 0 || !validSHA256Text(work.ArtifactSHA256) ||
			work.ArtifactStorageRef == "" || work.ArtifactSizeBytes == 0 || work.ArtifactMediaType == "" {
			return true, errors.New("control-plane interaction artifact lineage is invalid")
		}
		artifactRaw := work.InlinePayload
		if strings.HasPrefix(work.ArtifactStorageRef, "control-plane-inline:") {
			if len(artifactRaw) == 0 || uint64(len(artifactRaw)) != work.ArtifactSizeBytes ||
				digestBytes(artifactRaw) != work.ArtifactSHA256 {
				return true, errors.New("control-plane inline artifact readback is invalid")
			}
			stored, storeErr := service.objects.Put(ctx, work.ProjectID,
				fmt.Sprintf("owner-deliveries/%s/v%d/%s/%s", work.ArtifactID, work.ArtifactVersion,
					work.ArtifactSHA256, safeName(work.ArtifactName, 0)), artifactRaw,
				work.ArtifactMediaType, work.ArtifactSHA256)
			if storeErr != nil {
				return true, storeErr
			}
			work.ArtifactStorageRef = stored.Reference
		} else if len(artifactRaw) != 0 {
			return true, errors.New("unexpected control-plane inline artifact payload")
		}
		binding := entity.ArtifactBinding{
			ArtifactID: work.ArtifactID, Version: work.ArtifactVersion,
			Name: safeName(work.ArtifactName, 0), Path: "results/" + safeName(work.ArtifactName, 0),
			StorageRef: work.ArtifactStorageRef, SizeBytes: work.ArtifactSizeBytes, MediaType: work.ArtifactMediaType,
			SHA256: work.ArtifactSHA256, Provenance: "control-plane-owner:" + work.TurnID, ScanState: "CLEAN",
		}
		link, linkErr := service.issueArtifactDownload(ctx, delivery, boundary, binding)
		if linkErr != nil {
			return true, linkErr
		}
		if (work.Kind == "FINAL_MARKDOWN" || work.Kind == "STATUS" || work.Kind == "PROGRESS") &&
			work.ArtifactMediaType == "text/markdown" &&
			work.ArtifactSizeBytes <= uint64(service.config.MaximumPromptBytes) {
			raw := artifactRaw
			if len(raw) == 0 {
				var readErr error
				raw, readErr = service.objects.Get(ctx, work.ProjectID, work.ArtifactStorageRef,
					work.ArtifactSizeBytes, work.ArtifactSHA256)
				if readErr != nil {
					return true, readErr
				}
			}
			message += "\n\n" + string(raw)
		}
		message += "\n\n" + service.text(delivery.Locale, "card.artifact.protected_link",
			map[string]any{"URL": link, "Name": binding.Name, "Size": binding.SizeBytes, "SHA256": binding.SHA256})
	}
	if !utf8.ValidString(message) || utf8.RuneCountInString(message) > mattermostmodel.PostMessageMaxRunesV2 {
		return true, errors.New("Mattermost delivery message exceeds the provider limit")
	}
	payload := map[string]any{"message": message}
	if delivery.Kind == enum.DeliveryRun {
		token, tokenErr := service.authority.CallbackToken(delivery, work.ActorID)
		if tokenErr != nil {
			return true, tokenErr
		}
		attachment := map[string]any{"title": message, "text": service.text(delivery.Locale,
			"card.run.body", map[string]any{"SessionID": delivery.SessionID, "TurnID": delivery.TurnID})}
		switch work.LifecycleState {
		case "QUEUED", "CLAIMED", "RUNNING", "ADMITTED":
			attachment["actions"] = []map[string]any{service.runtimeActionButton(delivery, token, "STOP")}
		case "FAILED", "EXPIRED":
			attachment["actions"] = []map[string]any{service.runtimeActionButton(delivery, token, "RETRY")}
		}
		payload["props"] = map[string]any{"attachments": []map[string]any{attachment}}
	}
	delivery.Payload, delivery.PayloadSHA256, err = encodeDeliveryPayload(delivery.ID, payload)
	if err != nil {
		return true, err
	}
	_, _, err = service.repository.EnqueueDelivery(ctx, delivery)
	return true, err
}

func (service *Service) StageRuntimeOutput(ctx context.Context, grant, executionID string,
	output domaincontrol.RuntimeOutputMetadata, raw io.ReadSeeker,
) (domaincontrol.Artifact, error) {
	if uuid.Validate(executionID) != nil || raw == nil || output.SizeBytes == 0 ||
		output.Name == "" || len(output.Name) > 255 ||
		strings.ContainsAny(output.Name, "/\\\x00\r\n") || output.MediaType == "" ||
		output.Sequence == 0 || output.Total == 0 || output.Sequence > output.Total || output.Total > 4096 {
		return domaincontrol.Artifact{}, domainerrs.ErrConflict
	}
	authorization, err := service.control.AuthorizeRuntimeOutput(ctx, grant, executionID, output)
	if err != nil {
		return domaincontrol.Artifact{}, err
	}
	key := fmt.Sprintf("runtime-outputs/%s/%s/%04d/%s/%s", executionID,
		strings.ToLower(output.Kind), output.Sequence, output.SHA256, safeName(output.Name, 0))
	stored, err := service.objects.PutStream(ctx, authorization.ProjectID, key, raw, int64(output.SizeBytes),
		output.MediaType, output.SHA256)
	if err != nil || stored.Reference == "" || stored.SHA256 != output.SHA256 || stored.Size != output.SizeBytes {
		return domaincontrol.Artifact{}, errors.New("runtime output object readback failed")
	}
	// Upload может быть долгим: перед owner commit всегда читается fresh tuple.
	fresh, err := service.control.AuthorizeRuntimeOutput(ctx, grant, executionID, output)
	if err != nil || fresh.ProjectID != authorization.ProjectID ||
		fresh.OrganizationID != authorization.OrganizationID || fresh.GrantGeneration != authorization.GrantGeneration {
		return domaincontrol.Artifact{}, errors.New("runtime output authority changed")
	}
	artifact, err := service.control.RegisterRuntimeOutput(ctx, grant, executionID, fresh, output, stored.Reference)
	if err != nil || artifact.ID == "" || artifact.Version == 0 || artifact.SHA256 != output.SHA256 ||
		artifact.SizeBytes != output.SizeBytes || artifact.StorageRef != stored.Reference {
		return domaincontrol.Artifact{}, errors.New("runtime output registration failed")
	}
	return artifact, nil
}

func (service *Service) ExpireOwnerGate(ctx context.Context) error {
	return service.control.ExpireOwnerGate(ctx, uuid.NewString())
}

func (service *Service) CatchUp(ctx context.Context) error {
	boundaries, err := service.mattermost.ChannelBoundaries(ctx)
	if err != nil {
		return err
	}
	cursors, err := service.repository.LoadCursors(ctx, boundaries)
	if err != nil {
		return err
	}
	reactionPosts, err := service.repository.ListPendingReactionPosts(ctx, boundaries, 1024)
	if err != nil {
		return err
	}
	return service.mattermost.CatchUp(ctx, cursors, reactionPosts, func(eventContext context.Context, raw domainmattermost.RawEvent) error {
		return service.consumeStreamEvent(eventContext, raw)
	})
}

func (service *Service) ReconcileLifecycle(ctx context.Context) error {
	boundaries, err := service.mattermost.ChannelBoundaries(ctx)
	if err != nil {
		return err
	}
	knownThreads, err := service.repository.ListKnownThreads(ctx, boundaries, 4096)
	if err != nil {
		return err
	}
	return service.mattermost.ReconcileLifecycle(ctx, knownThreads, func(eventContext context.Context, raw domainmattermost.RawEvent) error {
		if raw.Kind == "CHANNEL_RESTORE" || raw.Kind == "THREAD_RESTORE" {
			boundary, verified, resolveErr := service.mattermost.ResolveInbound(eventContext, raw)
			if resolveErr != nil {
				return resolveErr
			}
			if resolveErr = service.requireBoundTeam(eventContext, boundary); resolveErr != nil {
				return resolveErr
			}
			sessionID := boundary.SessionID
			if raw.Kind == "THREAD_RESTORE" {
				sessionID, resolveErr = service.repository.ResolveThreadSession(eventContext, boundary.OrganizationID,
					boundary.ProjectID, verified.ChannelID, verified.RootPostID)
				if resolveErr != nil {
					return resolveErr
				}
			}
			pending, pendingErr := service.repository.HasDeletionPending(eventContext, boundary.OrganizationID,
				boundary.ProjectID, boundary.ChatID, sessionID)
			if pendingErr != nil || !pending {
				return pendingErr
			}
		}
		return service.consumeStreamEvent(eventContext, raw)
	})
}

func (service *Service) requireBoundTeam(ctx context.Context, boundary entity.Boundary) error {
	_, err := service.mapping.RequireBoundTeam(ctx, entity.TeamPrincipal{
		ActorID: boundary.MappingOwnerActorID, OrganizationID: boundary.OrganizationID, ProjectID: boundary.ProjectID,
	}, boundary.TeamID)
	return err
}

func (service *Service) Listen(ctx context.Context) error {
	return service.mattermost.Listen(ctx, func(eventContext context.Context, raw domainmattermost.RawEvent) error {
		return service.consumeStreamEvent(eventContext, raw)
	})
}

func (service *Service) consumeStreamEvent(ctx context.Context, raw domainmattermost.RawEvent) error {
	_, err := service.HandleRaw(ctx, raw)
	if err == nil || errors.Is(err, domainerrs.ErrIgnored) || errors.Is(err, domainerrs.ErrBusy) ||
		errors.Is(err, domainerrs.ErrUnauthorized) || errors.Is(err, domainerrs.ErrNotFound) ||
		errors.Is(err, domainerrs.ErrConflict) {
		return nil
	}
	return err
}

func (service *Service) GetDelivery(ctx context.Context, deliveryID string) (entity.Delivery, error) {
	if uuid.Validate(deliveryID) != nil {
		return entity.Delivery{}, domainerrs.ErrNotFound
	}
	delivery, err := service.repository.GetDelivery(ctx, deliveryID)
	if err != nil {
		return entity.Delivery{}, domainerrs.ErrNotFound
	}
	return delivery, nil
}

func (service *Service) GetDeliveryScoped(ctx context.Context, organizationID, projectID, deliveryID string) (entity.Delivery, error) {
	if uuid.Validate(organizationID) != nil || uuid.Validate(projectID) != nil || uuid.Validate(deliveryID) != nil {
		return entity.Delivery{}, domainerrs.ErrNotFound
	}
	delivery, err := service.repository.GetDeliveryScoped(ctx, organizationID, projectID, deliveryID)
	if err != nil {
		if errors.Is(err, domainrepo.ErrNotFound) {
			return entity.Delivery{}, domainerrs.ErrNotFound
		}
		return entity.Delivery{}, err
	}
	return delivery, nil
}

func (service *Service) ReadRuntimeMaterialization(ctx context.Context, grant, executionID, artifactID string,
	artifactVersion uint64, artifactSHA256 string,
) ([]byte, string, error) {
	materialization, err := service.control.GetRuntimeMaterialization(ctx, grant, executionID, artifactID,
		artifactVersion, artifactSHA256)
	if err != nil {
		return nil, "", err
	}
	raw, err := service.objects.Get(ctx, materialization.ProjectID, materialization.StorageRef,
		materialization.SizeBytes, materialization.SHA256)
	if err != nil {
		return nil, "", errors.New("read runtime materialization object")
	}
	return raw, materialization.MediaType, nil
}

func (service *Service) ValidateDeliveryReadback(ctx context.Context, grantID, deliveryID, organizationID,
	projectID, credentialSHA256 string, generation uint64,
) error {
	if uuid.Validate(grantID) != nil || uuid.Validate(deliveryID) != nil || uuid.Validate(organizationID) != nil ||
		uuid.Validate(projectID) != nil || !validSHA256Text(credentialSHA256) || generation == 0 {
		return domainerrs.ErrUnauthorized
	}
	active, err := service.control.ValidateInteractionDeliveryReadback(ctx,
		stableID(grantID, "interaction-delivery-readback:"+credentialSHA256), grantID, deliveryID,
		organizationID, projectID, credentialSHA256, generation)
	if err != nil {
		return domainerrs.ErrUnavailable
	}
	if !active {
		return domainerrs.ErrUnauthorized
	}
	return nil
}

func (service *Service) issueArtifactDownload(ctx context.Context, delivery entity.Delivery,
	boundary entity.Boundary, artifact entity.ArtifactBinding,
) (string, error) {
	if artifact.ScanState != "CLEAN" || boundary.ActorID == "" || boundary.MattermostUserID == "" ||
		boundary.BotStableKey == "" || boundary.BotProviderUserID == "" || boundary.BotProviderGeneration == 0 ||
		delivery.SessionID == "" || delivery.TurnID == "" || artifact.ArtifactID == "" || artifact.Version == 0 ||
		!validSHA256Text(artifact.SHA256) {
		return "", errors.New("artifact download lineage is invalid")
	}
	generation := uint64(1)
	grantID := stableID(delivery.ID, fmt.Sprintf("artifact-download:%s:%d:%s", artifact.ArtifactID, artifact.Version, artifact.SHA256))
	issuedDigest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		Version, Generation                                                       uint64
		GrantID, DeliveryID, OrganizationID, ProjectID, ActorID, MattermostUserID string
		TeamID, ChannelID, BotStableKey, BotProviderUserID, SessionID, TurnID     string
		ArtifactID, ArtifactSHA256                                                string
		BotProviderGeneration, ArtifactVersion                                    uint64
	}{
		1, generation, grantID, delivery.ID, delivery.OrganizationID, delivery.ProjectID, boundary.ActorID,
		boundary.MattermostUserID, delivery.TeamID, delivery.ChannelID, boundary.BotStableKey,
		boundary.BotProviderUserID, delivery.SessionID, delivery.TurnID, artifact.ArtifactID,
		artifact.SHA256, boundary.BotProviderGeneration, artifact.Version,
	})
	if err != nil {
		return "", errors.New("encode artifact download lineage")
	}
	grant := entity.DownloadGrant{
		ID: grantID, Generation: generation, OrganizationID: delivery.OrganizationID,
		ProjectID: delivery.ProjectID, ActorID: boundary.ActorID, MattermostUserID: boundary.MattermostUserID,
		TeamID: delivery.TeamID, ChannelID: delivery.ChannelID, BotStableKey: boundary.BotStableKey,
		BotProviderUserID: boundary.BotProviderUserID, BotProviderGeneration: boundary.BotProviderGeneration,
		SessionID: delivery.SessionID,
		TurnID:    delivery.TurnID, Artifact: artifact, ExpiresAt: service.now().UTC().Add(service.config.ArtifactDownloadTTL),
		IssuedPayloadSHA256: issuedDigest,
	}
	if err := service.repository.SaveDownloadGrant(ctx, grant); err != nil {
		return "", err
	}
	return strings.TrimSuffix(service.config.ArtifactDownloadBaseURL, "/") + "/mattermost/v1/artifacts/" + grant.ID + "/content", nil
}

func (service *Service) DownloadArtifact(ctx context.Context, grantID, authorization string) (entity.ArtifactBinding, []byte, error) {
	if uuid.Validate(grantID) != nil {
		return entity.ArtifactBinding{}, nil, domainerrs.ErrNotFound
	}
	grant, err := service.repository.GetDownloadGrant(ctx, grantID)
	if err != nil || !grant.ConsumedAt.IsZero() || !grant.RevokedAt.IsZero() ||
		!grant.ExpiresAt.After(service.now().UTC()) || grant.Artifact.ScanState != "CLEAN" {
		return entity.ArtifactBinding{}, nil, domainerrs.ErrNotFound
	}
	if err := service.mattermost.AuthenticateArtifactDownload(ctx, authorization, grant); err != nil {
		return entity.ArtifactBinding{}, nil, domainerrs.ErrUnauthorized
	}
	boundary, boundaryErr := service.mattermost.ResolveMappedChannel(ctx, grant.TeamID, grant.ChannelID)
	if boundaryErr != nil || boundary.OrganizationID != grant.OrganizationID || boundary.ProjectID != grant.ProjectID {
		return entity.ArtifactBinding{}, nil, domainerrs.ErrNotFound
	}
	if boundaryErr = service.requireBoundTeam(ctx, boundary); boundaryErr != nil {
		return entity.ArtifactBinding{}, nil, domainerrs.ErrNotFound
	}
	raw, err := service.objects.Get(ctx, grant.ProjectID, grant.Artifact.StorageRef,
		grant.Artifact.SizeBytes, grant.Artifact.SHA256)
	if err != nil {
		return entity.ArtifactBinding{}, nil, domainerrs.ErrUnavailable
	}
	if err := service.repository.ConsumeDownloadGrant(ctx, grant, grant.MattermostUserID); err != nil {
		return entity.ArtifactBinding{}, nil, domainerrs.ErrConflict
	}
	return grant.Artifact, raw, nil
}

func (service *Service) CheckInteraction(ctx context.Context) error {
	boundary, err := service.mattermost.ReadinessBoundary(ctx)
	if err != nil {
		return err
	}
	if err := service.requireBoundTeam(ctx, boundary); err != nil {
		return errors.New("Mattermost joined readiness mapping is not current")
	}
	inbound := entity.InboundEvent{
		ID: uuid.NewString(), ProviderEventID: "readiness:" + uuid.NewString(),
		Kind: enum.InboundPost, Revision: 1, TeamID: boundary.TeamID, ChannelID: boundary.ChannelID,
		UserID: boundary.BotStableKey, OrganizationID: boundary.OrganizationID, ProjectID: boundary.ProjectID,
		ChatID: boundary.ChatID, ActorID: boundary.ActorID, RoleID: boundary.RoleID,
		Locale: boundary.Locale, BotStableKey: boundary.BotStableKey,
		BotProviderUserID: boundary.BotProviderUserID, BotProviderGeneration: boundary.BotProviderGeneration,
		Text: "readiness",
	}
	inbound.DigestSHA256, err = eventDigest(inbound)
	if err != nil {
		return err
	}
	grant, err := service.authority.Sign(inbound)
	if err != nil {
		return err
	}
	return service.control.CheckInteraction(ctx, grant, boundary.ProjectID)
}

func (service *Service) processPrompt(ctx context.Context, inbound entity.InboundEvent) (Result, error) {
	if err := service.requireCurrentInboundBot(ctx, inbound); err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "MATTERMOST_BOT_IDENTITY_NOT_CURRENT")
	}
	grant, err := service.authority.Sign(inbound)
	if err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "AUTHORITY_SIGN_FAILED")
	}
	sessionScope := inbound.PostID
	if inbound.RootPostID != "" {
		sessionScope = inbound.RootPostID
	}
	if sessionScope == "" {
		sessionScope = inbound.ID
	}
	createdSession := false
	if inbound.SessionID == "" {
		if inbound.ChatID == "" || inbound.RoleID == "" {
			return Result{}, service.failInbound(ctx, inbound, "SESSION_MAPPING_MISSING")
		}
		sessionKey := stableID(inbound.ProjectID, "mattermost-thread:"+inbound.TeamID+":"+inbound.ChannelID+":"+sessionScope)
		session, createErr := service.control.CreateSession(ctx, grant, sessionKey,
			"Mattermost "+inbound.ChannelID, inbound.RoleID, inbound.ChatID)
		if createErr != nil {
			return Result{}, service.retryInbound(ctx, inbound, "SESSION_CREATE_FAILED")
		}
		inbound.SessionID = session.ID
		createdSession = true
	}
	turnIdempotencyKey := stableID(inbound.ID, "turn")
	expectedTurnID := stableID(turnIdempotencyKey, "control-plane-turn")
	expectedExecutionID := stableID(expectedTurnID, "runtime-execution:1")
	// Перед каждым turn bot-service заново подтверждает exact current Secret и
	// выдаёт свежую owner binding revision. Rotation меняет также UID/version/digest.
	binding, bindingErr := service.bot.EnsureRuntimeMCPBinding(ctx, domainbot.BindingRequest{
		ControlSessionID: inbound.SessionID, ChannelID: inbound.ChannelID, RootPostID: sessionScope,
		BotStableKey: inbound.BotStableKey, ExecutionID: expectedExecutionID, TurnID: expectedTurnID, Attempt: 1,
	})
	if bindingErr != nil {
		return Result{}, service.retryInbound(ctx, inbound, "MCP_BINDING_CREATE_FAILED")
	}
	_, bindingErr = service.control.BindSessionMCP(ctx, grant, domaincontrol.SessionMCPBindingInput{
		IdempotencyKey: stableID(inbound.ID, fmt.Sprintf("session-mcp-binding:%d:%s:%s",
			binding.AgentSessionVersion, binding.ProviderContentVersion, binding.ContentSHA256)),
		SessionID: inbound.SessionID, AgentSessionKey: binding.AgentSessionKey,
		AgentSessionID: binding.AgentSessionID, AgentSessionVersion: binding.AgentSessionVersion,
		AgentSessionBindingSHA256: binding.AgentSessionBindingSHA256,
		ImmutableSecretRef:        binding.ImmutableSecretRef, ProviderContentVersion: binding.ProviderContentVersion,
		ContentSHA256: binding.ContentSHA256,
	})
	if bindingErr != nil {
		return Result{}, service.retryInbound(ctx, inbound, "MCP_BINDING_REGISTER_FAILED")
	}
	if createdSession {
		inbound.State = enum.InboundProcessing
		inbound.NextAttemptAt = service.now()
		if err := service.repository.SaveInboundProgress(ctx, inbound); err != nil {
			return Result{}, err
		}
		if err := service.enqueueInformational(ctx, inbound, enum.DeliveryStatus, "card.status.created", map[string]any{"SessionID": inbound.SessionID}); err != nil {
			return Result{}, service.retryInbound(ctx, inbound, "STATUS_DELIVERY_ENQUEUE_FAILED")
		}
	}
	if len(inbound.FileIDs) > 0 {
		if len(inbound.FileIDs) > service.config.MaximumFiles {
			return Result{}, service.failInbound(ctx, inbound, "TOO_MANY_FILES")
		}
		for index, fileID := range inbound.FileIDs {
			if hasArtifactForFile(inbound.AttachmentArtifacts, fileID) {
				continue
			}
			raw, name, mediaType, downloadErr := service.mattermost.DownloadFile(ctx, inbound.ChannelID, fileID)
			if downloadErr != nil {
				return Result{}, service.retryInbound(ctx, inbound, "FILE_DOWNLOAD_FAILED")
			}
			name = safeName(name, index)
			digest := digestBytes(raw)
			object, putErr := service.objects.Put(ctx, inbound.ProjectID,
				path.Join("inbound", inbound.ID, "files", digest+"-"+name), raw, mediaType, digest)
			if putErr != nil {
				return Result{}, service.retryInbound(ctx, inbound, "OBJECT_WRITE_FAILED")
			}
			artifact, registerErr := service.control.RegisterArtifact(ctx, grant, domaincontrol.ArtifactInput{
				IdempotencyKey: stableID(inbound.ID, "file:"+fileID), Name: name, ParentID: inbound.SessionID,
				Kind: "mattermost-upload", Direction: "INPUT", StorageRef: object.Reference,
				SizeBytes: uint64(len(raw)), MediaType: mediaType, SHA256: digest, RetentionRef: service.config.RetentionRef,
			})
			if registerErr != nil {
				return Result{}, service.retryInbound(ctx, inbound, "ARTIFACT_REGISTER_FAILED")
			}
			inbound.AttachmentArtifacts = append(inbound.AttachmentArtifacts, entity.ArtifactBinding{
				ArtifactID: artifact.ID, Version: artifact.Version, FileID: fileID, Name: name,
				Path: fmt.Sprintf("inputs/%02d-%s", index+1, name), StorageRef: object.Reference,
				SizeBytes: uint64(len(raw)), MediaType: mediaType, SHA256: digest,
				Provenance: "mattermost://" + inbound.TeamID + "/" + inbound.ChannelID + "/files/" + fileID,
				ScanState:  artifact.ScanState,
			})
			inbound.State = enum.InboundProcessing
			if err := service.repository.SaveInboundProgress(ctx, inbound); err != nil {
				return Result{}, err
			}
		}
	}
	if inbound.PromptArtifactID == "" {
		if len(inbound.Text) == 0 || len([]byte(inbound.Text)) > service.config.MaximumPromptBytes {
			return Result{}, service.failInbound(ctx, inbound, "PROMPT_SIZE_INVALID")
		}
		promptRaw := []byte(inbound.Text)
		promptDigest := digestBytes(promptRaw)
		object, putErr := service.objects.Put(ctx, inbound.ProjectID,
			path.Join("inbound", inbound.ID, "prompt.md"), promptRaw, "text/markdown", promptDigest)
		if putErr != nil {
			return Result{}, service.retryInbound(ctx, inbound, "MANIFEST_WRITE_FAILED")
		}
		artifact, registerErr := service.control.RegisterArtifact(ctx, grant, domaincontrol.ArtifactInput{
			IdempotencyKey: stableID(inbound.ID, "prompt"), Name: "prompt.md",
			ParentID: inbound.SessionID, Kind: "prompt", Direction: "INPUT",
			StorageRef: object.Reference, SizeBytes: uint64(len(promptRaw)), MediaType: "text/markdown",
			SHA256: promptDigest, RetentionRef: service.config.RetentionRef,
		})
		if registerErr != nil {
			return Result{}, service.retryInbound(ctx, inbound, "PROMPT_REGISTER_FAILED")
		}
		inbound.PromptArtifactID = artifact.ID
	}
	clean, terminal, scanErr := service.artifactsReady(ctx, grant, &inbound)
	if scanErr != nil {
		return Result{}, service.retryInbound(ctx, inbound, "ARTIFACT_READ_FAILED")
	}
	if terminal {
		return Result{}, service.failInbound(ctx, inbound, "ARTIFACT_SCAN_REJECTED")
	}
	if !clean {
		if err := service.enqueueInformational(ctx, inbound, enum.DeliveryArtifact, "card.artifact.waiting", nil); err != nil {
			return Result{}, service.retryInbound(ctx, inbound, "ARTIFACT_DELIVERY_ENQUEUE_FAILED")
		}
		inbound.State, inbound.NextAttemptAt = enum.InboundWaitingScan, service.now().Add(service.config.ScanPollInterval)
		inbound.SemanticOutcome = "SUCCESS"
		inbound.ResponseMessage = service.text(inbound.Locale, "command.waiting_scan", nil)
		inbound.NextAction = "WAIT_FOR_SCAN"
		if err := service.repository.SaveInboundProgress(ctx, inbound); err != nil {
			return Result{}, err
		}
		return Result{Message: inbound.ResponseMessage}, nil
	}
	sourceReference := "mattermost://" + inbound.TeamID + "/" + inbound.ChannelID + "/posts/" + inbound.PostID
	if inbound.PostID == "" {
		sourceReference = "mattermost://" + inbound.TeamID + "/" + inbound.ChannelID + "/commands/" + inbound.ProviderEventID
	}
	attachmentIDs := make([]string, 0, len(inbound.AttachmentArtifacts))
	for _, attachment := range inbound.AttachmentArtifacts {
		attachmentIDs = append(attachmentIDs, attachment.ArtifactID)
	}
	turn, err := service.control.EnqueueTurn(ctx, grant, turnIdempotencyKey, inbound.SessionID,
		sourceReference, inbound.PromptArtifactID, attachmentIDs)
	if err != nil || turn.ID != expectedTurnID {
		return Result{}, service.retryInbound(ctx, inbound, "TURN_ENQUEUE_FAILED")
	}
	if err := service.enqueueRunCard(ctx, inbound, turn.ID); err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "RUN_DELIVERY_ENQUEUE_FAILED")
	}
	message := service.text(inbound.Locale, "command.accepted", map[string]any{"SessionID": inbound.SessionID})
	if err := service.repository.CompleteInbound(ctx, inbound, inbound.SessionID, turn.ID, message); err != nil {
		return Result{}, err
	}
	return Result{Message: message}, nil
}

func hasArtifactForFile(bindings []entity.ArtifactBinding, fileID string) bool {
	for _, binding := range bindings {
		if binding.FileID == fileID {
			return true
		}
	}
	return false
}

func (service *Service) artifactsReady(ctx context.Context, grant string, inbound *entity.InboundEvent) (bool, bool, error) {
	allClean := true
	for index := range inbound.AttachmentArtifacts {
		binding := &inbound.AttachmentArtifacts[index]
		artifact, err := service.control.GetArtifact(ctx, grant, binding.ArtifactID, 0)
		if err != nil {
			return false, false, err
		}
		binding.Version, binding.ScanState = artifact.Version, artifact.ScanState
		if artifact.ScanState == "QUARANTINED" || artifact.ScanState == "FAILED" {
			return false, true, nil
		}
		allClean = allClean && artifact.ScanState == "CLEAN"
	}
	prompt, err := service.control.GetArtifact(ctx, grant, inbound.PromptArtifactID, 0)
	if err != nil {
		return false, false, err
	}
	if prompt.ScanState == "QUARANTINED" || prompt.ScanState == "FAILED" {
		return false, true, nil
	}
	return allClean && prompt.ScanState == "CLEAN", false, nil
}

func (service *Service) handleReaction(ctx context.Context, inbound entity.InboundEvent) (Result, error) {
	if strings.HasPrefix(inbound.ProviderEventID, "reaction_removed:") {
		return Result{Ignored: true}, domainerrs.ErrIgnored
	}
	decision, ok := reactionDecision(inbound.Text)
	if !ok {
		return Result{Ignored: true}, domainerrs.ErrIgnored
	}
	if err := service.requireCurrentInboundBot(ctx, inbound); err != nil {
		return Result{}, domainerrs.ErrUnavailable
	}
	delivery, err := service.repository.GetDeliveryByProviderPost(ctx, inbound.PostID)
	if err != nil || delivery.OwnerGate == nil || delivery.ChannelID != inbound.ChannelID ||
		delivery.OwnerGate.RecipientActorID != inbound.ActorID {
		return Result{}, domainerrs.ErrNotFound
	}
	inbound.Kind, inbound.DeliveryID, inbound.Action = enum.InboundReaction, delivery.ID, decision
	inbound.ActionReason = "Mattermost reaction: " + inbound.Text
	inbound.DigestSHA256, err = eventDigest(inbound)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	stored, disposition, err := service.repository.ClaimInbound(ctx, inbound, service.config.InboundLease)
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	if disposition == domainrepo.InboundReplay {
		return service.replayInbound(stored)
	}
	if disposition == domainrepo.InboundBusy {
		return Result{}, domainerrs.ErrBusy
	}
	return service.resolveDecision(ctx, stored, delivery)
}

func (service *Service) requireCurrentInboundBot(ctx context.Context, inbound entity.InboundEvent) error {
	if inbound.TeamID == "" || inbound.ChannelID == "" || inbound.BotStableKey == "" ||
		inbound.BotProviderUserID == "" || inbound.BotProviderGeneration == 0 {
		return errors.New("Mattermost inbound bot identity boundary is invalid")
	}
	return service.mattermost.ValidateRuntimeBotIdentity(ctx, inbound.TeamID, inbound.ChannelID,
		inbound.BotStableKey, inbound.BotProviderUserID, inbound.BotProviderGeneration)
}

func (service *Service) resolveDecision(ctx context.Context, inbound entity.InboundEvent, delivery entity.Delivery) (Result, error) {
	grant, err := service.authority.Sign(inbound)
	if err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "AUTHORITY_SIGN_FAILED")
	}
	gate := delivery.OwnerGate
	err = service.control.ResolveOwnerGate(ctx, grant, domaincontrol.ResolveGateInput{
		IdempotencyKey: stableID(inbound.ID, "owner-decision"), GateID: gate.GateID,
		GateVersion: gate.GateVersion + 1, Decision: string(inbound.Action), Reason: inbound.ActionReason,
		ProcessRunID: gate.ProcessRunID, ProcessVersion: gate.ProcessVersion,
		SessionID: delivery.SessionID, TurnID: delivery.TurnID, Attempt: delivery.Attempt,
		ImmutableInputSHA256: delivery.ImmutableInputSHA256,
	})
	if err != nil {
		if errors.Is(err, domaincontrol.ErrConflict) {
			return Result{}, service.failInbound(ctx, inbound, "OWNER_DECISION_CONFLICT")
		}
		return Result{}, service.retryInbound(ctx, inbound, "OWNER_DECISION_FAILED")
	}
	if err := service.repository.MarkOwnerGateDecided(ctx, delivery); err != nil {
		return Result{}, err
	}
	message := service.text(inbound.Locale, "decision.recorded", nil)
	if err := service.repository.CompleteInbound(ctx, inbound, delivery.SessionID, delivery.TurnID, message); err != nil {
		return Result{}, err
	}
	return Result{Message: message}, nil
}

func (service *Service) acknowledgeDelivery(ctx context.Context, delivery entity.Delivery) error {
	if delivery.OwnerGate != nil {
		gate := delivery.OwnerGate
		if err := service.control.RecordOwnerGateDelivery(ctx, "", domaincontrol.RecordDeliveryInput{
			IdempotencyKey: stableID(delivery.ID, "record-owner-gate"), ProjectID: delivery.ProjectID, GateID: gate.GateID,
			GateVersion: gate.GateVersion, DeliveryID: delivery.ID, PayloadSHA256: gate.DeliveryPayloadSHA256,
			ClaimToken: gate.ClaimToken, ClaimFence: gate.ClaimFence, PostID: delivery.ProviderPostID,
			ChannelID: delivery.ChannelID, RootPostID: delivery.RootPostID,
			ProviderReceiptSHA256: delivery.ProviderReceiptSHA256,
		}); err != nil {
			return err
		}
	}
	if delivery.OwnerDelivery != nil {
		work := domaincontrol.InteractionDeliveryWork{
			DeliveryID: delivery.ID, ProjectID: delivery.ProjectID, Fence: delivery.OwnerDelivery.Fence,
			LeaseToken: delivery.OwnerDelivery.LeaseToken,
		}
		if err := service.control.RecordInteractionDelivery(ctx, stableID(delivery.ID, "record-owner-delivery"),
			work, delivery.ProviderReceiptSHA256); err != nil {
			return err
		}
	}
	return service.repository.CompleteDelivery(ctx, delivery)
}

func (service *Service) ownerGateCard(delivery entity.Delivery, claim entity.OwnerGateClaim, token string) map[string]any {
	contextFor := func(action enum.OwnerDecision) map[string]any {
		return map[string]any{"delivery_id": delivery.ID, "callback_token": token, "action": action}
	}
	actions := []map[string]any{
		{"id": "approve", "name": service.text(delivery.Locale, "card.owner.approve", nil), "type": "button", "integration": map[string]any{"url": service.config.ActionCallbackURL, "context": contextFor(enum.DecisionApprove)}},
		{"id": "reject", "name": service.text(delivery.Locale, "card.owner.reject", nil), "type": "button", "integration": map[string]any{"url": service.config.ActionCallbackURL, "context": contextFor(enum.DecisionReject)}},
		{"id": "changes", "name": service.text(delivery.Locale, "card.owner.changes", nil), "type": "button", "integration": map[string]any{"url": service.config.ActionCallbackURL, "context": contextFor(enum.DecisionChangesRequested)}},
		{"id": "cancel", "name": service.text(delivery.Locale, "card.owner.cancel", nil), "type": "button", "integration": map[string]any{"url": service.config.ActionCallbackURL, "context": contextFor(enum.DecisionCancel)}},
		{"id": "reason", "name": "…", "type": "button", "integration": map[string]any{"url": service.config.ActionCallbackURL, "context": map[string]any{"delivery_id": delivery.ID, "callback_token": token, "action": "OPEN_DIALOG"}}},
	}
	return map[string]any{
		"message": service.text(delivery.Locale, "card.owner.title", nil),
		"props": map[string]any{"attachments": []map[string]any{{
			"title":   service.text(delivery.Locale, "card.owner.title", nil),
			"text":    service.text(delivery.Locale, "card.owner.summary", map[string]any{"Summary": claim.Summary}),
			"actions": actions,
			"fields":  []map[string]any{{"short": true, "title": "session", "value": delivery.SessionID}, {"short": true, "title": "turn", "value": delivery.TurnID}},
		}}},
	}
}

func (service *Service) enqueueRunCard(ctx context.Context, inbound entity.InboundEvent, turnID string) error {
	deliveryID := stableID(inbound.ID, "card:"+string(enum.DeliveryRun))
	delivery := entity.Delivery{
		ID: deliveryID, Kind: enum.DeliveryRun, State: enum.DeliveryPending,
		OrganizationID: inbound.OrganizationID, ProjectID: inbound.ProjectID,
		SessionID: inbound.SessionID, TurnID: turnID, TeamID: inbound.TeamID,
		ChannelID: inbound.ChannelID, RootPostID: inbound.RootPostID,
		BotStableKey: inbound.BotStableKey, BotProviderUserID: inbound.BotProviderUserID,
		BotProviderGeneration: inbound.BotProviderGeneration, Locale: inbound.Locale,
		CreatedAt: service.now(), UpdatedAt: service.now(),
	}
	token, err := service.authority.CallbackToken(delivery, inbound.ActorID)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"message": service.text(inbound.Locale, "card.run.queued", nil),
		"props": map[string]any{"attachments": []map[string]any{{
			"title":   service.text(inbound.Locale, "card.run.queued", nil),
			"text":    service.text(inbound.Locale, "card.run.body", map[string]any{"SessionID": inbound.SessionID, "TurnID": turnID}),
			"actions": []map[string]any{service.runtimeActionButton(delivery, token, "STOP")},
		}}},
	}
	delivery.Payload, delivery.PayloadSHA256, err = encodeDeliveryPayload(delivery.ID, payload)
	if err != nil {
		return err
	}
	_, _, err = service.repository.EnqueueDelivery(ctx, delivery)
	return err
}

func (service *Service) enqueueRuntimeActionCard(ctx context.Context, inbound entity.InboundEvent,
	previous entity.Delivery, turn domaincontrol.Turn, stoppable bool, message string,
) error {
	delivery := entity.Delivery{
		ID: stableID(inbound.ID, "runtime-action-card"), Kind: enum.DeliveryRun,
		State: enum.DeliveryPending, OrganizationID: previous.OrganizationID, ProjectID: previous.ProjectID,
		SessionID: previous.SessionID, TurnID: previous.TurnID, Attempt: turn.Attempt,
		ImmutableInputSHA256: turn.ImmutableInputSHA256, TeamID: previous.TeamID, ChannelID: previous.ChannelID,
		RootPostID: previous.RootPostID, BotStableKey: previous.BotStableKey,
		BotProviderUserID: previous.BotProviderUserID, BotProviderGeneration: previous.BotProviderGeneration,
		Locale:       previous.Locale,
		UpdatePostID: previous.ProviderPostID, CreatedAt: service.now(), UpdatedAt: service.now(),
	}
	token, err := service.authority.CallbackToken(delivery, inbound.ActorID)
	if err != nil {
		return err
	}
	attachment := map[string]any{"title": message, "text": service.text(inbound.Locale, "card.run.body",
		map[string]any{"SessionID": delivery.SessionID, "TurnID": delivery.TurnID})}
	if stoppable {
		attachment["actions"] = []map[string]any{service.runtimeActionButton(delivery, token, "STOP")}
	}
	delivery.Payload, delivery.PayloadSHA256, err = encodeDeliveryPayload(delivery.ID,
		map[string]any{"message": message, "props": map[string]any{"attachments": []map[string]any{attachment}}})
	if err != nil {
		return err
	}
	_, _, err = service.repository.EnqueueDelivery(ctx, delivery)
	return err
}

func (service *Service) runtimeActionButton(delivery entity.Delivery, token, action string) map[string]any {
	label := service.text(delivery.Locale, "card.run.stop", nil)
	if action == "RETRY" {
		label = service.text(delivery.Locale, "card.run.retry", nil)
	}
	return map[string]any{
		"id": strings.ToLower(action), "name": label, "type": "button",
		"integration": map[string]any{
			"url":     service.config.ActionCallbackURL,
			"context": map[string]any{"delivery_id": delivery.ID, "callback_token": token, "action": action},
		},
	}
}

func (service *Service) enqueueInformational(ctx context.Context, inbound entity.InboundEvent,
	kind enum.DeliveryKind, messageID string, data map[string]any,
) error {
	return service.enqueueCard(ctx, inbound, "", kind, map[string]any{"message": service.text(inbound.Locale, messageID, data)})
}

func (service *Service) enqueueCard(ctx context.Context, inbound entity.InboundEvent, turnID string,
	kind enum.DeliveryKind, payload map[string]any,
) error {
	deliveryID := stableID(inbound.ID, "card:"+string(kind))
	raw, payloadDigest, err := encodeDeliveryPayload(deliveryID, payload)
	if err != nil {
		return err
	}
	delivery := entity.Delivery{
		ID: deliveryID, Kind: kind, State: enum.DeliveryPending,
		OrganizationID: inbound.OrganizationID, ProjectID: inbound.ProjectID,
		SessionID: inbound.SessionID, TurnID: turnID, TeamID: inbound.TeamID,
		ChannelID: inbound.ChannelID, RootPostID: inbound.RootPostID,
		BotStableKey: inbound.BotStableKey, BotProviderUserID: inbound.BotProviderUserID,
		BotProviderGeneration: inbound.BotProviderGeneration, Locale: inbound.Locale,
		Payload: raw, PayloadSHA256: payloadDigest, CreatedAt: service.now(), UpdatedAt: service.now(),
	}
	_, _, err = service.repository.EnqueueDelivery(ctx, delivery)
	return err
}

func (service *Service) retryInbound(ctx context.Context, inbound entity.InboundEvent, code string) error {
	terminal := inbound.Attempts >= service.config.MaximumAttempts
	next := service.now().Add(backoff(service.config.RetryBase, inbound.Attempts))
	if terminal {
		if err := service.enqueueInformational(ctx, inbound, enum.DeliveryIncident, "card.incident.body", map[string]any{
			"Code": code, "NextAction": service.text(inbound.Locale, "card.incident.next_action", nil),
		}); err != nil {
			return err
		}
	}
	message := ""
	if terminal {
		message = service.text(inbound.Locale, "command.failed", map[string]any{"Code": code})
	}
	if err := service.repository.RetryInbound(ctx, inbound, code, message, "RETRY_AFTER_RECOVERY", next, terminal); err != nil {
		return err
	}
	if terminal {
		return domainerrs.WithResponse(domainerrs.ErrUnavailable, message)
	}
	return domainerrs.ErrUnavailable
}

func (service *Service) failInbound(ctx context.Context, inbound entity.InboundEvent, code string) error {
	if err := service.enqueueInformational(ctx, inbound, enum.DeliveryIncident, "card.incident.body", map[string]any{
		"Code": code, "NextAction": service.text(inbound.Locale, "card.incident.next_action", nil),
	}); err != nil {
		return err
	}
	message := service.text(inbound.Locale, "command.failed", map[string]any{"Code": code})
	if err := service.repository.RetryInbound(ctx, inbound, code,
		message,
		"REVIEW_INCIDENT", service.now(), true); err != nil {
		return err
	}
	return domainerrs.WithResponse(domainerrs.ErrConflict, message)
}

func (service *Service) retryDelivery(ctx context.Context, delivery entity.Delivery, code string) error {
	attempt := delivery.Attempts
	if delivery.State == enum.DeliveryProviderAccepted {
		attempt = delivery.AckAttempts
	}
	terminal := attempt >= service.config.MaximumAttempts
	return service.repository.RetryDelivery(ctx, delivery, code,
		service.now().Add(backoff(service.config.RetryBase, attempt)), terminal)
}

func (service *Service) failDelivery(ctx context.Context, delivery entity.Delivery, code string) error {
	return service.repository.RetryDelivery(ctx, delivery, code, service.now(), true)
}

func (service *Service) replayInbound(inbound entity.InboundEvent) (Result, error) {
	result := Result{Message: inbound.ResponseMessage, Replay: true}
	switch inbound.SemanticOutcome {
	case "SUCCESS":
		return result, nil
	case "IGNORED":
		result.Ignored = true
		return result, domainerrs.ErrIgnored
	case "ERROR":
		if inbound.NextAction == "RETRY_AFTER_RECOVERY" {
			return result, domainerrs.ErrUnavailable
		}
		return result, domainerrs.ErrConflict
	default:
		return Result{}, domainerrs.ErrConflict
	}
}

func (service *Service) text(locale, messageID string, data map[string]any) string {
	localizer := service.localizers[i18n.NormalizeLocale(locale)]
	return localizer.T(messageID, data)
}

func (service *Service) observeInbound(kind string, result Result, err error) {
	outcome := "success"
	switch {
	case result.Replay || errors.Is(err, domainerrs.ErrBusy):
		outcome = "replay"
	case result.Ignored || errors.Is(err, domainerrs.ErrIgnored):
		outcome = "ignored"
	case errors.Is(err, domainerrs.ErrUnavailable):
		outcome = "retry"
	case err != nil:
		outcome = "failure"
	}
	service.observer.ObserveInbound(kind, outcome)
}

func buildInbound(raw domainmattermost.RawEvent, boundary entity.Boundary) (entity.InboundEvent, error) {
	kind := enum.InboundKind(raw.Kind)
	if !kind.Valid() || raw.ProviderEventID == "" || raw.Revision == 0 {
		return entity.InboundEvent{}, errors.New("inbound event is invalid")
	}
	inbound := entity.InboundEvent{
		ID:              stableID(uuid.NameSpaceURL.String(), "mattermost:"+raw.ProviderEventID),
		ProviderEventID: raw.ProviderEventID, Kind: kind, Revision: raw.Revision,
		TeamID: raw.TeamID, ChannelID: raw.ChannelID, PostID: raw.PostID,
		RootPostID: raw.RootPostID, UserID: raw.UserID, Text: strings.TrimSpace(raw.Text),
		FileIDs: append([]string(nil), raw.FileIDs...), OrganizationID: boundary.OrganizationID,
		ProjectID: boundary.ProjectID, ChatID: boundary.ChatID, ActorID: boundary.ActorID,
		RoleID: boundary.RoleID, Locale: i18n.NormalizeLocale(boundary.Locale),
		BotStableKey: boundary.BotStableKey, BotProviderUserID: boundary.BotProviderUserID,
		BotProviderGeneration: boundary.BotProviderGeneration, SessionID: boundary.SessionID,
		State: enum.InboundProcessing, NextAttemptAt: time.Now().UTC(),
	}
	var err error
	inbound.DigestSHA256, err = eventDigest(inbound)
	return inbound, err
}

func eventDigest(inbound entity.InboundEvent) (string, error) {
	copyValue := inbound
	copyValue.DigestSHA256, copyValue.State = "", ""
	copyValue.Attempts, copyValue.NextAttemptAt = 0, time.Time{}
	copyValue.CreatedAt, copyValue.UpdatedAt = time.Time{}, time.Time{}
	return internalrpcauth.CanonicalJSONSHA256(copyValue)
}

func encodePayload(payload any) (json.RawMessage, string, error) {
	raw, err := internalrpcauth.CanonicalJSON(payload)
	if err != nil || len(raw) == 0 || len(raw) > 512<<10 {
		return nil, "", errors.New("delivery payload is invalid")
	}
	return raw, digestBytes(raw), nil
}

func encodeDeliveryPayload(deliveryID string, payload map[string]any) (json.RawMessage, string, error) {
	if uuid.Validate(deliveryID) != nil {
		return nil, "", errors.New("delivery identity is invalid")
	}
	content, err := internalrpcauth.CanonicalJSON(payload)
	if err != nil {
		return nil, "", errors.New("delivery content is invalid")
	}
	props, ok := payload["props"].(map[string]any)
	if !ok {
		props = map[string]any{}
		payload["props"] = props
	}
	if _, exists := props["matter_codex_delivery_id"]; exists {
		return nil, "", errors.New("delivery identity property is reserved")
	}
	if _, exists := props["matter_codex_content_sha256"]; exists {
		return nil, "", errors.New("delivery digest property is reserved")
	}
	props["matter_codex_delivery_id"] = deliveryID
	props["matter_codex_content_sha256"] = digestBytes(content)
	return encodePayload(payload)
}

func stableID(namespace, value string) string {
	parsed, err := uuid.Parse(namespace)
	if err != nil {
		parsed = uuid.NameSpaceURL
	}
	return uuid.NewSHA1(parsed, []byte(value)).String()
}

func digestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validSHA256Text(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("create delivery lease token")
	}
	return hex.EncodeToString(raw), nil
}

func hasNoTrigger(text string) bool {
	for _, field := range strings.Fields(text) {
		if strings.EqualFold(field, "#notrigger") {
			return true
		}
	}
	return false
}

func safeName(name string, index int) string {
	base := path.Base(strings.TrimSpace(name))
	var builder strings.Builder
	for _, symbol := range base {
		if unicode.IsLetter(symbol) || unicode.IsDigit(symbol) || symbol == '.' || symbol == '-' || symbol == '_' {
			builder.WriteRune(symbol)
		} else {
			builder.WriteByte('-')
		}
		if builder.Len() >= 96 {
			break
		}
	}
	result := strings.Trim(builder.String(), ".-")
	if result == "" || result == "." || result == ".." {
		return fmt.Sprintf("file-%02d", index+1)
	}
	return result
}

func reactionDecision(emoji string) (enum.OwnerDecision, bool) {
	switch emoji {
	case "white_check_mark":
		return enum.DecisionApprove, true
	case "x":
		return enum.DecisionReject, true
	case "repeat":
		return enum.DecisionChangesRequested, true
	case "stop_sign":
		return enum.DecisionCancel, true
	default:
		return "", false
	}
}

func backoff(base time.Duration, attempt uint32) time.Duration {
	if attempt > 8 {
		attempt = 8
	}
	result := base * time.Duration(1<<attempt)
	return min(result, 15*time.Minute)
}
