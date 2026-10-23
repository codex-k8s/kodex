package restoreagent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	// directiveType pins the protected JWS type of a controller-signed role-bound restore directive, separate from ACK and role-credential types.
	// directiveType закрепляет защищённый тип JWS подписанной контроллером ролевой директивы восстановления отдельно от ACK и role credential.
	directiveType = "kodex-internal-rpc-restore-directive+jws"
	// ackType pins the protected JWS type of the workload's quiescence ACK; signing it does not itself persist the controller's acceptance receipt.
	// ackType закрепляет защищённый тип JWS подтверждения дренирования от workload; его подпись сама не сохраняет квитанцию принятия контроллером.
	ackType = "kodex-internal-rpc-restore-quiescence-ack+jws"
	// workloadAudience is the exact audience required in a restore directive targeted at this workload, not a caller-selected routing value.
	// workloadAudience — точная требуемая аудитория директивы восстановления для workload, а не выбираемое вызывающим значение маршрутизации.
	workloadAudience = "urn:kodex:internal-rpc-authority-restore-workload"
	// controllerAudience is the fixed audience for role credentials and outgoing workload ACKs accepted by the restore controller.
	// controllerAudience — фиксированная аудитория role credentials и исходящих подтверждений workload, принимаемых контроллером восстановления.
	controllerAudience = "urn:kodex:internal-rpc-authority-restore-controller"
	// controllerIssuer pins the expected controller workload SPIFFE identity in directive claims; TLS transport and application authorization remain separate checks.
	// controllerIssuer закрепляет ожидаемую SPIFFE-идентичность workload контроллера в claims директивы; TLS-транспорт и прикладная авторизация остаются отдельными проверками.
	controllerIssuer = "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-restore-controller"
	// roleCredentialIssuer is the fixed publisher SPIFFE identity required in verified restore-role credential claims, independently of a signer's key ID.
	// roleCredentialIssuer — фиксированная SPIFFE-идентичность издателя, требуемая в проверенных claims restore-role credential независимо от идентификатора ключа подписанта.
	roleCredentialIssuer = "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-publisher"
	// roleCredentialType distinguishes the publisher's restore-role credential JWS from controller directives and workload ACKs during header validation.
	// roleCredentialType отличает JWS restore-role credential издателя от директив контроллера и подтверждений workload при проверке заголовка.
	roleCredentialType = "kodex-internal-rpc-restore-role-credential+jws"
	// boundedTTL limits directive time validation and newly signed ACK lifetime to thirty seconds; it is not the five-minute role-credential bound.
	// boundedTTL ограничивает проверку времени директивы и срок нового подписанного ACK тридцатью секундами; это не пятиминутный предел role credential.
	boundedTTL = 30 * time.Second
	// clockSkew permits five seconds of skew in role-credential and directive time checks without changing the required issuer, audience or generation bindings.
	// clockSkew допускает пять секунд расхождения часов при проверке времени role credential и директивы без изменения привязок издателя, аудитории или поколений.
	clockSkew = 5 * time.Second
)

// AuthorityAdmission exposes the workload's admission, in-flight drain and served-state boundary to the restore agent. Implementations own synchronization and request accounting; the interface does not implement the restore controller.
// AuthorityAdmission предоставляет агенту восстановления границу допуска workload, дренирования текущих запросов и обслуживаемого состояния. Синхронизацией и учётом запросов владеет реализация; контроллер восстановления здесь не реализован.
type AuthorityAdmission interface {
	// SetAvailable changes the workload's availability flag; the caller coordinates it with the independent restore-blocked flag and verified external phase.
	// SetAvailable меняет флаг доступности workload; вызывающий согласует его с независимым флагом блокировки восстановления и проверенной внешней фазой.
	SetAvailable(bool)
	// SetRestoreBlocked controls the restore-specific admission fence without replacing general availability or automatically proving that in-flight work has drained.
	// SetRestoreBlocked управляет ограждением допуска для восстановления, не заменяя общую доступность и не доказывая автоматическое дренирование текущей работы.
	SetRestoreBlocked(bool)
	// WaitDrained waits under the supplied context for existing requests to finish; Poll additionally checks the in-flight count before producing an ACK.
	// WaitDrained ожидает завершения текущих запросов под переданным контекстом; Poll дополнительно проверяет их счётчик перед созданием ACK.
	WaitDrained(context.Context) error
	// Inflight reports the current request count used to refuse an incomplete drain even after WaitDrained returns successfully.
	// Inflight сообщает число текущих запросов, позволяя отклонить неполное дренирование даже после успешного возврата WaitDrained.
	Inflight() int64
	// SnapshotState supplies the workload's served snapshot binding for the signed ACK, not a new activation or authoritative controller transition.
	// SnapshotState предоставляет привязку обслуживаемого снимка workload для подписанного ACK, а не новую активацию или авторитетный переход контроллера.
	SnapshotState() model.SnapshotState
	// ServedStateReady verifies the workload's served-state readiness before a safe no-directive poll can clear the restore fence and reopen availability.
	// ServedStateReady проверяет готовность обслуживаемого состояния workload до снятия ограждения восстановления и открытия доступности безопасным poll без директивы.
	ServedStateReady(context.Context) error
}

