package app

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
)

const serviceName = "control-plane"

// Config задаёт только typed names/paths/endpoints; secret values читаются из файлов.
type Config struct {
	GRPCListen                        string        `env:"CONTROL_PLANE_GRPC_LISTEN"`
	TechnicalListen                   string        `env:"CONTROL_PLANE_TECHNICAL_LISTEN"`
	ServerCertificateFile             string        `env:"CONTROL_PLANE_TLS_CERTIFICATE_FILE"`
	ServerPrivateKeyFile              string        `env:"CONTROL_PLANE_TLS_PRIVATE_KEY_FILE"`
	ClientCAFile                      string        `env:"CONTROL_PLANE_TLS_CLIENT_CA_FILE"`
	PostgresDSNFile                   string        `env:"CONTROL_PLANE_POSTGRES_DSN_FILE"`
	PostgresRelayDSNFile              string        `env:"CONTROL_PLANE_POSTGRES_RELAY_DSN_FILE"`
	PostgresPrincipalName             string        `env:"CONTROL_PLANE_POSTGRES_PRINCIPAL_NAME"`
	PostgresPrincipalGeneration       uint64        `env:"CONTROL_PLANE_POSTGRES_PRINCIPAL_GENERATION"`
	PostgresContextKeyID              string        `env:"CONTROL_PLANE_POSTGRES_CONTEXT_KEY_ID"`
	PostgresContextKeyFile            string        `env:"CONTROL_PLANE_POSTGRES_CONTEXT_KEY_FILE"`
	PostgresTLSServerName             string        `env:"CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME"`
	PostgresCAFile                    string        `env:"CONTROL_PLANE_POSTGRES_CA_FILE"`
	PostgresMaxConnections            int32         `env:"CONTROL_PLANE_POSTGRES_MAX_CONNECTIONS"`
	RedisAddress                      string        `env:"CONTROL_PLANE_REDIS_ADDRESS"`
	RedisTLSServerName                string        `env:"CONTROL_PLANE_REDIS_TLS_SERVER_NAME"`
	RedisCAFile                       string        `env:"CONTROL_PLANE_REDIS_CA_FILE"`
	RedisUsername                     string        `env:"CONTROL_PLANE_REDIS_USERNAME"`
	RedisPasswordFile                 string        `env:"CONTROL_PLANE_REDIS_PASSWORD_FILE"`
	RedisDatabase                     int           `env:"CONTROL_PLANE_REDIS_DATABASE"`
	RedisPoolSize                     int           `env:"CONTROL_PLANE_REDIS_POOL_SIZE"`
	NATSURL                           string        `env:"CONTROL_PLANE_NATS_URL"`
	NATSTLSServerName                 string        `env:"CONTROL_PLANE_NATS_TLS_SERVER_NAME"`
	NATSCAFile                        string        `env:"CONTROL_PLANE_NATS_CA_FILE"`
	NATSCredentialsFile               string        `env:"CONTROL_PLANE_NATS_CREDENTIALS_FILE"`
	NATSStream                        string        `env:"CONTROL_PLANE_NATS_STREAM"`
	NATSReplicas                      int           `env:"CONTROL_PLANE_NATS_REPLICAS"`
	AuthorityPolicyFile               string        `env:"CONTROL_PLANE_AUTHORITY_POLICY_FILE"`
	ProofPrivateJWKFile               string        `env:"CONTROL_PLANE_PROOF_PRIVATE_JWK_FILE"`
	ProofTrustFile                    string        `env:"CONTROL_PLANE_PROOF_TRUST_FILE"`
	ProofSignerGeneration             uint64        `env:"CONTROL_PLANE_PROOF_SIGNER_GENERATION"`
	ContinuationGrantPrivateJWKFile   string        `env:"CONTROL_PLANE_CONTINUATION_GRANT_PRIVATE_JWK_FILE"`
	ContinuationGrantSignerGeneration uint64        `env:"CONTROL_PLANE_CONTINUATION_GRANT_SIGNER_GENERATION"`
	LeaseSigningKeyFile               string        `env:"CONTROL_PLANE_LEASE_SIGNING_KEY_FILE"`
	RuntimeAdmissionSigningKeyFile    string        `env:"CONTROL_PLANE_RUNTIME_ADMISSION_SIGNING_KEY_FILE"`
	RuntimeArchiveSigningKeyFile      string        `env:"CONTROL_PLANE_RUNTIME_ARCHIVE_SIGNING_KEY_FILE"`
	RuntimeRestoreSigningKeyFile      string        `env:"CONTROL_PLANE_RUNTIME_RESTORE_SIGNING_KEY_FILE"`
	RuntimeImageDigest                string        `env:"CONTROL_PLANE_RUNTIME_IMAGE_DIGEST"`
	PendingRescheduleDelay            time.Duration `env:"CONTROL_PLANE_PENDING_RESCHEDULE_DELAY"`
	OIDCTLSServerName                 string        `env:"CONTROL_PLANE_OIDC_TLS_SERVER_NAME"`
	OIDCCAFile                        string        `env:"CONTROL_PLANE_OIDC_CA_FILE"`
	ApplicationGrantTrustDir          string        `env:"CONTROL_PLANE_APPLICATION_GRANT_TRUST_DIR"`
	InstanceID                        string        `env:"POD_UID"`
	StartupTimeout                    time.Duration `env:"CONTROL_PLANE_STARTUP_TIMEOUT"`
	ReadinessTimeout                  time.Duration `env:"CONTROL_PLANE_READINESS_TIMEOUT"`
	ReadinessInterval                 time.Duration `env:"CONTROL_PLANE_READINESS_INTERVAL"`
	ShutdownTimeout                   time.Duration `env:"CONTROL_PLANE_SHUTDOWN_TIMEOUT"`
	CacheTTL                          time.Duration `env:"CONTROL_PLANE_CACHE_TTL"`
	CacheTimeout                      time.Duration `env:"CONTROL_PLANE_CACHE_TIMEOUT"`
	TurnLeaseDuration                 time.Duration `env:"CONTROL_PLANE_TURN_LEASE_DURATION"`
	ScheduleClaimLimit                int           `env:"CONTROL_PLANE_SCHEDULE_CLAIM_LIMIT"`
	RelayPollInterval                 time.Duration `env:"CONTROL_PLANE_RELAY_POLL_INTERVAL"`
	RelayLeaseDuration                time.Duration `env:"CONTROL_PLANE_RELAY_LEASE_DURATION"`
	RelayPublishTimeout               time.Duration `env:"CONTROL_PLANE_RELAY_PUBLISH_TIMEOUT"`
	RelayFinalizeTimeout              time.Duration `env:"CONTROL_PLANE_RELAY_FINALIZE_TIMEOUT"`
}

