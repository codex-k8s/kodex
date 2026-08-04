package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	continuationclient "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/continuation"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/payloadcipher"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/provider"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/schemavalidator"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/security/canonical"
	"github.com/google/uuid"
)

const (
	maximumArgumentsBytes     = 256 << 10
	maximumInvocationTTL      = 7 * 24 * time.Hour
	connectionValidationTTL   = 5 * time.Minute
	continuationSafetyWindow  = 15 * time.Second
	continuationLeaseDuration = 10 * time.Second
)

var continuationNamespace = uuid.MustParse("0ec40cf9-4a63-4fc7-967d-e12fd76d9115")

type Config struct {
	SessionTTL                time.Duration
	InvocationTTL             time.Duration
	FinalizationTimeout       time.Duration
	MaximumSessionRequests    uint64
	MaximumConcurrentRequests uint32
}

type Service struct {
	repository   domainrepo.Repository
	cipher       payloadcipher.Cipher
	provider     provider.Client
	credentials  provider.CredentialSource
	validator    schemavalidator.Validator
	continuation continuationclient.Client
	config       Config
	now          func() time.Time
}

type Authority struct {
	TenantID                  string
	ProjectID                 string
	OwnerActorID              string
	ProcessID                 string
	SessionID                 string
	SessionVersion            uint64
	ThreadID                  string
	TurnID                    string
	TurnVersion               uint64
	Attempt                   uint32
	InputDigest               string
	RuntimeRevisionID         string
	RuntimeRevisionVersion    uint64
	RuntimeRevisionDigest     string
	RuntimeManifestDigest     string
	RoleID                    string
	RoleVersion               uint64
	GrantGeneration           uint64
	TokenDigest               string
	ApplicationGrant          string
	ApplicationGrantExpiresAt time.Time
	Connections               []entity.Connection
	Grants                    []entity.Grant
}

type InvokeInput struct {
	Scope              domainrepo.Scope
	TransportSessionID string
	Authority          Authority
	Tool               entity.Tool
	Connection         entity.Connection
	Grant              entity.Grant
	DefinitionDigest   string
	Arguments          json.RawMessage
	SemanticKey        string
}

type InvocationReceipt struct {
	InvocationID string                `json:"invocation_id"`
	Status       enum.InvocationStatus `json:"status"`
	RequestHash  string                `json:"request_hash"`
	ApprovalID   string                `json:"approval_id,omitempty"`
	PollPath     string                `json:"poll_path"`
}

type InvocationView struct {
	InvocationID string                `json:"invocation_id"`
	Status       enum.InvocationStatus `json:"status"`
	RequestHash  string                `json:"request_hash"`
	Preview      json.RawMessage       `json:"preview"`
	Approval     *ApprovalView         `json:"approval,omitempty"`
	Result       json.RawMessage       `json:"result,omitempty"`
	ResultDigest string                `json:"result_digest,omitempty"`
	CompletedAt  *time.Time            `json:"completed_at,omitempty"`
}

type ApprovalView struct {
	ApprovalID  string              `json:"approval_id"`
	Status      enum.ApprovalStatus `json:"status"`
	RequestHash string              `json:"request_hash"`
	Preview     json.RawMessage     `json:"preview"`
	ExpiresAt   time.Time           `json:"expires_at"`
	DecidedAt   *time.Time          `json:"decided_at,omitempty"`
	ReasonCode  string              `json:"reason_code,omitempty"`
}

func New(repository domainrepo.Repository, cipher payloadcipher.Cipher, providerClient provider.Client, credentials provider.CredentialSource, validator schemavalidator.Validator, continuation continuationclient.Client, config Config) (*Service, error) {
	if repository == nil || cipher == nil || providerClient == nil || credentials == nil || validator == nil ||
		continuation == nil ||
		config.SessionTTL < time.Minute || config.SessionTTL > 24*time.Hour ||
		config.InvocationTTL < time.Minute || config.InvocationTTL > maximumInvocationTTL ||
		config.FinalizationTimeout < time.Second || config.FinalizationTimeout > 30*time.Second ||
		config.MaximumSessionRequests == 0 || config.MaximumConcurrentRequests == 0 {
		return nil, errors.New("integration gateway service configuration is invalid")
	}
	return &Service{
		repository: repository, cipher: cipher, provider: providerClient, credentials: credentials,
		validator: validator, continuation: continuation, config: config, now: time.Now,
	}, nil
}

func (service *Service) StoreDefinition(ctx context.Context, definition entity.Definition) error {
	return service.repository.Transact(ctx, domainrepo.Scope{}, func(tx domainrepo.Transaction) error {
		return tx.StoreDefinition(ctx, definition)
	})
}

