package readback

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/libs/go/securefile"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	attestationProtectedType = "mattercodex-internal-rpc-readback-attestation+jws"
	attestorAudience         = "urn:mattercodex:internal-rpc-authority-readback-attestor"
	attestationTTL           = 30 * time.Second
)

// Config задаёт один проход проверки обслуживаемого состояния.
type Config struct {
	Address                 string
	TLS                     *tls.Config
	IntentID                string
	CredentialCompact       string
	CredentialJTI           string
	WorkloadID              string
	WorkloadSPIFFEID        string
	Role                    string
	WorkloadGeneration      uint64
	CredentialGeneration    uint64
	PossessionKeyGeneration uint64
	PossessionKey           internalrpcauth.ES256Key
	UnaryInterceptor        grpc.UnaryClientInterceptor
}

// Client выполняет протокол одноразового запроса и подтверждения.
type Client struct {
	config Config
}

// FileConfig задаёт материал проверки, доставленный через файлы.
type FileConfig struct {
	Address                  string
	TLS                      *tls.Config
	IntentIDFile             string
	CredentialCompactFile    string
	CredentialJTIFile        string
	PossessionPrivateJWKFile string
	WorkloadID               string
	WorkloadSPIFFEID         string
	Role                     string
	WorkloadGeneration       uint64
	CredentialGeneration     uint64
	PossessionKeyGeneration  uint64
	UnaryInterceptor         grpc.UnaryClientInterceptor
}

// FileAttestor читает материал из закреплённых файлов перед проверкой.
type FileAttestor struct {
	config FileConfig
}

// VaultConfig задаёт материал проверки, доставленный через Vault.
type VaultConfig struct {
	Address                 string
	TLS                     *tls.Config
	CredentialPath          string
	PossessionPath          string
	Delivery                SecretReader
	WorkloadID              string
	WorkloadSPIFFEID        string
	Role                    string
	WorkloadGeneration      uint64
	CredentialGeneration    uint64
	PossessionKeyGeneration uint64
	UnaryInterceptor        grpc.UnaryClientInterceptor
}

// SecretReader читает версионированный материал из Vault KV v2.
type SecretReader interface {
	ReadKV2(context.Context, string) (repository.SecretMaterial, bool, error)
}

// VaultAttestor получает материал из Vault перед каждой проверкой.
type VaultAttestor struct {
	config VaultConfig
}

// NewVaultAttestor создаёт клиент только из полной конфигурации доверия.
func NewVaultAttestor(config VaultConfig) (*VaultAttestor, error) {
	if config.Address == "" ||
		config.TLS == nil ||
		config.CredentialPath == "" ||
		config.PossessionPath == "" ||
		config.Delivery == nil ||
		config.WorkloadID == "" ||
		config.WorkloadSPIFFEID == "" ||
		config.Role == "" ||
		config.WorkloadGeneration == 0 ||
		config.CredentialGeneration == 0 ||
		config.PossessionKeyGeneration == 0 ||
		config.UnaryInterceptor == nil {
		return nil, errors.New("invalid readback Vault client configuration")
	}
	return &VaultAttestor{config: config}, nil
}

