package platform

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// sharedHealthSourceFixture uses exact shipped SSH metadata while deliberately retaining NOT_READY; it qualifies no public Create, private material read or external SSH consumer.
// sharedHealthSourceFixture использует exact shipped SSH metadata с сохранением NOT_READY; public Create, private material read либо external SSH consumer не квалифицируются.
func sharedHealthSourceFixture(t *testing.T) (integrationpackage.Package, sharedHealthCredentialSnapshot, time.Time) {
	t.Helper()
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	created := now.Add(-time.Minute)
	configuration, err := json.Marshal(map[string]string{"target_ref": "target_shared_fixture", "service_subject_ref": "sub_shared_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return definitions[integrationpackage.SSHManagedDefinitionKey], sharedHealthCredentialSnapshot{
		configuration: configuration, identityID: "30000000-0000-4000-8000-000000000001", createdAt: &created, serviceRef: "sub_shared_fixture", serviceActive: true,
		identity: entity.IntegrationBrokerIdentityRevision{Ref: "biri_shared_fixture", Revision: 1, Purpose: entity.IntegrationBrokerIdentitySSH, TargetRef: "target_shared_fixture"},
	}, now
}

// TestSharedHealthBrokerSourceKeepsExactPurposeServiceAndTarget exercises the common authoritative selector with metadata only and denies mixed, missing, changed or future sources before any queue transition.
// TestSharedHealthBrokerSourceKeepsExactPurposeServiceAndTarget проверяет единый authoritative selector лишь с metadata и отклоняет mixed, missing, changed либо future sources до queue transition.
func TestSharedHealthBrokerSourceKeepsExactPurposeServiceAndTarget(t *testing.T) {
	definition, stored, now := sharedHealthSourceFixture(t)
	selected, err := sharedHealthCredentialFromSnapshot("org_fixture", "int_fixture", definition, stored, now)
	if err != nil || selected.delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || selected.identity == nil || selected.identity.Ref != stored.identity.Ref || selected.identityID != stored.identityID {
		t.Fatal("current metadata source was lost", err)
	}
	for _, test := range []struct {
		name   string
		change func(*sharedHealthCredentialSnapshot)
		want   error
	}{
		{"missing", func(s *sharedHealthCredentialSnapshot) { s.identityID = "" }, errs.ErrConflict},
		{"token-mixed", func(s *sharedHealthCredentialSnapshot) { s.tokenPresent = true }, errs.ErrUnavailable},
		{"purpose", func(s *sharedHealthCredentialSnapshot) {
			s.identity.Purpose = entity.IntegrationBrokerIdentityKubernetes
		}, errs.ErrUnavailable},
		{"target", func(s *sharedHealthCredentialSnapshot) { s.identity.TargetRef = "target_foreign_fixture" }, errs.ErrUnavailable},
		{"inactive-service", func(s *sharedHealthCredentialSnapshot) { s.serviceActive = false }, errs.ErrConflict},
		{"foreign-service", func(s *sharedHealthCredentialSnapshot) { s.serviceRef = "sub_other_fixture" }, errs.ErrConflict},
		{"missing-time", func(s *sharedHealthCredentialSnapshot) { s.createdAt = nil }, errs.ErrUnavailable},
		{"future-time", func(s *sharedHealthCredentialSnapshot) { future := now.Add(time.Second); s.createdAt = &future }, errs.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := stored
			test.change(&changed)
			actual, err := sharedHealthCredentialFromSnapshot("org_fixture", "int_fixture", definition, changed, now)
			if !errors.Is(err, test.want) || actual.identity != nil {
				t.Fatal("invalid broker source acquired metadata credit", err)
			}
		})
	}
}