func (service *Service) AdmitSession(ctx context.Context, authority Authority, transportSessionID string) (entity.TransportSession, error) {
	now := service.now().UTC()
	if authority.TenantID == "" || authority.ProjectID == "" || authority.SessionID == "" ||
		authority.ProcessID == "" || authority.SessionVersion == 0 || authority.ThreadID == "" ||
		authority.TurnID == "" || authority.TurnVersion == 0 ||
		authority.Attempt == 0 || authority.InputDigest == "" ||
		authority.RuntimeRevisionID == "" || authority.RuntimeRevisionVersion == 0 ||
		authority.RuntimeRevisionDigest == "" || authority.RuntimeManifestDigest == "" ||
		authority.RoleID == "" || authority.RoleVersion == 0 || authority.GrantGeneration == 0 ||
		authority.TokenDigest == "" || authority.ApplicationGrant == "" ||
		authority.ApplicationGrantExpiresAt.IsZero() || transportSessionID == "" {
		return entity.TransportSession{}, errs.ErrForbidden
	}
	expiresAt := now.Add(service.config.SessionTTL)
	grantDeadline := authority.ApplicationGrantExpiresAt.Add(-continuationSafetyWindow)
	if expiresAt.After(grantDeadline) {
		expiresAt = grantDeadline
	}
	if !expiresAt.After(now) {
		return entity.TransportSession{}, errs.ErrExpired
	}
	session := entity.TransportSession{
		ID: transportSessionID, TenantID: authority.TenantID, ProjectID: authority.ProjectID,
		ProcessID: authority.ProcessID, AgentSessionID: authority.SessionID,
		AgentSessionVersion: authority.SessionVersion, ThreadID: authority.ThreadID,
		TurnID: authority.TurnID, TurnVersion: authority.TurnVersion, Attempt: authority.Attempt,
		InputDigest: authority.InputDigest, RuntimeRevisionID: authority.RuntimeRevisionID,
		RuntimeRevisionVersion: authority.RuntimeRevisionVersion,
		RuntimeRevisionDigest:  authority.RuntimeRevisionDigest,
		RuntimeManifestDigest:  authority.RuntimeManifestDigest, RoleID: authority.RoleID,
		RoleVersion: authority.RoleVersion, GrantGeneration: authority.GrantGeneration,
		TokenDigest: authority.TokenDigest, Status: enum.SessionInitializing,
		RequestCount: 1, ConcurrentRequests: 1,
		ExpiresAt: expiresAt, LastSeenAt: now,
	}
	audit := entity.AuditEvent{
		ID: uuid.NewString(), TenantID: authority.TenantID, ProjectID: authority.ProjectID,
		ActorID: authority.OwnerActorID, Action: "transport_session.initialize", ResourceKind: "TRANSPORT_SESSION",
		ResourceID: transportSessionID, Outcome: "ACCEPTED", OccurredAt: now,
	}
	scope := domainrepo.Scope{TenantID: authority.TenantID, ProjectID: authority.ProjectID, ActorID: authority.OwnerActorID}
	err := service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		return tx.AdmitSession(ctx, domainrepo.SessionAdmission{Session: session, Connections: authority.Connections, Grants: authority.Grants, Audit: audit})
	})
	return session, err
}

func (service *Service) TouchSession(ctx context.Context, scope domainrepo.Scope, sessionID, tokenDigest string, applicationGrantExpiresAt time.Time) (entity.TransportSession, error) {
	now := service.now().UTC()
	expiresAt := now.Add(service.config.SessionTTL)
	grantDeadline := applicationGrantExpiresAt.Add(-continuationSafetyWindow)
	if expiresAt.After(grantDeadline) {
		expiresAt = grantDeadline
	}
	if !expiresAt.After(now) {
		return entity.TransportSession{}, errs.ErrExpired
	}
	return service.repository.TouchSession(ctx, scope, sessionID, tokenDigest, now, expiresAt, service.config.MaximumSessionRequests, service.config.MaximumConcurrentRequests)
}

func (service *Service) ReleaseSession(ctx context.Context, scope domainrepo.Scope, sessionID string) error {
	return service.repository.ReleaseSession(ctx, scope, sessionID)
}

func (service *Service) CloseSession(ctx context.Context, scope domainrepo.Scope, sessionID string) error {
	now := service.now().UTC()
	audit := entity.AuditEvent{
		ID: uuid.NewString(), TenantID: scope.TenantID, ProjectID: scope.ProjectID,
		ActorID: scope.ActorID, Action: "transport_session.close", ResourceKind: "TRANSPORT_SESSION",
		ResourceID: sessionID, Outcome: "CLOSED", OccurredAt: now,
	}
	return service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		return tx.CloseSession(ctx, sessionID, now, audit)
	})
}

func (service *Service) GetInvocation(ctx context.Context, scope domainrepo.Scope, invocationID string) (entity.Invocation, *entity.Approval, *entity.Result, error) {
	return service.repository.GetInvocation(ctx, scope, invocationID)
}

func (service *Service) ReadInvocation(ctx context.Context, scope domainrepo.Scope, invocationID, expectedTransportSessionID string) (InvocationView, error) {
	invocation, approval, result, err := service.repository.GetInvocation(ctx, scope, invocationID)
	if err != nil {
		return InvocationView{}, err
	}
	if expectedTransportSessionID != "" && invocation.TransportSessionID != expectedTransportSessionID {
		return InvocationView{}, errs.ErrForbidden
	}
	view := InvocationView{
		InvocationID: invocation.ID, Status: invocation.Status,
		RequestHash: invocation.CanonicalRequestHash, Preview: invocation.Preview,
	}
	if approval != nil {
		view.Approval = &ApprovalView{
			ApprovalID: approval.ID, Status: approval.Status,
			RequestHash: approval.RequestHash, Preview: approval.Preview, ExpiresAt: approval.ExpiresAt,
			DecidedAt: approval.DecidedAt, ReasonCode: approval.DecisionReasonCode,
		}
	}
	if result != nil {
		if len(result.EncryptedPayload) > 0 {
			payload, err := service.cipher.Decrypt(ctx, result.EncryptedPayload)
			if err != nil {
				return InvocationView{}, err
			}
			view.Result = payload
		}
		view.ResultDigest = result.PayloadDigest
		view.CompletedAt = &result.CompletedAt
	}
	return view, nil
}