// Attest читает актуальный материал и выполняет независимую проверку.
func (attestor *VaultAttestor) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (string, error) {
	credentialMaterial, found, err := attestor.config.Delivery.ReadKV2(
		ctx,
		attestor.config.CredentialPath,
	)
	if err != nil {
		return "", fmt.Errorf("read normal readback credential: %w", err)
	}
	if !found {
		return "", errors.New("read normal readback credential from Vault")
	}
	possessionMaterial, found, err := attestor.config.Delivery.ReadKV2(
		ctx,
		attestor.config.PossessionPath,
	)
	if err != nil {
		return "", fmt.Errorf("read readback possession key: %w", err)
	}
	if !found {
		return "", errors.New("read readback possession key from Vault")
	}
	key, err := internalrpcauth.ParsePrivateJWK(
		[]byte(possessionMaterial.Data["possession_private_jwk"]),
	)
	if err != nil {
		return "", fmt.Errorf("parse readback possession private key: %w", err)
	}
	client, err := New(Config{
		Address: attestor.config.Address, TLS: attestor.config.TLS,
		IntentID:                credentialMaterial.Data["pinned_intent_id"],
		CredentialCompact:       credentialMaterial.Data["readback_credential_compact_jws"],
		CredentialJTI:           credentialMaterial.Data["readback_credential_jti"],
		WorkloadID:              attestor.config.WorkloadID,
		WorkloadSPIFFEID:        attestor.config.WorkloadSPIFFEID,
		Role:                    attestor.config.Role,
		WorkloadGeneration:      attestor.config.WorkloadGeneration,
		CredentialGeneration:    attestor.config.CredentialGeneration,
		PossessionKeyGeneration: attestor.config.PossessionKeyGeneration,
		PossessionKey:           key,
		UnaryInterceptor:        attestor.config.UnaryInterceptor,
	})
	if err != nil {
		return "", err
	}
	return client.Attest(ctx, state)
}

// NewFileAttestor создаёт клиент для закреплённых файлов материала.
func NewFileAttestor(config FileConfig) (*FileAttestor, error) {
	if config.IntentIDFile == "" ||
		config.CredentialCompactFile == "" ||
		config.CredentialJTIFile == "" ||
		config.PossessionPrivateJWKFile == "" {
		return nil, errors.New("readback material file boundary is invalid")
	}
	if config.Address == "" ||
		config.TLS == nil ||
		config.WorkloadID == "" ||
		config.WorkloadSPIFFEID == "" ||
		config.Role == "" ||
		config.WorkloadGeneration == 0 ||
		config.CredentialGeneration == 0 ||
		config.PossessionKeyGeneration == 0 ||
		config.UnaryInterceptor == nil {
		return nil, errors.New("invalid readback file client configuration")
	}
	return &FileAttestor{config: config}, nil
}

// Attest читает файлы и выполняет независимую проверку.
func (attestor *FileAttestor) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (string, error) {
	intentID, err := readMountedValue(attestor.config.IntentIDFile, 128)
	if err != nil {
		return "", fmt.Errorf("read pinned readback intent: %w", err)
	}
	credential, err := readMountedValue(
		attestor.config.CredentialCompactFile,
		internalrpcauth.MaxCompactJWSBytes,
	)
	if err != nil {
		return "", fmt.Errorf("read normal readback credential: %w", err)
	}
	credentialJTI, err := readMountedValue(attestor.config.CredentialJTIFile, 128)
	if err != nil {
		return "", fmt.Errorf("read normal readback credential jti: %w", err)
	}
	privateRaw, err := readMountedValue(attestor.config.PossessionPrivateJWKFile, 64<<10)
	if err != nil {
		return "", fmt.Errorf("read readback possession private key: %w", err)
	}
	key, err := internalrpcauth.ParsePrivateJWK([]byte(privateRaw))
	if err != nil {
		return "", fmt.Errorf("parse readback possession private key: %w", err)
	}
	client, err := New(Config{
		Address: attestor.config.Address, TLS: attestor.config.TLS,
		IntentID: intentID, CredentialCompact: credential,
		CredentialJTI:           credentialJTI,
		WorkloadID:              attestor.config.WorkloadID,
		WorkloadSPIFFEID:        attestor.config.WorkloadSPIFFEID,
		Role:                    attestor.config.Role,
		WorkloadGeneration:      attestor.config.WorkloadGeneration,
		CredentialGeneration:    attestor.config.CredentialGeneration,
		PossessionKeyGeneration: attestor.config.PossessionKeyGeneration,
		PossessionKey:           key,
		UnaryInterceptor:        attestor.config.UnaryInterceptor,
	})
	if err != nil {
		return "", err
	}
	return client.Attest(ctx, state)
}

