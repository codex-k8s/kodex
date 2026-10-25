package grpc

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryResponse resolves the already verified principal for the exact RPC before invoking the
// caller's read and typed projection once. Principal errors pass through unchanged; callback
// errors use the existing transport mapper and discard any partial response. The callback returns
// raw domain/context errors, not pre-mapped status errors. This helper creates no authority,
// retries, transaction, deadline or nil-result fallback; owner eligibility stays in the service.
// queryResponse разрешает уже проверенный principal точного RPC до однократного вызова чтения
// и typed projection caller. Principal errors возвращаются без изменения; callback errors
// проходят прежний transport mapper с отбрасыванием partial response. Callback возвращает
// исходные domain/context errors, а не уже преобразованные status errors. Helper не создаёт
// authority, retries, transaction, deadline либо nil-result fallback; owner eligibility
// остаётся в сервисе.
func queryResponse[Response any](ctx context.Context, method string, read func(value.Principal) (*Response, error)) (*Response, error) {
	p, err := principal(ctx, method)
	if err != nil {
		return nil, err
	}
	response, err := read(p)
	if err != nil {
		return nil, transportError(err)
	}
	return response, nil
}