// Config pins controller transport, delivered role/ACK material, mounted trust files, workload identity and three generations. Secret access belongs to Delivery and configuration presence alone is not proof of authority.
// Config закрепляет транспорт контроллера, доставленный материал role/ACK, смонтированные файлы доверия, идентичность workload и три поколения. Доступом к секретам владеет Delivery; наличие конфигурации само не доказывает полномочия.
type Config struct {
	Address                    string
	TLS                        *tls.Config
	RoleCredentialSecret       string
	ACKPrivateJWKSecret        string
	Delivery                   SecretReader
	ControllerCertificateFile  string
	ManifestRootPublicJWKFile  string
	ManifestRootMetadataFile   string
	ManifestTrustBundleJWSFile string
	RestoreRoleTrustJWSFile    string
	WorkloadID                 string
	WorkloadSPIFFEID           string
	Role                       string
	WorkloadGeneration         uint64
	CredentialGeneration       uint64
	ACKKeyGeneration           uint64
	UnaryInterceptor           grpc.UnaryClientInterceptor
}

// SecretReader supplies versioned restore material through the delivery abstraction; the agent does not receive a general Kubernetes API client.
// SecretReader предоставляет версионированный материал восстановления через абстракцию доставки; общий клиент Kubernetes API агенту не выдаётся.
type SecretReader interface {
	// ReadVersioned reads one exact delivery path and distinguishes absence from failure; callers decide whether missing role material permits a discovery request or requires refusal.
	// ReadVersioned читает один точный путь доставки и отличает отсутствие от ошибки; вызывающий решает, допускает ли отсутствие role material запрос обнаружения или требует отказа.
	ReadVersioned(context.Context, string) (repository.SecretMaterial, bool, error)
}

// Agent tracks the last observed coordination revision and a local quiescing flag while polling the external restore controller. Its injected clock and optional poll fetcher support local tests; neither atomic field is durable restore authority.
// Agent хранит последнюю наблюдённую ревизию координации и локальный флаг дренирования при опросе внешнего контроллера. Внедрённые часы и необязательный источник poll служат локальным тестам; атомарные поля не являются устойчивыми полномочиями восстановления.
type Agent struct {
	config           Config
	observed         atomic.Uint64
	quiescing        atomic.Bool
	now              func() time.Time
	directiveFetcher func(context.Context) (*internalrpcauthorityv1.GetRestoreDirectiveResponse, string, error)
}

// New rejects incomplete identity, generation, material-path and interceptor configuration and TLS without verified server naming, then installs the real clock. It does not load trust files, fetch secrets or contact the controller yet.
// New отклоняет неполную конфигурацию идентичности, поколений, путей материала и interceptor, а также TLS без проверяемого имени сервера, затем устанавливает реальные часы. Файлы доверия и секреты ещё не читаются, связи с контроллером нет.
func New(config Config) (*Agent, error) {
	if config.Address == "" ||
		config.TLS == nil ||
		config.TLS.ServerName == "" ||
		config.TLS.InsecureSkipVerify ||
		config.RoleCredentialSecret == "" ||
		config.ACKPrivateJWKSecret == "" ||
		config.Delivery == nil ||
		config.ControllerCertificateFile == "" ||
		config.ManifestRootPublicJWKFile == "" ||
		config.ManifestRootMetadataFile == "" ||
		config.ManifestTrustBundleJWSFile == "" ||
		config.RestoreRoleTrustJWSFile == "" ||
		config.WorkloadID == "" ||
		config.WorkloadSPIFFEID == "" ||
		config.Role == "" ||
		config.WorkloadGeneration == 0 ||
		config.CredentialGeneration == 0 ||
		config.ACKKeyGeneration == 0 ||
		config.UnaryInterceptor == nil {
		return nil, errors.New("invalid restore workload agent configuration")
	}
	return &Agent{config: config, now: time.Now}, nil
}