func (service *Service) ListTools(ctx context.Context, scope domainrepo.Scope, transportSessionID string) ([]domainrepo.ToolBinding, error) {
	return service.repository.ListTools(ctx, scope, transportSessionID)
}

func (service *Service) ValidateConnection(ctx context.Context, scope domainrepo.Scope, connectionID string) (entity.Connection, error) {
	now := service.now().UTC()
	connection, err := service.repository.GetConnection(ctx, scope, connectionID)
	if err != nil {
		return entity.Connection{}, err
	}
	if connection.Status == enum.ConnectionRevoked {
		return entity.Connection{}, errs.ErrForbidden
	}
	if connection.ExpiresAt != nil && !connection.ExpiresAt.After(now) {
		return entity.Connection{}, errs.ErrExpired
	}
	credentials, err := service.credentials.Resolve(ctx, connection)
	code := enum.ValidationCredentialUnavailable
	if err == nil {
		code = service.provider.Validate(ctx, connection, credentials)
	}
	connection.ValidationCode = code
	connection.ValidatedAt = &now
	if code == enum.ValidationOK {
		connection.Status = enum.ConnectionValid
	} else {
		connection.Status = enum.ConnectionInvalid
	}
	audit := entity.AuditEvent{
		ID: uuid.NewString(), TenantID: scope.TenantID, ProjectID: scope.ProjectID,
		ActorID: scope.ActorID, Action: "connection.validate", ResourceKind: "INTEGRATION_CONNECTION",
		ResourceID: connection.ID, Outcome: string(code), OccurredAt: now,
	}
	err = service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		return tx.SetConnectionValidation(ctx, domainrepo.ConnectionValidation{
			ConnectionID:       connection.ID,
			ExpectedGeneration: connection.Generation, Status: connection, Audit: audit,
		})
	})
	return connection, err
}

