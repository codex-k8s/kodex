package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/url"
	"reflect"
	"slices"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

const localOpenAPIFixtureHostname = "integration-synthetic.kodex-system.svc.cluster.local"

//go:embed sql/integration_egress_origins.sql
var queryIntegrationEgressOrigins string

//go:embed sql/integration_egress_projection_lock.sql
var queryIntegrationEgressProjectionLock string

//go:embed sql/integration_egress_projection_update.sql
var queryIntegrationEgressProjectionUpdate string

// IntegrationEgressHostnames читает только действующие owner bindings.
// Наличие опубликованного draft без connection не открывает сетевой доступ.
func (repository *Repository) IntegrationEgressHostnames(ctx context.Context) ([]string, error) {
	rows, err := repository.pool.Query(ctx, queryIntegrationEgressOrigins)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	hosts := map[string]struct{}{}
	for rows.Next() {
		var key, version, digest, format, content string
		var rawConfiguration []byte
		if rows.Scan(&key, &version, &digest, &rawConfiguration, &format, &content) != nil {
			return nil, errs.ErrUnavailable
		}
		var public map[string]any
		if json.Unmarshal(rawConfiguration, &public) != nil {
			return nil, errs.ErrUnavailable
		}
		configuration, valid := integrationStringConfiguration(public)
		if !valid {
			return nil, errs.ErrUnavailable
		}
		definition, err := repository.executableIntegrationPackage(format, content)
		if err != nil || definition.Metadata.Key != key || definition.Metadata.Version != version || definition.Digest != digest ||
			!definition.ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) ||
			definition.Spec.Adapter != string(integrationpackage.AdapterOpenAPIMCP) || definition.ValidateConfiguration(configuration) != nil {
			return nil, errs.ErrUnavailable
		}
		origin := configuration["base_url"]
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
			(parsed.Port() != "" && parsed.Port() != "443") || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, errs.ErrUnavailable
		}
		host, err := mailpolicy.NormalizeHostname(parsed.Hostname())
		if err != nil || host != parsed.Hostname() {
			return nil, errs.ErrUnavailable
		}
		for _, capability := range definition.Spec.Capabilities {
			if capability.OpenAPI == nil || capability.OpenAPI.ServerOrigin != origin {
				return nil, errs.ErrUnavailable
			}
		}
		// Точный local-only fixture обслуживается прямым TLS client внутри
		// integration-gateway и никогда не расширяет внешний CONNECT policy.
		if host == localOpenAPIFixtureHostname {
			continue
		}
		hosts[host] = struct{}{}
		if len(hosts) > shared.MaximumDestinations {
			return nil, errs.ErrUnavailable
		}
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]string, 0, len(hosts))
	for host := range hosts {
		result = append(result, host)
	}
	slices.Sort(result)
	return result, nil
}

// PrepareIntegrationEgressProjection фиксирует поколение под owner row lock.
// Повтор двух реплик возвращает один документ, не создавая rollout storm.
func (repository *Repository) PrepareIntegrationEgressProjection(ctx context.Context, baseDigest string, resolver shared.Resolver) (shared.Document, error) {
	hosts, err := repository.IntegrationEgressHostnames(ctx)
	if err != nil {
		return shared.Document{}, err
	}
	candidate, err := shared.Produce(ctx, hosts, 1, baseDigest, resolver)
	if err != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	targetDigest := candidate.Digest()
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var generation int64
	var storedDigest string
	var storedRaw []byte
	if tx.QueryRow(ctx, queryIntegrationEgressProjectionLock).Scan(&generation, &storedDigest, &storedRaw) != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	if storedDigest == targetDigest {
		var stored shared.Document
		if json.Unmarshal(storedRaw, &stored) != nil || stored.Validate() != nil || stored.Generation != generation ||
			stored.GatewayPolicyDigest != baseDigest || stored.SourceDigest != candidate.SourceDigest ||
			!reflect.DeepEqual(stored.Destinations, candidate.Destinations) {
			return shared.Document{}, errs.ErrUnavailable
		}
		if tx.Commit(ctx) != nil {
			return shared.Document{}, errs.ErrUnavailable
		}
		return stored, nil
	}
	candidate.Generation = generation + 1
	if candidate.Validate() != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	raw, err := json.Marshal(candidate)
	if err != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	var updated int64
	if tx.QueryRow(ctx, queryIntegrationEgressProjectionUpdate, candidate.Generation, targetDigest, raw, generation).Scan(&updated) != nil || updated != candidate.Generation {
		return shared.Document{}, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	return candidate, nil
}