// Poll preserves existing admission while fetching the controller state. A verified safe no-directive response plus ready served state clears the fence; a valid directive sets the restore fence, drains work, validates the ACK key, signs an ACK and checks its receipt binding. Errors return without automatically reopening admission or rolling back prior fence effects.
// Poll сохраняет текущий допуск во время получения состояния контроллера. Проверенный безопасный ответ без директивы и готовое обслуживаемое состояние снимают ограждение; допустимая директива ставит его, дренирует работу, проверяет ключ ACK, подписывает подтверждение и проверяет привязку квитанции. Ошибки возвращаются без автоматического открытия допуска или отката уже установленного ограждения.
func (agent *Agent) Poll(
	ctx context.Context,
	admission AuthorityAdmission,
) error {
	if admission == nil {
		return errors.New("restore workload admission boundary is nil")
	}
	// Startup/rejoin закрывает VerifyStartup. Рабочий poll сохраняет admission
	// открытым до ошибки controller либо фактически полученной директивы.
	fetchDirective := agent.getDirective
	if agent.directiveFetcher != nil {
		fetchDirective = agent.directiveFetcher
	}
	response, roleCompact, err := fetchDirective(ctx)
	if err != nil {
		return err
	}
	if noDirective := response.GetNoDirective(); noDirective != nil {
		transition := noDirective.GetVerifiedTransition()
		if err := agent.verifySafeNoDirective(noDirective, transition); err != nil {
			return err
		}
		agent.observed.Store(noDirective.GetCoordinationRevision())
		if err := admission.ServedStateReady(ctx); err == nil {
			admission.SetRestoreBlocked(false)
			admission.SetAvailable(true)
			agent.quiescing.Store(false)
		} else {
			return errors.New("served state is not ready after verified restore poll")
		}
		return nil
	}
	directiveResult := response.GetDirective()
	if directiveResult == nil || directiveResult.GetDirectiveCompactJws() == "" {
		return errors.New("restore controller response is invalid")
	}
	roleClaims, roleDigest, err := agent.verifyRoleCredential(roleCompact)
	if err != nil {
		return err
	}
	directive, err := agent.verifyDirective(
		directiveResult.GetDirectiveCompactJws(),
		roleClaims,
		roleDigest,
	)
	if err != nil {
		return err
	}
	admission.SetRestoreBlocked(true)
	agent.quiescing.Store(true)
	if err := admission.WaitDrained(ctx); err != nil {
		return err
	}
	if admission.Inflight() != 0 {
		return errors.New("restore workload inflight drain is incomplete")
	}
	ackMaterial, found, err := agent.config.Delivery.ReadVersioned(
		ctx,
		agent.config.ACKPrivateJWKSecret,
	)
	if err != nil || !found {
		return errors.New("read restore ACK private key")
	}
	ackKey, err := internalrpcauth.ParsePrivateJWK(
		[]byte(ackMaterial.Data["ack_private_jwk"]),
	)
	if err != nil {
		return errors.New("parse restore ACK private key")
	}
	ackThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(
		ackKey.PublicOnly(),
	)
	if err != nil ||
		ackKey.KeyID != roleClaims.ACKKeyID ||
		ackThumbprint != roleClaims.ACKKeyThumbprintSHA256 ||
		roleClaims.ACKKeyGeneration != agent.config.ACKKeyGeneration {
		return errors.New("restore ACK private key binding rejected")
	}
	state := admission.SnapshotState()
	now := agent.now().UTC().Truncate(time.Second)
	ackJTI := deterministicUUID("restore-ack", directive.JTI, roleDigest)
	ackClaims := model.QuiescenceACKClaims{
		Version: model.ContractVersion, Issuer: agent.config.WorkloadSPIFFEID,
		Audience: controllerAudience, Subject: agent.config.WorkloadID,
		JTI: ackJTI, DirectiveJTI: directive.JTI,
		RestoreID: directive.RestoreID, RestoreEpoch: directive.RestoreEpoch,
		CoordinationRevision: directive.CoordinationRevision,
		WorkloadID:           agent.config.WorkloadID,
		WorkloadSPIFFEID:     agent.config.WorkloadSPIFFEID,
		Role:                 agent.config.Role,
		WorkloadGeneration:   agent.config.WorkloadGeneration,
		CredentialGeneration: agent.config.CredentialGeneration,
		ACKKeyID:             ackKey.KeyID, ACKKeyGeneration: agent.config.ACKKeyGeneration,
		ACKKeyThumbprintSHA256:     ackThumbprint,
		RoleCredentialDigestSHA256: roleDigest,
		ServedSnapshotDigest:       state.SourceDigestSHA256,
		AcceptingStopped:           true, InflightDrained: true, InflightCount: 0,
		IssuedAt: now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(boundedTTL).Unix(),
	}
	ackCompact, err := internalrpcauth.SignCanonicalJSON(
		ackClaims,
		ackKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  ackType,
			KeyID: ackKey.KeyID,
		},
	)
	if err != nil {
		return errors.New("sign restore quiescence ACK")
	}
	idempotencyKey := deterministicUUID(
		"restore-ack-request",
		directive.JTI,
		ackJTI,
	)
	connection, err := agent.newConnection()
	if err != nil {
		return err
	}
	defer connection.Close()
	api := internalrpcauthorityv1.NewRestoreControllerServiceClient(connection)
	correlationID := deterministicUUID(
		"restore-poll",
		agent.config.WorkloadID,
		roleCompact,
	)
	ackResponse, err := api.AcknowledgeQuiescence(
		ctx,
		&internalrpcauthorityv1.AcknowledgeQuiescenceRequest{
			CorrelationId:            correlationID,
			RoleCredentialCompactJws: roleCompact,
			DirectiveCompactJws:      directiveResult.GetDirectiveCompactJws(),
			QuiescenceAckCompactJws:  ackCompact,
			IdempotencyKey:           idempotencyKey,
		},
	)
	if err != nil {
		return errors.New("acknowledge restore quiescence")
	}
	if ackResponse.GetReceipt() == nil ||
		ackResponse.GetReceipt().GetIdempotencyKey() != idempotencyKey ||
		ackResponse.GetReceipt().GetAckJti() != ackJTI ||
		ackResponse.GetTransition() == nil {
		return errors.New("restore quiescence receipt binding rejected")
	}
	agent.observed.Store(ackResponse.GetReceipt().GetCoordinationRevision())
	return nil
}

