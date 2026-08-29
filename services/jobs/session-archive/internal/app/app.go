package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/controller"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"github.com/google/uuid"
)

const issuerUID, issuerGID = 29001, 29000

func Run(lifecycle, shutdownBase context.Context, buildVersion string) error {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancel := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancel()
	telemetryConfig, err := sharedobservability.RuntimeConfigFromEnv(serviceName, buildVersion)
	if err != nil {
		return err
	}
	telemetry, err := sharedobservability.NewRuntime(startup, telemetryConfig)
	if err != nil {
		return err
	}
	logger := telemetry.Logger(os.Stdout)
	metrics := sharedobservability.NewMetrics("session_archive", buildVersion, map[string]string{})
	owned := newArchiveMetrics()
	if err := metrics.Register(owned.collectors()...); err != nil {
		return err
	}
	readiness := serviceruntime.NewReadiness()
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{Target: config.ControlPlaneTarget,
		TLSServerName: config.ControlPlaneTLSServerName, CAFile: config.ControlPlaneCAFile,
		ClientCertificateFile: config.ControlPlaneCertificateFile, ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile, ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID,
		DialTimeout: config.RPCDeadline, Operations: controlplaneclient.SessionArchiveOperations()})
	if err != nil {
		return err
	}
	kubernetes, err := controller.InCluster(controller.Config{Namespace: config.Namespace, Environment: config.Environment,
		WorkerImage: config.WorkerImage, WorkerServiceAccount: config.WorkerServiceAccount, ObjectStorageSecret: config.ObjectStorageSecret,
		StorageClass: config.StorageClass, SessionPVCSize: config.SessionPVCSize,
		ObjectStorageEndpoint: config.ObjectStorageEndpoint, ObjectStorageRegion: config.ObjectStorageRegion,
		ObjectStorageBucket: config.ObjectStorageBucket, ObjectStorageAllowInsecureLocal: config.ObjectStorageAllowInsecureLocal,
		WorkerTimeout: config.WorkerTimeout})
	if err != nil {
		_ = control.Close()
		return err
	}
	if err := control.CheckLocalAuthority(startup); err != nil {
		_ = control.Close()
		return err
	}
	if err := kubernetes.Check(startup); err != nil {
		_ = control.Close()
		return err
	}
	if err := kubernetes.CleanupStale(startup); err != nil {
		_ = control.Close()
		return err
	}
	technical, err := httpserver.New(httpserver.Config{Address: config.TechnicalListen, ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 32 << 10,
		MaximumConnections: 128}, readiness, metrics.PrometheusHandler())
	if err != nil {
		return err
	}
	if err := technical.Listen(); err != nil {
		return err
	}
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	workers := serviceruntime.StartWorkers(lifecycle, serveTechnical(technical), runLoop(control, kubernetes, readiness, metrics, owned, logger, config))
	err = workers.Wait(context.WithoutCancel(lifecycle))
	readiness.Set(false, "stopping")
	metrics.SetReady(false)
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "session archive workers", Timeout: config.ShutdownTimeout / 2, Run: workers.Wait},
		serviceruntime.ShutdownOperation{Name: "control-plane client", Timeout: config.ShutdownTimeout / 4, Run: func(context.Context) error { return control.Close() }},
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: config.ShutdownTimeout / 4, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "tracing", Timeout: 5 * time.Second, Run: telemetry.ShutdownTracing},
		serviceruntime.ShutdownOperation{Name: "error reporting", Timeout: 5 * time.Second, Run: telemetry.FlushSentry})
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
			return ctx.Err()
		}
	}
}

