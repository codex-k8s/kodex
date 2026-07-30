package readback

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	attestationProtectedType = "mattercodex-internal-rpc-readback-attestation+jws"
	attestorAudience         = "urn:mattercodex:internal-rpc-authority-readback-attestor"
	attestationTTL           = 30 * time.Second
)

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

type Client struct {
	config Config
	now    func() time.Time
}

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

type FileAttestor struct {
	config FileConfig
}

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

type SecretReader interface {
	ReadKV2(context.Context, string) (repository.SecretMaterial, bool, error)
}

type VaultAttestor struct {
	config VaultConfig
}

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

func (attestor *VaultAttestor) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (string, error) {
	credentialMaterial, found, err := attestor.config.Delivery.ReadKV2(
		ctx,
		attestor.config.CredentialPath,
	)
	if err != nil || !found {
		return "", errors.New("read normal readback credential from Vault")
	}
	possessionMaterial, found, err := attestor.config.Delivery.ReadKV2(
		ctx,
		attestor.config.PossessionPath,
	)
	if err != nil || !found {
		return "", errors.New("read readback possession key from Vault")
	}
	key, err := internalrpcauth.ParsePrivateJWK(
		[]byte(possessionMaterial.Data["possession_private_jwk"]),
	)
	if err != nil {
		return "", errors.New("parse readback possession private key")
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

func (attestor *FileAttestor) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (string, error) {
	intentID, err := readMountedValue(attestor.config.IntentIDFile, 128)
	if err != nil {
		return "", errors.New("read pinned readback intent")
	}
	credential, err := readMountedValue(
		attestor.config.CredentialCompactFile,
		internalrpcauth.MaxCompactJWSBytes,
	)
	if err != nil {
		return "", errors.New("read normal readback credential")
	}
	credentialJTI, err := readMountedValue(attestor.config.CredentialJTIFile, 128)
	if err != nil {
		return "", errors.New("read normal readback credential jti")
	}
	privateRaw, err := readMountedValue(attestor.config.PossessionPrivateJWKFile, 64<<10)
	if err != nil {
		return "", errors.New("read readback possession private key")
	}
	key, err := internalrpcauth.ParsePrivateJWK([]byte(privateRaw))
	if err != nil {
		return "", errors.New("parse readback possession private key")
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
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(filepath.Dir(path), resolved)
	if err != nil ||
		relative == ".." ||
		filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, "../") {
		return "", errors.New("mounted readback file escapes its directory")
	}
	info, err := os.Stat(resolved)
	if err != nil ||
		!info.Mode().IsRegular() ||
		info.Mode().Perm()&0o007 != 0 ||
		info.Size() <= 0 ||
		info.Size() > int64(maximum) {
		return "", errors.New("mounted readback file is unsafe")
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", errors.New("mounted readback file is empty")
	}
	return value, nil
}

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
	return &Client{config: config, now: time.Now}, nil
}

func (client *Client) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (string, error) {
	credentialDigestRaw := sha256.Sum256([]byte(client.config.CredentialCompact))
	credentialDigest := hex.EncodeToString(credentialDigestRaw[:])
	challengeKey := deterministicUUID(
		"readback-challenge",
		client.config.IntentID,
		credentialDigest,
		state.SourceDigestSHA256,
	)
	connection, err := grpc.DialContext(
		ctx,
		client.config.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(client.config.TLS.Clone())),
		grpc.WithUnaryInterceptor(client.config.UnaryInterceptor),
		grpc.WithBlock(),
	)
	if err != nil {
		return "", errors.New("connect to readback attestor")
	}
	defer connection.Close()
	api := internalrpcauthorityv1.NewAuthorityReadbackAttestorServiceClient(connection)
	challenge, err := api.IssueAttestationChallenge(
		ctx,
		&internalrpcauthorityv1.IssueAttestationChallengeRequest{
			PinnedIntentId:               client.config.IntentID,
			ReadbackCredentialCompactJws: client.config.CredentialCompact,
			IdempotencyKey:               challengeKey,
			CorrelationId:                challengeKey,
		},
	)
	if err != nil {
		return "", errors.New("issue readback attestation challenge")
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
		challenge.GetPossessionKeyGeneration() != client.config.PossessionKeyGeneration {
		return "", errors.New("readback challenge binding rejected")
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(
		client.config.PossessionKey.PublicOnly(),
	)
	if err != nil {
		return "", errors.New("fingerprint readback possession key")
	}
	now := client.now().UTC().Truncate(time.Second)
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
		return "", errors.New("sign readback attestation evidence")
	}
	attestationKey := deterministicUUID(
		"readback-attestation",
		challenge.GetChallengeId(),
		evidence.JTI,
	)
	receipt, err := api.AttestServedState(
		ctx,
		&internalrpcauthorityv1.AttestServedStateRequest{
			PinnedIntentId:                   client.config.IntentID,
			ChallengeId:                      challenge.GetChallengeId(),
			ReadbackCredentialCompactJws:     client.config.CredentialCompact,
			ServedStateAttestationCompactJws: compact,
			IdempotencyKey:                   attestationKey,
			CorrelationId:                    attestationKey,
		},
	)
	if err != nil {
		return "", errors.New("attest served authority state")
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