// VerifyStartup synchronously sets restore-blocked and unavailable before querying the real controller and requiring a verified safe no-directive transition. Success records the observed revision but deliberately does not reopen admission; callers must run it before activation and bind.
// VerifyStartup синхронно ставит restore-blocked и недоступность перед запросом реального контроллера и требованием проверенного безопасного перехода без директивы. Успех сохраняет наблюдённую ревизию, но намеренно не открывает допуск; вызывающий выполняет это до активации и bind.
func (agent *Agent) VerifyStartup(
	ctx context.Context,
	admission AuthorityAdmission,
) error {
	if admission == nil {
		return errors.New("restore workload admission boundary is nil")
	}
	admission.SetRestoreBlocked(true)
	admission.SetAvailable(false)
	response, _, err := agent.getDirective(ctx)
	if err != nil {
		return err
	}
	noDirective := response.GetNoDirective()
	if noDirective == nil {
		return errors.New("restore external phase keeps startup fenced")
	}
	if err := agent.verifySafeNoDirective(
		noDirective,
		noDirective.GetVerifiedTransition(),
	); err != nil {
		return err
	}
	agent.observed.Store(noDirective.GetCoordinationRevision())
	return nil
}

// getDirective reads the role delivery, rejects a present empty credential but permits absence for discovery, opens a per-call connection and polls with the observed revision and deterministic correlation ID. It closes the connection on return without verifying a returned directive locally in this method.
// getDirective читает доставку роли, отклоняет присутствующий пустой credential, но допускает отсутствие для обнаружения, открывает соединение на вызов и опрашивает с наблюдённой ревизией и детерминированной корреляцией. Соединение закрывается при возврате; локальной проверки возвращённой директивы в этом методе нет.
func (agent *Agent) getDirective(
	ctx context.Context,
) (*internalrpcauthorityv1.GetRestoreDirectiveResponse, string, error) {
	roleMaterial, found, err := agent.config.Delivery.ReadVersioned(
		ctx,
		agent.config.RoleCredentialSecret,
	)
	if err != nil {
		return nil, "", errors.New("read restore role credential")
	}
	roleCompact := ""
	if found {
		roleCompact = roleMaterial.Data["role_credential_compact_jws"]
	}
	if found && roleCompact == "" {
		return nil, "", errors.New("restore role credential material is invalid")
	}
	connection, err := agent.newConnection()
	if err != nil {
		return nil, "", err
	}
	defer connection.Close()
	api := internalrpcauthorityv1.NewRestoreControllerServiceClient(connection)
	correlationID := deterministicUUID(
		"restore-poll",
		agent.config.WorkloadID,
		roleCompact,
	)
	response, err := api.GetRestoreDirective(
		ctx,
		&internalrpcauthorityv1.GetRestoreDirectiveRequest{
			RoleCredentialCompactJws:     roleCompact,
			ObservedCoordinationRevision: agent.observed.Load(),
			CorrelationId:                correlationID,
		},
	)
	if err != nil {
		return nil, "", errors.New("poll restore directive")
	}
	return response, roleCompact, nil
}

