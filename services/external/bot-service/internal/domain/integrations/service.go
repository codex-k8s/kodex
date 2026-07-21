package integrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	defaultApprovalTTL      = 4 * time.Hour
	defaultDeliveryLease    = 30 * time.Second
	defaultPollAfterSeconds = 2
)

// ServiceConfig задаёт порты интеграционного прикладного сценария.
type ServiceConfig struct {
	Repository    Repository
	Admission     SessionAdmission
	CardPublisher ApprovalCardPublisher
	Now           func() time.Time
	Random        io.Reader
	ApprovalTTL   time.Duration
	DeliveryLease time.Duration
}

// Service реализует каталог и T1 request/replay опасной capability.
type Service struct {
	repository    Repository
	admission     SessionAdmission
	cardPublisher ApprovalCardPublisher
	now           func() time.Time
	random        io.Reader
	approvalTTL   time.Duration
	deliveryLease time.Duration
}

func NewService(cfg ServiceConfig) *Service {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	randomSource := cfg.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	approvalTTL := cfg.ApprovalTTL
	if approvalTTL <= 0 {
		approvalTTL = defaultApprovalTTL
	}
	deliveryLease := cfg.DeliveryLease
	if deliveryLease <= 0 {
		deliveryLease = defaultDeliveryLease
	}
	return &Service{
		repository: cfg.Repository, admission: cfg.Admission, cardPublisher: cfg.CardPublisher,
		now: now, random: randomSource, approvalTTL: approvalTTL, deliveryLease: deliveryLease,
	}
}

// Catalog возвращает только возможности, разрешённые свежим grant текущей сессии.
func (svc *Service) Catalog(ctx context.Context, sessionKey string, token string) ([]CatalogEntry, error) {
	if svc == nil || svc.repository == nil || svc.admission == nil {
		return nil, ErrUnauthorized
	}
	session, err := svc.admission.AuthorizeIntegrationSession(ctx, sessionKey, token)
	if err != nil {
		return nil, ErrUnauthorized
	}
	entries, err := svc.repository.ListCatalog(ctx, session, svc.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("list integration catalog: %w", err)
	}
	return entries, nil
}

// RestartWorkload выполняет T1 или возвращает сохранённый результат после fresh authorization.
func (svc *Service) RestartWorkload(ctx context.Context, sessionKey string, token string, input RestartWorkloadInput) (ToolResult, error) {
	if svc == nil || svc.repository == nil || svc.admission == nil {
		return ToolResult{}, ErrUnauthorized
	}
	arguments, err := validateRestartInput(input)
	if err != nil {
		return ToolResult{}, err
	}
	session, err := svc.admission.AuthorizeIntegrationSession(ctx, sessionKey, token)
	if err != nil {
		return ToolResult{}, ErrUnauthorized
	}
	argumentsHash, err := hashArguments(arguments)
	if err != nil {
		return ToolResult{}, ErrInvalidInput
	}
	now := svc.now().UTC()
	invocationID, err := svc.randomPublicID("inv")
	if err != nil {
		return ToolResult{}, fmt.Errorf("generate invocation identity: %w", err)
	}
	approvalID, err := svc.randomPublicID("apr")
	if err != nil {
		return ToolResult{}, fmt.Errorf("generate approval identity: %w", err)
	}
	correlationID, err := svc.randomPublicID("cor")
	if err != nil {
		return ToolResult{}, fmt.Errorf("generate correlation identity: %w", err)
	}
	invocation, _, err := svc.repository.CreateOrReplayInvocation(ctx, CreateInvocationInput{
		Session: session, ConnectionPublicID: strings.TrimSpace(input.Connection), CapabilityKey: CapabilityRestartWorkload,
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), Arguments: arguments, ArgumentsHash: argumentsHash,
		InvocationPublicID: invocationID, ApprovalPublicID: approvalID, CorrelationID: correlationID,
		Now: now, ApprovalExpiresAt: now.Add(svc.approvalTTL),
	})
	if err != nil {
		return ToolResult{}, err
	}
	if invocation.Status == InvocationStatusPending && invocation.MattermostPostID == "" {
		_ = svc.ensureApprovalCard(ctx, invocation, now)
	}
	return toolResult(invocation), nil
}

func (svc *Service) ensureApprovalCard(ctx context.Context, invocation Invocation, now time.Time) error {
	if svc.cardPublisher == nil {
		return fmt.Errorf("approval card publisher is unavailable")
	}
	leaseOwner, err := svc.randomPublicID("delivery")
	if err != nil {
		return err
	}
	delivery, claimed, err := svc.repository.ClaimApprovalDelivery(ctx, invocation.ApprovalID, leaseOwner, now, svc.deliveryLease)
	if err != nil || !claimed {
		return err
	}
	postID, publishErr := svc.cardPublisher.EnsureApprovalCard(ctx, delivery)
	if publishErr != nil {
		releaseErr := svc.repository.ReleaseApprovalDelivery(ctx, invocation.ApprovalID, leaseOwner, "approval.delivery_failed", svc.now().UTC())
		return errors.Join(publishErr, releaseErr)
	}
	if err := svc.repository.CompleteApprovalDelivery(ctx, invocation.ApprovalID, leaseOwner, postID, svc.now().UTC()); err != nil {
		return fmt.Errorf("complete approval card delivery: %w", err)
	}
	return nil
}

// DecideApproval выполняет T2 для уже аутентифицированного Mattermost callback.
func (svc *Service) DecideApproval(ctx context.Context, input ApprovalDecisionInput) (ToolResult, error) {
	if svc == nil || svc.repository == nil {
		return ToolResult{}, ErrUnauthorized
	}
	input.Now = svc.now().UTC()
	invocation, err := svc.repository.DecideApproval(ctx, input)
	if err != nil {
		return ToolResult{}, err
	}
	return toolResult(invocation), nil
}

func (svc *Service) randomPublicID(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := io.ReadFull(svc.random, buffer); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(buffer), nil
}

func toolResult(invocation Invocation) ToolResult {
	status := invocation.Status
	reason := invocation.ReasonCode
	poll := 0
	if status == InvocationStatusApproved || status == InvocationStatusExecuting {
		status = InvocationStatusPending
		reason = "execution.pending"
		poll = defaultPollAfterSeconds
	} else if status == InvocationStatusPending {
		poll = defaultPollAfterSeconds
	}
	return ToolResult{
		Status: status, InvocationID: invocation.PublicID, ApprovalID: invocation.ApprovalPublicID,
		ArgumentsHash: invocation.ArgumentsHash, ReasonCode: reason, PollAfterSeconds: poll,
		Execution: invocation.Execution,
	}
}