func (service *Service) Invoke(ctx context.Context, input InvokeInput) (InvocationReceipt, error) {
	now := service.now().UTC()
	arguments, err := canonical.Normalize(input.Arguments, maximumArgumentsBytes)
	if err != nil {
		return InvocationReceipt{}, errs.ErrInvalid
	}
	if service.validator.Validate(input.Tool.InputSchema, arguments) != nil {
		return InvocationReceipt{}, errs.ErrInvalid
	}
	if input.Scope.TenantID == "" || input.Scope.ProjectID == "" || input.Scope.ActorID == "" ||
		input.TransportSessionID == "" || input.Tool.Name == "" || input.Connection.ID == "" || input.Grant.ID == "" ||
		input.Authority.TenantID != input.Scope.TenantID || input.Authority.ProjectID != input.Scope.ProjectID ||
		input.Authority.OwnerActorID != input.Scope.ActorID ||
		input.Grant.TenantID != input.Scope.TenantID || input.Grant.ProjectID != input.Scope.ProjectID ||
		input.Connection.TenantID != input.Scope.TenantID || input.Connection.ProjectID != input.Scope.ProjectID ||
		input.Grant.Status != enum.GrantActive || !slices.Contains(input.Grant.Capabilities, input.Tool.Capability) ||
		!validDigestText(input.DefinitionDigest) || entity.ConnectionBindingDigest(input.Connection) == "" ||
		!slices.Contains(input.Grant.Permissions, input.Tool.Permission) ||
		input.Connection.Status != enum.ConnectionValid || input.Grant.ConnectionID != input.Connection.ID ||
		input.Grant.IntegrationID != input.Connection.IntegrationID ||
		input.Grant.Generation == 0 || input.Connection.Generation == 0 ||
		input.Connection.ValidatedAt == nil || input.Connection.ValidatedAt.After(now.Add(5*time.Second)) ||
		now.Sub(*input.Connection.ValidatedAt) > connectionValidationTTL ||
		!input.Grant.ExpiresAt.After(now) ||
		input.Authority.ApplicationGrant == "" || input.Authority.ApplicationGrantExpiresAt.IsZero() ||
		input.Authority.ProcessID != input.Grant.ProcessID || input.Authority.SessionID != input.Grant.SessionID ||
		input.Authority.SessionVersion != input.Grant.SessionVersion || input.Authority.ThreadID != input.Grant.ThreadID ||
		input.Authority.TurnID != input.Grant.TurnID || input.Authority.TurnVersion != input.Grant.TurnVersion ||
		input.Authority.Attempt != input.Grant.Attempt || input.Authority.InputDigest != input.Grant.InputDigest ||
		input.Authority.RuntimeRevisionID != input.Grant.RuntimeRevisionID ||
		input.Authority.RuntimeRevisionVersion != input.Grant.RuntimeRevisionVersion ||
		input.Authority.RuntimeRevisionDigest != input.Grant.RuntimeRevisionDigest ||
		input.Authority.RuntimeManifestDigest != input.Grant.RuntimeManifestDigest ||
		input.Authority.RoleID != input.Grant.RoleID || input.Authority.RoleVersion != input.Grant.RoleVersion ||
		input.Authority.GrantGeneration != input.Grant.Generation {
		return InvocationReceipt{}, errs.ErrForbidden
	}
	if uuid.Validate(input.SemanticKey) != nil {
		return InvocationReceipt{}, errs.ErrInvalid
	}
	hash, _, err := canonical.Hash(canonical.Request{
		DefinitionID: input.Connection.DefinitionID, DefinitionVersion: input.Connection.DefinitionVersion,
		DefinitionDigest: input.DefinitionDigest,
		ConnectionID:     input.Connection.ID, ConnectionRevision: input.Connection.Revision,
		ConnectionGeneration: input.Connection.Generation, ConnectionBindingDigest: entity.ConnectionBindingDigest(input.Connection),
		Capability: input.Tool.Capability,
		ToolName:   input.Tool.Name, ToolVersion: input.Tool.Version, TenantID: input.Scope.TenantID,
		ProjectID: input.Scope.ProjectID, ProcessID: input.Grant.ProcessID,
		SessionID: input.Grant.SessionID, SessionVersion: input.Grant.SessionVersion,
		ThreadID: input.Grant.ThreadID, TurnID: input.Grant.TurnID, TurnVersion: input.Grant.TurnVersion,
		Attempt: input.Grant.Attempt, InputDigest: input.Grant.InputDigest,
		RuntimeRevisionID: input.Grant.RuntimeRevisionID, RuntimeRevisionVersion: input.Grant.RuntimeRevisionVersion,
		RuntimeRevisionDigest: input.Grant.RuntimeRevisionDigest,
		RuntimeManifestDigest: input.Grant.RuntimeManifestDigest,
		RoleID:                input.Grant.RoleID, RoleVersion: input.Grant.RoleVersion,
		GrantID: input.Grant.ID, GrantGeneration: input.Grant.Generation,
		Method: input.Tool.HTTP.Method, Path: input.Tool.HTTP.Path,
		Arguments: arguments,
	})
	if err != nil {
		return InvocationReceipt{}, errs.ErrInvalid
	}
	preview, err := canonical.Preview(arguments, input.Tool.RedactionPointers)
	if err != nil {
		return InvocationReceipt{}, errs.ErrInvalid
	}
	encrypted, err := service.cipher.Encrypt(ctx, arguments)
	if err != nil {
		return InvocationReceipt{}, err
	}
	approvalExpiresAt := now.Add(service.config.InvocationTTL)
	if input.Connection.ExpiresAt != nil && approvalExpiresAt.After(*input.Connection.ExpiresAt) {
		approvalExpiresAt = *input.Connection.ExpiresAt
	}
	if !approvalExpiresAt.After(now.Add(time.Minute + 5*time.Second)) {
		return InvocationReceipt{}, errs.ErrExpired
	}
	encryptedGrant, err := service.cipher.Encrypt(ctx, []byte(input.Authority.ApplicationGrant))
	if err != nil {
		return InvocationReceipt{}, err
	}
	status := enum.InvocationApproved
	approval := &entity.Approval{}
	invocationID := uuid.NewString()
	approval.ID = uuid.NewString()
	approval.InvocationID = invocationID
	approval.RequestHash = hash
	approval.Preview = preview
	approval.ExpiresAt = approvalExpiresAt
	if input.Tool.ApprovalPolicy == enum.ApprovalAlways || input.Tool.Risk.RequiresApproval() {
		status = enum.InvocationPendingApproval
		approval.Status = enum.ApprovalPending
	} else {
		approval.Status = enum.ApprovalApproved
		approval.DecidedBy = "system:integration-gateway-policy"
		approval.DecisionReasonCode = "SAFE_POLICY"
		approval.DecidedAt = &now
	}
	invocation := entity.Invocation{
		ID: invocationID, TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		TransportSessionID: input.TransportSessionID, ProcessID: input.Grant.ProcessID,
		AgentSessionID: input.Grant.SessionID, AgentSessionVersion: input.Grant.SessionVersion,
		ThreadID: input.Grant.ThreadID, TurnID: input.Grant.TurnID, TurnVersion: input.Grant.TurnVersion,
		Attempt: input.Grant.Attempt, InputDigest: input.Grant.InputDigest,
		RuntimeRevisionID: input.Grant.RuntimeRevisionID, RuntimeRevisionVersion: input.Grant.RuntimeRevisionVersion,
		RuntimeRevisionDigest: input.Grant.RuntimeRevisionDigest,
		RuntimeManifestDigest: input.Grant.RuntimeManifestDigest,
		RoleID:                input.Grant.RoleID, RoleVersion: input.Grant.RoleVersion,
		DefinitionID: input.Connection.DefinitionID, DefinitionVersion: input.Connection.DefinitionVersion,
		DefinitionDigest: input.DefinitionDigest,
		ConnectionID:     input.Connection.ID, ConnectionRevision: input.Connection.Revision,
		ConnectionGeneration:    input.Connection.Generation,
		ConnectionBindingDigest: entity.ConnectionBindingDigest(input.Connection),
		PinnedConnection:        input.Connection, PinnedTool: input.Tool, GrantID: input.Grant.ID,
		GrantGeneration: input.Grant.Generation, Capability: input.Tool.Capability, ToolName: input.Tool.Name,
		ToolVersion: input.Tool.Version, Risk: input.Tool.Risk, Permission: input.Tool.Permission,
		SemanticKey: input.SemanticKey, CanonicalRequestHash: hash, EncryptedArguments: encrypted,
		Preview: preview, Status: status, CreatedAt: now, UpdatedAt: now, ExpiresAt: approvalExpiresAt,
	}
	pins := make([]entity.PinnedCredentialBinding, 0, len(input.Connection.CredentialBindingRefs))
	for _, binding := range input.Connection.CredentialBindingRefs {
		pins = append(pins, entity.PinnedCredentialBinding{
			ID: binding.ID, Version: binding.Version,
			Digest: binding.ProjectionDigest,
		})
	}
	sort.Slice(pins, func(left, right int) bool { return pins[left].ID < pins[right].ID })
	desiredAction := enum.ContinuationNone
	if status == enum.InvocationApproved {
		desiredAction = enum.ContinuationApprove
	}
	effect := entity.ContinuationEffect{
		InvocationID: invocationID, TenantID: input.Scope.TenantID,
		ProjectID: input.Scope.ProjectID, ApprovalID: approval.ID, RequestDigest: hash,
		IntegrationID: input.Connection.IntegrationID, IntegrationVersion: input.Connection.IntegrationVersion,
		IntegrationDigest: input.Connection.IntegrationDigest, CredentialBindings: pins,
		EncryptedApplicationGrant: encryptedGrant,
		ApplicationGrantExpiresAt: input.Authority.ApplicationGrantExpiresAt,
		Action:                    enum.ContinuationSuspend, DesiredAction: desiredAction, AvailableAt: now,
	}
	receiptKeyHash := semanticReceiptKey(input.Scope, input.Grant, input.SemanticKey)
	audit := entity.AuditEvent{
		ID: uuid.NewString(), TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		ActorID: input.Scope.ActorID, Action: "tool.invoke", ResourceKind: "TOOL_INVOCATION", ResourceID: invocationID,
		RequestHash: hash, Outcome: string(status), OccurredAt: now,
	}
	var stored entity.Invocation
	var replay bool
	err = service.repository.Transact(ctx, input.Scope, func(tx domainrepo.Transaction) error {
		stored, replay, err = tx.ReserveInvocation(ctx, domainrepo.InvocationReservation{
			Invocation: invocation,
			Approval:   approval, Continuation: effect, ReceiptKeyHash: receiptKeyHash, RequestHash: hash, Audit: audit,
		})
		return err
	})
	if err != nil {
		return InvocationReceipt{}, err
	}
	if replay && stored.CanonicalRequestHash != hash {
		return InvocationReceipt{}, errs.ErrConflict
	}
	receipt := InvocationReceipt{
		InvocationID: stored.ID, Status: stored.Status, RequestHash: stored.CanonicalRequestHash,
		PollPath: "/api/v1/invocations/" + stored.ID,
	}
	if status == enum.InvocationPendingApproval && !replay {
		receipt.ApprovalID = approval.ID
	}
	return receipt, nil
}

