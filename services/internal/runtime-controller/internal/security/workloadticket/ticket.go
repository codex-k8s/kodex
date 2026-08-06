// Package workloadticket проверяет server-owned one-time ticket независимо от
// credential broker. Broker получает только public verifier material.
package workloadticket

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
)

type Payload struct {
	Issuer, Audience, TicketID                                string
	IssuedAt, ExpiresAt                                       time.Time
	ExecutionID, OrganizationID, ProjectID, SessionID, TurnID string
	Attempt                                                   uint32
	RuntimeRevisionID, RuntimeRevisionSHA256                  string
	RuntimeRevisionVersion, Version, Fence, GrantGeneration   uint64
	ImmutableInputSHA256, EffectiveRuntimeSHA256              string
	AgentBindingSHA256, CredentialSnapshotSHA256              string
	WorkloadTicketSHA256, ResourceClass, ClusterAccessProfile string
	CodexDeliveryRecoverySourceExecutionID                    string
	RestoreOperationID, RestoreSourceAuthoritySHA256          string
	RestoreOperationGeneration                                uint64
	State                                                     string
}

func DecodePublicKey(raw []byte) (ed25519.PublicKey, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return nil, errors.New("runtime workload ticket public key is invalid")
	}
	return ed25519.PublicKey(decoded), nil
}

func Verify(compact string, publicKey ed25519.PublicKey, execution entity.Execution, now time.Time) (Payload, error) {
	return VerifyForAudience(compact, publicKey, execution, "mattercodex-runtime-workload-admission", now)
}

func VerifyForAudience(compact string, publicKey ed25519.PublicKey, execution entity.Execution, audience string, now time.Time) (Payload, error) {
	expectedIssuer := map[string]string{
		"mattercodex-runtime-workload-admission": "mattercodex-control-plane-workload-admission",
		"mattercodex-runtime-s3-archive":         "mattercodex-control-plane-s3-archive",
		"mattercodex-runtime-s3-restore":         "mattercodex-control-plane-s3-restore",
	}[audience]
	if expectedIssuer == "" {
		return Payload{}, errors.New("runtime workload ticket audience is invalid")
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 2 {
		return Payload{}, errors.New("runtime workload ticket is invalid")
	}
	payloadRaw, payloadErr := base64.RawURLEncoding.DecodeString(parts[0])
	signature, signatureErr := base64.RawURLEncoding.DecodeString(parts[1])
	if payloadErr != nil || signatureErr != nil || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(publicKey, payloadRaw, signature) {
		return Payload{}, errors.New("runtime workload ticket signature is invalid")
	}
	var payload Payload
	decoder := json.NewDecoder(bytes.NewReader(payloadRaw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return Payload{}, errors.New("runtime workload ticket payload is invalid")
	}
	expectedTicketID := sha256.Sum256([]byte(execution.ID + "\x00" + fmt.Sprint(execution.Version) + "\x00" + fmt.Sprint(execution.Fence) + "\x00" + audience))
	now = now.UTC()
	if payload.Issuer != expectedIssuer || payload.Audience != audience ||
		payload.TicketID != hex.EncodeToString(expectedTicketID[:]) ||
		payload.IssuedAt.IsZero() || payload.ExpiresAt.IsZero() ||
		payload.ExpiresAt.Sub(payload.IssuedAt) > 5*time.Minute ||
		now.Before(payload.IssuedAt.Add(-30*time.Second)) || !now.Before(payload.ExpiresAt) ||
		payload.ExecutionID != execution.ID || payload.OrganizationID != execution.OrganizationID ||
		payload.ProjectID != execution.ProjectID || payload.SessionID != execution.SessionID ||
		payload.TurnID != execution.TurnID || payload.Attempt != execution.Attempt ||
		payload.RuntimeRevisionID != execution.RuntimeRevisionID ||
		payload.RuntimeRevisionVersion != execution.RuntimeRevisionVersion ||
		payload.RuntimeRevisionSHA256 != execution.RuntimeRevisionSHA256 ||
		payload.Version != execution.Version || payload.Fence != execution.Fence ||
		payload.GrantGeneration != execution.GrantGeneration ||
		payload.ImmutableInputSHA256 != execution.ImmutableInputSHA256 ||
		payload.EffectiveRuntimeSHA256 != execution.EffectiveRuntimeSHA256 ||
		payload.AgentBindingSHA256 != execution.AgentBindingSHA256 ||
		payload.CredentialSnapshotSHA256 != execution.CredentialSnapshotSHA256 ||
		payload.WorkloadTicketSHA256 != execution.WorkloadTicketSHA256 ||
		payload.ResourceClass != string(execution.ResourceClass) ||
		payload.ClusterAccessProfile != string(execution.AccessProfile) ||
		payload.CodexDeliveryRecoverySourceExecutionID != execution.CodexDeliveryRecoverySourceExecutionID ||
		payload.RestoreOperationID != execution.RestoreOperationID ||
		payload.RestoreOperationGeneration != execution.RestoreOperationGeneration ||
		payload.RestoreSourceAuthoritySHA256 != execution.RestoreSourceAuthoritySHA256 ||
		payload.State != string(execution.State) {
		return Payload{}, errors.New("runtime workload ticket tuple mismatch")
	}
	return payload, nil
}
