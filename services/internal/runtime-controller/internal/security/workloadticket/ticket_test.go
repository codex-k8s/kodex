package workloadticket

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
)

func TestVerifyForAudienceBindsIndependentIssuerAndExactTuple(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	execution := entity.Execution{
		ID: "11111111-1111-4111-8111-111111111111", OrganizationID: "22222222-2222-4222-8222-222222222222",
		ProjectID: "33333333-3333-4333-8333-333333333333", SessionID: "44444444-4444-4444-8444-444444444444",
		TurnID: "55555555-5555-4555-8555-555555555555", Attempt: 2,
		RuntimeRevisionID: "66666666-6666-4666-8666-666666666666", RuntimeRevisionVersion: 3,
		RuntimeRevisionSHA256: strings.Repeat("a", 64), ImmutableInputSHA256: strings.Repeat("b", 64),
		EffectiveRuntimeSHA256: strings.Repeat("c", 64), AgentBindingSHA256: strings.Repeat("d", 64),
		CredentialSnapshotSHA256: strings.Repeat("e", 64), WorkloadTicketSHA256: strings.Repeat("f", 64),
		Version: 7, Fence: 8, GrantGeneration: 9, ResourceClass: enum.ResourceStandard,
		AccessProfile: enum.AccessProjectRead, State: enum.ExecutionPending,
	}
	privateKey := ed25519.NewKeyFromSeed([]byte("runtime-archive-signing-test-key"))
	compact := signTestTicket(t, privateKey, execution, "mattercodex-control-plane-s3-archive", "mattercodex-runtime-s3-archive", now)
	if _, err := VerifyForAudience(compact, privateKey.Public().(ed25519.PublicKey), execution, "mattercodex-runtime-s3-archive", now.Add(time.Minute)); err != nil {
		t.Fatalf("exact archive ticket rejected: %v", err)
	}
	if _, err := Verify(compact, privateKey.Public().(ed25519.PublicKey), execution, now.Add(time.Minute)); err == nil {
		t.Fatal("archive issuer was accepted by Pod admission audience")
	}
	execution.Fence++
	if _, err := VerifyForAudience(compact, privateKey.Public().(ed25519.PublicKey), execution, "mattercodex-runtime-s3-archive", now.Add(time.Minute)); err == nil {
		t.Fatal("stale exact tuple was accepted")
	}
}

func TestVerifyForAudienceRejectsTamperExpiryAndUnknownField(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	execution := entity.Execution{ID: "11111111-1111-4111-8111-111111111111", Version: 1, Fence: 1}
	privateKey := ed25519.NewKeyFromSeed([]byte("runtime-restore-signing-test-key"))
	compact := signTestTicket(t, privateKey, execution, "mattercodex-control-plane-s3-restore", "mattercodex-runtime-s3-restore", now)
	parts := strings.Split(compact, ".")
	tamperedPrefix := "A"
	if parts[1][0] == 'A' {
		tamperedPrefix = "B"
	}
	parts[1] = tamperedPrefix + parts[1][1:]
	if _, err := VerifyForAudience(strings.Join(parts, "."), privateKey.Public().(ed25519.PublicKey), execution, "mattercodex-runtime-s3-restore", now); err == nil {
		t.Fatal("tampered signature was accepted")
	}
	if _, err := VerifyForAudience(compact, privateKey.Public().(ed25519.PublicKey), execution, "mattercodex-runtime-s3-restore", now.Add(6*time.Minute)); err == nil {
		t.Fatal("expired ticket was accepted")
	}
	payloadRaw, _ := base64.RawURLEncoding.DecodeString(strings.Split(compact, ".")[0])
	var payload map[string]any
	_ = json.Unmarshal(payloadRaw, &payload)
	payload["Unexpected"] = true
	payloadRaw, _ = json.Marshal(payload)
	unknown := base64.RawURLEncoding.EncodeToString(payloadRaw) + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payloadRaw))
	if _, err := VerifyForAudience(unknown, privateKey.Public().(ed25519.PublicKey), execution, "mattercodex-runtime-s3-restore", now); err == nil {
		t.Fatal("unknown signed field was accepted")
	}
}

func signTestTicket(t *testing.T, privateKey ed25519.PrivateKey, execution entity.Execution, issuer, audience string, now time.Time) string {
	t.Helper()
	ticketID := sha256.Sum256([]byte(execution.ID + "\x00" + fmt.Sprint(execution.Version) + "\x00" + fmt.Sprint(execution.Fence) + "\x00" + audience))
	payload := Payload{
		Issuer: issuer, Audience: audience, TicketID: hex.EncodeToString(ticketID[:]), IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
		ExecutionID: execution.ID, OrganizationID: execution.OrganizationID, ProjectID: execution.ProjectID,
		SessionID: execution.SessionID, TurnID: execution.TurnID, Attempt: execution.Attempt,
		RuntimeRevisionID: execution.RuntimeRevisionID, RuntimeRevisionSHA256: execution.RuntimeRevisionSHA256,
		RuntimeRevisionVersion: execution.RuntimeRevisionVersion, Version: execution.Version, Fence: execution.Fence,
		GrantGeneration: execution.GrantGeneration, ImmutableInputSHA256: execution.ImmutableInputSHA256,
		EffectiveRuntimeSHA256: execution.EffectiveRuntimeSHA256, AgentBindingSHA256: execution.AgentBindingSHA256,
		CredentialSnapshotSHA256: execution.CredentialSnapshotSHA256, WorkloadTicketSHA256: execution.WorkloadTicketSHA256,
		ResourceClass: string(execution.ResourceClass), ClusterAccessProfile: string(execution.AccessProfile), State: string(execution.State),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, raw))
}
