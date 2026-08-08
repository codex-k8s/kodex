package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
)

const serviceName = "control-plane"

// Config задаёт только typed names/paths/endpoints; secret values читаются из файлов.
type Config struct {
	GRPCListen                          string        `env:"CONTROL_PLANE_GRPC_LISTEN"`
	TechnicalListen                     string        `env:"CONTROL_PLANE_TECHNICAL_LISTEN"`
	ServerCertificateFile               string        `env:"CONTROL_PLANE_TLS_CERTIFICATE_FILE"`
	ServerPrivateKeyFile                string        `env:"CONTROL_PLANE_TLS_PRIVATE_KEY_FILE"`
	ClientCAFile                        string        `env:"CONTROL_PLANE_TLS_CLIENT_CA_FILE"`
	PostgresDSNFile                     string        `env:"CONTROL_PLANE_POSTGRES_DSN_FILE"`
	PostgresRelayDSNFile                string        `env:"CONTROL_PLANE_POSTGRES_RELAY_DSN_FILE"`
	PostgresPrincipalName               string        `env:"CONTROL_PLANE_POSTGRES_PRINCIPAL_NAME"`
	PostgresPrincipalGeneration         uint64        `env:"CONTROL_PLANE_POSTGRES_PRINCIPAL_GENERATION"`
	PostgresContextKeyID                string        `env:"CONTROL_PLANE_POSTGRES_CONTEXT_KEY_ID"`
	PostgresContextKeyFile              string        `env:"CONTROL_PLANE_POSTGRES_CONTEXT_KEY_FILE"`
	PostgresTLSServerName               string        `env:"CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME"`
	PostgresCAFile                      string        `env:"CONTROL_PLANE_POSTGRES_CA_FILE"`
	PostgresMaxConnections              int32         `env:"CONTROL_PLANE_POSTGRES_MAX_CONNECTIONS"`
	RedisAddress                        string        `env:"CONTROL_PLANE_REDIS_ADDRESS"`
	RedisTLSServerName                  string        `env:"CONTROL_PLANE_REDIS_TLS_SERVER_NAME"`
	RedisCAFile                         string        `env:"CONTROL_PLANE_REDIS_CA_FILE"`
	RedisUsername                       string        `env:"CONTROL_PLANE_REDIS_USERNAME"`
	RedisPasswordFile                   string        `env:"CONTROL_PLANE_REDIS_PASSWORD_FILE"`
	RedisDatabase                       int           `env:"CONTROL_PLANE_REDIS_DATABASE"`
	RedisPoolSize                       int           `env:"CONTROL_PLANE_REDIS_POOL_SIZE"`
	InstructionS3Endpoint               string        `env:"CONTROL_PLANE_INSTRUCTION_S3_ENDPOINT"`
	InstructionS3TLSServerName          string        `env:"CONTROL_PLANE_INSTRUCTION_S3_TLS_SERVER_NAME"`
	InstructionS3CAFile                 string        `env:"CONTROL_PLANE_INSTRUCTION_S3_CA_FILE"`
	InstructionS3ClientCertificateFile  string        `env:"CONTROL_PLANE_INSTRUCTION_S3_CLIENT_CERTIFICATE_FILE"`
	InstructionS3ClientPrivateKeyFile   string        `env:"CONTROL_PLANE_INSTRUCTION_S3_CLIENT_PRIVATE_KEY_FILE"`
	InstructionS3AccessKeyFile          string        `env:"CONTROL_PLANE_INSTRUCTION_S3_ACCESS_KEY_FILE"`
	InstructionS3SecretKeyFile          string        `env:"CONTROL_PLANE_INSTRUCTION_S3_SECRET_KEY_FILE"`
	InstructionS3SessionTokenFile       string        `env:"CONTROL_PLANE_INSTRUCTION_S3_SESSION_TOKEN_FILE"`
	InstructionS3Bucket                 string        `env:"CONTROL_PLANE_INSTRUCTION_S3_BUCKET"`
	NATSURL                             string        `env:"CONTROL_PLANE_NATS_URL"`
	NATSTLSServerName                   string        `env:"CONTROL_PLANE_NATS_TLS_SERVER_NAME"`
	NATSCAFile                          string        `env:"CONTROL_PLANE_NATS_CA_FILE"`
	NATSCredentialsFile                 string        `env:"CONTROL_PLANE_NATS_CREDENTIALS_FILE"`
	NATSStream                          string        `env:"CONTROL_PLANE_NATS_STREAM"`
	NATSReplicas                        int           `env:"CONTROL_PLANE_NATS_REPLICAS"`
	AuthorityPolicyFile                 string        `env:"CONTROL_PLANE_AUTHORITY_POLICY_FILE"`
	ProofPrivateJWKFile                 string        `env:"CONTROL_PLANE_PROOF_PRIVATE_JWK_FILE"`
	ProofTrustFile                      string        `env:"CONTROL_PLANE_PROOF_TRUST_FILE"`
	ProofSignerGeneration               uint64        `env:"CONTROL_PLANE_PROOF_SIGNER_GENERATION"`
	ContinuationGrantPrivateJWKFile     string        `env:"CONTROL_PLANE_CONTINUATION_GRANT_PRIVATE_JWK_FILE"`
	ContinuationGrantSignerGeneration   uint64        `env:"CONTROL_PLANE_CONTINUATION_GRANT_SIGNER_GENERATION"`
	InteractionReadbackPrivateJWKFile   string        `env:"CONTROL_PLANE_INTERACTION_READBACK_PRIVATE_JWK_FILE"`
	InteractionReadbackPublicKeysetFile string        `env:"CONTROL_PLANE_INTERACTION_READBACK_PUBLIC_KEYSET_FILE"`
	InteractionReadbackSignerGeneration uint64        `env:"CONTROL_PLANE_INTERACTION_READBACK_SIGNER_GENERATION"`
	LeaseSigningKeyFile                 string        `env:"CONTROL_PLANE_LEASE_SIGNING_KEY_FILE"`
	RuntimeAdmissionSigningKeyFile      string        `env:"CONTROL_PLANE_RUNTIME_ADMISSION_SIGNING_KEY_FILE"`
	RuntimeArchiveSigningKeyFile        string        `env:"CONTROL_PLANE_RUNTIME_ARCHIVE_SIGNING_KEY_FILE"`
	RuntimeRestoreSigningKeyFile        string        `env:"CONTROL_PLANE_RUNTIME_RESTORE_SIGNING_KEY_FILE"`
	ImagePolicyRevision                 uint64        `env:"CONTROL_PLANE_IMAGE_POLICY_REVISION"`
	ImagePolicySHA256                   string        `env:"CONTROL_PLANE_IMAGE_POLICY_SHA256"`
	ImageBuildLeaseDuration             time.Duration `env:"CONTROL_PLANE_IMAGE_BUILD_LEASE_DURATION"`
	ImageAdmissionClaimTTL              time.Duration `env:"CONTROL_PLANE_IMAGE_ADMISSION_CLAIM_TTL"`
	ImagePromotionClaimTTL              time.Duration `env:"CONTROL_PLANE_IMAGE_PROMOTION_CLAIM_TTL"`
	ImageMaximumAttempts                uint32        `env:"CONTROL_PLANE_IMAGE_MAXIMUM_ATTEMPTS"`
	StagingImageRepository              string        `env:"CONTROL_PLANE_STAGING_IMAGE_REPOSITORY"`
	PromotedImageRepository             string        `env:"CONTROL_PLANE_PROMOTED_IMAGE_REPOSITORY"`
	RoleImageInputRepository            string        `env:"CONTROL_PLANE_ROLE_IMAGE_INPUT_REPOSITORY"`
	TrustedRoleBaseRepository           string        `env:"CONTROL_PLANE_TRUSTED_ROLE_BASE_REPOSITORY"`
	TrustedRoleBaseDigest               string        `env:"CONTROL_PLANE_TRUSTED_ROLE_BASE_DIGEST"`
	RoleRuntimeContractRevision         uint64        `env:"CONTROL_PLANE_ROLE_RUNTIME_CONTRACT_REVISION"`
	RoleRuntimeContractSHA256           string        `env:"CONTROL_PLANE_ROLE_RUNTIME_CONTRACT_SHA256"`
	PendingRescheduleDelay              time.Duration `env:"CONTROL_PLANE_PENDING_RESCHEDULE_DELAY"`
	RecoveryPollInterval                time.Duration `env:"CONTROL_PLANE_RECOVERY_POLL_INTERVAL"`
	OIDCTLSServerName                   string        `env:"CONTROL_PLANE_OIDC_TLS_SERVER_NAME"`
	OIDCCAFile                          string        `env:"CONTROL_PLANE_OIDC_CA_FILE"`
	ApplicationGrantTrustDir            string        `env:"CONTROL_PLANE_APPLICATION_GRANT_TRUST_DIR"`
	InstanceID                          string        `env:"POD_UID"`
	StartupTimeout                      time.Duration `env:"CONTROL_PLANE_STARTUP_TIMEOUT"`
	ReadinessTimeout                    time.Duration `env:"CONTROL_PLANE_READINESS_TIMEOUT"`
	ReadinessInterval                   time.Duration `env:"CONTROL_PLANE_READINESS_INTERVAL"`
	ShutdownTimeout                     time.Duration `env:"CONTROL_PLANE_SHUTDOWN_TIMEOUT"`
	CacheTTL                            time.Duration `env:"CONTROL_PLANE_CACHE_TTL"`
	CacheTimeout                        time.Duration `env:"CONTROL_PLANE_CACHE_TIMEOUT"`
	TurnLeaseDuration                   time.Duration `env:"CONTROL_PLANE_TURN_LEASE_DURATION"`
	ScheduleClaimLimit                  int           `env:"CONTROL_PLANE_SCHEDULE_CLAIM_LIMIT"`
	RelayPollInterval                   time.Duration `env:"CONTROL_PLANE_RELAY_POLL_INTERVAL"`
	RelayLeaseDuration                  time.Duration `env:"CONTROL_PLANE_RELAY_LEASE_DURATION"`
	RelayPublishTimeout                 time.Duration `env:"CONTROL_PLANE_RELAY_PUBLISH_TIMEOUT"`
	RelayFinalizeTimeout                time.Duration `env:"CONTROL_PLANE_RELAY_FINALIZE_TIMEOUT"`
}

