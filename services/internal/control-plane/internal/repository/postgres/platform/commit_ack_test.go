package platform

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
)

// commitAckFaultKey marks a synthetic callback for ACK loss without carrying authority.
// commitAckFaultKey отмечает synthetic callback для потери ACK, не передавая authority.
type commitAckFaultKey struct{}

// commitAckReader withholds a real COMMIT/idle response only after explicit test arming.
// commitAckReader скрывает настоящий ответ COMMIT/idle только после явного test arming.
type commitAckReader struct {
	source             io.Reader
	pending            []byte
	armed, fired, idle atomic.Bool
	failure            error
	cancel             context.CancelFunc
}

// commitAckFrame reads one bounded PostgreSQL backend frame without interpreting row payloads.
// commitAckFrame читает один ограниченный PostgreSQL backend frame без разбора row payloads.
func commitAckFrame(source io.Reader) ([]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(source, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size < 4 || size > 16<<20 {
		return nil, errors.New("invalid synthetic PostgreSQL frame")
	}
	frame := make([]byte, int(size)+1)
	copy(frame, header[:])
	_, err := io.ReadFull(source, frame[5:])
	return frame, err
}

// Read forwards complete frames but converts one server-confirmed COMMIT into an ACK failure.
// Read передаёт полные frames, но превращает один подтверждённый сервером COMMIT в потерю ACK.
func (reader *commitAckReader) Read(output []byte) (int, error) {
	if len(output) == 0 {
		return 0, nil
	}
	if reader.fired.Load() {
		return 0, reader.failure
	}
	if len(reader.pending) == 0 {
		frame, err := commitAckFrame(reader.source)
		if err != nil {
			return 0, err
		}
		if reader.armed.Load() && frame[0] == 'C' && bytes.Equal(frame[5:], []byte("COMMIT\x00")) {
			idle, err := commitAckFrame(reader.source)
			if err != nil {
				return 0, err
			}
			if !bytes.Equal(idle, []byte{'Z', 0, 0, 0, 5, 'I'}) {
				return 0, errors.New("synthetic COMMIT did not reach idle")
			}
			reader.idle.Store(true)
			reader.fired.Store(true)
			if reader.cancel != nil {
				reader.cancel()
			}
			return 0, reader.failure
		}
		reader.pending = frame
	}
	n := copy(output, reader.pending)
	reader.pending = reader.pending[n:]
	return n, nil
}

// commitAckTrace arms only the COMMIT following a successful exact receipt INSERT in a marked call.
// commitAckTrace включает потерю только COMMIT после успешного exact receipt INSERT отмеченного вызова.
type commitAckTrace struct {
	mu          sync.Mutex
	readers     map[*pgproto3.Frontend]*commitAckReader
	eligible    map[*pgx.Conn]bool
	selected    *commitAckReader
	failure     error
	cancel      context.CancelFunc
	rollback    bool
	rollbacks   atomic.Int64
	rollbackTag atomic.Bool
}

// newCommitAckTrace creates a test-local protocol injector, never a production pool interface.
// newCommitAckTrace создаёт локальный protocol injector теста, не production pool interface.
func newCommitAckTrace(failure error) *commitAckTrace {
	return &commitAckTrace{readers: map[*pgproto3.Frontend]*commitAckReader{}, eligible: map[*pgx.Conn]bool{}, failure: failure}
}

// build constructs the normal pgx frontend over a bounded frame reader above the transport/TLS layer.
// build создаёт обычный pgx frontend над ограниченным frame reader поверх transport/TLS.
func (trace *commitAckTrace) build(source io.Reader, destination io.Writer) *pgproto3.Frontend {
	reader := &commitAckReader{source: source, failure: trace.failure}
	frontend := pgproto3.NewFrontend(reader, destination)
	trace.mu.Lock()
	trace.readers[frontend] = reader
	trace.mu.Unlock()
	return frontend
}

// TraceQueryStart arms only the selected owner transaction; ordinary preflight commits pass through.
// TraceQueryStart отмечает только выбранную owner transaction; обычные preflight commits проходят.
func (trace *commitAckTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(commitAckFaultKey{}) != true {
		return ctx
	}
	stage := ""
	switch data.SQL {
	case queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, queryArtifactsUploadartifactInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest:
		stage = "RECEIPT_INSERT"
	case "commit":
		stage = "COMMIT"
		trace.mu.Lock()
		if trace.eligible[conn] && trace.selected == nil {
			reader := trace.readers[conn.PgConn().Frontend()]
			trace.selected = reader
			if trace.rollback {
				trace.mu.Unlock()
				// Deliberately abort this synthetic transaction before its real COMMIT.
				// Намеренно прерываем эту synthetic transaction до её настоящего COMMIT.
				_, err := conn.PgConn().Exec(ctx, queryCommitAckAbort).ReadAll()
				if err != nil {
					trace.rollbacks.Add(1)
				}
				return context.WithValue(ctx, commitAckStageKey{}, stage)
			}
			if reader != nil {
				reader.failure = trace.failure
				reader.cancel = trace.cancel
				reader.armed.Store(true)
			}
		}
		trace.mu.Unlock()
	}
	return context.WithValue(ctx, commitAckStageKey{}, stage)
}

// commitAckStageKey stores a closed query label without retaining SQL, arguments or payloads.
// commitAckStageKey хранит закрытый query label без SQL, аргументов либо payloads.
type commitAckStageKey struct{}

// TraceQueryEnd accepts only a successful receipt insertion as evidence for the next COMMIT.
// TraceQueryEnd принимает только успешный receipt INSERT как условие следующего COMMIT.
func (trace *commitAckTrace) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(commitAckStageKey{}) == "COMMIT" && data.Err == nil && data.CommandTag.String() == "ROLLBACK" {
		trace.rollbackTag.Store(true)
	}
	if ctx.Value(commitAckStageKey{}) == "RECEIPT_INSERT" && data.Err == nil && data.CommandTag.RowsAffected() == 1 {
		trace.mu.Lock()
		trace.eligible[conn] = true
		trace.mu.Unlock()
	}
}