func runLoop(control *controlplaneclient.Client, kube *controller.Controller, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, owned *archiveMetrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		var lastKubernetesCheck time.Time
		kubernetesReady := true
		for {
			if lastKubernetesCheck.IsZero() || time.Since(lastKubernetesCheck) >= config.ReadinessInterval {
				check, cancel := context.WithTimeout(ctx, config.RPCDeadline)
				err := kube.Check(check)
				cancel()
				lastKubernetesCheck = time.Now()
				kubernetesReady = err == nil
				if err != nil {
					readiness.Set(false, "kubernetes_unavailable")
					metrics.SetReady(false)
					logger.WarnContext(ctx, "session archive Kubernetes check failed", "error_class", "kubernetes_api")
				}
			}
			if !kubernetesReady {
				owned.cycles.WithLabelValues("error").Inc()
			} else {
				cycle, cancel := context.WithTimeout(ctx, config.RPCDeadline)
				claimed, err := control.SessionArchive.ClaimSessionArchiveTasks(cycle, &controlplanev1.ClaimSessionArchiveTasksRequest{WorkloadInstance: config.InstanceID, Limit: 1})
				cancel()
				if err != nil {
					owned.cycles.WithLabelValues("error").Inc()
					readiness.Set(false, "control_plane_unavailable")
					metrics.SetReady(false)
					logger.WarnContext(ctx, "session archive claim failed", "error_class", "control_plane")
				} else {
					owned.cycles.WithLabelValues("success").Inc()
					readiness.Set(true, "ready")
					metrics.SetReady(true)
					if len(claimed.GetTasks()) > 0 {
						if err := process(ctx, control, kube, claimed.GetTasks()[0], owned, config); err != nil {
							logger.WarnContext(ctx, "session archive task processing failed", "error_class", "task_processing")
						}
					}
				}
			}
			timer := time.NewTimer(config.PollInterval)
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

func process(ctx context.Context, control *controlplaneclient.Client, kube *controller.Controller, claim *controlplanev1.SessionArchiveTask, metrics *archiveMetrics, config Config) error {
	if claim == nil || claim.GetLease() == nil {
		return errors.New("claimed session archive task is incomplete")
	}
	lease := claim.GetLease()
	task, err := model.FromProto(claim)
	if err != nil {
		metrics.tasks.WithLabelValues("INVALID", "error").Inc()
		if failErr := failClaim(ctx, control, claim.GetTaskRef(), lease, "SESSION_ARCHIVE_SOURCE_INVALID", config.RPCDeadline); failErr != nil {
			return errors.New("reject invalid session archive task")
		}
		return errors.New("invalid session archive task was rejected")
	}
	started := time.Now()
	metrics.active.Inc()
	defer metrics.active.Dec()
	defer func() { metrics.duration.WithLabelValues(task.Kind).Observe(time.Since(started).Seconds()) }()
	work, cancel := context.WithTimeout(ctx, config.WorkerTimeout+30*time.Second)
	defer cancel()
	renew := func(call context.Context) error {
		rpc, c := context.WithTimeout(call, config.RPCDeadline)
		defer c()
		_, err := control.SessionArchive.RenewSessionArchiveTask(rpc, &controlplanev1.RenewSessionArchiveTaskRequest{TaskRef: task.TaskRef, LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration()})
		return err
	}
	result, runErr := kube.Execute(work, task, renew)
	if runErr != nil {
		result = model.Result{Success: false, SafeErrorCode: "SESSION_ARCHIVE_KUBERNETES_UNAVAILABLE"}
	}
	rpc, cancelRPC := context.WithTimeout(context.WithoutCancel(ctx), config.RPCDeadline)
	defer cancelRPC()
	mutation := &controlplanev1.MutationContext{IdempotencyKey: uuid.NewSHA1(uuid.NameSpaceOID, []byte(task.TaskRef+"\x00"+fmt.Sprint(lease.GetGeneration())+"\x00complete")).String()}
	base := func() (string, string, string, int64) {
		return lease.GetRef(), lease.GetFence(), task.TaskRef, lease.GetGeneration()
	}
	lr, lf, tr, g := base()
	if !result.Success {
		_, err = control.SessionArchive.FailSessionArchiveTask(rpc, &controlplanev1.FailSessionArchiveTaskRequest{Mutation: mutation, TaskRef: tr, LeaseRef: lr, Fence: lf, Generation: g, SafeErrorCode: result.SafeErrorCode})
	} else {
		switch task.Kind {
		case "SNAPSHOT":
			_, err = control.SessionArchive.CompleteSessionSnapshot(rpc, &controlplanev1.CompleteSessionSnapshotRequest{Mutation: mutation, TaskRef: tr, LeaseRef: lr, Fence: lf, Generation: g, FormatVersion: result.FormatVersion, ObjectKey: result.ObjectKey, ObjectVersion: result.ObjectVersion, ObjectEtag: result.ObjectETag, ObjectDigest: result.ObjectDigest, ObjectSizeBytes: result.ObjectSizeBytes, SourceSizeBytes: result.SourceSizeBytes})
		case "RESTORE":
			_, err = control.SessionArchive.CompleteSessionRestore(rpc, &controlplanev1.CompleteSessionRestoreRequest{Mutation: mutation, TaskRef: tr, LeaseRef: lr, Fence: lf, Generation: g, FormatVersion: result.FormatVersion, ObjectKey: result.ObjectKey, ObjectVersion: result.ObjectVersion, ObjectEtag: result.ObjectETag, ObjectDigest: result.ObjectDigest, ObjectSizeBytes: result.ObjectSizeBytes, RestoredSourceSha256: result.SourceSHA256, RestoredSourceSizeBytes: result.SourceSizeBytes})
		case "DELETE_PVC":
			_, err = control.SessionArchive.CompleteSessionPVCDeletion(rpc, &controlplanev1.CompleteSessionPVCDeletionRequest{Mutation: mutation, TaskRef: tr, LeaseRef: lr, Fence: lf, Generation: g, PvcName: task.PVCName})
		case "DELETE_OBJECT":
			_, err = control.SessionArchive.CompleteSessionObjectDeletion(rpc, &controlplanev1.CompleteSessionObjectDeletionRequest{Mutation: mutation, TaskRef: tr, LeaseRef: lr, Fence: lf, Generation: g, ObjectKey: task.TargetObjectKey, ObjectVersion: task.TargetObjectVersion})
		}
	}
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	metrics.tasks.WithLabelValues(task.Kind, outcome).Inc()
	if err == nil && result.ObjectSizeBytes > 0 {
		metrics.bytes.WithLabelValues(task.Kind).Add(float64(result.ObjectSizeBytes))
	}
	return err
}

func failClaim(ctx context.Context, control *controlplaneclient.Client, taskRef string, lease *controlplanev1.WorkLease, safeErrorCode string, timeout time.Duration) error {
	if taskRef == "" || lease == nil || lease.GetRef() == "" || lease.GetFence() == "" || lease.GetGeneration() < 1 {
		return errors.New("session archive claim identity is invalid")
	}
	rpc, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	mutation := &controlplanev1.MutationContext{IdempotencyKey: uuid.NewSHA1(uuid.NameSpaceOID,
		[]byte(taskRef+"\x00"+fmt.Sprint(lease.GetGeneration())+"\x00fail-invalid")).String()}
	_, err := control.SessionArchive.FailSessionArchiveTask(rpc, &controlplanev1.FailSessionArchiveTaskRequest{
		Mutation: mutation, TaskRef: taskRef, LeaseRef: lease.GetRef(), Fence: lease.GetFence(),
		Generation: lease.GetGeneration(), SafeErrorCode: safeErrorCode,
	})
	return err
}
