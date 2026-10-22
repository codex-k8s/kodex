package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ConfigurationSourceCompletion binds fetched configuration bytes to their source-work lease, commit, content digest and ancestry result. These fields are completion evidence submitted to the owner, not authority to publish arbitrary content.
// ConfigurationSourceCompletion связывает полученные байты конфигурации с арендой получения исходника, коммитом, дайджестом содержимого и результатом проверки происхождения. Поля передаются владельцу как доказательства завершения, а не дают право публиковать произвольное содержимое.
type ConfigurationSourceCompletion struct {
	Lease                              entity.ManagedConfigurationSourceLease
	CommitSHA, ContentSHA256, Ancestry string
	Content                            []byte
}

// ConfigurationSourceWorkRepository is the owner port for claiming, renewing and finishing managed-configuration source work. Principal and exact lease bindings remain explicit across completion and failure rather than being inferred from a Git location.
// ConfigurationSourceWorkRepository — порт владельца для захвата, продления и завершения получения исходника управляемой конфигурации. Субъект и точные привязки аренды явно сохраняются при завершении и ошибке, а не выводятся из адреса Git.
type ConfigurationSourceWorkRepository interface {
	// ClaimConfigurationSourceWork claims a bounded batch for the authenticated principal and claimant identifier; returned work contains the leases used by subsequent operations.
	// ClaimConfigurationSourceWork захватывает ограниченную пачку для аутентифицированного субъекта и идентификатора исполнителя; возвращённые задания содержат аренды для последующих операций.
	ClaimConfigurationSourceWork(context.Context, value.Principal, string, int32) ([]entity.ManagedConfigurationSourceWork, error)
	// RenewConfigurationSourceWork renews the supplied exact source-work lease under the principal's boundary and returns the refreshed lease, not a newly selected source.
	// RenewConfigurationSourceWork продлевает переданную точную аренду получения исходника в границе субъекта и возвращает обновлённую аренду, а не новый выбранный источник.
	RenewConfigurationSourceWork(context.Context, value.Principal, entity.ManagedConfigurationSourceLease) (entity.ManagedConfigurationSourceLease, error)
	// CompleteConfigurationSourceWork submits fetched content and commit/ancestry evidence for the pinned lease and returns the resulting managed Git-source state.
	// CompleteConfigurationSourceWork передаёт полученное содержимое и доказательства коммита и происхождения для закреплённой аренды и возвращает итоговое состояние управляемого Git-источника.
	CompleteConfigurationSourceWork(context.Context, value.Principal, ConfigurationSourceCompletion) (entity.ManagedConfigurationGitSource, error)
	// FailConfigurationSourceWork records the failure code for the exact leased source task and returns its resulting source state; failure does not establish successful content publication.
	// FailConfigurationSourceWork фиксирует код ошибки точного арендованного задания и возвращает итоговое состояние источника; ошибка не подтверждает успешную публикацию содержимого.
	FailConfigurationSourceWork(context.Context, value.Principal, entity.ManagedConfigurationSourceLease, string) (entity.ManagedConfigurationGitSource, error)
}
