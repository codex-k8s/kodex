package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	businessmetrics "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/observability/metrics"
)

// sshComposeWorker owns the typed broker channel and one independent cancellable deployment slot.
// sshComposeWorker владеет typed broker channel и одним отдельным отменяемым deployment slot.
type sshComposeWorker struct {
	control *controlplaneclient.Client
	broker  sb.RuntimeCredentialProjectionServiceClient
	adapter *integration.Adapter
	config  Config
}

// runSSHComposeLoop preserves ordinary adapter budgets while joining each long SSH attempt before shutdown.
// runSSHComposeLoop сохраняет ordinary adapter budgets и завершает SSH attempt до shutdown.
func (worker *sshComposeWorker) runSSHComposeLoop(metrics *businessmetrics.Metrics) serviceruntime.Worker {
	return func(ctx context.Context) error {
		backoff := serviceruntime.NewIdleBackoff(worker.config.PollInterval, 5*time.Second)
		for ctx.Err() == nil {
			claimContext, cancel := context.WithTimeout(ctx, worker.config.RequestTimeout)
			claims, err := worker.control.Runtime.ClaimSSHComposeInvocations(claimContext, &cp.ClaimSSHComposeInvocationsRequest{WorkloadInstance: worker.config.InstanceID, Limit: 1})
			cancel()
			processed := 0
			if err == nil {
				for _, claim := range claims.GetClaims() {
					result, operationErr := worker.execute(ctx, claim)
					metrics.Operation(false, operationErr == nil, integration.IsUnknownOutcome(operationErr))
					completion, cancel := context.WithTimeout(context.WithoutCancel(ctx), worker.config.RequestTimeout)
					err = completeInvocation(completion, worker.control, claim, result, operationErr)
					cancel()
					processed++
					if err != nil {
						break
					}
				}
			}
			metrics.Cycle(err)
			timer := time.NewTimer(backoff.Next(processed > 0 && err == nil))
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
		return ctx.Err()
	}
}

// projectKey requests one exact lease-bound key and rejects target or expiry substitution locally.
// projectKey запрашивает exact lease-bound key и локально отклоняет target/expiry substitution.
func (worker *sshComposeWorker) projectKey(ctx context.Context, binding *cp.SSHComposeLeaseBinding, targetRef string) (*sb.ProjectSSHComposeCredentialResponse, error) {
	bounded, cancel := context.WithTimeout(ctx, worker.config.RequestTimeout)
	defer cancel()
	projected, err := worker.broker.ProjectSSHComposeCredential(bounded, &sb.ProjectSSHComposeCredentialRequest{WorkloadInstance: binding.GetWorkloadInstance(), InvocationRef: binding.GetInvocationRef(),
		ConnectionTestRef: binding.GetConnectionTestRef(), LeaseRef: binding.GetLeaseRef(), Fence: binding.GetFence(), Generation: binding.GetGeneration()})
	if err != nil {
		return nil, err
	}
	if projected.GetTargetRef() != targetRef || projected.GetExpiresAt() == nil || projected.GetExpiresAt().CheckValid() != nil || !projected.GetExpiresAt().AsTime().After(time.Now()) ||
		len(projected.GetPrivateKey()) == 0 || len(projected.GetPrivateKey()) > 16384 || len(projected.GetPermit()) == 0 {
		clear(projected.GetPrivateKey())
		return nil, sshcompose.ErrorInvalid
	}
	return projected, nil
}

// test checks only the pinned rootless target; it cannot select release data or deploy.
// test проверяет лишь pinned rootless target, не выбирает release и не выполняет deploy.
func (worker *sshComposeWorker) test(ctx context.Context, claim *cp.IntegrationConnectionTestClaim) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	configuration := map[string]string{}
	for key, value := range claim.GetPublicConfiguration().AsMap() {
		text, ok := value.(string)
		if !ok {
			return "", sshcompose.ErrorInvalid
		}
		configuration[key] = text
	}
	target, err := sshcompose.TargetFromConfiguration(configuration)
	if err != nil {
		return "", err
	}
	lease := claim.GetLease()
	projected, err := worker.projectKey(ctx, &cp.SSHComposeLeaseBinding{WorkloadInstance: worker.config.InstanceID, ConnectionTestRef: claim.GetTestRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration()}, target.Ref)
	if err != nil {
		return "", err
	}
	defer clear(projected.GetPrivateKey())
	_, err = integration.RunSSHCompose(ctx, target, projected.GetPrivateKey(), projected.GetPermit(), nil, nil)
	if err != nil {
		return "", &integration.SafeError{Code: "INTEGRATION_PROVIDER_UNAVAILABLE"}
	}
	return "SSH Compose target ready", nil
}

