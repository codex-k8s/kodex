// Package controlplane предоставляет узкий scheduler adapter поверх generated protected client.
package controlplane

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrNoWork       = errors.New("no schedule occurrence is available")
	ErrClaimRetired = errors.New("schedule occurrence claim is retired")
	ErrPending      = errors.New("schedule occurrence is not terminal")
)

type Config struct {
	Target                string
	TLSServerName         string
	CAFile                string
	ClientCertificateFile string
	ClientPrivateKeyFile  string
	ApplicationGrantFile  string
	OperationGrantFile    string
	ExpectedIssuerUID     uint32
	ExpectedIssuerGID     uint32
	DialTimeout           time.Duration
	RPCDeadline           time.Duration
}

type Claim struct {
	ProjectID      string
	OccurrenceID   string
	Attempt        uint32
	LeaseToken     string
	LeaseExpiresAt time.Time
}

type Client struct {
	shared      *sharedclient.Client
	readiness   *sharedclient.Client
	rpcDeadline time.Duration
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.RPCDeadline < 500*time.Millisecond || config.RPCDeadline > 10*time.Second {
		return nil, errors.New("automation scheduler client deadline is invalid")
	}
	readiness, err := sharedclient.Dial(ctx, sharedclient.Config{
		Target: config.Target, TLSServerName: config.TLSServerName,
		CAFile: config.CAFile, ClientCertificateFile: config.ClientCertificateFile,
		ClientPrivateKeyFile: config.ClientPrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID:    config.ExpectedIssuerUID, ExpectedIssuerGID: config.ExpectedIssuerGID,
		DialTimeout: config.DialTimeout,
		Operations:  sharedclient.AutomationSchedulerReadinessOperations(),
	})
	if err != nil {
		return nil, err
	}
	client, err := sharedclient.Dial(ctx, sharedclient.Config{
		Target: config.Target, TLSServerName: config.TLSServerName,
		CAFile: config.CAFile, ClientCertificateFile: config.ClientCertificateFile,
		ClientPrivateKeyFile: config.ClientPrivateKeyFile,
		ApplicationGrantFile: config.OperationGrantFile,
		ExpectedIssuerUID:    config.ExpectedIssuerUID, ExpectedIssuerGID: config.ExpectedIssuerGID,
		DialTimeout: config.DialTimeout,
		Operations:  sharedclient.AutomationSchedulerOperations(),
	})
	if err != nil {
		_ = readiness.Close()
		return nil, err
	}
	return &Client{shared: client, readiness: readiness, rpcDeadline: config.RPCDeadline}, nil
}

func (client *Client) Check(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	return client.readiness.Check(checkCtx)
}

func (client *Client) MaterializeDue(ctx context.Context, key string, limit int) (int, error) {
	request := &controlplanev1.ClaimDueSchedulesRequest{
		IdempotencyKey: key,
		Limit:          uint32(limit),
	}
	var response *controlplanev1.ClaimDueSchedulesResponse
	err := client.retryUnknown(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.ControlPlane.ClaimDueSchedules(callCtx, request)
		return callErr
	})
	if err != nil {
		return 0, err
	}
	return len(response.GetOccurrences()), nil
}

func (client *Client) ClaimNext(ctx context.Context, key string) (Claim, error) {
	request := &controlplanev1.ClaimScheduleOccurrenceRequest{IdempotencyKey: key}
	var response *controlplanev1.ClaimScheduleOccurrenceResponse
	err := client.retryUnknown(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.ControlPlane.ClaimScheduleOccurrence(callCtx, request)
		return callErr
	})
	if status.Code(err) == codes.NotFound {
		return Claim{}, ErrNoWork
	}
	if err != nil {
		return Claim{}, err
	}
	switch response.GetDisposition() {
	case controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_RETIRED:
		return Claim{}, ErrClaimRetired
	case controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_RESERVED,
		controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_MATERIALIZED:
	default:
		return Claim{}, errors.New("schedule occurrence claim disposition is invalid")
	}
	occurrence := response.GetOccurrence()
	if occurrence == nil || occurrence.GetOccurrenceId() == "" || occurrence.GetAttempt() == 0 ||
		response.GetProjectId() == "" || response.GetMaterializationCapability() == "" ||
		response.GetMaterializationIdempotencyKey() == "" || response.GetCapabilityExpiresAt() == nil {
		return Claim{}, errors.New("reserved schedule occurrence is incomplete")
	}
	materializeRequest := &controlplanev1.MaterializeScheduleOccurrenceRequest{
		IdempotencyKey: response.GetMaterializationIdempotencyKey(),
		OccurrenceId:   occurrence.GetOccurrenceId(), ProjectId: response.GetProjectId(),
		ExpectedAttempt:           occurrence.GetAttempt(),
		MaterializationCapability: response.GetMaterializationCapability(),
	}
	var materialized *controlplanev1.MaterializeScheduleOccurrenceResponse
	err = client.retryUnknown(ctx, func(callCtx context.Context) error {
		var callErr error
		materialized, callErr = client.shared.ControlPlane.MaterializeScheduleOccurrence(callCtx, materializeRequest)
		return callErr
	})
	if err != nil {
		return Claim{}, err
	}
	claimed := materialized.GetOccurrence()
	if claimed == nil || claimed.GetOccurrenceId() != occurrence.GetOccurrenceId() ||
		claimed.GetAttempt() != occurrence.GetAttempt() ||
		materialized.GetCompletionCapability() == "" || claimed.GetLeaseExpiresAt() == nil {
		return Claim{}, errors.New("materialized schedule occurrence is incomplete")
	}
	leaseExpiresAt := claimed.GetLeaseExpiresAt().AsTime()
	if leaseExpiresAt.IsZero() {
		return Claim{}, errors.New("materialized schedule occurrence lease is invalid")
	}
	return Claim{
		ProjectID:    response.GetProjectId(),
		OccurrenceID: claimed.GetOccurrenceId(), Attempt: claimed.GetAttempt(),
		LeaseToken: materialized.GetCompletionCapability(), LeaseExpiresAt: leaseExpiresAt,
	}, nil
}

func (client *Client) Complete(ctx context.Context, claim Claim, key string) (string, error) {
	request := &controlplanev1.CompleteScheduleOccurrenceRequest{
		IdempotencyKey: key, OccurrenceId: claim.OccurrenceID,
		CompletionCapability: claim.LeaseToken, ExpectedAttempt: claim.Attempt, ProjectId: claim.ProjectID,
	}
	var response *controlplanev1.CompleteScheduleOccurrenceResponse
	err := client.retryUnknown(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.ControlPlane.CompleteScheduleOccurrence(callCtx, request)
		return callErr
	})
	if status.Code(err) == codes.FailedPrecondition || status.Code(err) == codes.Aborted {
		return "", ErrPending
	}
	if err != nil {
		return "", err
	}
	if response.GetOccurrence() == nil {
		return "", errors.New("completed schedule occurrence is incomplete")
	}
	return response.GetOccurrence().GetState().String(), nil
}

func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	var result error
	if client.shared != nil {
		result = errors.Join(result, client.shared.Close())
	}
	if client.readiness != nil {
		result = errors.Join(result, client.readiness.Close())
	}
	return result
}

func (client *Client) retryUnknown(ctx context.Context, call func(context.Context) error) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
		err = call(callCtx)
		cancel()
		if err == nil || (status.Code(err) != codes.Unavailable && status.Code(err) != codes.DeadlineExceeded) {
			return err
		}
		if attempt == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	return err
}
