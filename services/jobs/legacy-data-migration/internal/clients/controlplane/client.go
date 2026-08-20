// Package controlplane предоставляет job только закрытый owner materializer #249.
package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Config struct {
	Target, TLSServerName, CAFile               string
	ClientCertificateFile, ClientPrivateKeyFile string
	ApplicationGrantFile                        string
	ExpectedIssuerUID, ExpectedIssuerGID        uint32
	DialTimeout, RPCDeadline                    time.Duration
}

type Client struct {
	shared      *sharedclient.Client
	rpcDeadline time.Duration
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.RPCDeadline < 500*time.Millisecond || config.RPCDeadline > 30*time.Second {
		return nil, errors.New("legacy materializer RPC deadline is invalid")
	}
	client, err := sharedclient.Dial(ctx, sharedclient.Config{
		Target: config.Target, TLSServerName: config.TLSServerName,
		CAFile: config.CAFile, ClientCertificateFile: config.ClientCertificateFile,
		ClientPrivateKeyFile: config.ClientPrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID:    config.ExpectedIssuerUID, ExpectedIssuerGID: config.ExpectedIssuerGID,
		DialTimeout: config.DialTimeout, Operations: sharedclient.LegacyDataMigrationOperations(),
	})
	if err != nil {
		return nil, err
	}
	return &Client{shared: client, rpcDeadline: config.RPCDeadline}, nil
}

func legacyRPCDiagnostic(err error) error {
	if err == nil {
		return nil
	}
	for _, candidate := range status.Convert(err).Details() {
		detail, ok := candidate.(*controlplanev1.ErrorDetail)
		if !ok {
			continue
		}
		code := detail.GetCode()
		if !strings.HasPrefix(code, "LEGACY_") || len(code) > 160 {
			continue
		}
		valid := true
		for _, symbol := range code {
			if symbol != '_' && (symbol < 'A' || symbol > 'Z') && (symbol < '0' || symbol > '9') {
				valid = false
				break
			}
		}
		if valid {
			return fmt.Errorf("legacy materializer RPC failed (%s): %w", code, err)
		}
	}
	return err
}

func (client *Client) Check(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	return client.shared.Check(callCtx)
}

func (client *Client) Prepare(ctx context.Context,
	request *controlplanev1.PrepareLegacyGraphMigrationRequest,
) (*controlplanev1.LegacyGraphMigration, error) {
	if request == nil {
		return nil, errors.New("legacy materializer prepare request is missing")
	}
	var response *controlplanev1.PrepareLegacyGraphMigrationResponse
	err := client.retry(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.ControlPlane.PrepareLegacyGraphMigration(callCtx, request)
		return callErr
	})
	if err != nil {
		return nil, legacyRPCDiagnostic(err)
	}
	migration := response.GetMigration()
	if err := validateMigration(migration, request.GetPlanId(), uint32(len(request.GetOperations())), false); err != nil {
		return nil, err
	}
	if migration.GetState() != controlplanev1.LegacyGraphMigrationState_LEGACY_GRAPH_MIGRATION_STATE_PREPARED &&
		migration.GetState() != controlplanev1.LegacyGraphMigrationState_LEGACY_GRAPH_MIGRATION_STATE_COMMITTED ||
		migration.GetSourceSnapshotSha256() != request.GetSourceSnapshotSha256() {
		return nil, errors.New("legacy materializer source snapshot readback mismatch")
	}
	return migration, nil
}

func (client *Client) Materialize(ctx context.Context, idempotencyKey, planID,
	semanticSHA256 string, expectedOperations uint32,
) (*controlplanev1.LegacyGraphMigration, error) {
	request := &controlplanev1.MaterializeLegacyGraphMigrationRequest{
		IdempotencyKey: idempotencyKey, PlanId: planID, ExpectedSemanticSha256: semanticSHA256,
	}
	var response *controlplanev1.MaterializeLegacyGraphMigrationResponse
	err := client.retry(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.ControlPlane.MaterializeLegacyGraphMigration(callCtx, request)
		return callErr
	})
	if err != nil {
		return nil, legacyRPCDiagnostic(err)
	}
	if err := validateMigration(response.GetMigration(), planID, expectedOperations, true); err != nil {
		return nil, err
	}
	return client.ReadCommitted(ctx, planID, semanticSHA256, expectedOperations)
}

