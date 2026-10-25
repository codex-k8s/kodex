package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// executeResponse invokes the existing specialized command boundary once with the supplied exact
// method, mutation and payload, returning its already mapped error unchanged. Only success reaches
// the caller's typed response projection. It adds no authority, retries, nil-result fallback or
// transaction ownership; response casting retains the domain result invariants required by callers.
// executeResponse однократно вызывает существующую specialized command boundary с переданными exact
// method, mutation и payload, возвращая уже преобразованную ошибку без изменения. Только success
// достигает typed response projection caller. Не добавляет authority, retry, nil-result fallback
// либо transaction ownership; casting сохраняет требуемые caller инварианты domain result.
func executeResponse[Response any](ctx context.Context, service *platformservice.Service, method string, kind command.Kind, requestMutation *controlplanev1.MutationContext, payload any, project func(command.Result) *Response) (*Response, error) {
	result, err := execute(ctx, service, method, kind, requestMutation, payload)
	if err != nil {
		return nil, err
	}
	return project(result), nil
}
