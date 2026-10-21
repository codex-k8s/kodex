// Package controlplane adapts the registered image-builder lifecycle RPCs while
// preserving owner-issued claim/version/fence inputs and caller-owned work ordering.
// Package controlplane адаптирует зарегистрированные RPC жизненного цикла builder,
// сохраняя owner claim/version/fence и заданный вызывающим кодом порядок работы.
package controlplane

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrNoWork distinguishes Claim's owner NotFound response from operational errors;
// it is not returned for arbitrary failures or interpreted as a completed build.
// ErrNoWork отличает owner NotFound операции Claim от рабочих ошибок;
// он не заменяет произвольные ошибки и не означает завершённую сборку.
var ErrNoWork = errors.New("no image build is available")

// Config supplies the shared RPC transport/credential references and builder call
// timing; the adapter validates the RPC deadline before acquiring the shared client.
// Config задаёт transport/credential ссылки общего клиента и интервалы builder;
// адаптер проверяет срок RPC до получения общего клиента.
type Config struct {
	RPCProfile                                                                 string
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ApplicationGrantFile                                                       string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout, RPCDeadline                                                   time.Duration
}

// Claim retains the private lease token and owner build version/attempt/fence plus
// the protobuf input pointer. Copying Claim does not deep-copy Input; report/renew
// update selected fields only after their response checks.
// Claim сохраняет закрытый lease token, owner build version/attempt/fence и указатель
// protobuf input. Копия Claim не копирует Input глубоко; report/renew меняют
// отдельные поля лишь после своих проверок ответа.
type Claim struct {
	BuildID, LeaseToken string
	Version, Fence      uint64
	Attempt             uint32
	LeaseExpiresAt      time.Time
	Input               *controlplanev1.RoleImageBuildInput
}

// BuildEvidence carries the worker's staging reference and image/provenance/build
// digests for completion; the fields themselves do not prove registry publication,
// ownership, integrity or eligibility without the owner's command validation.
// BuildEvidence переносит staging reference и image/provenance/build хеши worker
// для завершения; сами поля не доказывают публикацию, владение, целостность
// и допустимость без проверки команды владельцем.
type BuildEvidence struct {
	StagingReference, ManifestDigest, ProvenanceSHA256, ImmutableBuildSHA256 string
}

// Client owns the fixed builder-operation shared client and per-call timeout;
// it neither schedules workers nor retries uncertain owner mutations automatically.
// Client владеет общим клиентом фиксированных builder-операций и сроком вызова;
// он не планирует worker и не повторяет неопределённые изменения автоматически.
type Client struct {
	shared      *sharedclient.Client
	rpcDeadline time.Duration
}

// Dial requires a 1..15 second RPC deadline, then delegates transport and identity
// validation to the shared client with fixed builder caller/operations. It does not
// claim work or execute an infrastructure readiness read during construction.
// Dial требует срок RPC 1..15 секунд, затем передаёт transport/identity проверку
// общему клиенту с фиксированными builder caller/operations. Создание не забирает
// работу и не выполняет infrastructure readiness read.
func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.RPCDeadline < time.Second || config.RPCDeadline > 15*time.Second {
		return nil, errors.New("role image builder RPC deadline is invalid")
	}
	client, err := sharedclient.Dial(ctx, sharedclient.Config{
		RPCProfile: config.RPCProfile, CallerWorkload: "role-image-builder",
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

// Check bounds the shared full readiness path, including its protected bootstrap
// read; unlike CheckLocalAuthority, it may contact the remote control plane.
// Check ограничивает полный readiness-путь общего клиента, включая защищённое
// чтение bootstrap; в отличие от CheckLocalAuthority возможен удалённый RPC.
func (client *Client) Check(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	return client.shared.Check(callCtx)
}

// CheckLocalAuthority bounds only the workload-local issuer readiness path;
// service identity needs no local issuer RPC. It deliberately does not include
// remote control-plane health in the builder Pod's local readiness check.
// CheckLocalAuthority ограничивает только workload-local issuer readiness;
// service identity не требует RPC локального issuer. Готовность удалённого
// control-plane намеренно не включается в локальную readiness builder Pod.
func (client *Client) CheckLocalAuthority(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	return client.shared.CheckLocalAuthority(callCtx)
}

// Claim requests owner-selected build work, maps NotFound to ErrNoWork and checks
// the required lease/build tuple plus input recipe/version/generation/spec/build
// agreement. The returned input pointer remains shared with the protobuf response;
// local checks do not issue authority or guarantee a lease is currently unexpired.
// Claim запрашивает выбранную владельцем сборку, переводит NotFound в ErrNoWork
// и проверяет обязательные lease/build и совпадение input recipe/version/generation/
// spec/build. Указатель input остаётся указателем ответа; локальная проверка
// не выдаёт полномочия и не доказывает текущее отсутствие истечения lease.
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

// Report submits progress with the retained version/attempt/fence/token and updates
// only claim.Version after requiring a response build with a strictly higher version.
// It does not locally compare the returned build identity or the complete claim tuple.
// Report передаёт progress с сохранёнными version/attempt/fence/token и меняет лишь
// claim.Version после наличия build со строго большей версией. Идентичность build
// и полная связка claim в ответе локально не сравниваются.
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

// Renew sends the exact current lease tuple and replaces version/token/expiry only
// after checking a newer response version and nonempty token/expiry. It does not
// locally verify response identity, fencing equality or expiry against wall time.
// Renew передаёт текущую lease-связку и заменяет version/token/expiry после проверки
// роста версии и наличия token/expiry. Идентичность ответа, совпадение fence
// и expiry относительно текущих часов локально не проверяются.
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

// Complete submits the retained claim and evidence under one bounded owner call.
// Local success currently requires nonnil build/artifact responses, not exact
// identity/version/evidence readback or an independent registry verification.
// Complete передаёт сохранённый claim и evidence одним ограниченным owner-вызовом.
// Локальный успех требует наличия build/artifact, а не точного identity/version/
// evidence readback либо независимой проверки реестра.
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

// Fail sends the caller's failure/diagnostic fields with the retained lease tuple
// and returns the RPC error. It currently does not inspect a successful response's
// receipt or retry an uncertain outcome; callers own diagnostic content and recovery.
// Fail передаёт failure/diagnostic вызывающего кода с сохранённой lease-связкой
// и возвращает RPC-ошибку. Успешная квитанция здесь не проверяется, неопределённый
// исход не повторяется; содержимое диагностики и recovery принадлежат вызывающему.
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

// Close tolerates a nil adapter/shared client and otherwise releases its connections;
// in-flight worker cancellation/join remain responsibilities of the composition root.
// Close допускает nil адаптера/общего клиента, иначе освобождает соединения;
// отмена и ожидание активных worker остаются обязанностью composition root.
func (client *Client) Close() error {
	if client == nil || client.shared == nil {
		return nil
	}
	return client.shared.Close()
}

// call invokes one RPC closure with a derived timeout bounded by both rpcDeadline
// and the parent context, always cancelling afterward without adding retries.
// call вызывает одно RPC-замыкание со сроком, ограниченным rpcDeadline и родительским
// контекстом, всегда отменяя его после вызова и не добавляя повторов.
func (client *Client) call(ctx context.Context, invoke func(context.Context) error) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	return invoke(callCtx)
}
