package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// TestSSHOwnerInfrastructureCannotBecomeAuthorityLoss prevents unavailable dependencies, cancellation and joined infrastructure errors from manufacturing terminal denial or hidden-resource outcomes.
// TestSSHOwnerInfrastructureCannotBecomeAuthorityLoss не позволяет unavailable dependency, cancellation и joined infrastructure errors превращаться в terminal denial либо hidden-resource outcomes.
func TestSSHOwnerInfrastructureCannotBecomeAuthorityLoss(t *testing.T) {
	for _, err := range []error{errs.ErrUnavailable, context.Canceled, context.DeadlineExceeded, errs.ErrInvalid, errors.New("synthetic dependency failure"), errors.Join(errs.ErrUnavailable, errs.ErrForbidden)} {
		if brokerIdentitySetupAuthorityLost(err) || !errors.Is(sshSetupAccessError(err), errs.ErrUnavailable) {
			t.Fatal("infrastructure failure became semantic authority loss")
		}
	}
	for _, err := range []error{errs.ErrForbidden, errs.ErrNotFound, errs.ErrConflict, errs.ErrVersionMismatch} {
		wrapped := fmt.Errorf("synthetic bounded wrapper: %w", err)
		if !brokerIdentitySetupAuthorityLost(wrapped) || !errors.Is(sshSetupAccessError(wrapped), errs.ErrNotFound) {
			t.Fatal("definitive denial lost semantic identity")
		}
	}
	if brokerIdentitySetupAuthorityLost(nil) || sshSetupAccessError(nil) != nil {
		t.Fatal("nil failure acquired semantics")
	}
}

// TestSSHUploadRecoveryRequiresReportedOriginalUnknown distinguishes genuine durable ambiguity from an expired or abandoned write; neither handler retries nor terminal setup state may mint observation credit.
// TestSSHUploadRecoveryRequiresReportedOriginalUnknown отличает durable ambiguity от expired либо abandoned write; handler retry и terminal setup не создают observation credit.
func TestSSHUploadRecoveryRequiresReportedOriginalUnknown(t *testing.T) {
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	row := sshUploadRow{state: enum.SSHIdentitySetupPrepared, now: now, input: sshSetupImmutableInput{ExpiresAt: now.Add(time.Minute)}}
	if mode, err := (sshRecoveryEligibility{}).mode(row); err != nil || mode != enum.SSHIdentityUploadWrite {
		t.Fatalf("initial grant: %s %v", mode, err)
	}
	row.state = enum.SSHIdentitySetupAttempting
	row.authenticationDigest = strings.Repeat("a", 64)
	if _, err := (sshRecoveryEligibility{spent: true, generation: 1}).mode(row); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("unreported expired write acquired observation credit")
	}
	info := sshRecoveryEligibility{spent: true, unknown: true, generation: 1}
	if _, err := info.mode(row); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("old WRITE receipt restored an unreported OBSERVE grant")
	}
	row.state = enum.SSHIdentitySetupUnknown
	if _, err := (sshRecoveryEligibility{spent: true, generation: 2}).mode(row); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("missing latest completion restored an old UNKNOWN grant")
	}
	if mode, err := info.mode(row); err != nil || mode != enum.SSHIdentityUploadObserve {
		t.Fatalf("explicit unknown receipt: %s %v", mode, err)
	}
	for _, state := range []enum.SSHIdentitySetupState{enum.SSHIdentitySetupAcknowledgementRejected, enum.SSHIdentitySetupQuarantined, enum.SSHIdentitySetupUnreported, enum.SSHIdentitySetupCompleted, enum.SSHIdentitySetupCancelled, enum.SSHIdentitySetupExpired} {
		row.state = state
		if _, err := info.mode(row); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("terminal %s acquired grant", state)
		}
	}
	row.state = enum.SSHIdentitySetupUnknown
	for _, closed := range []sshRecoveryEligibility{{spent: true, unknown: true, active: true, generation: 1}, {spent: true, unknown: true, known: true, generation: 1}, {spent: true, unknown: true, rejected: true, generation: 1}, {spent: true, unknown: true, generation: 8}} {
		if _, err := closed.mode(row); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("closed material/lease acquired grant")
		}
	}
	row.now = row.input.ExpiresAt
	if _, err := info.mode(row); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("deadline equality acquired grant")
	}
}