func loadConfig() (Config, error) {
	config := Config{
		GRPCListen:                          ":8443",
		TechnicalListen:                     ":9090",
		ServerCertificateFile:               "/var/run/secrets/mattercodex/control-plane/workload-tls/tls.crt",
		ServerPrivateKeyFile:                "/var/run/secrets/mattercodex/control-plane/workload-tls/tls.key",
		ClientCAFile:                        "/var/run/config/mattercodex/control-plane/internal-ca/ca.pem",
		PostgresDSNFile:                     "/var/run/secrets/mattercodex/control-plane/postgres-runtime/dsn",
		PostgresRelayDSNFile:                "/var/run/secrets/mattercodex/control-plane/postgres-relay/dsn",
		PostgresPrincipalName:               "control_plane_runtime_g1",
		PostgresPrincipalGeneration:         1,
		PostgresContextKeyID:                "control-plane-db-context-g1",
		PostgresContextKeyFile:              "/var/run/secrets/mattercodex/control-plane/postgres-context/key",
		PostgresTLSServerName:               "control-plane-postgresql.mattercodex-system.svc.cluster.local",
		PostgresCAFile:                      "/var/run/config/mattercodex/control-plane/postgres/ca.pem",
		PostgresMaxConnections:              16,
		RedisAddress:                        "control-plane-redis.mattercodex-system.svc:6379",
		RedisTLSServerName:                  "control-plane-redis.mattercodex-system.svc.cluster.local",
		RedisCAFile:                         "/var/run/config/mattercodex/control-plane/redis/ca.pem",
		RedisUsername:                       "control-plane",
		RedisPasswordFile:                   "/var/run/secrets/mattercodex/control-plane/redis/password",
		RedisDatabase:                       0,
		RedisPoolSize:                       16,
		InstructionS3Endpoint:               "https://object-store.storage.svc.cluster.local",
		InstructionS3TLSServerName:          "object-store.storage.svc.cluster.local",
		InstructionS3CAFile:                 "/var/run/config/mattercodex/control-plane/object-store/ca.pem",
		InstructionS3ClientCertificateFile:  "/var/run/secrets/mattercodex/control-plane/workload-tls/tls.crt",
		InstructionS3ClientPrivateKeyFile:   "/var/run/secrets/mattercodex/control-plane/workload-tls/tls.key",
		InstructionS3AccessKeyFile:          "/var/run/secrets/mattercodex/control-plane/instruction-object-store/access-key",
		InstructionS3SecretKeyFile:          "/var/run/secrets/mattercodex/control-plane/instruction-object-store/secret-key",
		InstructionS3Bucket:                 "mattercodex-instruction-artifacts",
		NATSURL:                             "tls://nats.mattercodex-system.svc:4222",
		NATSTLSServerName:                   "nats.mattercodex-system.svc.cluster.local",
		NATSCAFile:                          "/var/run/config/mattercodex/control-plane/nats/ca.pem",
		NATSCredentialsFile:                 "/var/run/secrets/mattercodex/control-plane/nats/user.creds",
		NATSStream:                          "CONTROL_PLANE",
		NATSReplicas:                        3,
		AuthorityPolicyFile:                 "/var/run/config/mattercodex/control-plane/authority/policy.json",
		ProofPrivateJWKFile:                 "/var/run/secrets/mattercodex/internal-rpc-authority/proof-signer/private.jwk",
		ProofTrustFile:                      "/var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust/jwks.json",
		ProofSignerGeneration:               1,
		ContinuationGrantPrivateJWKFile:     "/var/run/secrets/mattercodex/control-plane/continuation-grant/private.jwk",
		ContinuationGrantSignerGeneration:   1,
		InteractionReadbackPrivateJWKFile:   "/var/run/secrets/mattercodex/control-plane/interaction-readback/private.jwk",
		InteractionReadbackPublicKeysetFile: "/var/run/config/mattercodex/control-plane/interaction-readback/public-keyset.json",
		InteractionReadbackSignerGeneration: 1,
		LeaseSigningKeyFile:                 "/var/run/secrets/mattercodex/control-plane/lease-signing/key",
		RuntimeAdmissionSigningKeyFile:      "/var/run/secrets/mattercodex/control-plane/runtime-workload-signing/admission-private-key.hex",
		RuntimeArchiveSigningKeyFile:        "/var/run/secrets/mattercodex/control-plane/runtime-workload-signing/archive-private-key.hex",
		RuntimeRestoreSigningKeyFile:        "/var/run/secrets/mattercodex/control-plane/runtime-workload-signing/restore-private-key.hex",
		ImageBuildLeaseDuration:             5 * time.Minute,
		ImageAdmissionClaimTTL:              30 * time.Minute,
		ImagePromotionClaimTTL:              15 * time.Minute,
		ImageMaximumAttempts:                3,
		StagingImageRepository:              "mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001/staging/role-images",
		PromotedImageRepository:             "registry-pull.invalid/mattercodex/roles",
		RoleImageInputRepository:            "mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/role-image-inputs",
		TrustedRoleBaseRepository:           "mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/agent-runner",
		TrustedRoleBaseDigest:               "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		RoleRuntimeContractRevision:         1,
		RoleRuntimeContractSHA256:           "0000000000000000000000000000000000000000000000000000000000000000",
		PendingRescheduleDelay:              30 * time.Second,
		RecoveryPollInterval:                time.Second,
		OIDCTLSServerName:                   "sso.mattercodex.local",
		OIDCCAFile:                          "/var/run/config/mattercodex/control-plane/oidc/ca.pem",
		ApplicationGrantTrustDir:            "/var/run/config/mattercodex/control-plane/application-grants",
		StartupTimeout:                      15 * time.Second,
		ReadinessTimeout:                    2 * time.Second,
		ReadinessInterval:                   10 * time.Second,
		ShutdownTimeout:                     10 * time.Second,
		CacheTTL:                            30 * time.Second,
		CacheTimeout:                        100 * time.Millisecond,
		TurnLeaseDuration:                   30 * time.Second,
		ScheduleClaimLimit:                  64,
		RelayPollInterval:                   250 * time.Millisecond,
		RelayLeaseDuration:                  10 * time.Second,
		RelayPublishTimeout:                 2 * time.Second,
		RelayFinalizeTimeout:                2 * time.Second,
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
	instructionEndpoint, instructionEndpointErr := url.Parse(config.InstructionS3Endpoint)
	if instructionEndpointErr != nil || instructionEndpoint.Scheme != "https" ||
		instructionEndpoint.Hostname() != config.InstructionS3TLSServerName || instructionEndpoint.Path != "" ||
		instructionEndpoint.User != nil || instructionEndpoint.RawQuery != "" || instructionEndpoint.Fragment != "" ||
		config.InstructionS3Bucket != "mattercodex-instruction-artifacts" {
		return errors.New("control-plane instruction object store endpoint is invalid")
	}
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
		config.ProofSignerGeneration == 0 || config.ContinuationGrantSignerGeneration == 0 || config.InteractionReadbackSignerGeneration == 0 ||
		config.PostgresPrincipalName == "" ||
		config.PostgresPrincipalGeneration == 0 ||
		config.PostgresContextKeyID == "" ||
		config.ImagePolicyRevision == 0 || !validSHA256(config.ImagePolicySHA256) ||
		config.ImageBuildLeaseDuration < 30*time.Second || config.ImageBuildLeaseDuration > 30*time.Minute ||
		config.ImageAdmissionClaimTTL < 30*time.Second || config.ImageAdmissionClaimTTL > 30*time.Minute ||
		config.ImagePromotionClaimTTL < 30*time.Second || config.ImagePromotionClaimTTL > 15*time.Minute ||
		config.ImageMaximumAttempts == 0 || config.ImageMaximumAttempts > 10 ||
		!validRepository(config.StagingImageRepository) || !validRepository(config.PromotedImageRepository) ||
		!validRepository(config.RoleImageInputRepository) ||
		!validRepository(config.TrustedRoleBaseRepository) || !validManifestDigest(config.TrustedRoleBaseDigest) ||
		config.RoleRuntimeContractRevision == 0 || !validSHA256(config.RoleRuntimeContractSHA256) ||
		config.StagingImageRepository == config.PromotedImageRepository ||
		config.InstanceID == "" || len(config.InstanceID) > 128 ||
		config.ScheduleClaimLimit < 1 || config.ScheduleClaimLimit > 128 {
		return errors.New("control-plane bounded configuration is invalid")
	}
	for _, serverName := range []string{
		config.PostgresTLSServerName,
		config.RedisTLSServerName,
		config.NATSTLSServerName,
		config.OIDCTLSServerName,
		config.InstructionS3TLSServerName,
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
		config.InteractionReadbackPrivateJWKFile,
		config.InteractionReadbackPublicKeysetFile,
		config.LeaseSigningKeyFile,
		config.RuntimeAdmissionSigningKeyFile,
		config.RuntimeArchiveSigningKeyFile,
		config.RuntimeRestoreSigningKeyFile,
		config.OIDCCAFile,
		config.ApplicationGrantTrustDir,
		config.InstructionS3CAFile,
		config.InstructionS3ClientCertificateFile,
		config.InstructionS3ClientPrivateKeyFile,
		config.InstructionS3AccessKeyFile,
		config.InstructionS3SecretKeyFile,
	} {
		if !filepath.IsAbs(path) {
			return errors.New("control-plane runtime path must be absolute")
		}
	}
	if config.InstructionS3SessionTokenFile != "" && !filepath.IsAbs(config.InstructionS3SessionTokenFile) {
		return errors.New("control-plane instruction object store session credential path must be absolute")
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
		config.RecoveryPollInterval < 100*time.Millisecond || config.RecoveryPollInterval > time.Minute ||
		config.PendingRescheduleDelay < 5*time.Second || config.PendingRescheduleDelay > 5*time.Minute {
		return errors.New("control-plane duration is invalid")
	}
	return nil
}

func validSHA256(input string) bool {
	const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"
	if len(input) != 64 || input == zeroDigest {
		return false
	}
	for _, symbol := range input {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

func validRepository(input string) bool {
	return len(input) >= 8 && len(input) <= 255 && input == strings.TrimSpace(input) &&
		!strings.ContainsAny(input, "@?# *") && strings.Contains(input, "/")
}

func validManifestDigest(input string) bool {
	return strings.HasPrefix(input, "sha256:") && validSHA256(strings.TrimPrefix(input, "sha256:"))
}

func expectedOperations() map[string]string {
	return map[string]string{
		"control.project.create":                                 controlplanev1.ControlPlaneService_CreateProject_FullMethodName,
		"control.project.list":                                   controlplanev1.ControlPlaneService_ListProjects_FullMethodName,
		"control.project.update":                                 controlplanev1.ControlPlaneService_UpdateProject_FullMethodName,
		"control.project.delete":                                 controlplanev1.ControlPlaneService_DeleteProject_FullMethodName,
		"control.resource.create":                                controlplanev1.ControlPlaneService_CreateResource_FullMethodName,
		"control.resource.update":                                controlplanev1.ControlPlaneService_UpdateResource_FullMethodName,
		"control.resource.transition":                            controlplanev1.ControlPlaneService_TransitionResource_FullMethodName,
		"control.resource.delete":                                controlplanev1.ControlPlaneService_DeleteResource_FullMethodName,
		"control.access.manage":                                  controlplanev1.ControlPlaneService_ManageAccessResource_FullMethodName,
		"control.access.detach":                                  controlplanev1.ControlPlaneService_DetachAccessResource_FullMethodName,
		"control.access.copy":                                    controlplanev1.ControlPlaneService_CopyAccessResource_FullMethodName,
		"control.resource.get":                                   controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.resource.list":                                  controlplanev1.ControlPlaneService_ListResources_FullMethodName,
		"control.resource.search":                                controlplanev1.ControlPlaneService_SearchResources_FullMethodName,
		"control.audit.list":                                     controlplanev1.ControlPlaneService_ListAuditEvents_FullMethodName,
		"control.diagnostics.get":                                controlplanev1.ControlPlaneService_GetDiagnostics_FullMethodName,
		"control.runtime-incident.list":                          controlplanev1.ControlPlaneService_ListRuntimeIncidents_FullMethodName,
		"control.readiness.check":                                controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.owner-session.admit":                            controlplanev1.ControlPlaneService_AdmitOwnerSession_FullMethodName,
		"control.owner-session.revoke":                           controlplanev1.ControlPlaneService_RevokeOwnerSession_FullMethodName,
		"control.gateway-public-tls.prepare":                     controlplanev1.ControlPlaneService_PrepareGatewayPublicTLS_FullMethodName,
		"control.gateway-public-tls.confirm":                     controlplanev1.ControlPlaneService_ConfirmGatewayPublicTLS_FullMethodName,
		"control.gateway-public-tls.check":                       controlplanev1.ControlPlaneService_CheckGatewayPublicTLS_FullMethodName,
		"control.agent-runner.readiness":                         controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.turn.claim":                                     controlplanev1.ControlPlaneService_ClaimTurn_FullMethodName,
		"control.runtime-execution.progress":                     controlplanev1.ControlPlaneService_ReportRuntimeProgress_FullMethodName,
		"control.runtime-materialization.get":                    controlplanev1.ControlPlaneService_GetRuntimeMaterialization_FullMethodName,
		"control.runtime-output.authorize":                       controlplanev1.ControlPlaneService_AuthorizeRuntimeOutput_FullMethodName,
		"control.runtime-output.register":                        controlplanev1.ControlPlaneService_RegisterRuntimeOutput_FullMethodName,
		"control.agent-runtime-execution.get":                    controlplanev1.ControlPlaneService_GetRuntimeExecution_FullMethodName,
		"control.schedule.claim-due":                             controlplanev1.ControlPlaneService_ClaimDueSchedules_FullMethodName,
		"control.schedule.claim-occurrence":                      controlplanev1.ControlPlaneService_ClaimScheduleOccurrence_FullMethodName,
		"control.schedule.complete-occurrence":                   controlplanev1.ControlPlaneService_CompleteScheduleOccurrence_FullMethodName,
		"control.schedule.materialize-occurrence":                controlplanev1.ControlPlaneService_MaterializeScheduleOccurrence_FullMethodName,
		"control.automation-scheduler.readiness":                 controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.schedule.manage":                                controlplanev1.ControlPlaneService_ManageSchedule_FullMethodName,
		"control.schedule.run-now":                               controlplanev1.ControlPlaneService_RunScheduleNow_FullMethodName,
		"control.schedule.occurrences.list":                      controlplanev1.ControlPlaneService_ListScheduleOccurrences_FullMethodName,
		"control.schedule.recovery.resolve":                      controlplanev1.ControlPlaneService_ResolveScheduleRecovery_FullMethodName,
		"control.owner-gate.resolve":                             controlplanev1.ControlPlaneService_ResolveOwnerGate_FullMethodName,
		"control.backup.list":                                    controlplanev1.ControlPlaneService_ListBackups_FullMethodName,
		"control.backup.get":                                     controlplanev1.ControlPlaneService_GetBackup_FullMethodName,
		"control.backup.restore":                                 controlplanev1.ControlPlaneService_RestoreBackup_FullMethodName,
		"control.restore-operation.get":                          controlplanev1.ControlPlaneService_GetRestoreOperation_FullMethodName,
		"control.restore-operation.list":                         controlplanev1.ControlPlaneService_ListRestoreOperations_FullMethodName,
		"control.role-definition.manage":                         controlplanev1.ControlPlaneService_ManageRoleDefinition_FullMethodName,
		"control.role-definition.git.reconcile":                  controlplanev1.ControlPlaneService_ReconcileGitRoleDefinition_FullMethodName,
		"control.role-definition.get":                            controlplanev1.ControlPlaneService_GetRoleDefinition_FullMethodName,
		"control.role-definition.list":                           controlplanev1.ControlPlaneService_ListRoleDefinitions_FullMethodName,
		"control.role-definition.history":                        controlplanev1.ControlPlaneService_ListRoleDefinitionHistory_FullMethodName,
		"control.agent.manage":                                   controlplanev1.ControlPlaneService_ManageAgent_FullMethodName,
		"control.agent.git.reconcile":                            controlplanev1.ControlPlaneService_ReconcileGitAgent_FullMethodName,
		"control.interaction.agent-bot.manage":                   controlplanev1.ControlPlaneService_ManageAgentMattermostBotIdentity_FullMethodName,
		"control.agent.get":                                      controlplanev1.ControlPlaneService_GetAgent_FullMethodName,
		"control.agent.list":                                     controlplanev1.ControlPlaneService_ListAgents_FullMethodName,
		"control.agent.history":                                  controlplanev1.ControlPlaneService_ListAgentHistory_FullMethodName,
		"control.agent-assignment.manage":                        controlplanev1.ControlPlaneService_ManageAgentAssignment_FullMethodName,
		"control.agent-assignment.get":                           controlplanev1.ControlPlaneService_GetAgentAssignment_FullMethodName,
		"control.agent-assignment.list":                          controlplanev1.ControlPlaneService_ListAgentAssignments_FullMethodName,
		"control.agent-assignment.history":                       controlplanev1.ControlPlaneService_ListAgentAssignmentHistory_FullMethodName,
		"control.instruction-set.manage":                         controlplanev1.ControlPlaneService_ManageInstructionSet_FullMethodName,
		"control.instruction-set.git.reconcile":                  controlplanev1.ControlPlaneService_ReconcileGitInstructionSet_FullMethodName,
		"control.instruction-set.get":                            controlplanev1.ControlPlaneService_GetInstructionSet_FullMethodName,
		"control.instruction-set.list":                           controlplanev1.ControlPlaneService_ListInstructionSets_FullMethodName,
		"control.instruction-set.history":                        controlplanev1.ControlPlaneService_ListInstructionSetHistory_FullMethodName,
		"control.instruction-set.compare":                        controlplanev1.ControlPlaneService_CompareInstructionSetVersions_FullMethodName,
		"control.provider-reference.get":                         controlplanev1.ControlPlaneService_GetProviderConnectionReference_FullMethodName,
		"control.provider-reference.list":                        controlplanev1.ControlPlaneService_ListProviderConnectionReferences_FullMethodName,
		"control.provider-reference.history":                     controlplanev1.ControlPlaneService_ListProviderConnectionReferenceHistory_FullMethodName,
		"control.provider-pool.manage":                           controlplanev1.ControlPlaneService_ManageProviderPool_FullMethodName,
		"control.provider-pool.git.reconcile":                    controlplanev1.ControlPlaneService_ReconcileGitProviderPool_FullMethodName,
		"control.provider-pool.get":                              controlplanev1.ControlPlaneService_GetProviderPool_FullMethodName,
		"control.provider-pool.list":                             controlplanev1.ControlPlaneService_ListProviderPools_FullMethodName,
		"control.provider-pool.history":                          controlplanev1.ControlPlaneService_ListProviderPoolHistory_FullMethodName,
		"control.schedule.bind":                                  controlplanev1.ControlPlaneService_BindScheduleConfiguration_FullMethodName,
		"control.schedule.create-from-selections":                controlplanev1.ControlPlaneService_CreateScheduleFromOwnerSelections_FullMethodName,
		"control.run.manage":                                     controlplanev1.ControlPlaneService_ManageRun_FullMethodName,
		"control.run.get":                                        controlplanev1.ControlPlaneService_GetRunDetail_FullMethodName,
		"control.run.timeline":                                   controlplanev1.ControlPlaneService_ListRunTimeline_FullMethodName,
		"control.run.lineage":                                    controlplanev1.ControlPlaneService_GetRunLineage_FullMethodName,
		"control.run.artifacts.list":                             controlplanev1.ControlPlaneService_ListRunArtifacts_FullMethodName,
		"control.workspace-backup.manage":                        controlplanev1.ControlPlaneService_ManageWorkspaceBackup_FullMethodName,
		"control.workspace-backup.get":                           controlplanev1.ControlPlaneService_GetWorkspaceBackup_FullMethodName,
		"control.workspace-backup.list":                          controlplanev1.ControlPlaneService_ListWorkspaceBackups_FullMethodName,
		"control.workspace-restore.manage":                       controlplanev1.ControlPlaneService_ManageWorkspaceRestore_FullMethodName,
		"control.workspace-restore.get":                          controlplanev1.ControlPlaneService_GetWorkspaceRestore_FullMethodName,
		"control.workspace-restore.list":                         controlplanev1.ControlPlaneService_ListWorkspaceRestores_FullMethodName,
		"control.runtime-incident.manage":                        controlplanev1.ControlPlaneService_ManageRuntimeIncident_FullMethodName,
		"control.runtime-incident.get":                           controlplanev1.ControlPlaneService_GetRuntimeIncident_FullMethodName,
		"control.runtime-incident.history":                       controlplanev1.ControlPlaneService_ListRuntimeIncidentHistory_FullMethodName,
		"control.workspace-mapping.get":                          controlplanev1.ControlPlaneService_GetWorkspaceMattermostMapping_FullMethodName,
		"control.workspace-mapping.list":                         controlplanev1.ControlPlaneService_ListWorkspaceMattermostMappings_FullMethodName,
		"control.legacy-cutover.get":                             controlplanev1.ControlPlaneService_GetLegacyConfigurationCutover_FullMethodName,
		"control.legacy-cutover.list":                            controlplanev1.ControlPlaneService_ListLegacyConfigurationCutovers_FullMethodName,
		"control.legacy-cutover.resolve":                         controlplanev1.ControlPlaneService_ResolveLegacyConfigurationCutover_FullMethodName,
		"control.legacy-graph-migration.prepare":                 controlplanev1.ControlPlaneService_PrepareLegacyGraphMigration_FullMethodName,
		"control.legacy-graph-migration.materialize":             controlplanev1.ControlPlaneService_MaterializeLegacyGraphMigration_FullMethodName,
		"control.legacy-graph-migration.read":                    controlplanev1.ControlPlaneService_GetLegacyGraphMigration_FullMethodName,
		"control.legacy-graph-migration.abort":                   controlplanev1.ControlPlaneService_AbortLegacyGraphMigration_FullMethodName,
		"control.legacy-data-migration.readiness":                controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"interaction.team.catalog.read":                          interactiongatewayv1.MattermostTeamService_ListMattermostTeams_FullMethodName,
		"interaction.team.create":                                interactiongatewayv1.MattermostTeamService_CreateMattermostTeam_FullMethodName,
		"interaction.team.link":                                  interactiongatewayv1.MattermostTeamService_LinkMattermostTeam_FullMethodName,
		"interaction.team.binding.get":                           interactiongatewayv1.MattermostTeamService_GetMattermostTeamBinding_FullMethodName,
		"interaction.team.mapping-operation.get":                 interactiongatewayv1.MattermostTeamService_GetMattermostTeamMappingOperation_FullMethodName,
		"interaction.team.relink":                                interactiongatewayv1.MattermostTeamService_RelinkMattermostTeam_FullMethodName,
		"interaction.team.unlink":                                interactiongatewayv1.MattermostTeamService_UnlinkMattermostTeam_FullMethodName,
		"interaction.team.provider.readback":                     interactiongatewayv1.MattermostTeamService_GetMattermostTeamProviderReadback_FullMethodName,
		"interaction.team.readiness":                             interactiongatewayv1.MattermostTeamService_CheckReadiness_FullMethodName,
		"integration.management.provider.catalog.list":           integrationgatewayv1.IntegrationManagementService_ListProviders_FullMethodName,
		"integration.management.provider.catalog.get":            integrationgatewayv1.IntegrationManagementService_GetProvider_FullMethodName,
		"integration.management.provider.authorization.start":    integrationgatewayv1.IntegrationManagementService_StartProviderAuthorization_FullMethodName,
		"integration.management.provider.authorization.get":      integrationgatewayv1.IntegrationManagementService_GetProviderAuthorization_FullMethodName,
		"integration.management.provider.authorization.restart":  integrationgatewayv1.IntegrationManagementService_RestartProviderAuthorization_FullMethodName,
		"integration.management.provider.authorization.cancel":   integrationgatewayv1.IntegrationManagementService_CancelProviderAuthorization_FullMethodName,
		"integration.management.provider.connection.list":        integrationgatewayv1.IntegrationManagementService_ListProviderConnections_FullMethodName,
		"integration.management.provider.connection.get":         integrationgatewayv1.IntegrationManagementService_GetProviderConnection_FullMethodName,
		"integration.management.provider.connection.reauthorize": integrationgatewayv1.IntegrationManagementService_ReauthorizeProviderConnection_FullMethodName,
		"integration.management.provider.connection.revoke":      integrationgatewayv1.IntegrationManagementService_RevokeProviderConnection_FullMethodName,
		"integration.management.provider.pool.manage":            integrationgatewayv1.IntegrationManagementService_ManageProviderPool_FullMethodName,
		"integration.management.provider.pool.get":               integrationgatewayv1.IntegrationManagementService_GetProviderPool_FullMethodName,
		"integration.management.provider.pool.list":              integrationgatewayv1.IntegrationManagementService_ListProviderPools_FullMethodName,
		"integration.management.definition.list":                 integrationgatewayv1.IntegrationManagementService_ListIntegrationDefinitions_FullMethodName,
		"integration.management.definition.get":                  integrationgatewayv1.IntegrationManagementService_GetIntegrationDefinition_FullMethodName,
		"integration.management.configuration.manage":            integrationgatewayv1.IntegrationManagementService_ConfigureIntegration_FullMethodName,
		"integration.management.configuration.get":               integrationgatewayv1.IntegrationManagementService_GetIntegrationConfiguration_FullMethodName,
		"integration.management.configuration.list":              integrationgatewayv1.IntegrationManagementService_ListIntegrationConfigurations_FullMethodName,
		"integration.management.test.start":                      integrationgatewayv1.IntegrationManagementService_TestIntegrationConnection_FullMethodName,
		"integration.management.test.get":                        integrationgatewayv1.IntegrationManagementService_GetIntegrationTestReceipt_FullMethodName,
		"integration.management.approval.list":                   integrationgatewayv1.IntegrationManagementService_ListIntegrationApprovals_FullMethodName,
		"integration.management.approval.get":                    integrationgatewayv1.IntegrationManagementService_GetIntegrationApproval_FullMethodName,
		"integration.management.approval.decide":                 integrationgatewayv1.IntegrationManagementService_DecideIntegrationApproval_FullMethodName,
		"integration.management.git.binding.manage":              integrationgatewayv1.IntegrationManagementService_ManageGitSourceBinding_FullMethodName,
		"integration.management.git.binding.get":                 integrationgatewayv1.IntegrationManagementService_GetGitSourceBinding_FullMethodName,
		"integration.management.git.binding.list":                integrationgatewayv1.IntegrationManagementService_ListGitSourceBindings_FullMethodName,
		"integration.management.git.reconcile":                   integrationgatewayv1.IntegrationManagementService_ReconcileGitSourceBinding_FullMethodName,
		"integration.management.diagnostics.get":                 integrationgatewayv1.IntegrationManagementService_GetManagementDiagnostics_FullMethodName,
		"control.integration.provider-reference.manage":          controlplanev1.ControlPlaneService_ManageProviderConnectionReference_FullMethodName,
		"control.integration.provider-reference.get":             controlplanev1.ControlPlaneService_GetProviderConnectionReference_FullMethodName,
		"control.integration.provider-reference.list":            controlplanev1.ControlPlaneService_ListProviderConnectionReferences_FullMethodName,
		"control.integration.provider-reference.readback.get":    controlplanev1.ControlPlaneService_GetProviderConnectionReference_FullMethodName,
		"control.integration.provider-reference.readback.list":   controlplanev1.ControlPlaneService_ListProviderConnectionReferences_FullMethodName,
		"control.integration.provider-pool.manage":               controlplanev1.ControlPlaneService_ManageProviderPool_FullMethodName,
		"control.interaction.workspace-mapping.manage":           controlplanev1.ControlPlaneService_ManageWorkspaceMattermostMapping_FullMethodName,
		"control.interaction.workspace-mapping.get":              controlplanev1.ControlPlaneService_GetWorkspaceMattermostMapping_FullMethodName,
		"control.interaction.workspace-mapping.list":             controlplanev1.ControlPlaneService_ListWorkspaceMattermostMappings_FullMethodName,
		"control.artifact.scan":                                  controlplanev1.ControlPlaneService_RecordArtifactScan_FullMethodName,
		"control.artifact-scanner.readiness":                     controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.role-image-recipe.manage":                       controlplanev1.ControlPlaneService_ManageRoleImageRecipe_FullMethodName,
		"control.role-image-recipe.get":                          controlplanev1.ControlPlaneService_GetRoleImageRecipe_FullMethodName,
		"control.image-build.manage":                             controlplanev1.ControlPlaneService_ManageImageBuild_FullMethodName,
		"control.image-build.get":                                controlplanev1.ControlPlaneService_GetRoleImageBuild_FullMethodName,
		"control.role-image-builder.readiness":                   controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.image-build.claim":                              controlplanev1.ControlPlaneService_ClaimImageBuild_FullMethodName,
		"control.image-build.renew":                              controlplanev1.ControlPlaneService_RenewImageBuild_FullMethodName,
		"control.image-build.progress":                           controlplanev1.ControlPlaneService_ReportImageBuildProgress_FullMethodName,
		"control.image-build.complete":                           controlplanev1.ControlPlaneService_CompleteImageBuild_FullMethodName,
		"control.image-build.fail":                               controlplanev1.ControlPlaneService_FailImageBuild_FullMethodName,
		"control.image-admission.readiness":                      controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.image-admission.claim":                          controlplanev1.ControlPlaneService_ClaimImageAdmission_FullMethodName,
		"control.image-admission.record":                         controlplanev1.ControlPlaneService_RecordImageAdmission_FullMethodName,
		"control.image-promotion.readiness":                      controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.image-promotion.claim":                          controlplanev1.ControlPlaneService_ClaimImagePromotion_FullMethodName,
		"control.image-promotion.authorize":                      controlplanev1.ControlPlaneService_AuthorizeImagePromotion_FullMethodName,
		"control.image-promotion.complete":                       controlplanev1.ControlPlaneService_CompleteImagePromotion_FullMethodName,
		"control.owner-gate.deliver":                             controlplanev1.ControlPlaneService_RecordOwnerGateDelivery_FullMethodName,
		"control.owner-gate.claim-delivery":                      controlplanev1.ControlPlaneService_ClaimOwnerGateDelivery_FullMethodName,
		"control.owner-gate.expire":                              controlplanev1.ControlPlaneService_ExpireOwnerGate_FullMethodName,
		"control.owner-gate-delivery.readiness":                  controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.interaction.session.create":                     controlplanev1.ControlPlaneService_ManageSession_FullMethodName,
		"control.interaction.session.mcp.bind":                   controlplanev1.ControlPlaneService_BindSessionMCP_FullMethodName,
		"control.interaction.turn.enqueue":                       controlplanev1.ControlPlaneService_EnqueueTurn_FullMethodName,
		"control.interaction.artifact.register":                  controlplanev1.ControlPlaneService_RegisterArtifact_FullMethodName,
		"control.interaction.resource.read":                      controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.interaction.conversation.lifecycle":             controlplanev1.ControlPlaneService_ManageConversationLifecycle_FullMethodName,
		"control.interaction.owner-gate.resolve":                 controlplanev1.ControlPlaneService_ResolveOwnerGate_FullMethodName,
		"control.interaction.runtime-action.manage":              controlplanev1.ControlPlaneService_ManageRuntimeAction_FullMethodName,
		"control.interaction.delivery.claim":                     controlplanev1.ControlPlaneService_ClaimInteractionDelivery_FullMethodName,
		"control.interaction.delivery.record":                    controlplanev1.ControlPlaneService_RecordInteractionDelivery_FullMethodName,
		"control.interaction.delivery.readback.issue":            controlplanev1.ControlPlaneService_IssueInteractionDeliveryReadbackGrant_FullMethodName,
		"control.interaction.delivery.readback.validate":         controlplanev1.ControlPlaneService_ValidateInteractionDeliveryReadbackGrant_FullMethodName,
		"control.runtime-revision.get":                           controlplanev1.ControlPlaneService_GetRuntimeRevision_FullMethodName,
		"control.runtime-resource.get":                           controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.runtime-controller.readiness":                   controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-archive.readiness":                      controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-execution.archive.record":               controlplanev1.ControlPlaneService_RecordRuntimeArchive_FullMethodName,
		"control.memory.index":                                   controlplanev1.ControlPlaneService_RecordMemoryEmbedding_FullMethodName,
		"control.memory-indexer.readiness":                       controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-execution.claim":                        controlplanev1.ControlPlaneService_ClaimRuntimeExecution_FullMethodName,
		"control.runtime-execution.get":                          controlplanev1.ControlPlaneService_GetRuntimeExecution_FullMethodName,
		"control.runtime-execution.admit":                        controlplanev1.ControlPlaneService_AdmitRuntimeExecution_FullMethodName,
		"control.runtime-execution.restore.materialize":          controlplanev1.ControlPlaneService_AuthorizeRuntimeRestoreEffect_FullMethodName,
		"control.runtime-execution.restore.credential":           controlplanev1.ControlPlaneService_AuthorizeRuntimeRestoreEffect_FullMethodName,
		"control.runtime-execution.heartbeat":                    controlplanev1.ControlPlaneService_HeartbeatRuntimeExecution_FullMethodName,
		"control.runtime-execution.incident":                     controlplanev1.ControlPlaneService_RecordRuntimeIncident_FullMethodName,
		"control.runtime-execution.complete":                     controlplanev1.ControlPlaneService_CompleteRuntimeExecution_FullMethodName,
		"control.runtime-execution.expire":                       controlplanev1.ControlPlaneService_ExpireRuntimeExecution_FullMethodName,
		"control.runtime-execution.restore.verify":               controlplanev1.ControlPlaneService_VerifyRuntimeRestore_FullMethodName,
		"control.runtime-execution.restore.bind":                 controlplanev1.ControlPlaneService_BindRuntimeRestoreTarget_FullMethodName,
		"control.runtime-execution.rehydrate.complete":           controlplanev1.ControlPlaneService_CompleteRuntimeRehydrate_FullMethodName,
		"control.runtime-execution.cleanup.authorize":            controlplanev1.ControlPlaneService_AuthorizeRuntimeCleanup_FullMethodName,
		"control.runtime-execution.cleanup.consume":              controlplanev1.ControlPlaneService_ConsumeRuntimeCleanupAuthorization_FullMethodName,
		"control.runtime-execution.cleanup.expire":               controlplanev1.ControlPlaneService_ExpireRuntimeCleanupAuthorization_FullMethodName,
		"control.runtime-restore-verifier.readiness":             controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-restore-effect.readiness":               controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-cleanup-authorizer.readiness":           controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.integration-gateway.readiness":                  controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.integration-session.resolve":                    controlplanev1.ControlPlaneService_ResolveIntegrationSession_FullMethodName,
		"control.integration-continuation.suspend":               controlplanev1.ControlPlaneService_SuspendForIntegrationApproval_FullMethodName,
		"control.integration-invocation.approve":                 controlplanev1.ControlPlaneService_ApproveIntegrationInvocation_FullMethodName,
		"control.integration-invocation.reject":                  controlplanev1.ControlPlaneService_RejectIntegrationInvocation_FullMethodName,
		"control.integration-invocation.expire":                  controlplanev1.ControlPlaneService_ExpireIntegrationInvocation_FullMethodName,
		"control.integration-invocation.cancel":                  controlplanev1.ControlPlaneService_CancelIntegrationInvocation_FullMethodName,
		"control.integration-execution.begin":                    controlplanev1.ControlPlaneService_BeginIntegrationExecution_FullMethodName,
		"control.integration-execution.complete":                 controlplanev1.ControlPlaneService_CompleteIntegrationExecution_FullMethodName,
		"control.integration-execution.fail":                     controlplanev1.ControlPlaneService_FailIntegrationExecution_FullMethodName,
	}
}