func semanticReceiptKey(scope domainrepo.Scope, grant entity.Grant, semanticKey string) string {
	digest := sha256.Sum256([]byte(
		scope.TenantID + "\x00" + scope.ProjectID + "\x00" + grant.ProcessID + "\x00" +
			grant.SessionID + "\x00" + strconv.FormatUint(grant.SessionVersion, 10) + "\x00" + grant.ThreadID + "\x00" +
			grant.TurnID + "\x00" + strconv.FormatUint(grant.TurnVersion, 10) + "\x00" + grant.InputDigest + "\x00" +
			grant.RuntimeRevisionID + "\x00" + strconv.FormatUint(grant.RuntimeRevisionVersion, 10) + "\x00" +
			grant.RuntimeRevisionDigest + "\x00" + grant.RuntimeManifestDigest + "\x00" + grant.RoleID + "\x00" +
			strconv.FormatUint(grant.RoleVersion, 10) + "\x00" + grant.ID + "\x00" +
			strconv.FormatUint(uint64(grant.Attempt), 10) + "\x00" + strconv.FormatUint(grant.Generation, 10) + "\x00" + semanticKey,
	))
	return hex.EncodeToString(digest[:])
}

func operationReceiptKey(scope domainrepo.Scope, operation, resourceID, semanticKey string) string {
	digest := sha256.Sum256([]byte(scope.TenantID + "\x00" + scope.ProjectID + "\x00" + scope.ActorID + "\x00" + operation + "\x00" + resourceID + "\x00" + semanticKey))
	return hex.EncodeToString(digest[:])
}

func operationRequestHash(operation, resourceID, outcome, reasonCode string) string {
	digest := sha256.Sum256([]byte(operation + "\x00" + resourceID + "\x00" + outcome + "\x00" + reasonCode))
	return hex.EncodeToString(digest[:])
}

func (service *Service) Decide(ctx context.Context, scope domainrepo.Scope, approvalID string, approve bool, reasonCode, semanticKey string) (entity.Invocation, error) {
	if uuid.Validate(approvalID) != nil || reasonCode == "" || len(reasonCode) > 64 || uuid.Validate(semanticKey) != nil {
		return entity.Invocation{}, errs.ErrInvalid
	}
	now := service.now().UTC()
	decision := map[bool]string{true: "APPROVED", false: "REJECTED"}[approve]
	audit := entity.AuditEvent{
		ID: uuid.NewString(), TenantID: scope.TenantID, ProjectID: scope.ProjectID,
		ActorID: scope.ActorID, Action: "approval.decide", ResourceKind: "APPROVAL_REQUEST",
		ResourceID: approvalID, Outcome: decision,
		ReasonCode: reasonCode, OccurredAt: now,
	}
	receiptKeyHash := operationReceiptKey(scope, "approval.decide", approvalID, semanticKey)
	requestHash := operationRequestHash("approval.decide", approvalID, decision, reasonCode)
	var invocation entity.Invocation
	err := service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		var err error
		invocation, _, err = tx.DecideApproval(ctx, domainrepo.Decision{
			ApprovalID: approvalID, Approve: approve,
			ActorID: scope.ActorID, ReasonCode: reasonCode, ReceiptKeyHash: receiptKeyHash,
			RequestHash: requestHash, DecidedAt: now, Audit: audit,
		})
		return err
	})
	return invocation, err
}