// TestSSHCompletionOutcomeUnionAndIntegrity requires a single explicit outcome and makes locator or witness changes produce distinct immutable receipt identities.
// TestSSHCompletionOutcomeUnionAndIntegrity требует single explicit outcome; изменение locator либо witness даёт другую immutable receipt identity.
func TestSSHCompletionOutcomeUnionAndIntegrity(t *testing.T) {
	locator := managedssh.IdentityLocator{Purpose: managedssh.IdentityPurpose, IdentityRevisionRef: "bki_example_123", TargetRef: "target_example_123", Revision: 1, Namespace: sshIdentityMaterialNamespace, ContentSHA256: strings.Repeat("a", 64), AuthenticationSHA256: strings.Repeat("b", 64), SecretUID: "uid-one", SecretResourceVersion: "17"}
	locator.SecretName = managedssh.IdentitySecretName(locator.IdentityRevisionRef, locator.Revision)
	first, body, digest, err := sshCompletionReceipt(command.SSHIdentityUploadComplete{Identity: &locator})
	if err != nil || first.Outcome != "KNOWN" || !strings.Contains(body, "uid-one") || !managedssh.Digest(digest) {
		t.Fatal("valid material receipt rejected")
	}
	locator.SecretUID = "uid-two"
	_, _, changed, err := sshCompletionReceipt(command.SSHIdentityUploadComplete{Identity: &locator})
	if err != nil || changed == digest {
		t.Fatal("replacement UID lost receipt identity")
	}
	witness := &command.SSHIdentityRejectedAcknowledgement{ResponseSHA256: strings.Repeat("c", 64), ObservedUID: "seen-uid"}
	for _, input := range []command.SSHIdentityUploadComplete{{}, {Unknown: true, Identity: &locator}, {Unknown: true, Rejected: witness}, {Identity: &locator, Rejected: witness}, {Rejected: &command.SSHIdentityRejectedAcknowledgement{}}, {Rejected: &command.SSHIdentityRejectedAcknowledgement{ResponseSHA256: strings.Repeat("c", 64), ObservedUID: strings.Repeat("x", 257)}}} {
		if _, _, _, err := sshCompletionReceipt(input); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("ambiguous or malformed outcome accepted")
		}
	}
	if receipt, _, _, err := sshCompletionReceipt(command.SSHIdentityUploadComplete{Unknown: true}); err != nil || receipt.Outcome != "UNKNOWN" {
		t.Fatal("explicit unknown rejected")
	}
	if receipt, _, _, err := sshCompletionReceipt(command.SSHIdentityUploadComplete{Rejected: witness}); err != nil || receipt.Outcome != "ACK_REJECTED" {
		t.Fatal("rejected success conflated with transport ambiguity")
	}
}

// sshOwnerRowFixture implements only the positional database row boundary needed to test independently stored coordinates against the canonical snapshot body.
// sshOwnerRowFixture реализует лишь positional database row boundary для проверки independently stored coordinates против canonical snapshot body.
type sshOwnerRowFixture []any

