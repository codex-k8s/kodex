package httptransport

import (
	"net/http"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// providerAccountCommandResponse admits only the three specialized verify, reauthorize and delete
// replies that already share an exact-account readback check. It does not admit other provider
// commands or define their lifecycle, authority or RPC error profile.
// providerAccountCommandResponse допускает только три отдельных ответа проверки, повторной
// авторизации и удаления, уже имеющих одинаковую проверку точного аккаунта. Не допускает другие
// команды провайдера и не задаёт их жизненный цикл, полномочия или профиль ошибок RPC.
type providerAccountCommandResponse interface {
	*cp.VerifyProviderAccountDeviceAuthorizationResponse | *cp.ReauthorizeProviderAccountDeviceCodeResponse | *cp.DeleteProviderAccountResponse
	proto.Message

	// GetAccount reads the returned account; each admitted generated pointer type safely returns
	// nil for a typed-nil reply or an absent account field.
	// GetAccount читает возвращённый аккаунт; каждый допустимый сгенерированный указатель безопасно
	// возвращает nil при типизированном nil-ответе или отсутствующем поле аккаунта.
	GetAccount() *cp.ProviderAccount
}

// writeProviderAccountCommandResponse checks the unchanged exact-account predicate after a
// successful specialized RPC, then uses the existing account projector with the caller's HTTP
// status. It adds no version, state, authorization, mutation or retry logic; HTTP202 remains an
// accepted command response rather than proof of completed external authorization or cleanup.
// writeProviderAccountCommandResponse проверяет прежний предикат точного аккаунта после успешного
// отдельного RPC, затем использует прежнюю проекцию аккаунта с HTTP-статусом вызывающего. Не
// добавляет проверки версии, состояния, полномочий, mutation или retry; HTTP202 означает принятую
// команду, а не доказательство завершения внешней авторизации или очистки.
func writeProviderAccountCommandResponse[R providerAccountCommandResponse](w http.ResponseWriter, statusCode int, response R, ref string) {
	if response.GetAccount() == nil || response.GetAccount().GetRef() != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, statusCode, response, "account", "")
}