// newConnection creates a gRPC connection with cloned controller TLS settings and the required unary interceptor. It does not prove remote readiness, and each caller owns closing the returned connection.
// newConnection создаёт соединение gRPC с копией настроек TLS контроллера и обязательным unary interceptor. Удалённая готовность не доказывается; закрытием возвращённого соединения владеет вызывающий.
func (agent *Agent) newConnection() (*grpc.ClientConn, error) {
	connection, err := grpc.NewClient(
		agent.config.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(agent.config.TLS.Clone())),
		grpc.WithUnaryInterceptor(agent.config.UnaryInterceptor),
	)
	if err != nil {
		return nil, errors.New("connect to restore controller")
	}
	return connection, nil
}

// verifySafeNoDirective requires nonzero coordination/epoch/anchor fields, matching epochs and a sixty-four-character evidence digest, then accepts OPEN without a safe-window timestamp or COMPLETED after its valid safe-window time. It checks digest length rather than hexadecimal content or signature and rejects all other phases.
// verifySafeNoDirective требует ненулевые поля координации, эпохи и опорной ревизии, совпадение эпох и дайджест доказательства длиной шестьдесят четыре символа, затем принимает OPEN без времени безопасного окна либо COMPLETED после корректного времени окна. Проверяется длина дайджеста, а не hex-содержимое или подпись; остальные фазы отклоняются.
func (agent *Agent) verifySafeNoDirective(
	result *internalrpcauthorityv1.NoRestoreDirective,
	transition *internalrpcauthorityv1.RestoreTransition,
) error {
	if result == nil ||
		transition == nil ||
		result.GetCoordinationRevision() == 0 ||
		result.GetRestoreEpoch() == 0 ||
		transition.GetRestoreEpoch() != result.GetRestoreEpoch() ||
		transition.GetAnchorRevision() == 0 ||
		len(transition.GetEvidenceDigestSha256()) != sha256.Size*2 {
		return errors.New("restore external state proof is incomplete")
	}
	switch transition.GetPhase() {
	case internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_OPEN:
		if transition.GetRestoreId() == "" ||
			transition.GetSafeWindowNotBefore() != nil {
			return errors.New("restore OPEN state binding rejected")
		}
	case internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_COMPLETED:
		safeAt := transition.GetSafeWindowNotBefore()
		if transition.GetRestoreId() == "" ||
			safeAt == nil ||
			safeAt.CheckValid() != nil ||
			agent.now().UTC().Before(safeAt.AsTime().UTC()) {
			return errors.New("restore completion safe window is not open")
		}
	default:
		return errors.New("restore external phase keeps workload fenced")
	}
	return nil
}

// Quiescing reports only the local atomic flag set after a verified directive and cleared after a successful safe poll; it is not the external phase or the startup admission-fence state.
// Quiescing сообщает только локальный атомарный флаг, устанавливаемый после проверенной директивы и снимаемый успешным безопасным poll; это не внешняя фаза и не состояние ограждения допуска при запуске.
func (agent *Agent) Quiescing() bool {
	return agent.quiescing.Load()
}

