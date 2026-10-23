package receipt

import (
	"errors"
	"reflect"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/jackc/pgx/v5"
)

// scanRowFunc substitutes only pgx.Row.Scan for deterministic column-order and error tests without a database connection.
// scanRowFunc подставляет только pgx.Row.Scan для воспроизводимых проверок порядка колонок и ошибок без соединения с БД.
type scanRowFunc func(...any) error

// Scan passes destination pointers to the test callback without changing ordering, values or returned error identity.
// Scan передаёт указатели назначения тестовому callback без изменения порядка, значений или идентичности ошибки.
func (row scanRowFunc) Scan(destinations ...any) error { return row(destinations...) }

// TestScanRecordPreservesColumnsAndErrors checks every shared column target, a partially scanned failure and ErrNoRows without treating either failure as success.
// TestScanRecordPreservesColumnsAndErrors проверяет назначение каждой общей колонки, частично прочитанный отказ и ErrNoRows, не считая ошибки успехом.
func TestScanRecordPreservesColumnsAndErrors(t *testing.T) {
	t.Parallel()
	values := []any{"receipt", "effect", "input", "unknown", "resource", "uid", uint32(7), "folder", "content", "actor", "agent", "grant", api.OperationSend, int64(9), int64(10), true, int64(11)}
	want := port.Record{ID: "receipt", Key: "effect", Digest: "input", Status: "unknown", Resource: "resource", UID: "uid", UIDValidity: 7, Folder: "folder", ContentDigest: "content", ReportVersion: 11,
		Audit: port.Audit{Actor: "actor", Agent: "agent", Grant: "grant", Operation: api.OperationSend, ConfigurationRevision: 9, CredentialGeneration: 10, GateApproved: true}}
	partialFailure := errors.New("synthetic scan failure")
	for _, test := range []struct {
		name  string
		count int
		err   error
		want  port.Record
	}{
		{"complete", len(values), nil, want},
		{"partial", 3, partialFailure, port.Record{ID: "receipt", Key: "effect", Digest: "input"}},
		{"missing", 0, pgx.ErrNoRows, port.Record{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := scanRowFunc(func(destinations ...any) error {
				if len(destinations) != len(values) {
					t.Fatal("receipt column count changed")
				}
				for index := range test.count {
					destination, value := reflect.ValueOf(destinations[index]), reflect.ValueOf(values[index])
					if destination.Kind() != reflect.Pointer || destination.Elem().Type() != value.Type() {
						t.Fatal("receipt column type changed")
					}
					destination.Elem().Set(value)
				}
				return test.err
			})
			got, err := scanRecord(row)
			if got != test.want || !errors.Is(err, test.err) {
				t.Fatal("receipt column order or scan error changed")
			}
		})
	}
}
