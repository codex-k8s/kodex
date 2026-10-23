package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// managedRebindRequestInput contains mapped mutation metadata, a validated impact digest, and
// ordered consumer selectors shared by prompt, integration-definition, and STT rebind requests.
// It contains no actor or command choice; the endpoint keeps its exact typed RPC and the owner
// revalidates resource ownership, versions, and impact before applying a binding change.
// managedRebindRequestInput содержит преобразованные метаданные изменения, проверенный дайджест влияния и
// упорядоченный выбор потребителей запросов перепривязки промпта, определения интеграции и STT.
// Субъект и выбор команды отсутствуют; endpoint сохраняет точный типизированный RPC, а владелец
// повторно проверяет владение, версии и влияние перед изменением привязки.
type managedRebindRequestInput struct {
	mutation     *controlplanev1.MutationContext
	impactDigest string
	consumers    []*controlplanev1.ManagedConfigurationConsumer
}

// readManagedRebindRequest preserves the shared decode, consumer-validation, and mutation-header
// parsing order of the three rebind endpoints. It retains the existing optional-ETag behavior and
// does not validate the idempotency key's presence itself. A rejection writes the existing problem
// response and returns false without dispatch; it neither reads a resource nor establishes authority.
// readManagedRebindRequest сохраняет общий порядок чтения JSON, проверки потребителей и заголовков
// изменения трёх endpoints перепривязки. Сохраняется прежняя необязательность ETag; наличие ключа
// идемпотентности сама функция не проверяет. Отказ пишет прежний ответ проблемы и возвращает false без
// вызова команды; функция не читает ресурс и не устанавливает полномочия.
func readManagedRebindRequest(w http.ResponseWriter, r *http.Request, key, etag string) (managedRebindRequestInput, bool) {
	body, ok := decodeJSON[generated.ManagedConfigurationRebindInput](w, r)
	if !ok {
		return managedRebindRequestInput{}, false
	}
	consumers, ok := managedConsumerInput(w, body)
	if !ok {
		return managedRebindRequestInput{}, false
	}
	mutation, ok := requireMutation(w, key, etag)
	if !ok {
		return managedRebindRequestInput{}, false
	}
	return managedRebindRequestInput{mutation: mutation, impactDigest: body.ImpactDigest, consumers: consumers}, true
}
