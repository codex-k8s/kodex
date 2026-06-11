package app

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

func workerCommandMeta(actor string, reasonPrefix string, phase string, expectedVersion *int64) value.CommandMeta {
	return value.CommandMeta{
		CommandID:       uuid.New(),
		ExpectedVersion: expectedVersion,
		Actor:           value.Actor{Type: "service", ID: actor},
		Reason:          reasonPrefix + " " + phase,
		RequestID:       actor + "-" + phase,
		RequestContext:  value.RequestContext{Source: actor},
	}
}
