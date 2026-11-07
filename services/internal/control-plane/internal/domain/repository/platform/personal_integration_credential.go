package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// PersonalIntegrationCredentialReader resolves only the current principal's own binding after
// Connection visibility checks. It has no API for reading by an arbitrary owner or Secret ref.
// PersonalIntegrationCredentialReader разрешает только собственную привязку текущего principal
// после проверки видимости Connection. API чтения по произвольному owner либо Secret ref нет.
type PersonalIntegrationCredentialReader interface {
	// GetPersonalIntegrationCredential reads only the current principal's visible self binding.
	// GetPersonalIntegrationCredential читает лишь видимую собственную привязку текущего principal.
	GetPersonalIntegrationCredential(context.Context, value.Principal, string) (entity.PersonalIntegrationCredential, error)
	// ReadPersonalIntegrationCredentialReceipt rejoins an exact self mutation without secret reads.
	// ReadPersonalIntegrationCredentialReceipt согласует exact self mutation без чтения secret.
	ReadPersonalIntegrationCredentialReceipt(context.Context, value.Principal, value.Mutation, command.PersonalIntegrationCredentialInput) (entity.PersonalIntegrationCredential, bool, error)
}

// PersonalIntegrationConnectionCatalog reads one bounded owner-qualified page without creating
// bindings, probing providers or returning plaintext; the separate reader supports addressed writes.
// PersonalIntegrationConnectionCatalog читает ограниченную owner-qualified страницу без создания
// привязок, provider probes и plaintext; отдельный reader поддерживает адресные команды записи.
type PersonalIntegrationConnectionCatalog interface {
	// ListPersonalIntegrationConnections returns one visibility-filtered page and bounded cursor.
	// ListPersonalIntegrationConnections возвращает страницу с visibility filter и bounded cursor.
	ListPersonalIntegrationConnections(context.Context, value.Principal, query.PersonalIntegrationConnectionFilter) ([]entity.PersonalIntegrationConnection, string, error)
}

// PersonalIntegrationCredentialWork owns bounded claims and one-time probe admission. Every
// payload remains pinned to a stored job; the worker cannot select a user or credential.
// PersonalIntegrationCredentialWork владеет bounded claims и одноразовым допуском probe. Каждый
// payload закреплён за сохранённым заданием; worker не выбирает пользователя либо credential.
type PersonalIntegrationCredentialWork interface {
	// ClaimPersonalIntegrationCredentialTests leases immutable self-owned snapshots to one worker.
	// ClaimPersonalIntegrationCredentialTests выдаёт worker lease на immutable self-owned snapshots.
	ClaimPersonalIntegrationCredentialTests(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	// BeginPersonalIntegrationCredentialProbe admits the pinned READ batch once after fresh checks.
	// BeginPersonalIntegrationCredentialProbe однократно допускает pinned READ batch после fresh checks.
	BeginPersonalIntegrationCredentialProbe(context.Context, value.Principal, command.PersonalIntegrationCredentialTestCompletion) error
}

// PersonalIntegrationInvocationWork consumes an exact gateway lease after fresh credential checks.
// It exposes no operation to select a user or read a general personal credential by descriptor.
// PersonalIntegrationInvocationWork расходует exact gateway lease после свежих credential checks.
// Операций выбора пользователя либо чтения произвольной личной credential по descriptor нет.
type PersonalIntegrationInvocationWork interface {
	// BeginPersonalIntegrationInvocation returns admission only for the fresh root credential pin.
	// BeginPersonalIntegrationInvocation даёт допуск только для актуального root credential pin.
	BeginPersonalIntegrationInvocation(context.Context, value.Principal, command.PersonalIntegrationInvocationAdmission) (entity.PersonalIntegrationInvocationAdmissionResult, error)
}
