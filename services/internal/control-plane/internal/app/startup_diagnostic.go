package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/postgresdiagnostic"
)

// startupStage names inventoried fixed service phases; values never originate in connection metadata.
// startupStage обозначает учтённые фиксированные этапы сервиса; значения не происходят из метаданных соединения.
type startupStage string

const (
	// stageReadObjectStorageAccessKey preserves the fixed phase "read object storage access key" while keeping nested dependency details private.
	// stageReadObjectStorageAccessKey сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageReadObjectStorageAccessKey startupStage = "read object storage access key"
	// stageReadObjectStorageSecretKey preserves the fixed phase "read object storage secret key" while keeping nested dependency details private.
	// stageReadObjectStorageSecretKey сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageReadObjectStorageSecretKey startupStage = "read object storage secret key"
	// stageConstructObjectStorage preserves the fixed phase "construct object storage" while keeping nested dependency details private.
	// stageConstructObjectStorage сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructObjectStorage startupStage = "construct object storage"
	// stageConstructSessionArchiveStorage preserves the fixed phase "construct session archive storage" while keeping nested dependency details private.
	// stageConstructSessionArchiveStorage сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructSessionArchiveStorage startupStage = "construct session archive storage"
	// stageConstructPlatformRepository preserves the fixed phase "construct platform repository" while keeping nested dependency details private.
	// stageConstructPlatformRepository сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructPlatformRepository startupStage = "construct platform repository"
	// stageConfigureProjectPurgeStorage preserves the fixed phase "configure project purge storage" while keeping nested dependency details private.
	// stageConfigureProjectPurgeStorage сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConfigureProjectPurgeStorage startupStage = "configure project purge storage"
	// stageConfigureRepositoryRPCProfile preserves the fixed phase "configure repository RPC profile" while keeping nested dependency details private.
	// stageConfigureRepositoryRPCProfile сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConfigureRepositoryRPCProfile startupStage = "configure repository RPC profile"
	// stageConfigureRuntimeSecrets preserves the fixed phase "configure runtime secrets" while keeping nested dependency details private.
	// stageConfigureRuntimeSecrets сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConfigureRuntimeSecrets startupStage = "configure runtime secrets"
	// stageConfigureRuntimeSecretStaging preserves the fixed phase "configure runtime secret staging" while keeping nested dependency details private.
	// stageConfigureRuntimeSecretStaging сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConfigureRuntimeSecretStaging startupStage = "configure runtime secret staging"
	// stageInitializeEmailProjection preserves the fixed phase "initialize email projection" while keeping nested dependency details private.
	// stageInitializeEmailProjection сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageInitializeEmailProjection startupStage = "initialize email projection"
	// stageConstructSkillScanner preserves the fixed phase "construct skill scanner" while keeping nested dependency details private.
	// stageConstructSkillScanner сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructSkillScanner startupStage = "construct skill scanner"
	// stageConfigureSkillScanner preserves the fixed phase "configure skill scanner" while keeping nested dependency details private.
	// stageConfigureSkillScanner сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConfigureSkillScanner startupStage = "configure skill scanner"
	// stageConfigureArtifactScanner preserves the fixed phase "configure artifact scanner" while keeping nested dependency details private.
	// stageConfigureArtifactScanner сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConfigureArtifactScanner startupStage = "configure artifact scanner"
	// stageConfigureDefaultProviderCredential preserves the fixed phase "configure default provider credential" while keeping nested dependency details private.
	// stageConfigureDefaultProviderCredential сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConfigureDefaultProviderCredential startupStage = "configure default provider credential"
	// stageReadRoleImageLeaseSigningKey preserves the fixed phase "read role image lease signing key" while keeping nested dependency details private.
	// stageReadRoleImageLeaseSigningKey сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageReadRoleImageLeaseSigningKey startupStage = "read role image lease signing key"
	// stageConfigureRoleImageLifecycle preserves the fixed phase "configure role image lifecycle" while keeping nested dependency details private.
	// stageConfigureRoleImageLifecycle сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConfigureRoleImageLifecycle startupStage = "configure role image lifecycle"
	// stageConstructIntegrationCredentialMaterializer preserves the fixed phase "construct integration credential materializer" while keeping nested dependency details private.
	// stageConstructIntegrationCredentialMaterializer сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructIntegrationCredentialMaterializer startupStage = "construct integration credential materializer"
	// stageConstructProviderCredentialMaterializerClient preserves the fixed phase "construct provider credential materializer client" while keeping nested dependency details private.
	// stageConstructProviderCredentialMaterializerClient сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructProviderCredentialMaterializerClient startupStage = "construct provider credential materializer client"
	// stageConstructProviderCredentialMaterializerAdapter preserves the fixed phase "construct provider credential materializer adapter" while keeping nested dependency details private.
	// stageConstructProviderCredentialMaterializerAdapter сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructProviderCredentialMaterializerAdapter startupStage = "construct provider credential materializer adapter"
	// stageConstructEmailCredentialMaterializer preserves the fixed phase "construct email credential materializer" while keeping nested dependency details private.
	// stageConstructEmailCredentialMaterializer сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructEmailCredentialMaterializer startupStage = "construct email credential materializer"
	// stageConstructPlatformService preserves the fixed phase "construct platform service" while keeping nested dependency details private.
	// stageConstructPlatformService сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructPlatformService startupStage = "construct platform service"
	// stageBootstrapPlatform preserves the fixed phase "bootstrap platform" while keeping nested dependency details private.
	// stageBootstrapPlatform сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageBootstrapPlatform startupStage = "bootstrap platform"
	// stageLoadRoleEnvironmentCatalog preserves the fixed phase "load role environment catalog" while keeping nested dependency details private.
	// stageLoadRoleEnvironmentCatalog сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageLoadRoleEnvironmentCatalog startupStage = "load role environment catalog"
	// stageConstructRoleImageService preserves the fixed phase "construct role image service" while keeping nested dependency details private.
	// stageConstructRoleImageService сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructRoleImageService startupStage = "construct role image service"
	// stageConstructTrustedClusterReader preserves the fixed phase "construct trusted cluster reader" while keeping nested dependency details private.
	// stageConstructTrustedClusterReader сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructTrustedClusterReader startupStage = "construct trusted cluster reader"
	// stageConstructAuthorityProofResolver preserves the fixed phase "construct authority proof resolver" while keeping nested dependency details private.
	// stageConstructAuthorityProofResolver сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructAuthorityProofResolver startupStage = "construct authority proof resolver"
	// stageConnectAuthorizationVerifier preserves the fixed phase "connect authorization verifier" while keeping nested dependency details private.
	// stageConnectAuthorizationVerifier сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConnectAuthorizationVerifier startupStage = "connect authorization verifier"
	// stageConstructServiceIdentityReader preserves the fixed phase "construct service identity reader" while keeping nested dependency details private.
	// stageConstructServiceIdentityReader сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructServiceIdentityReader startupStage = "construct service identity reader"
	// stageConstructNATSPublisher preserves the fixed phase "construct NATS publisher" while keeping nested dependency details private.
	// stageConstructNATSPublisher сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructNATSPublisher startupStage = "construct NATS publisher"
	// stageVerifyNATSStream preserves the fixed phase "verify NATS stream" while keeping nested dependency details private.
	// stageVerifyNATSStream сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageVerifyNATSStream startupStage = "verify NATS stream"
	// stageVerifyOutbox preserves the fixed phase "verify outbox" while keeping nested dependency details private.
	// stageVerifyOutbox сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageVerifyOutbox startupStage = "verify outbox"
	// stageConstructGRPCTransport preserves the fixed phase "construct gRPC transport" while keeping nested dependency details private.
	// stageConstructGRPCTransport сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructGRPCTransport startupStage = "construct gRPC transport"
	// stageConstructRoleImageTransport preserves the fixed phase "construct role image transport" while keeping nested dependency details private.
	// stageConstructRoleImageTransport сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructRoleImageTransport startupStage = "construct role image transport"
	// stageConstructAuthorityProofTransport preserves the fixed phase "construct authority proof transport" while keeping nested dependency details private.
	// stageConstructAuthorityProofTransport сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructAuthorityProofTransport startupStage = "construct authority proof transport"
	// stageConstructProviderCredentialCleanupWorker preserves the fixed phase "construct provider credential cleanup worker" while keeping nested dependency details private.
	// stageConstructProviderCredentialCleanupWorker сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructProviderCredentialCleanupWorker startupStage = "construct provider credential cleanup worker"
	// stageConstructProviderModelCatalogWorker preserves the fixed phase "construct provider model catalog worker" while keeping nested dependency details private.
	// stageConstructProviderModelCatalogWorker сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructProviderModelCatalogWorker startupStage = "construct provider model catalog worker"
	// stageConstructProviderCredentialRenewalWorker preserves the fixed phase "construct provider credential renewal worker" while keeping nested dependency details private.
	// stageConstructProviderCredentialRenewalWorker сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageConstructProviderCredentialRenewalWorker startupStage = "construct provider credential renewal worker"
	// stageListenControlPlaneGRPC preserves the fixed phase "listen control-plane gRPC" while keeping nested dependency details private.
	// stageListenControlPlaneGRPC сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageListenControlPlaneGRPC startupStage = "listen control-plane gRPC"
	// stageOpenProtectedFile preserves the fixed phase "open protected file" while keeping nested dependency details private.
	// stageOpenProtectedFile сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageOpenProtectedFile startupStage = "open protected file"
	// stageProtectedFileIsInvalid preserves the fixed phase "protected file is invalid" while keeping nested dependency details private.
	// stageProtectedFileIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageProtectedFileIsInvalid startupStage = "protected file is invalid"
	// stageReadProtectedFile preserves the fixed phase "read protected file" while keeping nested dependency details private.
	// stageReadProtectedFile сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageReadProtectedFile startupStage = "read protected file"
	// stageLoadPostgreSQLConfiguration preserves the fixed phase "load PostgreSQL configuration" while keeping nested dependency details private.
	// stageLoadPostgreSQLConfiguration сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageLoadPostgreSQLConfiguration startupStage = "load PostgreSQL configuration"
	// stageParsePostgreSQLConfiguration preserves the fixed phase "parse PostgreSQL configuration" while keeping nested dependency details private.
	// stageParsePostgreSQLConfiguration сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageParsePostgreSQLConfiguration startupStage = "parse PostgreSQL configuration"
	// stageLoadPostgreSQLCA preserves the fixed phase "load PostgreSQL CA" while keeping nested dependency details private.
	// stageLoadPostgreSQLCA сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageLoadPostgreSQLCA startupStage = "load PostgreSQL CA"
	// stageParsePostgreSQLCA preserves the fixed phase "parse PostgreSQL CA" while keeping nested dependency details private.
	// stageParsePostgreSQLCA сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageParsePostgreSQLCA startupStage = "parse PostgreSQL CA"
	// stageOpenPostgreSQLPool preserves the fixed phase "open PostgreSQL pool" while keeping nested dependency details private.
	// stageOpenPostgreSQLPool сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageOpenPostgreSQLPool startupStage = "open PostgreSQL pool"
	// stageVerifyPostgreSQLConnection preserves the fixed phase "verify PostgreSQL connection" while keeping nested dependency details private.
	// stageVerifyPostgreSQLConnection сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageVerifyPostgreSQLConnection startupStage = "verify PostgreSQL connection"
	// stageLoadControlPlaneServerCertificate preserves the fixed phase "load control-plane server certificate" while keeping nested dependency details private.
	// stageLoadControlPlaneServerCertificate сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageLoadControlPlaneServerCertificate startupStage = "load control-plane server certificate"
	// stageLoadInternalClientCA preserves the fixed phase "load internal client CA" while keeping nested dependency details private.
	// stageLoadInternalClientCA сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageLoadInternalClientCA startupStage = "load internal client CA"
	// stageParseInternalClientCA preserves the fixed phase "parse internal client CA" while keeping nested dependency details private.
	// stageParseInternalClientCA сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageParseInternalClientCA startupStage = "parse internal client CA"
	// stageParseControlPlaneEnvironment preserves the fixed phase "parse control-plane environment" while keeping nested dependency details private.
	// stageParseControlPlaneEnvironment сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageParseControlPlaneEnvironment startupStage = "parse control-plane environment"
	// stageControlPlaneRPCProfileIsInvalid preserves the fixed phase "control-plane RPC profile is invalid" while keeping nested dependency details private.
	// stageControlPlaneRPCProfileIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneRPCProfileIsInvalid startupStage = "control-plane RPC profile is invalid"
	// stageControlPlaneSkillScannerConfigurationIsInvalid preserves the fixed phase "control-plane skill scanner configuration is invalid" while keeping nested dependency details private.
	// stageControlPlaneSkillScannerConfigurationIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneSkillScannerConfigurationIsInvalid startupStage = "control-plane skill scanner configuration is invalid"
	// stageControlPlaneListenAddressIsInvalid preserves the fixed phase "control-plane listen address is invalid" while keeping nested dependency details private.
	// stageControlPlaneListenAddressIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneListenAddressIsInvalid startupStage = "control-plane listen address is invalid"
	// stageControlPlaneFilePathIsInvalid preserves the fixed phase "control-plane file path is invalid" while keeping nested dependency details private.
	// stageControlPlaneFilePathIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneFilePathIsInvalid startupStage = "control-plane file path is invalid"
	// stageControlPlaneInteractionGrantTrustPathIsInvalid preserves the fixed phase "control-plane interaction grant trust path is invalid" while keeping nested dependency details private.
	// stageControlPlaneInteractionGrantTrustPathIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneInteractionGrantTrustPathIsInvalid startupStage = "control-plane interaction grant trust path is invalid"
	// stageControlPlaneEmailGrantTrustPathIsInvalid preserves the fixed phase "control-plane email grant trust path is invalid" while keeping nested dependency details private.
	// stageControlPlaneEmailGrantTrustPathIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneEmailGrantTrustPathIsInvalid startupStage = "control-plane email grant trust path is invalid"
	// stageControlPlaneEmailConfigurationPathIsInvalid preserves the fixed phase "control-plane email configuration path is invalid" while keeping nested dependency details private.
	// stageControlPlaneEmailConfigurationPathIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneEmailConfigurationPathIsInvalid startupStage = "control-plane email configuration path is invalid"
	// stageControlPlaneBoundedConfigurationIsInvalid preserves the fixed phase "control-plane bounded configuration is invalid" while keeping nested dependency details private.
	// stageControlPlaneBoundedConfigurationIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneBoundedConfigurationIsInvalid startupStage = "control-plane bounded configuration is invalid"
	// stageControlPlaneOIDCBoundaryIsInvalid preserves the fixed phase "control-plane OIDC boundary is invalid" while keeping nested dependency details private.
	// stageControlPlaneOIDCBoundaryIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneOIDCBoundaryIsInvalid startupStage = "control-plane OIDC boundary is invalid"
	// stageControlPlaneObjectStorageBoundaryIsInvalid preserves the fixed phase "control-plane object storage boundary is invalid" while keeping nested dependency details private.
	// stageControlPlaneObjectStorageBoundaryIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneObjectStorageBoundaryIsInvalid startupStage = "control-plane object storage boundary is invalid"
	// stageControlPlaneAuthoritySocketDirectoryIsInvalid preserves the fixed phase "control-plane authority socket directory is invalid" while keeping nested dependency details private.
	// stageControlPlaneAuthoritySocketDirectoryIsInvalid сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageControlPlaneAuthoritySocketDirectoryIsInvalid startupStage = "control-plane authority socket directory is invalid"
	// stageServiceLifecycle preserves the fixed phase "service lifecycle stopped" while keeping nested dependency details private.
	// stageServiceLifecycle сохраняет фиксированный этап отказа без раскрытия вложенной ошибки зависимости.
	stageServiceLifecycle startupStage = "service lifecycle stopped"
	// unknownStartupStage is the closed fallback for unregistered phase values and untyped failures.
	// unknownStartupStage является закрытым fallback для незарегистрированных этапов и нетипизированных отказов.
	unknownStartupStage = "unknown"
)