func (service *Service) Cancel(ctx context.Context, scope domainrepo.Scope, invocationID, expectedTransportSessionID, reasonCode, semanticKey string) (InvocationReceipt, error) {
	if uuid.Validate(invocationID) != nil || reasonCode != "OWNER_CANCELLED" || uuid.Validate(semanticKey) != nil {
		return InvocationReceipt{}, errs.ErrInvalid
	}
	now := service.now().UTC()
	audit := entity.AuditEvent{
		ID: uuid.NewString(), TenantID: scope.TenantID, ProjectID: scope.ProjectID,
		ActorID: scope.ActorID, Action: "invocation.cancel", ResourceKind: "TOOL_INVOCATION",
		ResourceID: invocationID, Outcome: string(enum.InvocationCancelled), ReasonCode: reasonCode, OccurredAt: now,
	}
	receiptKeyHash := operationReceiptKey(scope, "invocation.cancel", invocationID, semanticKey)
	requestHash := operationRequestHash("invocation.cancel", invocationID, string(enum.InvocationCancelled), reasonCode)
	var invocation entity.Invocation
	err := service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		var err error
		invocation, _, err = tx.CancelInvocation(ctx, domainrepo.Cancellation{
			InvocationID: invocationID, ExpectedTransportSessionID: expectedTransportSessionID,
			ActorID: scope.ActorID, ReasonCode: reasonCode, ReceiptKeyHash: receiptKeyHash,
			RequestHash: requestHash, CancelledAt: now, Audit: audit,
		})
		return err
	})
	if err != nil {
		return InvocationReceipt{}, err
	}
	return InvocationReceipt{
		InvocationID: invocation.ID, Status: invocation.Status,
		RequestHash: invocation.CanonicalRequestHash, PollPath: "/api/v1/invocations/" + invocation.ID,
	}, nil
}

func (service *Service) ExecuteOne(ctx context.Context, finalizationBase context.Context) (bool, enum.InvocationStatus, error) {
	if finalizationBase == nil {
		return false, "", errors.New("execution finalization context is required")
	}
	now := service.now().UTC()
	scope, available, err := service.repository.NextExecutionScope(ctx)
	if err != nil || !available {
		return available, "", err
	}
	var claim domainrepo.ExecutionClaim
	var found bool
	err = service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		var err error
		claim, found, err = tx.ClaimExecution(ctx, now)
		return err
	})
	if err != nil || !found {
		return found, "", err
	}
	if !claim.ProviderReady {
		return true, "", nil
	}
	arguments, err := service.cipher.Decrypt(ctx, claim.Invocation.EncryptedArguments)
	if err != nil {
		return true, enum.InvocationFailed, service.finishBounded(finalizationBase, claim, enum.InvocationFailed, nil, "PAYLOAD_DECRYPT_FAILED")
	}
	credentials, err := service.credentials.Resolve(ctx, claim.Connection)
	if err != nil {
		return true, enum.InvocationFailed, service.finishBounded(finalizationBase, claim, enum.InvocationFailed, nil, "CREDENTIAL_UNAVAILABLE")
	}
	dispatchedAt := service.now().UTC()
	err = service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		return tx.MarkProviderDispatched(ctx, claim.Invocation.ID, claim.Attempt.ID, dispatchedAt)
	})
	if err != nil {
		return true, "", err
	}
	claim.Attempt.ProviderDispatchedAt = &dispatchedAt
	providerResult, providerErr := service.provider.Execute(ctx, claim.Connection, claim.Tool, arguments, credentials, claim.Attempt.ProviderIdempotencyKey)
	if providerErr != nil && providerResult.Status == "" {
		providerResult.Status = enum.InvocationUnknown
		providerResult.Effect = provider.EffectAmbiguous
	}
	if providerResult.Status == enum.InvocationFailed && providerResult.Effect != provider.EffectNoEffect {
		providerResult.Status = enum.InvocationUnknown
		providerResult.Effect = provider.EffectAmbiguous
	}
	if providerResult.Status == enum.InvocationSucceeded && providerResult.Effect != provider.EffectCommitted {
		providerResult.Status = enum.InvocationUnknown
		providerResult.Effect = provider.EffectAmbiguous
	}
	if providerResult.Status != enum.InvocationSucceeded && providerResult.Status != enum.InvocationFailed && providerResult.Status != enum.InvocationUnknown {
		providerResult.Status = enum.InvocationUnknown
		providerResult.Effect = provider.EffectAmbiguous
	}
	if providerResult.Status == enum.InvocationSucceeded && service.validator.Validate(claim.Tool.OutputSchema, providerResult.Payload) != nil {
		providerResult.Status = enum.InvocationUnknown
		providerResult.Effect = provider.EffectAmbiguous
		providerResult.Payload = json.RawMessage(`{"status":"provider_output_schema_mismatch"}`)
	}
	encryptedResult, encryptErr := service.cipher.Encrypt(ctx, providerResult.Payload)
	if encryptErr != nil {
		return true, enum.InvocationUnknown, service.finishBounded(finalizationBase, claim, enum.InvocationUnknown, nil, "RESULT_ENCRYPT_FAILED")
	}
	payloadDigest := sha256.Sum256(providerResult.Payload)
	result := &entity.Result{
		InvocationID: claim.Invocation.ID, AttemptID: claim.Attempt.ID,
		Status: providerResult.Status, EncryptedPayload: encryptedResult,
		PayloadDigest: hex.EncodeToString(payloadDigest[:]), ProviderReceipt: providerResult.ProviderReceipt,
		CompletedAt: service.now().UTC(),
	}
	return true, providerResult.Status, service.finishBounded(finalizationBase, claim, providerResult.Status, result, "")
}

