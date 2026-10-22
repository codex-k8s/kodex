package codex

import (
	"errors"
	"regexp"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// providerProcessUserAgentPattern extracts the server package prefix from the pinned initialize shape;
// the trailing clientInfo version is deliberately not treated as the provider process version.
// providerProcessUserAgentPattern извлекает версию пакета сервера из закреплённой формы initialize;
// версия clientInfo в конце намеренно не считается версией процесса провайдера.
var providerProcessUserAgentPattern = regexp.MustCompile(`^kodex-agent-runner/([^ ]{1,64}) \([A-Za-z0-9 ._-]{1,128}; [A-Za-z0-9_-]{1,32}\) [A-Za-z0-9/._-]{1,128} \(kodex-agent-runner; 1\)$`)

// errProviderProcessInitialize reports an invalid initialize version without echoing host or metadata.
// errProviderProcessInitialize сообщает неверную версию initialize без вывода хоста или метаданных.
var errProviderProcessInitialize = errors.New("provider process initialize version is invalid")

// parseProviderProcessVersion enforces the bounded pinned user-agent shape and closed version grammar.
// It validates reported initialize metadata, not an independent executable or image attestation.
// parseProviderProcessVersion проверяет ограниченную закреплённую форму user-agent и допустимую грамматику версии.
// Это проверка заявленных метаданных initialize, а не независимая аттестация бинаря или образа.
func parseProviderProcessVersion(userAgent string) (string, error) {
	matched := providerProcessUserAgentPattern.FindStringSubmatch(userAgent)
	if len(userAgent) > 512 || len(matched) != 2 || !runtimecontract.ValidProviderProcessVersion(matched[1]) {
		return "", errProviderProcessInitialize
	}
	return matched[1], nil
}

// publishProviderProcess binds the observed version to the supplied execution snapshot and publishes
// its safe activity projection; the downstream broker separately verifies peer and execution pins.
// publishProviderProcess привязывает наблюдаемую версию к снимку исполнения и публикует безопасное
// событие; downstream broker отдельно сверяет идентичность peer и точные привязки исполнения.
func (state *protocolState) publishProviderProcess(input model.Input) error {
	value := runtimecontract.BindProviderProcessObservation(input, state.processVersion)
	return state.publishActivity(runtimecontract.RuntimeActivity{ProviderProcess: &value})
}