// TestSharedHealthNoneAndTokenKeepRepresentation preserves explicit credential-free delivery and the existing token test behavior, without interpreting any broker residue as either representation.
// TestSharedHealthNoneAndTokenKeepRepresentation сохраняет explicit credential-free delivery и прежнее token test behavior, не считая broker residue одной из этих representations.
func TestSharedHealthNoneAndTokenKeepRepresentation(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"email", "gitlab"} {
		definition := definitions[key]
		delivery, known := definition.ConnectionCredentialDelivery()
		if !known {
			t.Fatal("published delivery missing")
		}
		if key == "email" && delivery.Kind != integrationpackage.CredentialDeliveryNone || key == "gitlab" && delivery.Kind != integrationpackage.CredentialDeliveryToken {
			t.Fatal("fixture representation changed")
		}
		for _, present := range []bool{false, true} {
			actual, err := sharedHealthCredentialFromSnapshot("org_fixture", "int_fixture", definition, sharedHealthCredentialSnapshot{tokenPresent: present}, time.Now())
			if key == "email" && present {
				if !errors.Is(err, errs.ErrUnavailable) || actual != (sharedHealthCredentialSource{}) {
					t.Fatal("EMAIL NONE accepted generic token residue", err)
				}
				continue
			}
			if err != nil || actual.delivery != delivery || actual.identity != nil {
				t.Fatal("NONE/TOKEN acquired broker metadata or required another selector", err)
			}
		}
		for _, change := range []func(*sharedHealthCredentialSnapshot){func(s *sharedHealthCredentialSnapshot) { s.identityID = "forbidden_id" }, func(s *sharedHealthCredentialSnapshot) { s.identity.Purpose = entity.IntegrationBrokerIdentitySSH }, func(s *sharedHealthCredentialSnapshot) { s.serviceRef = "sub_foreign" }} {
			var stored sharedHealthCredentialSnapshot
			change(&stored)
			if _, err := sharedHealthCredentialFromSnapshot("org_fixture", "int_fixture", definition, stored, time.Now()); !errors.Is(err, errs.ErrUnavailable) {
				t.Fatal("mixed representation became ordinary delivery", err)
			}
		}
	}
}

// sharedHealthFaultRow returns a successful-query scan fault without needing an aborted PostgreSQL transaction, so zero bool fields cannot manufacture a semantic source denial.
// sharedHealthFaultRow возвращает Scan fault после successful query без aborted PostgreSQL transaction; zero bool fields не создают semantic source denial.
type sharedHealthFaultRow struct{ failure error }

// Scan propagates only the controlled infrastructure failure to the metadata reader.
// Scan передаёт metadata reader лишь controlled infrastructure failure.
func (row sharedHealthFaultRow) Scan(...any) error { return row.failure }

// sharedHealthFaultTx embeds the unused transaction methods and replaces only the exact metadata read; unexpected additional operations fail instead of silently succeeding.
// sharedHealthFaultTx встраивает unused transaction methods и заменяет лишь exact metadata read; неожиданные дополнительные операции не получают ложного success.
type sharedHealthFaultTx struct {
	pgx.Tx
	failure error
}

// QueryRow supplies the controlled failed Scan without a fake token or broker identity result.
// QueryRow возвращает controlled failed Scan без fake token либо broker identity result.
func (tx sharedHealthFaultTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return sharedHealthFaultRow{failure: tx.failure}
}

// TestSharedHealthSourceScanFailureIsUnavailable preserves infrastructure classification before inspecting zero-value representation or active-service fields; no owner transition is performed by this unit test.
// TestSharedHealthSourceScanFailureIsUnavailable сохраняет infrastructure classification до проверки zero-value representation либо active-service fields; owner transition этим unit test не выполняется.
func TestSharedHealthSourceScanFailureIsUnavailable(t *testing.T) {
	definition, _, _ := sharedHealthSourceFixture(t)
	for _, failure := range []error{errors.New("synthetic successful query scan failure"), pgx.ErrNoRows} {
		var repository Repository
		result, err := repository.readSharedHealthCredentialSourceTx(t.Context(), sharedHealthFaultTx{failure: failure}, "org_fixture", "30000000-0000-4000-8000-000000000001", "int_fixture", definition)
		if !errors.Is(err, errs.ErrUnavailable) || result.identity != nil {
			t.Fatal("scan fault became semantic owner transition", err)
		}
	}
}
