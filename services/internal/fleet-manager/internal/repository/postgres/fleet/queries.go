package fleet

import "fmt"

var (
	queryCommandResultCreate              = mustLoadQuery("command_result__create")
	queryCommandResultGet                 = mustLoadQuery("command_result__get")
	queryFleetScopeCreate                 = mustLoadQuery("fleet_scope__create")
	queryFleetScopeGetByID                = mustLoadQuery("fleet_scope__get_by_id")
	queryFleetScopeList                   = mustLoadQuery("fleet_scope__list")
	queryFleetScopeSeedCreate             = mustLoadQuery("fleet_scope__seed_create")
	queryFleetScopeUpdate                 = mustLoadQuery("fleet_scope__update")
	queryKubernetesClusterCreate          = mustLoadQuery("kubernetes_cluster__create")
	queryKubernetesClusterGetByID         = mustLoadQuery("kubernetes_cluster__get_by_id")
	queryKubernetesClusterList            = mustLoadQuery("kubernetes_cluster__list")
	queryKubernetesClusterSeedCreate      = mustLoadQuery("kubernetes_cluster__seed_create")
	queryKubernetesClusterUpdate          = mustLoadQuery("kubernetes_cluster__update")
	queryOutboxEventClaim                 = mustLoadQuery("outbox_event__claim")
	queryOutboxEventInsert                = mustLoadQuery("outbox_event__insert")
	queryOutboxEventMarkFailed            = mustLoadQuery("outbox_event__mark_failed")
	queryOutboxEventMarkPermanentlyFailed = mustLoadQuery("outbox_event__mark_permanently_failed")
	queryOutboxEventMarkPublished         = mustLoadQuery("outbox_event__mark_published")
	queryServerCreate                     = mustLoadQuery("server__create")
	queryServerGetByID                    = mustLoadQuery("server__get_by_id")
	queryServerList                       = mustLoadQuery("server__list")
	queryServerUpdate                     = mustLoadQuery("server__update")
)

func mustLoadQuery(name string) string {
	query, err := loadQuery(name)
	if err != nil {
		panic(err)
	}
	return query
}

func loadQuery(name string) (string, error) {
	data, err := SQLFiles.ReadFile("sql/" + name + ".sql")
	if err != nil {
		return "", fmt.Errorf("load sql query %s: %w", name, err)
	}
	return string(data), nil
}
