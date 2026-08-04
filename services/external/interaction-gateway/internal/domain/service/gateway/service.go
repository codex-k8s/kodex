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
}

type Config struct {
	ActionCallbackURL  string
	DialogCallbackURL  string
	RetentionRef       string
	MaximumPromptBytes int
	MaximumFiles       int
	MaximumAttempts    uint32
	InboundLease       time.Duration
	DeliveryLease      time.Duration
	ScanPollInterval   time.Duration
	RetryBase          time.Duration
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
		if verified.Verified && verified.Kind == "POST" && verified.Cursor > 0 {
			if cursorErr := service.repository.AdvanceCursor(ctx, verified.ChannelID, verified.Cursor); cursorErr != nil {
				return Result{}, cursorErr
			}
		}
		return Result{}, domainerrs.ErrUnauthorized
	}
	if boundary.IgnoredBot || hasNoTrigger(verified.Text) {
		if verified.Kind == "POST" && verified.ChannelID != "" && verified.Cursor > 0 {
			if err := service.repository.AdvanceCursor(ctx, verified.ChannelID, verified.Cursor); err != nil {
				return Result{}, err
			}
		}
		return Result{Ignored: true}, domainerrs.ErrIgnored
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
		if err := service.repository.AdvanceCursor(ctx, inbound.ChannelID, verified.Cursor); err != nil {
			return Result{}, err
		}
	}
	switch disposition {
	case domainrepo.InboundReplay:
		return Result{Message: service.text(inbound.Locale, "command.accepted", map[string]any{"SessionID": stored.SessionID}), Replay: true}, nil
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
		return Result{Message: service.text(inbound.Locale, "decision.recorded", nil), Replay: true}, nil
	}
	if disposition == domainrepo.InboundBusy {
		return Result{}, domainerrs.ErrBusy
	}
	delivery, err := service.repository.GetDelivery(ctx, deliveryID)
	if err != nil || delivery.OwnerGate == nil || delivery.ProviderPostID != inbound.PostID ||
		delivery.ChannelID != inbound.ChannelID || delivery.TeamID != inbound.TeamID ||
		delivery.OwnerGate.RecipientActorID != inbound.ActorID ||
		!service.authority.VerifyCallback(delivery, inbound.ActorID, callbackToken) {
		_ = service.repository.RetryInbound(ctx, inbound.ID, "CALLBACK_LINEAGE_MISMATCH", service.now(), true)
		return Result{}, domainerrs.ErrNotFound
	}
	return service.resolveDecision(ctx, stored, delivery)
}

func (service *Service) OpenDecisionDialog(ctx context.Context, raw domainmattermost.RawEvent,
	deliveryID, callbackToken, triggerID string) (Result, error) {
	boundary, verified, err := service.mattermost.ResolveInbound(ctx, raw)
	if err != nil || boundary.IgnoredBot || triggerID == "" {
		return Result{}, domainerrs.ErrUnauthorized
	}
	delivery, err := service.repository.GetDelivery(ctx, deliveryID)
	if err != nil || delivery.OwnerGate == nil || delivery.ProviderPostID != verified.PostID ||
		delivery.ChannelID != verified.ChannelID || delivery.OwnerGate.RecipientActorID != boundary.ActorID ||
		!service.authority.VerifyCallback(delivery, boundary.ActorID, callbackToken) {
		return Result{}, domainerrs.ErrNotFound
	}
	stateRaw, err := internalrpcauth.CanonicalJSON(map[string]string{
		"delivery_id": deliveryID, "callback_token": callbackToken, "post_id": delivery.ProviderPostID,
	})
	if err != nil {
		return Result{}, domainerrs.ErrConflict
	}
	state := base64.RawURLEncoding.EncodeToString(stateRaw)
	if err := service.mattermost.OpenDecisionDialog(ctx, delivery.BotStableKey, triggerID,
		service.config.DialogCallbackURL, state, delivery.Locale); err != nil {
		return Result{}, domainerrs.ErrUnavailable
	}
	return Result{Message: service.text(delivery.Locale, "decision.recorded", nil)}, nil
}