func readMountedValue(path string, maximum int) (string, error) {
	raw, err := securefile.Read(path, int64(maximum))
	if err != nil {
		return "", errors.New("mounted readback file is unsafe")
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", errors.New("mounted readback file is empty")
	}
	return value, nil
}

// New создаёт клиент протокола проверки обслуживаемого состояния.
func New(config Config) (*Client, error) {
	if config.Address == "" ||
		config.TLS == nil ||
		config.TLS.ServerName == "" ||
		config.TLS.InsecureSkipVerify ||
		config.IntentID == "" ||
		config.CredentialCompact == "" ||
		config.CredentialJTI == "" ||
		config.WorkloadID == "" ||
		config.WorkloadSPIFFEID == "" ||
		config.Role == "" ||
		config.WorkloadGeneration == 0 ||
		config.CredentialGeneration == 0 ||
		config.PossessionKeyGeneration == 0 ||
		config.PossessionKey.Private == nil ||
		config.UnaryInterceptor == nil {
		return nil, errors.New("invalid readback client configuration")
	}
	return &Client{config: config}, nil
}

// Attest запрашивает одноразовый challenge и отправляет доказательство владения.
func (client *Client) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (string, error) {
	credentialDigestRaw := sha256.Sum256([]byte(client.config.CredentialCompact))
	credentialDigest := hex.EncodeToString(credentialDigestRaw[:])
	// Receipt короче credential, поэтому каждый новый цикл attestation обязан
	// получить независимый challenge. Один request ID переиспользуется только
	// внутри этого вызова для безопасного повтора неоднозначного ответа.
	challengeKey, err := newRequestUUID()
	if err != nil {
		return "", fmt.Errorf("create readback challenge request identifier: %w", err)
	}
	connection, err := grpc.NewClient(
		client.config.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(client.config.TLS.Clone())),
		grpc.WithUnaryInterceptor(client.config.UnaryInterceptor),
	)
	if err != nil {
		return "", fmt.Errorf("connect to readback attestor: %w", err)
	}
	defer connection.Close()
	api := internalrpcauthorityv1.NewAuthorityReadbackAttestorServiceClient(connection)
	challengeRequest := &internalrpcauthorityv1.IssueAttestationChallengeRequest{
		PinnedIntentId:               client.config.IntentID,
		ReadbackCredentialCompactJws: client.config.CredentialCompact,
		IdempotencyKey:               challengeKey,
		CorrelationId:                challengeKey,
	}
	var challenge *internalrpcauthorityv1.IssueAttestationChallengeResponse
	for attempt, delay := range []time.Duration{0, 100 * time.Millisecond, 250 * time.Millisecond} {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			case <-timer.C:
			}
		}
		challenge, err = api.IssueAttestationChallenge(ctx, challengeRequest)
		if err == nil || status.Code(err) != codes.Unavailable || attempt == 2 {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("issue readback attestation challenge: %w", err)
	}
	if challenge.GetKind() !=
		internalrpcauthorityv1.ReadbackAttestationKind_READBACK_ATTESTATION_KIND_SNAPSHOT ||
		challenge.GetChallengeId() == "" ||
		challenge.GetChallengeJti() == "" ||
		challenge.GetChallengeNonce() == "" ||
		challenge.GetChallengeDigestSha256() == "" ||
		challenge.GetReadbackCredentialDigestSha256() != credentialDigest ||
		challenge.GetWorkloadGeneration() != client.config.WorkloadGeneration ||
		challenge.GetCredentialGeneration() != client.config.CredentialGeneration ||
		challenge.GetPossessionKeyGeneration() != client.config.PossessionKeyGeneration ||
		challenge.GetIssuedAt() == nil || challenge.GetIssuedAt().CheckValid() != nil ||
		challenge.GetExpiresAt() == nil || challenge.GetExpiresAt().CheckValid() != nil ||
		!challenge.GetExpiresAt().AsTime().After(challenge.GetIssuedAt().AsTime()) {
		return "", errors.New("readback challenge binding rejected")
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(
		client.config.PossessionKey.PublicOnly(),
	)
	if err != nil {
		return "", fmt.Errorf("fingerprint readback possession key: %w", err)
	}
	// Повтор после неоднозначного ответа обязан подписывать байт-в-байт то же
	// evidence. Server-issued challenge time исключает локальные timestamp из
	// idempotency digest нескольких replica одного workload.
	now := challenge.GetIssuedAt().AsTime().UTC().Truncate(time.Second)
	evidence := model.ReadbackAttestationClaims{
		Version: model.ContractVersion, Issuer: client.config.WorkloadSPIFFEID,
		Audience: attestorAudience, Subject: client.config.WorkloadID,
		JTI:     deterministicUUID("readback-evidence", challenge.GetChallengeId()),
		Purpose: "SNAPSHOT_READBACK", IntentID: client.config.IntentID,
		IntentKind:               "SNAPSHOT",
		IntentRevision:           challenge.GetPinnedIntentRevision(),
		WorkloadID:               client.config.WorkloadID,
		WorkloadSPIFFEID:         client.config.WorkloadSPIFFEID,
		Role:                     client.config.Role,
		WorkloadGeneration:       client.config.WorkloadGeneration,
		CredentialGeneration:     client.config.CredentialGeneration,
		ReadbackCredentialJTI:    client.config.CredentialJTI,
		ReadbackCredentialDigest: credentialDigest,
		PossessionKeyID:          client.config.PossessionKey.KeyID,
		PossessionKeyGeneration:  client.config.PossessionKeyGeneration,
		PossessionKeyThumbprint:  thumbprint,
		SourceRevision:           state.SourceRevision,
		ServedStateDigestSHA256:  state.SourceDigestSHA256,
		ChallengeID:              challenge.GetChallengeId(),
		ChallengeJTI:             challenge.GetChallengeJti(),
		ChallengeNonce:           challenge.GetChallengeNonce(),
		ChallengeDigestSHA256:    challenge.GetChallengeDigestSha256(),
		IssuedAt:                 now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(attestationTTL).Unix(),
	}
	compact, err := internalrpcauth.SignCanonicalJSON(
		evidence,
		client.config.PossessionKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  attestationProtectedType,
			KeyID: client.config.PossessionKey.KeyID,
		},
	)
	if err != nil {
		return "", fmt.Errorf("sign readback attestation evidence: %w", err)
	}
	attestationKey := deterministicUUID(
		"readback-attestation",
		challenge.GetChallengeId(),
		evidence.JTI,
	)
	request := &internalrpcauthorityv1.AttestServedStateRequest{
		PinnedIntentId:                   client.config.IntentID,
		ChallengeId:                      challenge.GetChallengeId(),
		ReadbackCredentialCompactJws:     client.config.CredentialCompact,
		ServedStateAttestationCompactJws: compact,
		IdempotencyKey:                   attestationKey,
		CorrelationId:                    attestationKey,
	}
	var receipt *internalrpcauthorityv1.AttestServedStateResponse
	for attempt, delay := range []time.Duration{0, 100 * time.Millisecond, 250 * time.Millisecond} {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			case <-timer.C:
			}
		}
		receipt, err = api.AttestServedState(ctx, request)
		if err == nil || status.Code(err) != codes.Unavailable || attempt == 2 {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("attest served authority state: %w", err)
	}
	if receipt.GetKind() !=
		internalrpcauthorityv1.ReadbackAttestationKind_READBACK_ATTESTATION_KIND_SNAPSHOT ||
		receipt.GetAttestationReceiptId() == "" ||
		receipt.GetEvidenceDigestSha256() == "" ||
		receipt.GetVerifierGeneration() == 0 {
		return "", errors.New("readback attestation receipt binding rejected")
	}
	return receipt.GetAttestationReceiptId(), nil
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

func newRequestUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}
