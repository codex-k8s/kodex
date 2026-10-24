package platform

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

// TestProjectPurgeRecoveryConfirmationPreservesFailurePriority checks positive/negative owner
// confirmations, missing/wrapped rows and failed scans including a partially written true flag.
// Exactly one boolean destination is scanned once; read errors stay unavailable before the
// false-to-conflict mapping and never expose the synthetic driver detail.
// TestProjectPurgeRecoveryConfirmationPreservesFailurePriority проверяет положительное и
// отрицательное подтверждения, отсутствие строки с обёрткой и ошибки чтения, включая уже записанный
// true. Одно поле boolean читается однократно; ошибка даёт Unavailable раньше преобразования
// false в Conflict и не раскрывает синтетические детали драйвера.
func TestProjectPurgeRecoveryConfirmationPreservesFailurePriority(t *testing.T) {
	driverFailure := errors.New("synthetic private driver detail")
	for _, test := range []struct {
		name        string
		flag        bool
		cause, want error
	}{
		{name: "confirmed", flag: true},
		{name: "not-confirmed", want: errs.ErrConflict},
		{name: "scan-failure", cause: driverFailure, want: errs.ErrUnavailable},
		{name: "partial-confirmed-failure", flag: true, cause: driverFailure, want: errs.ErrUnavailable},
		{name: "missing", cause: pgx.ErrNoRows, want: errs.ErrUnavailable},
		{name: "wrapped-missing", cause: errors.Join(driverFailure, pgx.ErrNoRows), want: errs.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			row := proofResolutionRow(func(dest ...any) error {
				calls++
				if len(dest) != 1 {
					t.Fatal("recovery confirmation changed scan shape")
				}
				*dest[0].(*bool) = test.flag
				return test.cause
			})
			err := scanProjectPurgeRecoveryConfirmation(row)
			if !errors.Is(err, test.want) || calls != 1 {
				t.Fatal("recovery confirmation changed error priority or scan count")
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("recovery confirmation exposed driver detail")
			}
		})
	}
}

// TestProviderCredentialProjectionPreservesPrefixAndTypedTails checks runtime/STT scan layouts
// against seven literal binding fields and their distinct typed tails across seven row outcomes.
// Partial internal output is preserved on error for caller-side discard; wrapped missing rows are
// not-found and other failures unavailable. The row-only fixture cannot query, commit or read secrets.
// TestProviderCredentialProjectionPreservesPrefixAndTypedTails проверяет runtime/STT порядок
// семи фиксированных полей привязки и разных типизированных окончаний для семи исходов чтения.
// Частичный внутренний результат сохраняется при ошибке для отбрасывания вызывающим; обёрнутое
// отсутствие строки даёт NotFound, другой сбой — Unavailable. Оснастка строки не выполняет запросы,
// commit и чтение секретов.
func TestProviderCredentialProjectionPreservesPrefixAndTypedTails(t *testing.T) {
	full := platformrepo.ProviderCredentialBinding{AccountRef: "account-public", CredentialRevisionRef: "credential-public", CredentialRevision: 7, SecretName: "fixture-object", SecretUID: "fixture-uid", SecretResourceVersion: "resource-version", ContentSHA256: "fixture-digest"}
	partial := platformrepo.ProviderCredentialBinding{AccountRef: "account-public", CredentialRevisionRef: "credential-public"}
	deadline := time.Date(2026, 10, 25, 1, 2, 3, 0, time.UTC)
	driverFailure := errors.New("synthetic private driver detail")
	for _, mode := range []string{"runtime", "stt"} {
		t.Run(mode, func(t *testing.T) {
			for _, test := range []struct {
				name           string
				fill           int
				cause, wantErr error
				want           platformrepo.ProviderCredentialBinding
			}{
				{name: "success", fill: 7, want: full},
				{name: "missing", cause: pgx.ErrNoRows, wantErr: errs.ErrNotFound},
				{name: "wrapped-missing", cause: errors.Join(driverFailure, pgx.ErrNoRows), wantErr: errs.ErrNotFound},
				{name: "scan-failure", cause: driverFailure, wantErr: errs.ErrUnavailable},
				{name: "partial-failure", fill: 2, cause: driverFailure, wantErr: errs.ErrUnavailable, want: partial},
				{name: "full-output-failure", fill: 7, cause: driverFailure, wantErr: errs.ErrUnavailable, want: full},
				{name: "partial-missing", fill: 2, cause: pgx.ErrNoRows, wantErr: errs.ErrNotFound, want: partial},
			} {
				t.Run(test.name, func(t *testing.T) {
					calls := 0
					var binding platformrepo.ProviderCredentialBinding
					var raw []byte
					var expires time.Time
					var model, language string
					tail := []any{&raw, &expires}
					if mode == "stt" {
						tail = []any{&model, &language, &raw}
					}
					row := proofResolutionRow(func(dest ...any) error {
						calls++
						if len(dest) != 7+len(tail) {
							t.Fatal("credential projection changed scan width")
						}
						if test.fill >= 2 {
							*dest[0].(*string), *dest[1].(*string) = "account-public", "credential-public"
						}
						if test.fill == 7 {
							*dest[2].(*int64) = 7
							*dest[3].(*string), *dest[4].(*string) = "fixture-object", "fixture-uid"
							*dest[5].(*string), *dest[6].(*string) = "resource-version", "fixture-digest"
							if mode == "runtime" {
								*dest[7].(*[]byte), *dest[8].(*time.Time) = []byte("[]"), deadline
							} else {
								*dest[7].(*string), *dest[8].(*string), *dest[9].(*[]byte) = "model-fixture", "ru", []byte("{}")
							}
						}
						return test.cause
					})
					err := scanProviderCredentialProjection(row, &binding, tail...)
					if !errors.Is(err, test.wantErr) || !reflect.DeepEqual(binding, test.want) || calls != 1 {
						t.Fatal("credential projection changed binding, error or scan count")
					}
					if test.fill == 7 {
						if mode == "runtime" && (string(raw) != "[]" || expires != deadline || model != "" || language != "") || mode == "stt" && (model != "model-fixture" || language != "ru" || string(raw) != "{}" || !expires.IsZero()) {
							t.Fatal("credential projection changed typed tail order")
						}
					} else if raw != nil || !expires.IsZero() || model != "" || language != "" {
						t.Fatal("credential projection unexpectedly changed unread tail")
					}
					if err != nil && strings.Contains(err.Error(), "private") {
						t.Fatal("credential projection exposed driver detail")
					}
				})
			}
		})
	}
}