func (client *Client) ReadCommitted(ctx context.Context, planID, semanticSHA256 string,
	expectedOperations uint32,
) (*controlplanev1.LegacyGraphMigration, error) {
	request := &controlplanev1.GetLegacyGraphMigrationRequest{PlanId: planID, VerifyCommitted: true}
	var response *controlplanev1.GetLegacyGraphMigrationResponse
	err := client.retry(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.ControlPlane.GetLegacyGraphMigration(callCtx, request)
		return callErr
	})
	if err != nil {
		return nil, legacyRPCDiagnostic(err)
	}
	migration := response.GetMigration()
	if err := validateMigration(migration, planID, expectedOperations, true); err != nil ||
		migration.GetSemanticSha256() != semanticSHA256 {
		return nil, errors.New("legacy materializer committed readback mismatch")
	}
	return migration, nil
}

func (client *Client) Abort(ctx context.Context, idempotencyKey, planID, semanticSHA256 string,
	expectedOperations uint32,
) error {
	request := &controlplanev1.AbortLegacyGraphMigrationRequest{
		IdempotencyKey: idempotencyKey, PlanId: planID, ExpectedSemanticSha256: semanticSHA256,
	}
	var response *controlplanev1.AbortLegacyGraphMigrationResponse
	err := client.retry(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.ControlPlane.AbortLegacyGraphMigration(callCtx, request)
		return callErr
	})
	if status.Code(err) == codes.FailedPrecondition {
		return errors.New("legacy materializer cannot abort a committed plan")
	}
	if err != nil {
		return legacyRPCDiagnostic(err)
	}
	migration := response.GetMigration()
	if err := validateMigration(migration, planID, expectedOperations, false); err != nil ||
		migration.GetState() != controlplanev1.LegacyGraphMigrationState_LEGACY_GRAPH_MIGRATION_STATE_ABORTED ||
		migration.GetSemanticSha256() != semanticSHA256 {
		return errors.New("legacy materializer abort readback mismatch")
	}
	return nil
}

func validateMigration(migration *controlplanev1.LegacyGraphMigration, planID string,
	expectedOperations uint32, committed bool,
) error {
	if migration == nil || migration.GetPlanId() != planID || migration.GetOperationCount() != expectedOperations ||
		migration.GetSemanticSha256() == "" || migration.GetSourceSnapshotSha256() == "" ||
		len(migration.GetDrift()) != 0 {
		return errors.New("legacy materializer result is incomplete")
	}
	if uint32(len(migration.GetOperationReceipts())) != expectedOperations {
		return errors.New("legacy materializer operation receipt cardinality mismatch")
	}
	if committed && (migration.GetState() != controlplanev1.LegacyGraphMigrationState_LEGACY_GRAPH_MIGRATION_STATE_COMMITTED ||
		migration.GetVerificationState() != controlplanev1.LegacyGraphVerificationState_LEGACY_GRAPH_VERIFICATION_STATE_VERIFIED) {
		return errors.New("legacy materializer terminal result is not verified")
	}
	seenOrdinals := make(map[uint32]struct{}, expectedOperations)
	for _, receipt := range migration.GetOperationReceipts() {
		if receipt.GetOrdinal() == 0 || receipt.GetOrdinal() > expectedOperations ||
			receipt.GetOperationKind() == "" || receipt.GetInputSha256() == "" ||
			receipt.GetTargetId() == "" || receipt.GetTargetKind() == "" {
			return errors.New("legacy materializer operation readback is incomplete")
		}
		if committed && (receipt.GetTargetVersion() == 0 ||
			receipt.GetTargetState() == controlplanev1.LifecycleState_LIFECYCLE_STATE_UNSPECIFIED ||
			receipt.GetProjectionSha256() == "" || receipt.GetProvenanceSha256() == "" ||
			receipt.GetProvenanceEvidenceSha256() == "" || len(receipt.GetAuditIds()) != 1) {
			return errors.New("legacy materializer committed operation readback is incomplete")
		}
		if _, duplicate := seenOrdinals[receipt.GetOrdinal()]; duplicate {
			return errors.New("legacy materializer operation readback is duplicated")
		}
		seenOrdinals[receipt.GetOrdinal()] = struct{}{}
	}
	return nil
}

func (client *Client) Close() error {
	if client == nil || client.shared == nil {
		return nil
	}
	return client.shared.Close()
}

func (client *Client) retry(ctx context.Context, call func(context.Context) error) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
		err = call(callCtx)
		cancel()
		if err == nil || status.Code(err) != codes.Unavailable && status.Code(err) != codes.DeadlineExceeded {
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
