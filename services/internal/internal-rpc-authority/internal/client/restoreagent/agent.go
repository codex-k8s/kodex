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
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	directiveType        = "kodex-internal-rpc-restore-directive+jws"
	ackType              = "kodex-internal-rpc-restore-quiescence-ack+jws"
	workloadAudience     = "urn:kodex:internal-rpc-authority-restore-workload"
	controllerAudience   = "urn:kodex:internal-rpc-authority-restore-controller"
	controllerIssuer     = "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-restore-controller"
	roleCredentialIssuer = "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-publisher"
	roleCredentialType   = "kodex-internal-rpc-restore-role-credential+jws"
	boundedTTL           = 30 * time.Second
	clockSkew            = 5 * time.Second
)

// AuthorityAdmission управляет приёмом запросов и дренированием workload.
type AuthorityAdmission interface {
	SetAvailable(bool)
	SetRestoreBlocked(bool)
	WaitDrained(context.Context) error
	Inflight() int64
	SnapshotState() model.SnapshotState
	ServedStateReady(context.Context) error
}

// Config задаёт идентичность, доверие и материал роли восстановления.
type Config struct {
	Address                    string
	TLS                        *tls.Config
	RoleCredentialVaultPath    string
	ACKPrivateJWKVaultPath     string
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

// SecretReader читает версионированный материал из Vault KV v2.
type SecretReader interface {
	ReadKV2(context.Context, string) (repository.SecretMaterial, bool, error)
}

// Agent обрабатывает директиву и отправляет одноразовое подтверждение.
type Agent struct {
	config           Config
	observed         atomic.Uint64
	quiescing        atomic.Bool
	now              func() time.Time
	directiveFetcher func(context.Context) (*internalrpcauthorityv1.GetRestoreDirectiveResponse, string, error)
}

// New создаёт агент только из полностью связанной конфигурации.
func New(config Config) (*Agent, error) {
	if config.Address == "" ||
		config.TLS == nil ||
		config.TLS.ServerName == "" ||
		config.TLS.InsecureSkipVerify ||
		config.RoleCredentialVaultPath == "" ||
		config.ACKPrivateJWKVaultPath == "" ||
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

// Poll выполняет один цикл обнаружения, остановки, дренирования и подтверждения.
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
	ackMaterial, found, err := agent.config.Delivery.ReadKV2(
		ctx,
		agent.config.ACKPrivateJWKVaultPath,
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

// VerifyStartup синхронно доказывает безопасную внешнюю restore-фазу, не
// открывая admission. Метод обязан завершиться до активации snapshot и bind.
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

func (agent *Agent) getDirective(
	ctx context.Context,
) (*internalrpcauthorityv1.GetRestoreDirectiveResponse, string, error) {
	roleMaterial, found, err := agent.config.Delivery.ReadKV2(
		ctx,
		agent.config.RoleCredentialVaultPath,
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

// Quiescing сообщает, действует ли сейчас ограждение восстановления.
func (agent *Agent) Quiescing() bool {
	return agent.quiescing.Load()
}

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

func compactDigest(compact string) string {
	digest := sha256.Sum256([]byte(compact))
	return hex.EncodeToString(digest[:])
}

func deterministicUUID(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	value := digest.Sum(nil)[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32]
}

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
