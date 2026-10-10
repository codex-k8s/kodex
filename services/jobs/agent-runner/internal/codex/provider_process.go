package codex

import (
	"errors"
	"regexp"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// Закреплённый upstream формирует prefix из CARGO_PKG_VERSION процесса;
// последний suffix — clientInfo.version, он никогда не служит версией сервера.
var providerProcessUserAgentPattern = regexp.MustCompile(`^kodex-agent-runner/([^ ]{1,64}) \([A-Za-z0-9 ._-]{1,128}; [A-Za-z0-9_-]{1,32}\) [A-Za-z0-9/._-]{1,128} \(kodex-agent-runner; 1\)$`)

var errProviderProcessInitialize = errors.New("provider process initialize version is invalid")

func parseProviderProcessVersion(userAgent string) (string, error) {
	matched := providerProcessUserAgentPattern.FindStringSubmatch(userAgent)
	if len(userAgent) > 512 || len(matched) != 2 || !runtimecontract.ValidProviderProcessVersion(matched[1]) {
		return "", errProviderProcessInitialize
	}
	return matched[1], nil
}

func (state *protocolState) publishProviderProcess(input model.Input) error {
	value := runtimecontract.BindProviderProcessObservation(input, state.processVersion)
	return state.publishActivity(runtimecontract.RuntimeActivity{ProviderProcess: &value})
}