func (service *Service) ExpireOneScope(ctx context.Context) (bool, error) {
	scope, available, err := service.repository.NextLifecycleScope(ctx)
	if err != nil || !available {
		return available, err
	}
	var count int64
	err = service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		var err error
		count, err = tx.Expire(ctx, service.now().UTC(), 100)
		return err
	})
	return count > 0, err
}

func (service *Service) SyncContinuationOne(ctx context.Context) (bool, error) {
	scope, available, err := service.repository.NextContinuationScope(ctx)
	if err != nil || !available {
		return available, err
	}
	var claim domainrepo.ContinuationClaim
	var found bool
	err = service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		var claimErr error
		claim, found, claimErr = tx.ClaimContinuation(ctx, continuationLeaseDuration)
		return claimErr
	})
	if err != nil || !found {
		return found, err
	}
	applicationGrant, err := service.cipher.Decrypt(ctx, claim.Effect.EncryptedApplicationGrant)
	if err != nil {
		return true, service.retryContinuation(ctx, scope, claim, err)
	}
	command, err := continuationCommand(claim, string(applicationGrant))
	if err != nil {
		return true, service.retryContinuation(ctx, scope, claim, err)
	}
	state, err := service.continuation.Apply(ctx, command)
	if err != nil {
		return true, service.retryContinuation(ctx, scope, claim, err)
	}
	if !continuationStateMatches(claim.Effect.Action, state) {
		return true, service.retryContinuation(ctx, scope, claim,
			errors.New("control-plane continuation transition is inconsistent"))
	}
	var encryptedTransitionGrant []byte
	if state.TransitionGrant != "" {
		encryptedTransitionGrant, err = service.cipher.Encrypt(ctx, []byte(state.TransitionGrant))
		if err != nil {
			return true, service.retryContinuation(ctx, scope, claim, err)
		}
	}
	err = service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		return tx.CompleteContinuation(ctx, domainrepo.ContinuationCompletion{
			InvocationID: claim.Invocation.ID, Action: claim.Effect.Action,
			LeaseID: claim.Effect.LeaseID, LeaseFence: claim.Effect.LeaseFence,
			State: domainrepo.ContinuationState{
				ID: state.ID, Version: state.Version, Fence: state.Fence,
				ApprovalState: state.ApprovalState, ExecutionState: state.ExecutionState,
				ContinuationState: state.ContinuationState,
			},
			EncryptedTransitionGrant: encryptedTransitionGrant,
			TransitionGrantExpiresAt: state.TransitionGrantExpiresAt,
		})
	})
	return true, err
}

func (service *Service) retryContinuation(
	ctx context.Context,
	scope domainrepo.Scope,
	claim domainrepo.ContinuationClaim,
	cause error,
) error {
	backoff := time.Duration(1<<min(claim.Effect.Attempts, uint32(4))) * 250 * time.Millisecond
	retryErr := service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		return tx.RetryContinuation(ctx, domainrepo.ContinuationRetry{
			InvocationID: claim.Invocation.ID, Action: claim.Effect.Action,
			LeaseID: claim.Effect.LeaseID, LeaseFence: claim.Effect.LeaseFence,
			Backoff: backoff,
		})
	})
	return errors.Join(cause, retryErr)
}

