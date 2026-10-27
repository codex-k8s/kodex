package platform

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// TestArtifactUploadReceiptReaderPreservesOwnerKeyAndFailureOrder checks exact context/SQL and
// organization/actor/operation/key arguments, two scan destinations, missing/wrapped rows and
// scan/partial failures without leaking driver detail. Intent reuse is rejected before malformed
// JSON; valid/null/unknown-field payloads preserve the existing candidate decode contract. The
// helper never commits or performs the fresh receipt authorization still owned by callers.
// TestArtifactUploadReceiptReaderPreservesOwnerKeyAndFailureOrder проверяет точные context/SQL,
// organization/actor/operation/key arguments, две scan destinations, missing/wrapped rows и
// scan/partial failures без утечки driver detail. Иной intent отклоняется до неверного JSON;
// valid/null/unknown-field payloads сохраняют прежний candidate decode contract. Helper не делает
// commit и свежую receipt authorization, которая остаётся у вызывающих.
func TestArtifactUploadReceiptReaderPreservesOwnerKeyAndFailureOrder(t *testing.T) {
	payload := `{"Ref":"artifact-public","ProjectRef":"project-public","RunRef":"run-public","CurrentRevisionRef":"revision-public","ScanState":"CLEAN","SizeBytes":9,"Version":4}`
	expected := &entity.Artifact{Ref: "artifact-public", ProjectRef: "project-public", RunRef: "run-public",
		CurrentRevisionRef: "revision-public", ScanState: "CLEAN", SizeBytes: 9, Version: 4}
	driverFailure := errors.New("synthetic private driver detail")
	for _, test := range []struct {
		name, digest, raw string
		cause, wantErr    error
		fill              bool
		want              *entity.Artifact
	}{
		{name: "success", digest: "intent-exact", raw: payload, fill: true, want: expected},
		{name: "missing", cause: pgx.ErrNoRows},
		{name: "wrapped-missing", cause: errors.Join(driverFailure, pgx.ErrNoRows)},
		{name: "scan-failure", cause: driverFailure, wantErr: errs.ErrUnavailable},
		{name: "partial-scan-failure", digest: "intent-exact", raw: payload, fill: true, cause: driverFailure, wantErr: errs.ErrUnavailable},
		{name: "intent-reuse", digest: "intent-other", raw: payload, fill: true, wantErr: errs.ErrIdempotencyReuse},
		{name: "intent-before-json", digest: "intent-other", raw: "{", fill: true, wantErr: errs.ErrIdempotencyReuse},
		{name: "malformed-json", digest: "intent-exact", raw: "{", fill: true, wantErr: errs.ErrConflict},
		{name: "null-candidate", digest: "intent-exact", raw: "null", fill: true, want: &entity.Artifact{}},
		{name: "unknown-field-candidate", digest: "intent-exact", raw: `{"Ref":"artifact-public","Unexpected":"compatible"}`, fill: true, want: &entity.Artifact{Ref: "artifact-public"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := scope{organizationID: "organization-internal", actorID: "actor-internal"}
			mutation := value.Mutation{Operation: "artifact.upload", IdempotencyKey: "operation-key", IntentDigest: "intent-exact"}
			tx := &proofResolutionTx{t: t, ctx: t.Context(),
				query: queryArtifactsUploadartifactSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
				args:  []any{current.organizationID, current.actorID, mutation.Operation, mutation.IdempotencyKey}}
			tx.row = proofResolutionRow(func(dest ...any) error {
				if len(dest) != 2 {
					t.Fatal("receipt reader changed scan projection")
				}
				if test.fill {
					*dest[0].(*string) = test.digest
					*dest[1].(*[]byte) = []byte(test.raw)
				}
				return test.cause
			})
			got, err := readArtifactUploadReceipt(tx.ctx, tx, current, mutation)
			if !errors.Is(err, test.wantErr) || !reflect.DeepEqual(got, test.want) {
				t.Fatal("receipt candidate or failure precedence changed")
			}
			if tx.queryCalls != 1 || tx.commitCalls != 0 {
				t.Fatal("receipt reader changed transaction ownership or query boundary")
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("receipt reader exposed driver detail")
			}
		})
	}
}