// startupError retains a private typed cause, optional exhausted wait budget and registered phase.
// The wait budget does not replace the primary refusal or authorize retries outside initial connectivity.
// startupError сохраняет приватную типизированную причину, необязательное исчерпание wait budget и этап.
// Wait budget не заменяет первичный отказ и не разрешает повторы вне initial connectivity.
type startupError struct {
	stage  startupStage
	cause  error
	budget error
}

// Error returns only the normalized fixed phase, never the underlying dependency message.
// Error возвращает только нормализованный фиксированный этап, никогда сообщение внешней зависимости.
func (failure *startupError) Error() string { return normalizedStartupStage(failure.stage) }

// Unwrap preserves private errors.Is/As inspection for classification and cancellation handling.
// Unwrap сохраняет приватную проверку errors.Is/As для классификации и обработки отмены.
func (failure *startupError) Unwrap() error {
	if failure.budget != nil {
		return errors.Join(failure.cause, failure.budget)
	}
	return failure.cause
}

// startupFailure records an explicit failed phase, including a fixed validation rejection without a cause.
// startupFailure фиксирует явно неуспешный этап, включая фиксированный validation отказ без внешней причины.
func startupFailure(stage startupStage, cause error) error {
	return &startupError{stage: stage, cause: cause}
}

// normalizedStartupStage accepts only the complete inventoried phase registry and closes all unknown input.
// normalizedStartupStage принимает только полный учтённый реестр этапов, закрывая любой неизвестный ввод.
func normalizedStartupStage(stage startupStage) string {
	switch stage {
	case stageReadObjectStorageAccessKey:
		return string(stage)
	case stageReadObjectStorageSecretKey:
		return string(stage)
	case stageConstructObjectStorage:
		return string(stage)
	case stageConstructSessionArchiveStorage:
		return string(stage)
	case stageConstructPlatformRepository:
		return string(stage)
	case stageConfigureProjectPurgeStorage:
		return string(stage)
	case stageConfigureRepositoryRPCProfile:
		return string(stage)
	case stageConfigureRuntimeSecrets:
		return string(stage)
	case stageConfigureRuntimeSecretStaging:
		return string(stage)
	case stageInitializeEmailProjection:
		return string(stage)
	case stageConstructSkillScanner:
		return string(stage)
	case stageConfigureSkillScanner:
		return string(stage)
	case stageConfigureArtifactScanner:
		return string(stage)
	case stageConfigureDefaultProviderCredential:
		return string(stage)
	case stageReadRoleImageLeaseSigningKey:
		return string(stage)
	case stageConfigureRoleImageLifecycle:
		return string(stage)
	case stageConstructIntegrationCredentialMaterializer:
		return string(stage)
	case stageConstructProviderCredentialMaterializerClient:
		return string(stage)
	case stageConstructProviderCredentialMaterializerAdapter:
		return string(stage)
	case stageConstructEmailCredentialMaterializer:
		return string(stage)
	case stageConstructPlatformService:
		return string(stage)
	case stageBootstrapPlatform:
		return string(stage)
	case stageLoadRoleEnvironmentCatalog:
		return string(stage)
	case stageConstructRoleImageService:
		return string(stage)
	case stageConstructTrustedClusterReader:
		return string(stage)
	case stageConstructAuthorityProofResolver:
		return string(stage)
	case stageConnectAuthorizationVerifier:
		return string(stage)
	case stageConstructServiceIdentityReader:
		return string(stage)
	case stageConstructNATSPublisher:
		return string(stage)
	case stageVerifyNATSStream:
		return string(stage)
	case stageVerifyOutbox:
		return string(stage)
	case stageConstructGRPCTransport:
		return string(stage)
	case stageConstructRoleImageTransport:
		return string(stage)
	case stageConstructAuthorityProofTransport:
		return string(stage)
	case stageConstructProviderCredentialCleanupWorker:
		return string(stage)
	case stageConstructProviderModelCatalogWorker:
		return string(stage)
	case stageConstructProviderCredentialRenewalWorker:
		return string(stage)
	case stageListenControlPlaneGRPC:
		return string(stage)
	case stageOpenProtectedFile:
		return string(stage)
	case stageProtectedFileIsInvalid:
		return string(stage)
	case stageReadProtectedFile:
		return string(stage)
	case stageLoadPostgreSQLConfiguration:
		return string(stage)
	case stageParsePostgreSQLConfiguration:
		return string(stage)
	case stageLoadPostgreSQLCA:
		return string(stage)
	case stageParsePostgreSQLCA:
		return string(stage)
	case stageOpenPostgreSQLPool:
		return string(stage)
	case stageVerifyPostgreSQLConnection:
		return string(stage)
	case stageLoadControlPlaneServerCertificate:
		return string(stage)
	case stageLoadInternalClientCA:
		return string(stage)
	case stageParseInternalClientCA:
		return string(stage)
	case stageParseControlPlaneEnvironment:
		return string(stage)
	case stageControlPlaneRPCProfileIsInvalid:
		return string(stage)
	case stageControlPlaneSkillScannerConfigurationIsInvalid:
		return string(stage)
	case stageControlPlaneListenAddressIsInvalid:
		return string(stage)
	case stageControlPlaneFilePathIsInvalid:
		return string(stage)
	case stageControlPlaneInteractionGrantTrustPathIsInvalid:
		return string(stage)
	case stageControlPlaneEmailGrantTrustPathIsInvalid:
		return string(stage)
	case stageControlPlaneEmailConfigurationPathIsInvalid:
		return string(stage)
	case stageControlPlaneBoundedConfigurationIsInvalid:
		return string(stage)
	case stageControlPlaneOIDCBoundaryIsInvalid:
		return string(stage)
	case stageControlPlaneObjectStorageBoundaryIsInvalid:
		return string(stage)
	case stageControlPlaneAuthoritySocketDirectoryIsInvalid:
		return string(stage)
	case stageServiceLifecycle:
		return string(stage)
	default:
		return unknownStartupStage
	}
}

