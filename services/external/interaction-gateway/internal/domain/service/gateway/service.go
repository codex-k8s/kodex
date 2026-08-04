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
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/codex-k8s/matter-codex/libs/go/i18n"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
	domainmattermost "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/mattermost"
	domainobjectstore "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/objectstore"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
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

type Config struct {
	ActionCallbackURL          string
	DialogCallbackURL          string
	RetentionRef               string
	MaximumPromptBytes         int
	MaximumFiles               int
	MaximumAttempts            uint32
	MaximumMattermostFileBytes int64
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
	authority  Authority
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
	control domaincontrol.Client, authority Authority, observer Observer, config Config) (*Service, error) {
	callback, err := url.Parse(config.ActionCallbackURL)
	dialogCallback, dialogErr := url.Parse(config.DialogCallbackURL)
	if repository == nil || mattermost == nil || objects == nil || control == nil || authority == nil || observer == nil ||
		err != nil || callback.Scheme != "https" || callback.Host == "" || callback.User != nil ||
		callback.RawQuery != "" || callback.Fragment != "" || config.RetentionRef == "" ||
		dialogErr != nil || dialogCallback.Scheme != "https" || dialogCallback.Host == "" || dialogCallback.User != nil ||
		dialogCallback.RawQuery != "" || dialogCallback.Fragment != "" ||
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
	return &Service{repository: repository, mattermost: mattermost, objects: objects,
		control: control, authority: authority, observer: observer, config: config, localizers: localizers, now: time.Now}, nil
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
	deliveryID, callbackToken string, decision enum.OwnerDecision, reason string) (result Result, resultErr error) {
	defer func() { service.observeInbound(raw.Kind, result, resultErr) }()
	if !decision.Valid() || uuid.Validate(deliveryID) != nil || len(reason) > 2048 {
		return Result{}, domainerrs.ErrConflict
	}
	boundary, verified, err := service.mattermost.ResolveInbound(ctx, raw)
	if err != nil || boundary.IgnoredBot {
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

func (service *Service) OpenDecisionDialog(ctx context.Context, raw domainmattermost.RawEvent,
	deliveryID, callbackToken, triggerID string) (Result, error) {
	boundary, verified, err := service.mattermost.ResolveInbound(ctx, raw)
	if err != nil || boundary.IgnoredBot || triggerID == "" {
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
	delivery, err := service.repository.GetDelivery(ctx, inbound.DeliveryID)
	if err != nil || delivery.OwnerGate == nil || delivery.ProviderPostID != inbound.PostID ||
		delivery.ChannelID != inbound.ChannelID || delivery.TeamID != inbound.TeamID ||
		delivery.OwnerGate.RecipientActorID != inbound.ActorID ||
		!service.authority.VerifyCallback(delivery, inbound.ActorID, inbound.CallbackToken) {
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
	if err := service.mattermost.OpenDecisionDialog(ctx, delivery.BotStableKey, inbound.TriggerID,
		service.config.DialogCallbackURL, state, delivery.Locale); err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "MATTERMOST_DIALOG_FAILED")
	}
	message := service.text(delivery.Locale, "decision.dialog_opened", nil)
	if err := service.repository.CompleteInbound(ctx, inbound, delivery.SessionID, delivery.TurnID, message); err != nil {
		return Result{}, err
	}
	return Result{Message: message}, nil
}

func (service *Service) ProcessWaiting(ctx context.Context) (bool, error) {
	inbound, ok, err := service.repository.ClaimWaitingInbound(ctx, service.config.InboundLease)
	if err != nil || !ok {
		return ok, err
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
	boundary entity.Boundary) (Result, error) {
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
	providerFileIDs := make([]string, 0, len(delivery.Attachments))
	for _, binding := range delivery.Attachments {
		if binding.ScanState != "CLEAN" {
			outcome = "dead_letter"
			return true, service.failDelivery(ctx, delivery, "ARTIFACT_NOT_CLEAN")
		}
		receipt, exists, receiptErr := service.repository.GetUploadReceipt(ctx, delivery, binding.ArtifactID)
		if receiptErr != nil {
			outcome = "failure"
			return true, receiptErr
		}
		if exists {
			if receipt.ChannelID != delivery.ChannelID || receipt.Name != binding.Name ||
				receipt.SizeBytes != binding.SizeBytes || receipt.MediaType != binding.MediaType ||
				receipt.SHA256 != binding.SHA256 {
				outcome = "dead_letter"
				return true, service.failDelivery(ctx, delivery, "UPLOAD_RECEIPT_MISMATCH")
			}
			providerFileIDs = append(providerFileIDs, receipt.ProviderFileID)
			continue
		}
		raw, readErr := service.objects.Get(ctx, delivery.ProjectID, binding.StorageRef, binding.SizeBytes, binding.SHA256)
		if readErr != nil {
			outcome = "retry"
			if delivery.Attempts >= service.config.MaximumAttempts {
				outcome = "dead_letter"
			}
			return true, service.retryDelivery(ctx, delivery, "OBJECT_READ_FAILED")
		}
		providerFileID, uploadErr := service.mattermost.UploadFile(ctx, delivery, binding, raw)
		if uploadErr != nil {
			service.observer.ObserveExternalEffect("upload_file", "failure")
			outcome = "retry"
			return true, service.retryDelivery(ctx, delivery, "MATTERMOST_UPLOAD_FAILED")
		}
		service.observer.ObserveExternalEffect("upload_file", "success")
		receipt = entity.UploadReceipt{DeliveryID: delivery.ID, ArtifactID: binding.ArtifactID,
			ProviderFileID: providerFileID, ChannelID: delivery.ChannelID, Name: binding.Name,
			SizeBytes: binding.SizeBytes, MediaType: binding.MediaType, SHA256: binding.SHA256}
		if err := service.repository.SaveUploadReceipt(ctx, delivery, receipt); err != nil {
			outcome = "failure"
			return true, err
		}
		providerFileIDs = append(providerFileIDs, providerFileID)
	}
	published, err := service.mattermost.Publish(ctx, delivery, providerFileIDs)
	effect := "create_post"
	if delivery.UpdatePostID != "" {
		effect = "update_post"
	}
	if err != nil {
		service.observer.ObserveExternalEffect(effect, "failure")
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
	boundary, err := service.mattermost.ResolveDelivery(claim.ProjectID, claim.RecipientActorID)
	if err != nil {
		return true, err
	}
	delivery := entity.Delivery{
		ID:   claim.DeliveryID,
		Kind: enum.DeliveryOwnerDecision, State: enum.DeliveryPending,
		OrganizationID: boundary.OrganizationID, ProjectID: claim.ProjectID,
		SessionID: claim.SessionID, TurnID: claim.TurnID, Attempt: claim.Attempt,
		ImmutableInputSHA256: claim.ImmutableInputSHA256, TeamID: boundary.TeamID,
		ChannelID: boundary.ChannelID, BotStableKey: boundary.BotStableKey, Locale: boundary.Locale,
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
	if publishResult && resultObject.Size <= uint64(service.config.MaximumMattermostFileBytes) {
		delivery.Attachments = []entity.ArtifactBinding{{
			ArtifactID: stableID(claim.GateID, "result-delivery"),
			Name:       safeName(resultObject.Name, 0), Path: "results/" + safeName(resultObject.Name, 0),
			StorageRef: resultObject.Reference, SizeBytes: resultObject.Size, MediaType: resultObject.MediaType,
			SHA256: resultObject.SHA256, Provenance: "control-plane-owner-gate:" + claim.GateID, ScanState: "CLEAN",
		}}
	} else if publishResult {
		protectedURL, err = service.objects.ProtectedURL(ctx, claim.ProjectID, resultObject.Reference,
			resultObject.Size, resultObject.SHA256, safeName(resultObject.Name, 0), 15*time.Minute)
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
			"card.artifact.protected_link", map[string]any{"URL": protectedURL, "Name": resultObject.Name,
				"Size": resultObject.Size, "SHA256": resultObject.SHA256}))
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

func (service *Service) ExpireOwnerGate(ctx context.Context) error {
	return service.control.ExpireOwnerGate(ctx, uuid.NewString())
}

func (service *Service) CatchUp(ctx context.Context) error {
	cursors, err := service.repository.LoadCursors(ctx, service.mattermost.ChannelBoundaries())
	if err != nil {
		return err
	}
	reactionPosts, err := service.repository.ListPendingReactionPosts(ctx, service.mattermost.ChannelBoundaries(), 1024)
	if err != nil {
		return err
	}
	return service.mattermost.CatchUp(ctx, cursors, reactionPosts, func(eventContext context.Context, raw domainmattermost.RawEvent) error {
		return service.consumeStreamEvent(eventContext, raw)
	})
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

func (service *Service) CheckInteraction(ctx context.Context) error {
	boundary, err := service.mattermost.ReadinessBoundary()
	if err != nil {
		return err
	}
	inbound := entity.InboundEvent{ID: uuid.NewString(), ProviderEventID: "readiness:" + uuid.NewString(),
		Kind: enum.InboundPost, Revision: 1, TeamID: boundary.TeamID, ChannelID: boundary.ChannelID,
		UserID: boundary.BotStableKey, OrganizationID: boundary.OrganizationID, ProjectID: boundary.ProjectID,
		ChatID: boundary.ChatID, ActorID: boundary.ActorID, RoleID: boundary.RoleID,
		Locale: boundary.Locale, BotStableKey: boundary.BotStableKey, Text: "readiness"}
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
	grant, err := service.authority.Sign(inbound)
	if err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "AUTHORITY_SIGN_FAILED")
	}
	if inbound.SessionID == "" {
		if inbound.ChatID == "" || inbound.RoleID == "" {
			return Result{}, service.failInbound(ctx, inbound, "SESSION_MAPPING_MISSING")
		}
		sessionScope := inbound.PostID
		if inbound.RootPostID != "" {
			sessionScope = inbound.RootPostID
		}
		if sessionScope == "" {
			sessionScope = inbound.ID
		}
		sessionKey := stableID(inbound.ProjectID, "mattermost-thread:"+inbound.TeamID+":"+inbound.ChannelID+":"+sessionScope)
		session, createErr := service.control.CreateSession(ctx, grant, sessionKey,
			"Mattermost "+inbound.ChannelID, inbound.RoleID, inbound.ChatID)
		if createErr != nil {
			return Result{}, service.retryInbound(ctx, inbound, "SESSION_CREATE_FAILED")
		}
		inbound.SessionID = session.ID
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
		manifest := entity.WorkspaceManifest{
			Version: 1, OrganizationID: inbound.OrganizationID, ProjectID: inbound.ProjectID,
			ChatID: inbound.ChatID, SessionID: inbound.SessionID, ProviderEventID: inbound.ProviderEventID,
			Prompt: inbound.Text, PromptSHA256: digestBytes([]byte(inbound.Text)), Files: inbound.AttachmentArtifacts,
		}
		manifestRaw, manifestErr := internalrpcauth.CanonicalJSON(manifest)
		if manifestErr != nil {
			return Result{}, service.failInbound(ctx, inbound, "MANIFEST_INVALID")
		}
		manifestDigest := digestBytes(manifestRaw)
		object, putErr := service.objects.Put(ctx, inbound.ProjectID,
			path.Join("inbound", inbound.ID, "workspace-manifest.json"), manifestRaw, "application/json", manifestDigest)
		if putErr != nil {
			return Result{}, service.retryInbound(ctx, inbound, "MANIFEST_WRITE_FAILED")
		}
		artifact, registerErr := service.control.RegisterArtifact(ctx, grant, domaincontrol.ArtifactInput{
			IdempotencyKey: stableID(inbound.ID, "prompt-manifest"), Name: "workspace-manifest.json",
			ParentID: inbound.SessionID, Kind: "workspace-manifest", Direction: "INPUT",
			StorageRef: object.Reference, SizeBytes: uint64(len(manifestRaw)), MediaType: "application/json",
			SHA256: manifestDigest, RetentionRef: service.config.RetentionRef,
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
	turn, err := service.control.EnqueueTurn(ctx, grant, stableID(inbound.ID, "turn"), inbound.SessionID,
		sourceReference, inbound.PromptArtifactID)
	if err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "TURN_ENQUEUE_FAILED")
	}
	if err := service.enqueueRunCard(ctx, inbound, turn.ID); err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "RUN_DELIVERY_ENQUEUE_FAILED")
	}
	if err := service.repository.SaveTurnWatch(ctx, inbound, turn.ID); err != nil {
		return Result{}, service.retryInbound(ctx, inbound, "TURN_WATCH_SAVE_FAILED")
	}
	message := service.text(inbound.Locale, "command.accepted", map[string]any{"SessionID": inbound.SessionID})
	if err := service.repository.CompleteInbound(ctx, inbound, inbound.SessionID, turn.ID, message); err != nil {
		return Result{}, err
	}
	return Result{Message: message}, nil
}

// ProcessTurnDelivery читает только авторитетную версию Turn и превращает её
// в один fenced delivery effect. Gateway не выводит terminal outcome из
// локального состояния EnqueueTurn.
func (service *Service) ProcessTurnDelivery(ctx context.Context, instanceID string) (bool, error) {
	leaseToken, err := randomToken()
	if err != nil {
		return false, err
	}
	watch, ok, err := service.repository.ClaimTurnWatch(ctx, instanceID, leaseToken, service.config.DeliveryLease)
	if err != nil || !ok {
		return ok, err
	}
	grant, err := service.authority.Sign(watch.Inbound)
	if err != nil {
		return true, service.repository.AdvanceTurnWatch(ctx, watch, watch.LastVersion, false,
			service.now().Add(service.config.RetryBase))
	}
	turn, err := service.control.GetTurn(ctx, grant, watch.TurnID)
	if err != nil || turn.ID != watch.TurnID || turn.SessionID != watch.Inbound.SessionID ||
		turn.Attempt == 0 || !validSHA256Text(turn.ImmutableInputSHA256) {
		return true, service.repository.AdvanceTurnWatch(ctx, watch, watch.LastVersion, false,
			service.now().Add(service.config.RetryBase))
	}
	terminal := turn.State == "SUCCEEDED" || turn.State == "FAILED" || turn.State == "CANCELLED" || turn.State == "EXPIRED"
	if turn.Version <= watch.LastVersion {
		return true, service.repository.AdvanceTurnWatch(ctx, watch, watch.LastVersion, terminal,
			service.now().Add(service.config.ScanPollInterval))
	}
	if watch.LastVersion > 0 {
		predecessor, predecessorErr := service.repository.GetDelivery(ctx,
			stableID(turn.ID, fmt.Sprintf("owner-turn-version:%d", watch.LastVersion)))
		if predecessorErr != nil || predecessor.State != enum.DeliveryDelivered {
			return true, service.repository.AdvanceTurnWatch(ctx, watch, watch.LastVersion, false,
				service.now().Add(service.config.RetryBase))
		}
	}
	runDelivery, err := service.repository.GetDelivery(ctx, stableID(watch.Inbound.ID, "card:"+string(enum.DeliveryRun)))
	if err != nil || runDelivery.State != enum.DeliveryDelivered || runDelivery.ProviderPostID == "" {
		return true, service.repository.AdvanceTurnWatch(ctx, watch, watch.LastVersion, false,
			service.now().Add(service.config.RetryBase))
	}
	delivery := entity.Delivery{
		ID: stableID(turn.ID, fmt.Sprintf("owner-turn-version:%d", turn.Version)), Kind: enum.DeliveryStatus,
		State: enum.DeliveryPending, OrganizationID: watch.Inbound.OrganizationID, ProjectID: watch.Inbound.ProjectID,
		SessionID: turn.SessionID, TurnID: turn.ID, Attempt: turn.Attempt,
		ImmutableInputSHA256: turn.ImmutableInputSHA256, TeamID: watch.Inbound.TeamID,
		ChannelID: watch.Inbound.ChannelID, RootPostID: watch.Inbound.RootPostID,
		BotStableKey: watch.Inbound.BotStableKey, Locale: watch.Inbound.Locale,
		UpdatePostID: runDelivery.ProviderPostID, CreatedAt: service.now(), UpdatedAt: service.now(),
	}
	messageID := "card.run.progress"
	if terminal {
		messageID = "card.run.terminal"
		delivery.Kind = enum.DeliveryRun
	}
	card := map[string]any{"message": service.text(delivery.Locale, messageID, map[string]any{
		"State": turn.State, "Outcome": turn.Outcome, "SessionID": turn.SessionID,
		"TurnID": turn.ID, "Attempt": turn.Attempt, "InputSHA256": turn.ImmutableInputSHA256,
	})}
	if turn.ResultArtifactID != "" {
		artifact, artifactErr := service.control.GetArtifact(ctx, grant, turn.ResultArtifactID,
			turn.ResultArtifactVersion)
		if artifactErr != nil || artifact.ID != turn.ResultArtifactID || artifact.Direction != "OUTPUT" ||
			artifact.Version != turn.ResultArtifactVersion || artifact.SHA256 != turn.ResultArtifactSHA256 {
			return true, service.repository.AdvanceTurnWatch(ctx, watch, watch.LastVersion, false,
				service.now().Add(service.config.RetryBase))
		}
		if artifact.ScanState == "CLEAN" {
			binding := entity.ArtifactBinding{ArtifactID: artifact.ID, Version: artifact.Version,
				Name: safeName(artifact.Name, 0), Path: "results/" + safeName(artifact.Name, 0),
				StorageRef: artifact.StorageRef, SizeBytes: artifact.SizeBytes, MediaType: artifact.MediaType,
				SHA256: artifact.SHA256, Provenance: "control-plane-turn:" + turn.ID,
				ScanState: artifact.ScanState}
			if artifact.SizeBytes <= uint64(service.config.MaximumMattermostFileBytes) {
				delivery.Attachments = []entity.ArtifactBinding{binding}
			} else {
				protected, linkErr := service.objects.ProtectedURL(ctx, delivery.ProjectID, artifact.StorageRef,
					artifact.SizeBytes, artifact.SHA256, binding.Name, 15*time.Minute)
				if linkErr != nil {
					return true, service.repository.AdvanceTurnWatch(ctx, watch, watch.LastVersion, false,
						service.now().Add(service.config.RetryBase))
				}
				card["message"] = fmt.Sprintf("%s\n\n%s", card["message"], service.text(delivery.Locale,
					"card.artifact.protected_link", map[string]any{"URL": protected, "Name": binding.Name,
						"Size": binding.SizeBytes, "SHA256": binding.SHA256}))
			}
		} else if terminal && (artifact.ScanState == "QUARANTINED" || artifact.ScanState == "FAILED") {
			delivery.Kind = enum.DeliveryIncident
			card["message"] = service.text(delivery.Locale, "card.incident.body", map[string]any{
				"Code": "ARTIFACT_SCAN_REJECTED", "NextAction": service.text(delivery.Locale, "card.incident.next_action", nil)})
		} else {
			return true, service.repository.AdvanceTurnWatch(ctx, watch, watch.LastVersion, false,
				service.now().Add(service.config.ScanPollInterval))
		}
	}
	delivery.Payload, delivery.PayloadSHA256, err = encodeDeliveryPayload(delivery.ID, card)
	if err != nil {
		return true, err
	}
	if _, _, err := service.repository.EnqueueDelivery(ctx, delivery); err != nil {
		return true, err
	}
	return true, service.repository.AdvanceTurnWatch(ctx, watch, turn.Version, terminal,
		service.now().Add(service.config.ScanPollInterval))
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
			IdempotencyKey: stableID(delivery.ID, "record-owner-gate"), GateID: gate.GateID,
			GateVersion: gate.GateVersion, DeliveryID: delivery.ID, PayloadSHA256: gate.DeliveryPayloadSHA256,
			ClaimToken: gate.ClaimToken, ClaimFence: gate.ClaimFence, PostID: delivery.ProviderPostID,
			ChannelID: delivery.ChannelID, RootPostID: delivery.RootPostID,
		}); err != nil {
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
	payload := map[string]any{
		"message": service.text(inbound.Locale, "card.run.queued", nil),
		"props": map[string]any{"attachments": []map[string]any{{
			"title": service.text(inbound.Locale, "card.run.queued", nil),
			"text":  service.text(inbound.Locale, "card.run.body", map[string]any{"SessionID": inbound.SessionID, "TurnID": turnID}),
		}}},
	}
	return service.enqueueCard(ctx, inbound, turnID, enum.DeliveryRun, payload)
}

func (service *Service) enqueueInformational(ctx context.Context, inbound entity.InboundEvent,
	kind enum.DeliveryKind, messageID string, data map[string]any) error {
	return service.enqueueCard(ctx, inbound, "", kind, map[string]any{"message": service.text(inbound.Locale, messageID, data)})
}

func (service *Service) enqueueCard(ctx context.Context, inbound entity.InboundEvent, turnID string,
	kind enum.DeliveryKind, payload map[string]any) error {
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
		BotStableKey: inbound.BotStableKey, Locale: inbound.Locale,
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
		BotStableKey: boundary.BotStableKey, SessionID: boundary.SessionID,
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
