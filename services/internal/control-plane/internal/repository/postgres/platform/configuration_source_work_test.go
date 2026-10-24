package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// TestSourceActionDigestPreservesWireEnvelope compares hashes of six literal JSON oracles with the
// real source digest: every lease coordinate/order, zero-time shape, nil content, HTML escaping,
// byte encoding and the historical nil-byte hash on marshal failure. No credential or provider is used.
// TestSourceActionDigestPreservesWireEnvelope сравнивает hashes шести буквальных JSON oracles с
// настоящим source digest: все lease координаты/порядок, zero-time форму, nil content, HTML escaping,
// byte encoding и прежний hash nil bytes при marshal failure. Credential либо provider не используются.
func TestSourceActionDigestPreservesWireEnvelope(t *testing.T) {
	stamp := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	lease := entity.ManagedConfigurationSourceLease{WorkRef: "work", Claimant: "worker", Fence: "fence", SourceGeneration: 7, Attempt: 2, ClaimGeneration: 3, ExpiresAt: stamp}
	leaseJSON := `{"WorkRef":"work","Claimant":"worker","Fence":"fence","SourceGeneration":7,"Attempt":2,"ClaimGeneration":3,"ExpiresAt":"2024-01-02T03:04:05Z"}`
	for _, test := range []struct {
		name, action, wire string
		lease              entity.ManagedConfigurationSourceLease
		content            any
	}{
		{"failure", "FAIL", `{"Action":"FAIL","Lease":` + leaseJSON + `,"Content":"UNAVAILABLE"}`, lease, "UNAVAILABLE"},
		{"nil", "COMPLETE", `{"Action":"COMPLETE","Lease":` + leaseJSON + `,"Content":null}`, lease, nil},
		{"HTML and nil slice", "COMPLETE", `{"Action":"COMPLETE","Lease":` + leaseJSON + `,"Content":{"html":"\u003ctag\u003e\u0026","items":null}}`, lease, map[string]any{"html": "<tag>&", "items": []string(nil)}},
		{"typed byte content", "COMPLETE", `{"Action":"COMPLETE","Lease":` + leaseJSON + `,"Content":{"Commit":"commit","Digest":"digest","Ancestry":"INITIAL","Content":"AP8="}}`, lease, struct {
			Commit, Digest, Ancestry string
			Content                  []byte
		}{"commit", "digest", "INITIAL", []byte{0, 255}}},
		{"zero lease", "FAIL", `{"Action":"FAIL","Lease":{"WorkRef":"","Claimant":"","Fence":"","SourceGeneration":0,"Attempt":0,"ClaimGeneration":0,"ExpiresAt":"0001-01-01T00:00:00Z"},"Content":null}`, entity.ManagedConfigurationSourceLease{}, nil},
		{"marshal error", "COMPLETE", "", lease, func() {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			sum := sha256.Sum256([]byte(test.wire))
			if sourceActionDigest(test.action, test.lease, test.content) != hex.EncodeToString(sum[:]) {
				t.Fatal("source action digest changed its exact JSON envelope or marshal-error fallback")
			}
		})
	}
}