func (service *Service) ProcessWaiting(ctx context.Context) (bool, error) {
	inbound, ok, err := service.repository.ClaimWaitingInbound(ctx, service.config.InboundLease)
	if err != nil || !ok {
		return ok, err
	}
	_, err = service.processPrompt(ctx, inbound)
	return true, err
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
			if delivery.Attempts >= service.config.MaximumAttempts {
				outcome = "dead_letter"
			}
			return true, service.retryDelivery(ctx, delivery, "DOMAIN_ACKNOWLEDGEMENT_FAILED")
		}
		return true, nil
	}
	attachments := make(map[string][]byte, len(delivery.Attachments))
	for _, binding := range delivery.Attachments {
		if binding.ScanState != "CLEAN" {
			outcome = "dead_letter"
			return true, service.failDelivery(ctx, delivery, "ARTIFACT_NOT_CLEAN")
		}
		raw, readErr := service.objects.Get(ctx, delivery.ProjectID, binding.StorageRef, binding.SizeBytes, binding.SHA256)
		if readErr != nil {
			outcome = "retry"
			if delivery.Attempts >= service.config.MaximumAttempts {
				outcome = "dead_letter"
			}
			return true, service.retryDelivery(ctx, delivery, "OBJECT_READ_FAILED")
		}
		attachments[binding.ArtifactID] = raw
	}
	published, err := service.mattermost.Publish(ctx, delivery, attachments)
	if err != nil {
		outcome = "retry"
		if delivery.Attempts >= service.config.MaximumAttempts {
			outcome = "dead_letter"
		}
		return true, service.retryDelivery(ctx, delivery, "MATTERMOST_DELIVERY_FAILED")
	}
	if err := service.repository.MarkProviderAccepted(ctx, delivery.ID, delivery.Fence, delivery.LeaseToken,
		published.PostID, published.ReceiptSHA256, published.RootPostID); err != nil {
		outcome = "failure"
		return true, err
	}
	delivery.ProviderPostID, delivery.ProviderReceiptSHA256, delivery.RootPostID = published.PostID, published.ReceiptSHA256, published.RootPostID
	delivery.State = enum.DeliveryProviderAccepted
	err = service.acknowledgeDelivery(ctx, delivery)
	if err != nil {
		outcome = "retry"
		if delivery.Attempts >= service.config.MaximumAttempts {
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
	if publishResult {
		delivery.Attachments = []entity.ArtifactBinding{{
			ArtifactID: stableID(claim.GateID, "result-delivery"),
			Name:       safeName(resultObject.Name, 0), Path: "results/" + safeName(resultObject.Name, 0),
			StorageRef: resultObject.Reference, SizeBytes: resultObject.Size, MediaType: resultObject.MediaType,
			SHA256: resultObject.SHA256, Provenance: "control-plane-owner-gate:" + claim.GateID, ScanState: "CLEAN",
		}}
	}
	token, err := service.authority.CallbackToken(delivery, claim.RecipientActorID)
	if err != nil {
		return true, err
	}
	payload := service.ownerGateCard(delivery, claim, token)
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
	cursors, err := service.repository.LoadCursors(ctx, service.mattermost.ChannelIDs())
	if err != nil {
		return err
	}
	reactionPosts, err := service.repository.ListPendingReactionPosts(ctx, 1024)
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
		if err := service.repository.SaveInboundProgress(ctx, inbound); err != nil {
			return Result{}, err
		}
		return Result{Message: service.text(inbound.Locale, "command.waiting_scan", nil)}, nil
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
	if err := service.repository.CompleteInbound(ctx, inbound.ID, inbound.SessionID, turn.ID); err != nil {
		return Result{}, err
	}
	return Result{Message: service.text(inbound.Locale, "command.accepted", map[string]any{"SessionID": inbound.SessionID})}, nil
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
		return Result{Message: service.text(inbound.Locale, "decision.recorded", nil), Replay: true}, nil
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
	if err := service.repository.MarkOwnerGateDecided(ctx, delivery.ID); err != nil {
		return Result{}, err
	}
	if err := service.repository.CompleteInbound(ctx, inbound.ID, delivery.SessionID, delivery.TurnID); err != nil {
		return Result{}, err
	}
	return Result{Message: service.text(inbound.Locale, "decision.recorded", nil)}, nil
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
	return service.repository.CompleteDelivery(ctx, delivery.ID, delivery.Fence)
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
		if err := service.enqueueInformational(ctx, inbound, enum.DeliveryIncident, "card.incident.body", map[string]any{"Code": code}); err != nil {
			return err
		}
	}
	if err := service.repository.RetryInbound(ctx, inbound.ID, code, next, terminal); err != nil {
		return err
	}
	return domainerrs.ErrUnavailable
}

func (service *Service) failInbound(ctx context.Context, inbound entity.InboundEvent, code string) error {
	if err := service.enqueueInformational(ctx, inbound, enum.DeliveryIncident, "card.incident.body", map[string]any{"Code": code}); err != nil {
		return err
	}
	if err := service.repository.RetryInbound(ctx, inbound.ID, code, service.now(), true); err != nil {
		return err
	}
	return domainerrs.ErrConflict
}

func (service *Service) retryDelivery(ctx context.Context, delivery entity.Delivery, code string) error {
	terminal := delivery.Attempts >= service.config.MaximumAttempts
	return service.repository.RetryDelivery(ctx, delivery.ID, delivery.Fence, code,
		service.now().Add(backoff(service.config.RetryBase, delivery.Attempts)), terminal)
}

func (service *Service) failDelivery(ctx context.Context, delivery entity.Delivery, code string) error {
	return service.repository.RetryDelivery(ctx, delivery.ID, delivery.Fence, code, service.now(), true)
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