// verifyRoleCredential loads restore-role trust, requires a current signer with the exact purpose and protected type, verifies canonical JWS and validates five-minute timing plus configured workload/role/generation bindings. It returns decoded claims and the exact compact digest without reserving controller state.
// verifyRoleCredential загружает доверие restore-role, требует текущего подписанта с точными назначением и защищённым типом, проверяет канонический JWS, пятиминутное время и настроенные привязки workload, роли и поколений. Возвращаются разобранные claims и дайджест точного compact без резервирования состояния контроллера.
func (agent *Agent) verifyRoleCredential(
	compact string,
) (model.RestoreRoleCredentialClaims, string, error) {
	trust, _, err := snapshot.LoadRestoreRoleTrust(snapshot.RestoreRoleTrustOptions{
		ManifestRootPublicJWKFile:  agent.config.ManifestRootPublicJWKFile,
		ManifestRootMetadataFile:   agent.config.ManifestRootMetadataFile,
		ManifestTrustBundleJWSFile: agent.config.ManifestTrustBundleJWSFile,
		RestoreRoleTrustJWSFile:    agent.config.RestoreRoleTrustJWSFile,
		Now:                        agent.now(),
	})
	if err != nil {
		return model.RestoreRoleCredentialClaims{}, "", errors.New(
			"load restore role credential trust",
		)
	}
	header, err := internalrpcauth.ParseProtectedHeader(compact)
	if err != nil || header.Type != roleCredentialType {
		return model.RestoreRoleCredentialClaims{}, "", errors.New(
			"restore role credential header rejected",
		)
	}
	record, ok := trust[header.KeyID]
	if !ok ||
		record.Status != "CURRENT" ||
		record.Purpose != "RESTORE_ROLE_CREDENTIAL" {
		return model.RestoreRoleCredentialClaims{}, "", errors.New(
			"restore role credential signer rejected",
		)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		record.Key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  roleCredentialType,
			KeyID: header.KeyID,
		},
	)
	if err != nil {
		return model.RestoreRoleCredentialClaims{}, "", errors.New(
			"restore role credential signature rejected",
		)
	}
	var claims model.RestoreRoleCredentialClaims
	now := agent.now().UTC().Truncate(time.Second)
	if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims) != nil ||
		internalrpcauth.ValidateTimes(
			now,
			time.Unix(claims.IssuedAt, 0),
			time.Unix(claims.NotBefore, 0),
			time.Unix(claims.ExpiresAt, 0),
			5*time.Minute,
			clockSkew,
		) != nil ||
		claims.Version != model.ContractVersion ||
		claims.Issuer != roleCredentialIssuer ||
		claims.Audience != controllerAudience ||
		claims.Subject != agent.config.WorkloadID ||
		claims.WorkloadID != agent.config.WorkloadID ||
		claims.WorkloadSPIFFEID != agent.config.WorkloadSPIFFEID ||
		claims.Role != agent.config.Role ||
		claims.WorkloadGeneration != agent.config.WorkloadGeneration ||
		claims.CredentialGeneration != agent.config.CredentialGeneration ||
		claims.ACKKeyGeneration != agent.config.ACKKeyGeneration {
		return model.RestoreRoleCredentialClaims{}, "", errors.New(
			"restore role credential binding rejected",
		)
	}
	return claims, compactDigest(compact), nil
}