// TestSourceWorkLockPreservesSnapshotBoundary checks the exact fourteen-column tenant/work query
// against thirteen synthetic outcomes, including strict unknown/trailing JSON rejection, typed
// digest/work/configuration/deadline binding, receipt errors, nullable expiry and zero result on
// failure. It exercises the real lock scanner without opening a database or granting work authority.
// TestSourceWorkLockPreservesSnapshotBoundary проверяет точный четырнадцатиколоночный tenant/work
// query в тринадцати synthetic outcomes, включая отказ unknown/trailing JSON, typed
// digest/work/configuration/deadline binding, receipt errors, nullable expiry и нулевой результат при
// ошибке. Использует настоящий lock scanner без БД либо выдачи work authority.
func TestSourceWorkLockPreservesSnapshotBoundary(t *testing.T) {
	stamp := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	deadline, expires := stamp.Add(15*time.Minute), stamp.Add(time.Minute)
	failure := errors.New("synthetic source row failure")
	for _, name := range []string{"valid", "whitespace", "missing", "driver", "invalid snapshot", "unknown field", "trailing value", "digest", "work ref", "configuration ref", "deadline", "receipts", "nullable expiry"} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			work := entity.ManagedConfigurationSourceWork{Lease: entity.ManagedConfigurationSourceLease{WorkRef: "work-ref", SourceGeneration: 7}, ConfigurationRef: "configuration-ref", SourceRef: "source-ref", Kind: "ROLE_IMAGE", MaximumContentBytes: 256 << 10, Deadline: deadline, PublicConfiguration: map[string]any{"fixture": true}, DefinitionPackage: []byte("{}")}
			snapshot := work
			switch name {
			case "work ref":
				snapshot.Lease.WorkRef = "other-work"
			case "configuration ref":
				snapshot.ConfigurationRef = "other-configuration"
			case "deadline":
				snapshot.Deadline = deadline.Add(time.Second)
			}
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal("encode synthetic source snapshot")
			}
			sum := sha256.Sum256(raw)
			digest := hex.EncodeToString(sum[:])
			receipts := []byte(`{"1/2":{"Action":"FAIL","Digest":"receipt-digest","Result":{"Ref":"source-ref","Version":6}}}`)
			var expiry *time.Time = &expires
			var rowError, wantError error
			switch name {
			case "whitespace":
				raw = append(append([]byte(" \n"), raw...), '\n')
			case "missing":
				rowError, wantError = pgx.ErrNoRows, errs.ErrNotFound
			case "driver":
				rowError, wantError = failure, errs.ErrUnavailable
			case "invalid snapshot":
				raw = []byte("{")
				wantError = errs.ErrUnavailable
			case "unknown field":
				raw = []byte(strings.TrimSuffix(string(raw), "}") + `,"UnknownFixtureField":true}`)
				wantError = errs.ErrUnavailable
			case "trailing value":
				raw = append(raw, []byte(` {}`)...)
				wantError = errs.ErrUnavailable
			case "digest":
				digest = strings.Repeat("0", 64)
				wantError = errs.ErrUnavailable
			case "work ref", "configuration ref", "deadline":
				wantError = errs.ErrUnavailable
			case "receipts":
				receipts = []byte("{")
				wantError = errs.ErrUnavailable
			case "nullable expiry":
				expiry = nil
			}
			tx := &proofResolutionTx{t: t, ctx: ctx, query: queryConfigurationSourceLockWork, args: []any{"tenant-id", "work-ref"}}
			tx.row = proofResolutionRow(func(dest ...any) error {
				if len(dest) != 14 {
					t.Fatal("source lock column count changed")
				}
				if rowError != nil {
					return rowError
				}
				*dest[0].(*string), *dest[1].(*string) = "work-id", "CLAIMED"
				*dest[2].(*int64), *dest[3].(*int64) = 2, 3
				*dest[4].(*string), *dest[5].(*string) = "worker", "fence"
				*dest[6].(**time.Time), *dest[7].(*time.Time) = expiry, deadline
				*dest[8].(*[]byte), *dest[9].(*string), *dest[10].(*[]byte) = raw, digest, receipts
				*dest[11].(*string), *dest[12].(*string), *dest[13].(*string) = "configuration-ref", "actor-ref", "organization-ref"
				return nil
			})
			got, err := lockSourceWork(ctx, tx, scope{organizationID: "tenant-id"}, "work-ref")
			want := sourceWorkRow{}
			if wantError == nil {
				lease := entity.ManagedConfigurationSourceLease{WorkRef: "work-ref", SourceGeneration: 7, Attempt: 2, ClaimGeneration: 3, Claimant: "worker", Fence: "fence"}
				if expiry != nil {
					lease.ExpiresAt = expires
				}
				want = sourceWorkRow{id: "work-id", state: "CLAIMED", configurationRef: "configuration-ref", actorRef: "actor-ref", organizationRef: "organization-ref", work: work, lease: lease, deadline: deadline, receipts: map[string]sourceWorkReceipt{"1/2": {Action: "FAIL", Digest: "receipt-digest", Result: entity.ManagedConfigurationGitSource{Ref: "source-ref", Version: 6}}}}
			}
			if !errors.Is(err, wantError) || !reflect.DeepEqual(got, want) || tx.queryCalls != 1 || tx.commitCalls != 0 {
				t.Fatal("source work lock changed snapshot boundary, error mapping, projection or transaction ownership")
			}
		})
	}
}
