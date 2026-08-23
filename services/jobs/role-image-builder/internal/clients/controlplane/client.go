// Package controlplane предоставляет узкий protected adapter builder lifecycle.
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

var ErrNoWork = errors.New("no image build is available")

type Config struct {
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ApplicationGrantFile                                                       string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout, RPCDeadline                                                   time.Duration
}

type Claim struct {
	BuildID, LeaseToken string
	Version, Fence      uint64
	Attempt             uint32
	LeaseExpiresAt      time.Time
	Input               *controlplanev1.RoleImageBuildInput
}

type BuildEvidence struct {
	StagingReference, ManifestDigest, ProvenanceSHA256, ImmutableBuildSHA256 string
}

type Client struct {
	shared      *sharedclient.Client
	rpcDeadline time.Duration
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.RPCDeadline < time.Second || config.RPCDeadline > 15*time.Second {
		return nil, errors.New("role image builder RPC deadline is invalid")
	}
	client, err := sharedclient.Dial(ctx, sharedclient.Config{
		Target: config.Target, TLSServerName: config.TLSServerName, CAFile: config.CAFile,
		ClientCertificateFile: config.ClientCertificateFile, ClientPrivateKeyFile: config.ClientPrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID:    config.ExpectedIssuerUID, ExpectedIssuerGID: config.ExpectedIssuerGID,
		DialTimeout: config.DialTimeout, Operations: sharedclient.RoleImageBuilderOperations(),
	})
	if err != nil {
		return nil, err
	}
	return &Client{shared: client, rpcDeadline: config.RPCDeadline}, nil
}

func (client *Client) Check(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	return client.shared.Check(callCtx)
}

// CheckLocalAuthority проверяет только workload-local issuer sidecar. Соседний
// control-plane проверяется рабочими RPC и отдельным diagnostic-контуром, но не
// участвует в Kubernetes readiness builder Pod.
func (client *Client) CheckLocalAuthority(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	return client.shared.CheckLocalAuthority(callCtx)
}

func (client *Client) Claim(ctx context.Context, key string) (Claim, error) {
	var response *controlplanev1.ClaimImageBuildResponse
	err := client.call(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.RoleImages.ClaimImageBuild(callCtx,
			&controlplanev1.ClaimImageBuildRequest{IdempotencyKey: key})
		return callErr
	})
	if status.Code(err) == codes.NotFound {
		return Claim{}, ErrNoWork
	}
	if err != nil {
		return Claim{}, err
	}
	build, input := response.GetImageBuild(), response.GetInput()
	if build == nil || input == nil || build.GetRef() == "" || build.GetVersion() == 0 ||
		build.GetAttempt() == 0 || response.GetFence() == 0 || response.GetLeaseToken() == "" ||
		response.GetLeaseExpiresAt() == nil || input.GetRecipeRef() != build.GetRecipeRef() ||
		input.GetRecipeVersion() != build.GetRecipeVersion() || input.GetRecipeGeneration() != build.GetRecipeGeneration() ||
		input.GetSpecSha256() != build.GetSpecSha256() || input.GetImmutableBuildSha256() != build.GetImmutableBuildSha256() {
		return Claim{}, errors.New("claimed image build tuple is incomplete")
	}
	return Claim{BuildID: build.GetRef(), Version: build.GetVersion(), Attempt: build.GetAttempt(),
		Fence: response.GetFence(), LeaseToken: response.GetLeaseToken(),
		LeaseExpiresAt: response.GetLeaseExpiresAt().AsTime(), Input: input}, nil
}

func (client *Client) Report(ctx context.Context, claim *Claim, key string, stage controlplanev1.ImageBuildStage, percent uint32) error {
	var response *controlplanev1.ReportImageBuildProgressResponse
	err := client.call(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.RoleImages.ReportImageBuildProgress(callCtx, &controlplanev1.ReportImageBuildProgressRequest{
			IdempotencyKey: key, ImageBuildRef: claim.BuildID, ExpectedVersion: claim.Version,
			ExpectedAttempt: claim.Attempt, ExpectedFence: claim.Fence, LeaseToken: claim.LeaseToken,
			Stage: stage, ProgressPercent: percent,
		})
		return callErr
	})
	if err != nil {
		return err
	}
	if response.GetImageBuild() == nil || response.GetImageBuild().GetVersion() <= claim.Version {
		return errors.New("image build progress response is invalid")
	}
	claim.Version = response.GetImageBuild().GetVersion()
	return nil
}

func (client *Client) Renew(ctx context.Context, claim *Claim, key string) error {
	var response *controlplanev1.RenewImageBuildResponse
	err := client.call(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.RoleImages.RenewImageBuild(callCtx, &controlplanev1.RenewImageBuildRequest{
			IdempotencyKey: key, ImageBuildRef: claim.BuildID, ExpectedVersion: claim.Version,
			ExpectedAttempt: claim.Attempt, ExpectedFence: claim.Fence, LeaseToken: claim.LeaseToken,
		})
		return callErr
	})
	if err != nil {
		return err
	}
	if response.GetImageBuild() == nil || response.GetImageBuild().GetVersion() <= claim.Version ||
		response.GetLeaseToken() == "" || response.GetLeaseExpiresAt() == nil {
		return errors.New("image build renewal response is invalid")
	}
	claim.Version, claim.LeaseToken = response.GetImageBuild().GetVersion(), response.GetLeaseToken()
	claim.LeaseExpiresAt = response.GetLeaseExpiresAt().AsTime()
	return nil
}

func (client *Client) Complete(ctx context.Context, claim Claim, key string, evidence BuildEvidence) error {
	var response *controlplanev1.CompleteImageBuildResponse
	err := client.call(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.shared.RoleImages.CompleteImageBuild(callCtx, &controlplanev1.CompleteImageBuildRequest{
			IdempotencyKey: key, ImageBuildRef: claim.BuildID, ExpectedVersion: claim.Version,
			ExpectedAttempt: claim.Attempt, ExpectedFence: claim.Fence, LeaseToken: claim.LeaseToken,
			StagingReference: evidence.StagingReference, ManifestDigest: evidence.ManifestDigest,
			ProvenanceSha256: evidence.ProvenanceSHA256, ImmutableBuildSha256: evidence.ImmutableBuildSHA256,
		})
		return callErr
	})
	if err != nil {
		return err
	}
	if response.GetImageBuild() == nil || response.GetImageArtifact() == nil {
		return errors.New("completed image build response is incomplete")
	}
	return nil
}

func (client *Client) Fail(ctx context.Context, claim Claim, key, errorCode, diagnosticCode, diagnosticSummary string) error {
	return client.call(ctx, func(callCtx context.Context) error {
		_, callErr := client.shared.RoleImages.FailImageBuild(callCtx, &controlplanev1.FailImageBuildRequest{
			IdempotencyKey: key, ImageBuildRef: claim.BuildID, ExpectedVersion: claim.Version,
			ExpectedAttempt: claim.Attempt, ExpectedFence: claim.Fence, LeaseToken: claim.LeaseToken, ErrorCode: errorCode,
			DiagnosticCode: diagnosticCode, DiagnosticSummary: diagnosticSummary,
		})
		return callErr
	})
}

func (client *Client) Close() error {
	if client == nil || client.shared == nil {
		return nil
	}
	return client.shared.Close()
}

func (client *Client) call(ctx context.Context, invoke func(context.Context) error) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	return invoke(callCtx)
}