// verifyDirective uses one pinned mounted certificate's P-256 key to verify the directive JWS and checks timing, identity, role-credential digest and restore coordination with mandatory stop/drain claims. It does not validate a certificate chain here; the existing implementation dereferences the parse result before checking a DER parsing error.
// verifyDirective использует ключ P-256 одного закреплённого смонтированного сертификата для проверки JWS директивы и проверяет время, идентичность, дайджест role credential и координацию восстановления с обязательными stop/drain claims. Цепочка сертификата здесь не проверяется; существующая реализация обращается к результату разбора до проверки ошибки DER.
func (agent *Agent) verifyDirective(
	compact string,
	credential model.RestoreRoleCredentialClaims,
	credentialDigest string,
) (model.RoleBoundRestoreDirectiveClaims, error) {
	raw, err := readMountedValue(agent.config.ControllerCertificateFile, 64<<10)
	if err != nil {
		return model.RoleBoundRestoreDirectiveClaims{}, errors.New(
			"read restore controller certificate",
		)
	}
	block, remainder := pem.Decode([]byte(raw))
	if block == nil || block.Type != "CERTIFICATE" || len(remainder) != 0 {
		return model.RoleBoundRestoreDirectiveClaims{}, errors.New(
			"decode restore controller certificate",
		)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if err != nil || !ok || publicKey.Curve != elliptic.P256() {
		return model.RoleBoundRestoreDirectiveClaims{}, errors.New(
			"parse restore controller certificate",
		)
	}
	certificateDigest := sha256.Sum256(certificate.Raw)
	key := internalrpcauth.ES256Key{
		KeyID:  "controller-tls-" + hex.EncodeToString(certificateDigest[:12]),
		Public: publicKey,
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  directiveType,
			KeyID: key.KeyID,
		},
	)
	if err != nil {
		return model.RoleBoundRestoreDirectiveClaims{}, errors.New(
			"restore directive signature rejected",
		)
	}
	var claims model.RoleBoundRestoreDirectiveClaims
	now := agent.now().UTC().Truncate(time.Second)
	if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims) != nil ||
		internalrpcauth.ValidateTimes(
			now,
			time.Unix(claims.IssuedAt, 0),
			time.Unix(claims.NotBefore, 0),
			time.Unix(claims.ExpiresAt, 0),
			boundedTTL,
			clockSkew,
		) != nil ||
		claims.Version != model.ContractVersion ||
		claims.Issuer != controllerIssuer ||
		claims.Audience != workloadAudience ||
		claims.Subject != agent.config.WorkloadID ||
		claims.Phase != "QUIESCING" ||
		claims.WorkloadID != agent.config.WorkloadID ||
		claims.WorkloadSPIFFEID != agent.config.WorkloadSPIFFEID ||
		claims.Role != agent.config.Role ||
		claims.WorkloadGeneration != agent.config.WorkloadGeneration ||
		claims.CredentialGeneration != agent.config.CredentialGeneration ||
		claims.RoleCredentialDigestSHA256 != credentialDigest ||
		claims.RestoreID != credential.RestoreID ||
		claims.RestoreEpoch != credential.RestoreEpoch ||
		claims.CoordinationRevision != credential.CoordinationRevision ||
		!claims.StopAcceptingRequired ||
		!claims.DrainInflightRequired {
		return model.RoleBoundRestoreDirectiveClaims{}, errors.New(
			"restore directive binding rejected",
		)
	}
	return claims, nil
}

// compactDigest hashes the exact compact JWS bytes as lowercase SHA-256 hex for credential binding; hashing is neither signature verification nor canonical JSON decoding.
// compactDigest хеширует точные байты compact JWS в строчный hex SHA-256 для привязки credential; хеширование не является проверкой подписи или разбором канонического JSON.
func compactDigest(compact string) string {
	digest := sha256.Sum256([]byte(compact))
	return hex.EncodeToString(digest[:])
}

// deterministicUUID delegates restore ACK and request identities to the shared
// NUL-terminated encoding without changing namespaces or tuple bytes. The ID
// is deterministic SHA-256 truncation, not randomness or proof of authority.
// deterministicUUID передаёт идентичности restore ACK и запросов общему
// кодированию с завершающим NUL, не меняя пространства имён и байты кортежа.
// ID — детерминированное усечение SHA-256, а не случайность или полномочие.
func deterministicUUID(parts ...string) string {
	return value.TerminatedAuthorityUUID(parts...)
}

// readMountedValue reads through the bounded secure-file boundary, trims whitespace and rejects unsafe or empty material with fixed diagnostics. The returned string is not explicitly erased.
// readMountedValue читает через ограниченную безопасную файловую границу, обрезает пробелы и отклоняет небезопасный или пустой материал фиксированной диагностикой. Возвращённая строка явно не стирается.
func readMountedValue(path string, maximum int) (string, error) {
	raw, err := securefile.Read(path, int64(maximum))
	if err != nil {
		return "", errors.New("mounted restore file is unsafe")
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", errors.New("mounted restore file is empty")
	}
	return value, nil
}
