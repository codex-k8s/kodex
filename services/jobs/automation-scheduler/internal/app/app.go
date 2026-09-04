// Package app содержит composition root server-owned automation scheduler.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	schedulerobservability "github.com/codex-k8s/kodex/services/jobs/automation-scheduler/internal/observability"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	issuerUID = 29001
	issuerGID = 29000
)

func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	telemetryConfig, err := sharedobservability.RuntimeConfigFromEnv(serviceName, buildVersion)
	if err != nil {
		return err
	}
	telemetry, err := sharedobservability.NewRuntime(startup, telemetryConfig)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, serviceruntime.RunShutdown(shutdownBase,
			serviceruntime.ShutdownOperation{Name: "tracing", Timeout: 5 * time.Second, Run: telemetry.ShutdownTracing},
			serviceruntime.ShutdownOperation{Name: "error reporting", Timeout: 5 * time.Second, Run: telemetry.FlushSentry},
		))
	}()
	logger := telemetry.Logger(os.Stdout)
	metrics := sharedobservability.NewMetrics(metricsSubsystem, buildVersion, map[string]string{})
	ownedMetrics := schedulerobservability.NewMetrics()
	if err := metrics.Register(ownedMetrics.Collectors()...); err != nil {
		return err
	}
	readiness := serviceruntime.NewReadiness()
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
		CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneCertificateFile,
		ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile, ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID, DialTimeout: config.RPCDeadline,
		Operations: controlplaneclient.AutomationSchedulerOperations(),
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	if err := control.CheckLocalAuthority(startup); err != nil {
		return fmt.Errorf("automation scheduler startup barrier failed: %w", err)
	}
	technical, err := httpserver.New(httpserver.Config{
		Address: config.TechnicalListen, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 32 << 10, MaximumConnections: 128,
	}, readiness, metrics.PrometheusHandler())
	if err != nil {
		return err
	}
	if err := technical.Listen(); err != nil {
		return err
	}
	workers := serviceruntime.StartWorkers(
		lifecycle,
		serveTechnical(technical),
		runScheduleLoop(control, readiness, metrics, ownedMetrics, logger, config),
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	readiness.Set(false, "stopping")
	metrics.SetReady(false)
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "scheduler workers", Timeout: config.ShutdownTimeout / 2, Run: workers.Wait},
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: config.ShutdownTimeout / 4, Run: technical.Shutdown},
	)
	return errors.Join(err, shutdownErr)
}

func serveTechnical(server *httpserver.Server) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.Serve() }()
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err := server.Shutdown(shutdown)
			return errors.Join(ctx.Err(), err, <-done)
		}
	}
}

