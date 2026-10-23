package emailbridgeapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ExecutionHeader carries the bounded mail execution binding on the internal transport.
// RU: ExecutionHeader передаёт ограниченную привязку почтового исполнения во внутреннем транспорте.
const ExecutionHeader = "X-Kodex-Email-Execution"

// executionContextKey isolates the typed binding from unrelated context values.
// RU: executionContextKey отделяет типизированную привязку от посторонних значений контекста.
type executionContextKey struct{}

// ValidExecutionBinding checks that exactly one invocation or connection test
// owns the bounded lease and that its identifiers cannot escape the header.
// RU: ValidExecutionBinding проверяет единственного владельца lease — вызов
// либо тест подключения — и ограничивает идентификаторы безопасной формой заголовка.
func ValidExecutionBinding(b *ExecutionBinding) bool {
	if b == nil || (b.InvocationRef == nil) == (b.ConnectionTestRef == nil) || len(b.Lease.Ref) > 128 || b.Lease.Generation < 1 || b.Lease.Generation > 9007199254740991 || b.Lease.ExpiresAt.IsZero() {
		return false
	}
	for _, value := range []string{b.Lease.Ref, b.Lease.Fence} {
		if value == "" || len(value) > 512 || strings.ContainsAny(value, " \t\r\n\x00") {
			return false
		}
	}
	ref := b.InvocationRef
	if ref == nil {
		ref = b.ConnectionTestRef
	}
	return len(*ref) > 0 && len(*ref) <= 128 && !strings.ContainsAny(*ref, " \t\r\n\x00")
}

// ExecutionHeaderValue serializes a structurally valid binding for internal
// transport; freshness is rechecked by the receiving parser.
// RU: ExecutionHeaderValue сериализует структурно допустимую привязку для
// внутреннего транспорта; получатель отдельно проверяет срок её действия.
func ExecutionHeaderValue(b *ExecutionBinding) (string, error) {
	if !ValidExecutionBinding(b) {
		return "", errors.New("invalid email execution binding")
	}
	raw, err := json.Marshal(b)
	return string(raw), err
}

// ParseExecutionHeader strictly decodes a bounded binding and rejects expired
// leases before any mail operation can use the claimed execution scope.
// RU: ParseExecutionHeader строго декодирует ограниченную привязку и
// отклоняет истёкший lease до использования заявленной области исполнения.
func ParseExecutionHeader(raw string) (*ExecutionBinding, error) {
	var b ExecutionBinding
	if len(raw) > 4096 || Decode([]byte(raw), &b) != nil || !ValidExecutionBinding(&b) || !b.Lease.ExpiresAt.After(time.Now()) {
		return nil, errors.New("invalid email execution binding")
	}
	return &b, nil
}

// WithExecutionBinding attaches the transport binding to one request context;
// it does not change the semantic command payload or its digest.
// RU: WithExecutionBinding помещает транспортную привязку в контекст одного
// запроса, не меняя смысловую команду и её дайджест.
func WithExecutionBinding(ctx context.Context, b *ExecutionBinding) context.Context {
	return context.WithValue(ctx, executionContextKey{}, b)
}

// ExecutionFromContext reads a previously attached mail execution binding
// without accepting arbitrary untyped context values.
// RU: ExecutionFromContext извлекает ранее добавленную почтовую привязку,
// не принимая посторонние нетипизированные значения контекста.
func ExecutionFromContext(ctx context.Context) *ExecutionBinding {
	b, _ := ctx.Value(executionContextKey{}).(*ExecutionBinding)
	return b
}