// execute consumes only an owner snapshot and never rebuilds write authority from model input.
// execute потребляет лишь owner snapshot и не восстанавливает write authority из model input.
func (worker *sshComposeWorker) execute(ctx context.Context, claim *cp.IntegrationInvocationClaim) (integration.Result, error) {
	var snapshot sshcompose.Snapshot
	if claim.GetDefinitionKey() != "ssh-compose" || sshcompose.Decode(claim.GetSshComposeSnapshot(), &snapshot, 32768) != nil || snapshot.Target.Validate() != nil {
		return integration.Result{}, sshcompose.ErrorInvalid
	}
	budget := worker.config.OperationTimeout
	deploy := claim.GetOperation() == "ssh.compose.deploy" && claim.GetWorkMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY
	if deploy {
		budget = sshcompose.MaximumLifetime
	}
	operation, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	lease := claim.GetLease()
	binding := &cp.SSHComposeLeaseBinding{WorkloadInstance: worker.config.InstanceID, InvocationRef: claim.GetInvocationRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration()}
	renew := func(ctx context.Context) ([]byte, error) {
		bounded, cancel := context.WithTimeout(ctx, worker.config.RequestTimeout)
		defer cancel()
		response, err := worker.control.Runtime.RenewSSHComposeInvocation(bounded, &cp.RenewSSHComposeInvocationRequest{Binding: binding})
		if err != nil {
			return nil, err
		}
		claim.Lease = response.GetLease()
		return response.GetPermit(), nil
	}
	var bundle *sshcompose.Bundle
	if deploy {
		// Fresh owner admission precedes hidden source credential/network reads.
		// Свежий owner admission предшествует скрытому чтению source credential/network.
		if _, err := renew(operation); err != nil {
			return integration.Result{}, err
		}
		fetch, cancel := context.WithTimeout(operation, worker.config.OperationTimeout)
		value, err := worker.adapter.ReadSSHComposeRelease(fetch, snapshot)
		cancel()
		if err != nil {
			return integration.Result{}, err
		}
		bundle = &value
		if _, err := renew(operation); err != nil {
			return integration.Result{}, err
		}
	}
	projected, err := worker.projectKey(operation, binding, snapshot.Target.Ref)
	if err != nil {
		return integration.Result{}, err
	}
	defer clear(projected.GetPrivateKey())
	if claim.GetOperation() == "ssh.compose.inspect" && snapshot.Source != nil {
		value, err := worker.adapter.ReadSSHComposeRelease(operation, snapshot)
		if err != nil {
			return integration.Result{}, err
		}
		bundle = &value
	}
	var renewal integration.SSHComposeRenewal
	if deploy {
		renewal = renew
	}
	permit := projected.GetPermit()
	if claim.GetWorkMode() == cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY {
		permit = claim.GetSshComposePermit()
	}
	deploymentBundle := bundle
	if !deploy {
		deploymentBundle = nil
	}
	response, err := integration.RunSSHCompose(operation, snapshot.Target, projected.GetPrivateKey(), permit, deploymentBundle, renewal)
	if err != nil {
		if claim.GetOperation() != "ssh.compose.deploy" {
			return integration.Result{}, &integration.SafeError{Code: "INTEGRATION_PROVIDER_UNAVAILABLE"}
		}
		return integration.Result{}, &integration.UnknownOutcomeError{}
	}
	var output map[string]any
	switch claim.GetOperation() {
	case "ssh.compose.inspect":
		output = map[string]any{"ready": true, "target_ref": snapshot.Target.Ref}
		if snapshot.Source != nil {
			if bundle == nil {
				return integration.Result{}, sshcompose.ErrorInvalid
			}
			raw, _, err := bundle.Canonical()
			if err != nil {
				return integration.Result{}, err
			}
			output["merged_sha"], output["tree_sha"], output["bundle_sha256"] = bundle.MergedSHA, bundle.TreeSHA, sshcompose.Digest(raw)
		}
	case "ssh.compose.deploy", "ssh.compose.status":
		if response.Receipt == nil {
			return integration.Result{}, &integration.UnknownOutcomeError{}
		}
		receipt := response.Receipt
		if receipt.TargetRef != snapshot.Target.Ref || receipt.ProjectRef != snapshot.Target.ProjectRef || receipt.DeploymentRef != snapshot.DeploymentRef || receipt.InputDigest != snapshot.DeploymentInputDigest || receipt.EffectKey != snapshot.DeploymentEffectKey ||
			receipt.MergedSHA != snapshot.MergedSHA || receipt.TreeSHA != snapshot.TreeSHA || receipt.BundleSHA256 != snapshot.BundleSHA256 || receipt.Image != snapshot.Target.RuntimeImage {
			return integration.Result{}, &integration.UnknownOutcomeError{}
		}
		if claim.GetOperation() == "ssh.compose.deploy" && (receipt.State != "SUCCEEDED" || !receipt.HealthOK) {
			if receipt.State == "FAILED" || receipt.State == "CANCELLED" {
				return integration.Result{}, &integration.SafeError{Code: "INTEGRATION_PROVIDER_UNAVAILABLE"}
			}
			return integration.Result{}, &integration.UnknownOutcomeError{}
		}
		raw, _ := json.Marshal(receipt)
		output = map[string]any{"receipt": string(raw)}
		if claim.GetOperation() == "ssh.compose.deploy" {
			output["target_ref"], output["deployed_sha"], output["bundle_sha256"], output["image_digest"], output["health_ok"] = snapshot.Target.Ref, snapshot.MergedSHA, snapshot.BundleSHA256, snapshot.Target.RuntimeImage[len(snapshot.Target.RuntimeImage)-71:], true
		}
	default:
		return integration.Result{}, errors.New("SSH operation rejected")
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return integration.Result{}, err
	}
	return integration.Result{Summary: string(raw), Receipt: integration.Receipt{EffectKey: claim.GetEffectKey(), InputDigest: claim.GetInputDigest(), ProviderEffectRef: "ssh-compose:" + snapshot.Target.Ref + ":" + claim.GetInvocationRef(), ResponseDigest: sshcompose.Digest(raw)}}, nil
}