func runScheduleLoop(control *controlplaneclient.Client, readiness *serviceruntime.Readiness, sharedMetrics *sharedobservability.Metrics, ownedMetrics *schedulerobservability.Metrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		idleBackoff := serviceruntime.NewIdleBackoff(config.PollInterval, 5*time.Second)
		degraded := false
		for {
			local, cancel := context.WithTimeout(ctx, config.RPCDeadline)
			err := control.CheckLocalAuthority(local)
			cancel()
			readiness.Set(err == nil, "local_authority")
			sharedMetrics.SetReady(err == nil)
			processed := 0
			if err == nil {
				processed, err = materializeDue(ctx, control.Runtime, ownedMetrics, config)
			}
			ownedMetrics.Cycle(err != nil)
			if err != nil && !degraded {
				degraded = true
				logger.WarnContext(ctx, "schedule materialization degraded", "error_class", "control_plane")
			} else if err == nil && degraded {
				degraded = false
				logger.InfoContext(ctx, "schedule materialization restored")
			}
			timer := time.NewTimer(idleBackoff.Next(err == nil && processed > 0))
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}

type scheduleRuntimeClient interface {
	ClaimDueSchedules(context.Context, *controlplanev1.ClaimDueSchedulesRequest, ...grpc.CallOption) (*controlplanev1.ClaimDueSchedulesResponse, error)
	RenewScheduleOccurrence(context.Context, *controlplanev1.RenewScheduleOccurrenceRequest, ...grpc.CallOption) (*controlplanev1.RenewScheduleOccurrenceResponse, error)
	MaterializeScheduleOccurrence(context.Context, *controlplanev1.MaterializeScheduleOccurrenceRequest, ...grpc.CallOption) (*controlplanev1.MaterializeScheduleOccurrenceResponse, error)
	FailScheduleOccurrence(context.Context, *controlplanev1.FailScheduleOccurrenceRequest, ...grpc.CallOption) (*controlplanev1.FailScheduleOccurrenceResponse, error)
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func materializeDue(ctx context.Context, control scheduleRuntimeClient, metrics *schedulerobservability.Metrics, config Config) (int, error) {
	processed := 0
	for processed < config.DueLimit {
		count, err := materializeOne(ctx, control, metrics, config)
		processed += count
		if err != nil || count == 0 {
			return processed, err
		}
	}
	return processed, nil
}

func materializeOne(ctx context.Context, control scheduleRuntimeClient, metrics *schedulerobservability.Metrics, config Config) (int, error) {
	rpc, cancel := context.WithTimeout(ctx, config.RPCDeadline)
	claimed, err := control.ClaimDueSchedules(rpc, &controlplanev1.ClaimDueSchedulesRequest{WorkloadInstance: config.InstanceID, Limit: 1})
	cancel()
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, claim := range claimed.GetClaims() {
		lease := claim.GetLease()
		metrics.Track(1)
		if err := validateScheduleClaim(claim, time.Now()); err != nil {
			metrics.Track(-1)
			metrics.Occurrence("invalid")
			if lease != nil && claim.GetOccurrenceRef() != "" {
				_ = failScheduleOccurrence(ctx, control, claim, "SCHEDULE_CLAIM_INVALID", false, config.RPCDeadline)
			}
			return processed, err
		}
		rpc, cancel = context.WithTimeout(ctx, config.RPCDeadline)
		renewed, renewErr := control.RenewScheduleOccurrence(rpc, &controlplanev1.RenewScheduleOccurrenceRequest{
			OccurrenceRef: claim.GetOccurrenceRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(),
		})
		cancel()
		if renewErr != nil || renewed.GetLease() == nil ||
			renewed.GetLease().GetRef() != lease.GetRef() || renewed.GetLease().GetFence() != lease.GetFence() ||
			renewed.GetLease().GetGeneration() != lease.GetGeneration() {
			metrics.Track(-1)
			metrics.Occurrence("renew_error")
			if renewErr != nil {
				return processed, renewErr
			}
			return processed, errors.New("renewed schedule lease is incomplete")
		}
		lease = renewed.GetLease()
		rpc, cancel = context.WithTimeout(ctx, config.RPCDeadline)
		_, err := control.MaterializeScheduleOccurrence(rpc, &controlplanev1.MaterializeScheduleOccurrenceRequest{
			Mutation:      &controlplanev1.MutationContext{IdempotencyKey: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s\x00%d\x00materialize", claim.GetOccurrenceRef(), lease.GetGeneration()))).String()},
			OccurrenceRef: claim.GetOccurrenceRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(),
		})
		cancel()
		metrics.Track(-1)
		if err != nil {
			metrics.Occurrence("materialize_error")
			failErr := failScheduleOccurrence(ctx, control, claimWithLease(claim, lease), "SCHEDULE_MATERIALIZATION_FAILED", retryableRPCError(err), config.RPCDeadline)
			if failErr != nil {
				return processed, errors.Join(err, failErr)
			}
			return processed, err
		}
		metrics.Occurrence("materialized")
		processed++
	}
	return processed, nil
}

func validateScheduleClaim(claim *controlplanev1.ScheduleClaim, now time.Time) error {
	if claim == nil || claim.GetSchedule() == nil || claim.GetSchedule().GetRef() == "" || claim.GetSchedule().GetVersion() < 1 ||
		claim.GetOccurrenceRef() == "" || claim.GetScheduledFor() == nil || claim.GetScheduledFor().CheckValid() != nil ||
		claim.GetAttempt() < 1 || claim.GetAttempt() > 3 || claim.GetScheduleRevisionRef() == "" || claim.GetScheduleRevision() < 1 ||
		claim.GetTargetRef() == "" || claim.GetTargetVersion() < 1 ||
		!digestPattern.MatchString(claim.GetInputDigest()) || !digestPattern.MatchString(claim.GetScheduleRevisionDigest()) ||
		!digestPattern.MatchString(claim.GetTargetDigest()) || !digestPattern.MatchString(claim.GetAutomationTextDigest()) ||
		!digestPattern.MatchString(claim.GetPromptInputsDigest()) {
		return errors.New("schedule claim provenance is incomplete")
	}
	lease := claim.GetLease()
	if lease == nil || lease.GetRef() == "" || lease.GetFence() == "" || lease.GetGeneration() < 1 ||
		lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil || !lease.GetExpiresAt().AsTime().After(now) {
		return errors.New("schedule claim lease is invalid")
	}
	return nil
}

func claimWithLease(claim *controlplanev1.ScheduleClaim, lease *controlplanev1.WorkLease) *controlplanev1.ScheduleClaim {
	copy := proto.Clone(claim).(*controlplanev1.ScheduleClaim)
	copy.Lease = lease
	return copy
}

func failScheduleOccurrence(ctx context.Context, control scheduleRuntimeClient, claim *controlplanev1.ScheduleClaim, safeErrorCode string, retryable bool, timeout time.Duration) error {
	lease := claim.GetLease()
	if lease == nil {
		return errors.New("schedule claim lease is missing")
	}
	rpc, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	_, err := control.FailScheduleOccurrence(rpc, &controlplanev1.FailScheduleOccurrenceRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: uuid.NewSHA1(uuid.NameSpaceOID,
			[]byte(fmt.Sprintf("%s\x00%d\x00fail", claim.GetOccurrenceRef(), lease.GetGeneration()))).String()},
		OccurrenceRef: claim.GetOccurrenceRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(),
		Generation: lease.GetGeneration(), SafeErrorCode: safeErrorCode, Retryable: retryable,
	})
	return err
}

func retryableRPCError(err error) bool {
	switch status.Code(err) {
	case codes.Canceled, codes.DeadlineExceeded, codes.Internal, codes.Unknown, codes.Unavailable:
		return true
	default:
		return false
	}
}