// ReportFailure is the service process boundary: it prints only the registered fixed phase and closed class.
// Raw error text, DSN, host, user, SQL and secret values are never formatted; the original cause stays private.
// ReportFailure является границей процесса: печатает только зарегистрированный этап и закрытый класс.
// Сырые ошибки, DSN, host, user, SQL и секреты не форматируются; исходная причина остаётся приватной.
func ReportFailure(output io.Writer, err error) {
	stage := unknownStartupStage
	class := postgresdiagnostic.Class(err)
	waitStatus := ""
	var failure *startupError
	if errors.As(err, &failure) {
		stage = normalizedStartupStage(failure.stage)
		if failure.stage == stageVerifyPostgreSQLConnection && failure.budget != nil {
			class = postgresdiagnostic.Class(failure.cause)
			outcome := "unknown"
			if errors.Is(failure.budget, context.Canceled) {
				outcome = "canceled"
			} else if errors.Is(failure.budget, context.DeadlineExceeded) {
				outcome = "deadline"
			}
			waitStatus = " wait_status=" + outcome
		}
		switch failure.stage {
		case stageParseControlPlaneEnvironment, stageControlPlaneRPCProfileIsInvalid, stageControlPlaneSkillScannerConfigurationIsInvalid, stageControlPlaneListenAddressIsInvalid, stageControlPlaneFilePathIsInvalid, stageControlPlaneInteractionGrantTrustPathIsInvalid, stageControlPlaneEmailGrantTrustPathIsInvalid, stageControlPlaneEmailConfigurationPathIsInvalid, stageControlPlaneBoundedConfigurationIsInvalid, stageControlPlaneOIDCBoundaryIsInvalid, stageControlPlaneObjectStorageBoundaryIsInvalid, stageControlPlaneAuthoritySocketDirectoryIsInvalid:
			class = "configuration"
		case stageParsePostgreSQLCA, stageParseInternalClientCA:
			class = "tls_configuration"
		case stageOpenProtectedFile, stageProtectedFileIsInvalid, stageReadProtectedFile, stageLoadPostgreSQLConfiguration, stageLoadPostgreSQLCA, stageLoadInternalClientCA:
			class = "filesystem"
		}
	}
	_, _ = fmt.Fprintf(output, "control-plane failed stage=%q error_class=%s%s\n", stage, class, waitStatus)
}
