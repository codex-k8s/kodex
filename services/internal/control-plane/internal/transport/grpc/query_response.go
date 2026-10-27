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
	return principalResponse(ctx, method, principal, read)
}

// roleImageResponse retains the dedicated role-image principal entrypoint and maps the typed
// service response once. Each caller owns its exact method, read/command and complete lease,
// claim or evidence bindings; this wrapper selects no action and performs no lifecycle effect.
// roleImageResponse сохраняет отдельный вход principal образов и однократно отображает typed
// service response. Каждый caller владеет exact method, чтением/командой и полными lease,
// claim либо evidence bindings; wrapper не выбирает действие и не выполняет lifecycle effect.
func roleImageResponse[Response any](ctx context.Context, method string, invoke func(value.Principal) (*Response, error)) (*Response, error) {
	return principalResponse(ctx, method, roleImagePrincipal, invoke)
}

// principalResponse invokes the server-selected resolver before the callback, preserving
// authentication errors unchanged and mapping raw domain/context errors once. Partial responses
// are discarded on error; successful nil results remain nil. No resolver fallback, request-owned
// authority, retry, transaction or deadline is introduced.
// principalResponse вызывает выбранный сервером resolver до callback, сохраняет authentication
// errors без изменения и отображает raw domain/context errors один раз. Partial response при
// ошибке отбрасывается; успешный nil остаётся nil. Fallback resolver, authority из request,
// retry, transaction либо deadline не добавляются.
func principalResponse[Response any](ctx context.Context, method string,
	resolve func(context.Context, string) (value.Principal, error),
	invoke func(value.Principal) (*Response, error),
) (*Response, error) {
	p, err := resolve(ctx, method)
	if err != nil {
		return nil, err
	}
	response, err := invoke(p)
	if err != nil {
		return nil, transportError(err)
	}
	return response, nil
}