// commitAckStore records exact version receipts and effect counts without exposing object locators.
// commitAckStore сохраняет exact version receipts и effect counts без вывода object locators.
type commitAckStore struct {
	objectstorage.Store
	mu       sync.Mutex
	receipts []objectstorage.Receipt
	deletes  atomic.Int64
}

// Put delegates one real object-store write and privately retains its immutable receipt.
// Put выполняет одну запись object store и приватно сохраняет её immutable receipt.
func (store *commitAckStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	receipt, err := store.Store.Put(ctx, input)
	if err == nil {
		store.mu.Lock()
		store.receipts = append(store.receipts, receipt)
		store.mu.Unlock()
	}
	return receipt, err
}

// Delete records attempted compensation before delegating the exact key/version deletion.
// Delete учитывает попытку компенсации перед удалением точных key/version.
func (store *commitAckStore) Delete(ctx context.Context, key, version string) error {
	store.deletes.Add(1)
	return store.Store.Delete(ctx, key, version)
}

// TestCommitAckWire verifies framing, explicit arming, COMMIT/idle proof and bounded malformed input.
// TestCommitAckWire проверяет frames, явное arming, COMMIT/idle proof и ограниченный malformed input.
func TestCommitAckWire(t *testing.T) {
	commit := []byte{'C', 0, 0, 0, 11, 'C', 'O', 'M', 'M', 'I', 'T', 0}
	idle := []byte{'Z', 0, 0, 0, 5, 'I'}
	wire := append(append([]byte(nil), commit...), idle...)
	for _, armed := range []bool{false, true} {
		reader := &commitAckReader{source: bytes.NewReader(wire), failure: io.ErrUnexpectedEOF}
		reader.armed.Store(armed)
		body, err := io.ReadAll(reader)
		if armed {
			if !errors.Is(err, io.ErrUnexpectedEOF) || len(body) != 0 || !reader.fired.Load() || !reader.idle.Load() {
				t.Fatal("armed COMMIT ACK was not withheld")
			}
		} else if err != nil || !bytes.Equal(body, wire) || reader.fired.Load() {
			t.Fatal("unarmed frames changed")
		}
	}
	for _, malformed := range [][]byte{{'C', 0, 0, 0, 3}, {'C', 127, 255, 255, 255}, {'C', 0, 0, 0, 11, 'C'}, append(append([]byte(nil), commit...), []byte{'Z', 0, 0, 0, 5, 'T'}...)} {
		reader := &commitAckReader{source: bytes.NewReader(malformed), failure: io.ErrUnexpectedEOF}
		reader.armed.Store(true)
		if _, err := io.ReadAll(reader); err == nil || reader.fired.Load() {
			t.Fatal("malformed response claimed committed idle")
		}
	}
	reader := &commitAckReader{source: bytes.NewReader(wire), failure: context.Canceled}
	reader.armed.Store(true)
	if _, err := io.ReadAll(reader); !errors.Is(err, context.Canceled) || !reader.idle.Load() {
		t.Fatal("cancel ACK classification changed")
	}
}