func loadConfig() (Config, error) {
	config := Config{
		GRPCListen:                        ":8443",
		TechnicalListen:                   ":9090",
		ServerCertificateFile:             "/var/run/secrets/mattercodex/control-plane/workload-tls/tls.crt",
		ServerPrivateKeyFile:              "/var/run/secrets/mattercodex/control-plane/workload-tls/tls.key",
		ClientCAFile:                      "/var/run/config/mattercodex/control-plane/internal-ca/ca.pem",
		PostgresDSNFile:                   "/var/run/secrets/mattercodex/control-plane/postgres-runtime/dsn",
		PostgresRelayDSNFile:              "/var/run/secrets/mattercodex/control-plane/postgres-relay/dsn",
		PostgresPrincipalName:             "control_plane_runtime_g1",
		PostgresPrincipalGeneration:       1,
		PostgresContextKeyID:              "control-plane-db-context-g1",
		PostgresContextKeyFile:            "/var/run/secrets/mattercodex/control-plane/postgres-context/key",
		PostgresTLSServerName:             "control-plane-postgresql.mattercodex-system.svc.cluster.local",
		PostgresCAFile:                    "/var/run/config/mattercodex/control-plane/postgres/ca.pem",
		PostgresMaxConnections:            16,
		RedisAddress:                      "control-plane-redis.mattercodex-system.svc:6379",
		RedisTLSServerName:                "control-plane-redis.mattercodex-system.svc.cluster.local",
		RedisCAFile:                       "/var/run/config/mattercodex/control-plane/redis/ca.pem",
		RedisUsername:                     "control-plane",
		RedisPasswordFile:                 "/var/run/secrets/mattercodex/control-plane/redis/password",
		RedisDatabase:                     0,
		RedisPoolSize:                     16,
		NATSURL:                           "tls://nats.mattercodex-system.svc:4222",
		NATSTLSServerName:                 "nats.mattercodex-system.svc.cluster.local",
		NATSCAFile:                        "/var/run/config/mattercodex/control-plane/nats/ca.pem",
		NATSCredentialsFile:               "/var/run/secrets/mattercodex/control-plane/nats/user.creds",
		NATSStream:                        "CONTROL_PLANE",
		NATSReplicas:                      3,
		AuthorityPolicyFile:               "/var/run/config/mattercodex/control-plane/authority/policy.json",
		ProofPrivateJWKFile:               "/var/run/secrets/mattercodex/internal-rpc-authority/proof-signer/private.jwk",
		ProofTrustFile:                    "/var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust/jwks.json",
		ProofSignerGeneration:             1,
		ContinuationGrantPrivateJWKFile:   "/var/run/secrets/mattercodex/control-plane/continuation-grant/private.jwk",
		ContinuationGrantSignerGeneration: 1,
		LeaseSigningKeyFile:               "/var/run/secrets/mattercodex/control-plane/lease-signing/key",
		RuntimeAdmissionSigningKeyFile:    "/var/run/secrets/mattercodex/control-plane/runtime-workload-signing/admission-private-key.hex",
		RuntimeArchiveSigningKeyFile:      "/var/run/secrets/mattercodex/control-plane/runtime-workload-signing/archive-private-key.hex",
		RuntimeRestoreSigningKeyFile:      "/var/run/secrets/mattercodex/control-plane/runtime-workload-signing/restore-private-key.hex",
		PendingRescheduleDelay:            30 * time.Second,
		OIDCTLSServerName:                 "sso.mattercodex.local",
		OIDCCAFile:                        "/var/run/config/mattercodex/control-plane/oidc/ca.pem",
		ApplicationGrantTrustDir:          "/var/run/config/mattercodex/control-plane/application-grants",
		StartupTimeout:                    15 * time.Second,
		ReadinessTimeout:                  2 * time.Second,
		ReadinessInterval:                 10 * time.Second,
		ShutdownTimeout:                   10 * time.Second,
		CacheTTL:                          30 * time.Second,
		CacheTimeout:                      100 * time.Millisecond,
		TurnLeaseDuration:                 30 * time.Second,
		ScheduleClaimLimit:                64,
		RelayPollInterval:                 250 * time.Millisecond,
		RelayLeaseDuration:                10 * time.Second,
		RelayPublishTimeout:               2 * time.Second,
		RelayFinalizeTimeout:              2 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) validate() error {
	for _, endpoint := range []string{
		config.GRPCListen,
		config.TechnicalListen,
		config.RedisAddress,
	} {
		if _, _, err := net.SplitHostPort(endpoint); err != nil {
			return errors.New("control-plane endpoint is invalid")
		}
	}
	if !strings.HasPrefix(config.NATSURL, "tls://") ||
		config.NATSStream != "CONTROL_PLANE" ||
		config.NATSReplicas < 1 || config.NATSReplicas > 5 ||
		config.PostgresMaxConnections < 2 ||
		config.PostgresMaxConnections > 64 ||
		config.RedisPoolSize < 1 || config.RedisPoolSize > 64 ||
		config.RedisDatabase < 0 || config.RedisDatabase > 15 ||
		config.ProofSignerGeneration == 0 || config.ContinuationGrantSignerGeneration == 0 ||
		config.PostgresPrincipalName == "" ||
		config.PostgresPrincipalGeneration == 0 ||
		config.PostgresContextKeyID == "" ||
		!validRuntimeImageDigest(config.RuntimeImageDigest) ||
		config.InstanceID == "" || len(config.InstanceID) > 128 ||
		config.ScheduleClaimLimit < 1 || config.ScheduleClaimLimit > 128 {
		return errors.New("control-plane bounded configuration is invalid")
	}
	for _, serverName := range []string{
		config.PostgresTLSServerName,
		config.RedisTLSServerName,
		config.NATSTLSServerName,
		config.OIDCTLSServerName,
	} {
		if serverName == "" || net.ParseIP(serverName) != nil {
			return errors.New("control-plane TLS server name is invalid")
		}
	}
	for _, path := range []string{
		config.ServerCertificateFile,
		config.ServerPrivateKeyFile,
		config.ClientCAFile,
		config.PostgresDSNFile,
		config.PostgresRelayDSNFile,
		config.PostgresContextKeyFile,
		config.PostgresCAFile,
		config.RedisCAFile,
		config.RedisPasswordFile,
		config.NATSCAFile,
		config.NATSCredentialsFile,
		config.AuthorityPolicyFile,
		config.ProofPrivateJWKFile,
		config.ProofTrustFile,
		config.ContinuationGrantPrivateJWKFile,
		config.LeaseSigningKeyFile,
		config.RuntimeAdmissionSigningKeyFile,
		config.RuntimeArchiveSigningKeyFile,
		config.RuntimeRestoreSigningKeyFile,
		config.OIDCCAFile,
		config.ApplicationGrantTrustDir,
	} {
		if !filepath.IsAbs(path) {
			return errors.New("control-plane runtime path must be absolute")
		}
	}
	if config.StartupTimeout < time.Second ||
		config.StartupTimeout > time.Minute ||
		config.ReadinessTimeout < 100*time.Millisecond ||
		config.ReadinessTimeout > 5*time.Second ||
		config.ReadinessInterval < time.Second ||
		config.ReadinessInterval > time.Minute ||
		config.ShutdownTimeout < time.Second ||
		config.ShutdownTimeout > 30*time.Second ||
		config.CacheTTL <= 0 || config.CacheTTL > time.Minute ||
		config.CacheTimeout < 10*time.Millisecond ||
		config.CacheTimeout > time.Second ||
		config.TurnLeaseDuration < 5*time.Second ||
		config.TurnLeaseDuration > 5*time.Minute ||
		config.PendingRescheduleDelay < 5*time.Second || config.PendingRescheduleDelay > 5*time.Minute {
		return errors.New("control-plane duration is invalid")
	}
	return nil
}

func validRuntimeImageDigest(input string) bool {
	const zeroDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if len(input) != 71 || !strings.HasPrefix(input, "sha256:") || input == zeroDigest {
		return false
	}
	for _, symbol := range strings.TrimPrefix(input, "sha256:") {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

func expectedOperations() map[string]string {
	return map[string]string{
		"control.project.create":                       controlplanev1.ControlPlaneService_CreateProject_FullMethodName,
		"control.project.list":                         controlplanev1.ControlPlaneService_ListProjects_FullMethodName,
		"control.resource.create":                      controlplanev1.ControlPlaneService_CreateResource_FullMethodName,
		"control.resource.update":                      controlplanev1.ControlPlaneService_UpdateResource_FullMethodName,
		"control.resource.transition":                  controlplanev1.ControlPlaneService_TransitionResource_FullMethodName,
		"control.resource.delete":                      controlplanev1.ControlPlaneService_DeleteResource_FullMethodName,
		"control.access.manage":                        controlplanev1.ControlPlaneService_ManageAccessResource_FullMethodName,
		"control.resource.get":                         controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.resource.list":                        controlplanev1.ControlPlaneService_ListResources_FullMethodName,
		"control.resource.search":                      controlplanev1.ControlPlaneService_SearchResources_FullMethodName,
		"control.audit.list":                           controlplanev1.ControlPlaneService_ListAuditEvents_FullMethodName,
		"control.tombstone.list":                       controlplanev1.ControlPlaneService_ListTombstones_FullMethodName,
		"control.diagnostics.get":                      controlplanev1.ControlPlaneService_GetDiagnostics_FullMethodName,
		"control.readiness.check":                      controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.turn.enqueue":                         controlplanev1.ControlPlaneService_EnqueueTurn_FullMethodName,
		"control.turn.retry":                           controlplanev1.ControlPlaneService_RetryTurn_FullMethodName,
		"control.turn.cancel":                          controlplanev1.ControlPlaneService_CancelTurn_FullMethodName,
		"control.schedule.manage":                      controlplanev1.ControlPlaneService_ManageSchedule_FullMethodName,
		"control.schedule.cancel-occurrence":           controlplanev1.ControlPlaneService_CancelScheduleOccurrence_FullMethodName,
		"control.schedule.list-occurrences":            controlplanev1.ControlPlaneService_ListScheduleOccurrences_FullMethodName,
		"control.process.start":                        controlplanev1.ControlPlaneService_StartProcess_FullMethodName,
		"control.process.cancel":                       controlplanev1.ControlPlaneService_CancelProcess_FullMethodName,
		"control.process.complete":                     controlplanev1.ControlPlaneService_CompleteProcess_FullMethodName,
		"control.outbox.read":                          controlplanev1.ControlPlaneService_ListOutboxFailures_FullMethodName,
		"control.outbox.repair":                        controlplanev1.ControlPlaneService_RepairOutboxEvent_FullMethodName,
		"control.owner-gate.request":                   controlplanev1.ControlPlaneService_RequestOwnerGate_FullMethodName,
		"control.owner-gate.resolve":                   controlplanev1.ControlPlaneService_ResolveOwnerGate_FullMethodName,
		"control.artifact.register":                    controlplanev1.ControlPlaneService_RegisterArtifact_FullMethodName,
		"control.session.manage":                       controlplanev1.ControlPlaneService_ManageSession_FullMethodName,
		"control.memory.manage":                        controlplanev1.ControlPlaneService_ManageMemoryRecord_FullMethodName,
		"control.memory.search":                        controlplanev1.ControlPlaneService_SearchMemoryRecords_FullMethodName,
		"control.work-claim.manage":                    controlplanev1.ControlPlaneService_ManageWorkClaim_FullMethodName,
		"control.agent-work-claim.manage":              controlplanev1.ControlPlaneService_ManageWorkClaim_FullMethodName,
		"control.agent-runner.readiness":               controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.turn.claim":                           controlplanev1.ControlPlaneService_ClaimTurn_FullMethodName,
		"control.turn.renew":                           controlplanev1.ControlPlaneService_RenewTurn_FullMethodName,
		"control.turn.complete":                        controlplanev1.ControlPlaneService_CompleteTurn_FullMethodName,
		"control.schedule.claim-due":                   controlplanev1.ControlPlaneService_ClaimDueSchedules_FullMethodName,
		"control.schedule.claim-occurrence":            controlplanev1.ControlPlaneService_ClaimScheduleOccurrence_FullMethodName,
		"control.schedule.complete-occurrence":         controlplanev1.ControlPlaneService_CompleteScheduleOccurrence_FullMethodName,
		"control.automation-scheduler.readiness":       controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.schedule-resource.get":                controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.artifact.scan":                        controlplanev1.ControlPlaneService_RecordArtifactScan_FullMethodName,
		"control.artifact-scanner.readiness":           controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.owner-gate.deliver":                   controlplanev1.ControlPlaneService_RecordOwnerGateDelivery_FullMethodName,
		"control.owner-gate.claim-delivery":            controlplanev1.ControlPlaneService_ClaimOwnerGateDelivery_FullMethodName,
		"control.owner-gate.expire":                    controlplanev1.ControlPlaneService_ExpireOwnerGate_FullMethodName,
		"control.owner-gate-delivery.readiness":        controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-revision.get":                 controlplanev1.ControlPlaneService_GetRuntimeRevision_FullMethodName,
		"control.runtime-resource.get":                 controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.runtime-controller.readiness":         controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.memory.index":                         controlplanev1.ControlPlaneService_RecordMemoryEmbedding_FullMethodName,
		"control.memory-indexer.readiness":             controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-execution.claim":              controlplanev1.ControlPlaneService_ClaimRuntimeExecution_FullMethodName,
		"control.runtime-execution.agent.bind":         controlplanev1.ControlPlaneService_BindRuntimeAgentSession_FullMethodName,
		"control.runtime-execution.agent.materialize":  controlplanev1.ControlPlaneService_MaterializeRuntimeAgentTurn_FullMethodName,
		"control.runtime-execution.agent.resolve":      controlplanev1.ControlPlaneService_ResolveRuntimeAgentBindingIntent_FullMethodName,
		"control.runtime-retention.set":                controlplanev1.ControlPlaneService_SetResourceRetentionPolicy_FullMethodName,
		"control.runtime-retention.retire":             controlplanev1.ControlPlaneService_RetireResourceRetentionPolicy_FullMethodName,
		"control.runtime-retention.get":                controlplanev1.ControlPlaneService_GetResourceRetentionPolicy_FullMethodName,
		"control.runtime-retention.hold":               controlplanev1.ControlPlaneService_HoldRuntimeRetention_FullMethodName,
		"control.runtime-retention.release":            controlplanev1.ControlPlaneService_ReleaseRuntimeRetention_FullMethodName,
		"control.runtime-execution.get":                controlplanev1.ControlPlaneService_GetRuntimeExecution_FullMethodName,
		"control.runtime-execution.admit":              controlplanev1.ControlPlaneService_AdmitRuntimeExecution_FullMethodName,
		"control.runtime-execution.heartbeat":          controlplanev1.ControlPlaneService_HeartbeatRuntimeExecution_FullMethodName,
		"control.runtime-execution.incident":           controlplanev1.ControlPlaneService_RecordRuntimeIncident_FullMethodName,
		"control.runtime-execution.complete":           controlplanev1.ControlPlaneService_CompleteRuntimeExecution_FullMethodName,
		"control.runtime-execution.cancel":             controlplanev1.ControlPlaneService_CancelRuntimeExecution_FullMethodName,
		"control.runtime-execution.retry":              controlplanev1.ControlPlaneService_RetryRuntimeExecution_FullMethodName,
		"control.runtime-execution.reschedule":         controlplanev1.ControlPlaneService_RescheduleRuntimeExecution_FullMethodName,
		"control.runtime-execution.expire":             controlplanev1.ControlPlaneService_ExpireRuntimeExecution_FullMethodName,
		"control.runtime-execution.archive":            controlplanev1.ControlPlaneService_RecordRuntimeArchive_FullMethodName,
		"control.runtime-execution.restore.verify":     controlplanev1.ControlPlaneService_VerifyRuntimeRestore_FullMethodName,
		"control.runtime-execution.restore.bind":       controlplanev1.ControlPlaneService_BindRuntimeRestoreTarget_FullMethodName,
		"control.runtime-execution.rehydrate.complete": controlplanev1.ControlPlaneService_CompleteRuntimeRehydrate_FullMethodName,
		"control.runtime-execution.cleanup.authorize":  controlplanev1.ControlPlaneService_AuthorizeRuntimeCleanup_FullMethodName,
		"control.runtime-execution.cleanup.consume":    controlplanev1.ControlPlaneService_ConsumeRuntimeCleanupAuthorization_FullMethodName,
		"control.runtime-execution.cleanup.expire":     controlplanev1.ControlPlaneService_ExpireRuntimeCleanupAuthorization_FullMethodName,
		"control.runtime-restore-verifier.readiness":   controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-cleanup-authorizer.readiness": controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.integration-gateway.readiness":        controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.integration-session.resolve":          controlplanev1.ControlPlaneService_ResolveIntegrationSession_FullMethodName,
		"control.integration-continuation.suspend":     controlplanev1.ControlPlaneService_SuspendForIntegrationApproval_FullMethodName,
		"control.integration-invocation.approve":       controlplanev1.ControlPlaneService_ApproveIntegrationInvocation_FullMethodName,
		"control.integration-invocation.reject":        controlplanev1.ControlPlaneService_RejectIntegrationInvocation_FullMethodName,
		"control.integration-invocation.expire":        controlplanev1.ControlPlaneService_ExpireIntegrationInvocation_FullMethodName,
		"control.integration-invocation.cancel":        controlplanev1.ControlPlaneService_CancelIntegrationInvocation_FullMethodName,
		"control.integration-execution.begin":          controlplanev1.ControlPlaneService_BeginIntegrationExecution_FullMethodName,
		"control.integration-execution.complete":       controlplanev1.ControlPlaneService_CompleteIntegrationExecution_FullMethodName,
		"control.integration-execution.fail":           controlplanev1.ControlPlaneService_FailIntegrationExecution_FullMethodName,
		"control.integration-continuation.get":         controlplanev1.ControlPlaneService_GetIntegrationContinuation_FullMethodName,
		"control.integration-continuation.acknowledge": controlplanev1.ControlPlaneService_AcknowledgeIntegrationContinuation_FullMethodName,
		"control.integration-result.validate":          controlplanev1.ControlPlaneService_ValidateIntegrationResultAccess_FullMethodName,
		"integration.result.resolve":                   integrationgatewayv1.IntegrationResultService_ResolveIntegrationResult_FullMethodName,
		"integration.result.acknowledge":               integrationgatewayv1.IntegrationResultService_AcknowledgeIntegrationResult_FullMethodName,
		"integration.result.readiness":                 integrationgatewayv1.IntegrationResultService_CheckReadiness_FullMethodName,
	}
}