func continuationCommand(
	claim domainrepo.ContinuationClaim,
	applicationGrant string,
) (continuationclient.Command, error) {
	effect := claim.Effect
	if effect.InvocationID != claim.Invocation.ID || effect.ApprovalID != claim.Approval.ID ||
		effect.RequestDigest != claim.Invocation.CanonicalRequestHash || applicationGrant == "" {
		return continuationclient.Command{}, errors.New("continuation claim binding is invalid")
	}
	command := continuationclient.Command{
		Action: effect.Action,
		IdempotencyKey: uuid.NewSHA1(continuationNamespace,
			[]byte(effect.InvocationID+"\x00"+string(effect.Action))).String(),
		ApplicationGrant: applicationGrant, InvocationID: effect.InvocationID,
		ApprovalID: effect.ApprovalID, RequestDigest: effect.RequestDigest,
		IntegrationID: effect.IntegrationID, IntegrationVersion: effect.IntegrationVersion,
		IntegrationDigest:  effect.IntegrationDigest,
		CredentialBindings: slices.Clone(effect.CredentialBindings),
		ApprovalExpiresAt:  claim.Approval.ExpiresAt, ContinuationID: effect.ContinuationID,
		ExpectedVersion: effect.Version, ExpectedFence: effect.Fence,
	}
	switch effect.Action {
	case enum.ContinuationApprove, enum.ContinuationReject, enum.ContinuationCancel:
		command.DecisionReference = fmt.Sprintf(
			"integration-gateway://approvals/%s/decisions/%s",
			claim.Approval.ID, strings.ToLower(string(effect.Action)),
		)
		command.DecisionDigest = digestText(command.DecisionReference + "\x00" +
			claim.Approval.RequestHash + "\x00" + claim.Approval.DecidedBy + "\x00" +
			claim.Approval.DecisionReasonCode)
	case enum.ContinuationSucceed:
		if claim.Result == nil || claim.Attempt == nil {
			return continuationclient.Command{}, errors.New("continuation result binding is missing")
		}
		command.ResultReference = fmt.Sprintf(
			"integration-gateway://invocations/%s/results/%s",
			claim.Invocation.ID, claim.Attempt.ID,
		)
		command.ResultDigest = claim.Result.PayloadDigest
		if !validDigestText(command.ResultDigest) {
			return continuationclient.Command{}, errors.New("continuation result digest is invalid")
		}
	case enum.ContinuationFail:
		if claim.Result == nil || claim.Attempt == nil {
			return continuationclient.Command{}, errors.New("continuation error binding is missing")
		}
		command.ErrorCode = "INTEGRATION_EXECUTION_FAILED"
		if claim.Result.Status == enum.InvocationUnknown {
			command.ErrorCode = "PROVIDER_OUTCOME_UNKNOWN"
		}
		command.ErrorReference = fmt.Sprintf(
			"integration-gateway://invocations/%s/results/%s",
			claim.Invocation.ID, claim.Attempt.ID,
		)
		command.ErrorDigest = claim.Result.PayloadDigest
		if !validDigestText(command.ErrorDigest) {
			return continuationclient.Command{}, errors.New("continuation error digest is invalid")
		}
	case enum.ContinuationSuspend, enum.ContinuationExpire, enum.ContinuationBegin:
	default:
		return continuationclient.Command{}, errors.New("continuation action is invalid")
	}
	return command, nil
}

func continuationStateMatches(action enum.ContinuationAction, state continuationclient.State) bool {
	switch action {
	case enum.ContinuationSuspend:
		return state.ApprovalState == "PENDING" && state.ExecutionState == "NOT_STARTED" &&
			state.ContinuationState == "SUSPENDED"
	case enum.ContinuationApprove:
		return state.ApprovalState == "APPROVED" && state.ExecutionState == "NOT_STARTED" &&
			state.ContinuationState == "SUSPENDED"
	case enum.ContinuationReject:
		return state.ApprovalState == "REJECTED" && state.ExecutionState == "NOT_APPLICABLE" &&
			state.ContinuationState == "READY"
	case enum.ContinuationCancel:
		return state.ApprovalState == "CANCELLED" && state.ExecutionState == "NOT_APPLICABLE" &&
			state.ContinuationState == "READY"
	case enum.ContinuationExpire:
		return state.ApprovalState == "EXPIRED" && state.ExecutionState == "NOT_APPLICABLE" &&
			state.ContinuationState == "READY"
	case enum.ContinuationBegin:
		return state.ApprovalState == "APPROVED" && state.ExecutionState == "EXECUTING" &&
			state.ContinuationState == "SUSPENDED"
	case enum.ContinuationSucceed:
		return state.ApprovalState == "APPROVED" && state.ExecutionState == "SUCCEEDED" &&
			state.ContinuationState == "READY"
	case enum.ContinuationFail:
		return state.ApprovalState == "APPROVED" && state.ExecutionState == "FAILED" &&
			state.ContinuationState == "READY"
	default:
		return false
	}
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func validDigestText(value string) bool {
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

func (service *Service) finishBounded(base context.Context, claim domainrepo.ExecutionClaim, status enum.InvocationStatus, result *entity.Result, reason string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(base), service.config.FinalizationTimeout)
	defer cancel()
	now := service.now().UTC()
	if result == nil {
		payload, err := json.Marshal(struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}{Status: strings.ToLower(string(status)), Reason: reason})
		if err != nil {
			return err
		}
		encrypted, err := service.cipher.Encrypt(ctx, payload)
		if err != nil {
			return err
		}
		payloadDigest := sha256.Sum256(payload)
		result = &entity.Result{
			InvocationID: claim.Invocation.ID, AttemptID: claim.Attempt.ID,
			Status: status, EncryptedPayload: encrypted,
			PayloadDigest: hex.EncodeToString(payloadDigest[:]), CompletedAt: now,
		}
	} else if !validDigestText(result.PayloadDigest) {
		result.PayloadDigest = digestText(string(status) + "\x00" + reason)
	}
	audit := entity.AuditEvent{
		ID: uuid.NewString(), TenantID: claim.Invocation.TenantID, ProjectID: claim.Invocation.ProjectID,
		Action: "tool.execute", ResourceKind: "TOOL_INVOCATION", ResourceID: claim.Invocation.ID,
		RequestHash: claim.Invocation.CanonicalRequestHash, Outcome: string(status), ReasonCode: reason, OccurredAt: now,
	}
	return service.repository.Transact(ctx, domainrepo.Scope{TenantID: claim.Invocation.TenantID, ProjectID: claim.Invocation.ProjectID}, func(tx domainrepo.Transaction) error {
		return tx.CompleteExecution(ctx, domainrepo.ExecutionCompletion{
			InvocationID: claim.Invocation.ID,
			AttemptID:    claim.Attempt.ID, Fence: claim.Attempt.Fence,
			ConnectionGeneration: claim.Attempt.ConnectionGeneration, GrantGeneration: claim.Attempt.GrantGeneration,
			Result: *result, Audit: audit,
		})
	})
}