// Scan assigns exact typed fixture columns and fails on width or type drift instead of silently accepting a changed production projection.
// Scan назначает exact typed fixture columns и отказывает при width либо type drift вместо принятия изменённой production projection.
func (row sshOwnerRowFixture) Scan(dest ...any) error {
	if len(dest) != len(row) {
		return errors.New("fixture projection width mismatch")
	}
	for i, value := range row {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Pointer || target.IsNil() || !reflect.TypeOf(value).AssignableTo(target.Elem().Type()) {
			return errors.New("fixture projection type mismatch")
		}
		target.Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

// sshOwnerFixture builds independent SQL coordinates and a hashed owner snapshot; callers can corrupt either channel without deriving one from the other during decoding.
// sshOwnerFixture создаёт independent SQL coordinates и hashed owner snapshot; caller может повредить любой канал без вывода одного из другого при decoding.
func sshOwnerFixture(t *testing.T) sshOwnerRowFixture {
	t.Helper()
	input := sshSetupImmutableInput{SetupRef: "ssu_example_123", OrganizationID: "10000000-0000-4000-8000-000000000001", ActorID: "10000000-0000-4000-8000-000000000002", ConnectionID: "10000000-0000-4000-8000-000000000003", ConnectionRef: "conn_example_123", IdentityID: "10000000-0000-4000-8000-000000000004", IdentityRef: "bki_example_123", Namespace: sshIdentityMaterialNamespace, SubjectID: "10000000-0000-4000-8000-000000000002", BindingID: "10000000-0000-4000-8000-000000000005", BindingRef: "ipc_example_123", TargetRef: "target_example_123", DefinitionDigest: strings.Repeat("a", 64), ConfigurationDigest: strings.Repeat("b", 64), ContentDigest: strings.Repeat("c", 64), IdentityRevision: 1, ConnectionVersion: 2, BindingVersion: 3, BindingGeneration: 4, Policy: entity.IntegrationCredentialInitiatorRequired, ContentBytes: 123, ExpiresAt: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)}
	digest, err := input.digest()
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return sshOwnerRowFixture{"10000000-0000-4000-8000-000000000006", string(body), digest, enum.SSHIdentitySetupPrepared, int64(1), input.ExpiresAt, input.ExpiresAt.Add(-time.Minute), "", "org", "user", true, "ACTIVE", input.ConnectionID, input.ConnectionRef, input.OrganizationID, input.ActorID, input.IdentityID, input.IdentityRef, input.SubjectID, input.BindingID, input.BindingRef, input.TargetRef, input.IdentityRevision, input.Policy, input.Namespace, input.ConnectionVersion, input.BindingVersion, input.BindingGeneration, input.DefinitionDigest, input.ConfigurationDigest, input.ContentDigest, input.ContentBytes, input.SetupRef}
}

// TestSSHPrivateOwnerSnapshotRejectsAliasesAndForeignCoordinates covers duplicate/case-aliased JSON, foreign tenant/target/setup rows and namespace substitution before any owner field becomes execution authority.
// TestSSHPrivateOwnerSnapshotRejectsAliasesAndForeignCoordinates проверяет duplicate/case-aliased JSON, foreign tenant/target/setup и namespace substitution до использования owner field как execution authority.
func TestSSHPrivateOwnerSnapshotRejectsAliasesAndForeignCoordinates(t *testing.T) {
	row := sshOwnerFixture(t)
	if _, err := scanSSHUploadRow(row); err != nil {
		t.Fatal("canonical independent owner row rejected", err)
	}
	for _, index := range []int{12, 14, 15, 16, 17, 18, 19, 20, 21, 24, 28, 32} {
		changed := append(sshOwnerRowFixture(nil), row...)
		if index == 24 {
			changed[index] = "another-namespace"
		} else if index == 28 {
			changed[index] = strings.Repeat("d", 64)
		} else {
			changed[index] = "foreign_12345678"
		}
		if _, err := scanSSHUploadRow(changed); err == nil {
			t.Fatalf("foreign column %d accepted", index)
		}
	}
	for _, body := range []string{strings.Replace(row[1].(string), "\"SetupRef\":", "\"setupref\":", 1), strings.Replace(row[1].(string), "{", "{\"SetupRef\":\"ssu_example_123\",", 1), strings.Replace(row[1].(string), "{", "{\"FutureOwner\":null,", 1), "null", row[1].(string) + " "} {
		changed := append(sshOwnerRowFixture(nil), row...)
		changed[1] = body
		if _, err := scanSSHUploadRow(changed); err == nil {
			t.Fatal("noncanonical private snapshot accepted")
		}
	}
}
