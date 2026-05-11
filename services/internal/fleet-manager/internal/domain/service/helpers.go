package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
)

func newBase(id uuid.UUID, now time.Time) entity.Base {
	return entity.Base{ID: id, Version: 1, CreatedAt: now, UpdatedAt: now}
}

func updatedBase(current entity.Base, now time.Time) entity.Base {
	return entity.Base{ID: current.ID, Version: current.Version + 1, CreatedAt: current.CreatedAt, UpdatedAt: now}
}

func defaultJSON(payload []byte) []byte {
	if len(payload) == 0 {
		return []byte("{}")
	}
	return payload
}

func requireJSONObject(payload []byte) error {
	var decoded any
	if err := json.Unmarshal(defaultJSON(payload), &decoded); err != nil {
		return errs.ErrInvalidArgument
	}
	if _, ok := decoded.(map[string]any); !ok {
		return errs.ErrInvalidArgument
	}
	return nil
}

func trimString(value string) string {
	return strings.TrimSpace(value)
}

func applyString(current string, next *string) string {
	if next == nil {
		return current
	}
	return trimString(*next)
}

func applyBytes(current []byte, next *[]byte) []byte {
	if next == nil {
		return current
	}
	return defaultJSON(*next)
}

func requireID(id uuid.UUID) error {
	if id == uuid.Nil {
		return errs.ErrInvalidArgument
	}
	return nil
}

func defaultScopeStatus(status enum.FleetScopeStatus) enum.FleetScopeStatus {
	if status == "" {
		return enum.FleetScopeStatusActive
	}
	return status
}

func defaultServerProvider(provider enum.ServerProviderType) enum.ServerProviderType {
	if provider == "" {
		return enum.ServerProviderTypeUnknown
	}
	return provider
}

func defaultServerStatus(status enum.ServerStatus) enum.ServerStatus {
	if status == "" {
		return enum.ServerStatusActive
	}
	return status
}

func defaultClusterStatus(status enum.KubernetesClusterStatus) enum.KubernetesClusterStatus {
	if status == "" {
		return enum.KubernetesClusterStatusActive
	}
	return status
}

func defaultClusterHealth(status enum.ClusterHealthStatus) enum.ClusterHealthStatus {
	if status == "" {
		return enum.ClusterHealthStatusUnknown
	}
	return status
}

func validateFleetScope(scope entity.FleetScope) error {
	if scope.ID == uuid.Nil || trimString(scope.ScopeKey) == "" || trimString(scope.DisplayName) == "" {
		return errs.ErrInvalidArgument
	}
	if scope.ScopeType == "" || defaultScopeStatus(scope.Status) == "" {
		return errs.ErrInvalidArgument
	}
	if err := requireJSONObject(scope.OwnerRefJSON); err != nil {
		return err
	}
	return nil
}

func validateServer(server entity.Server) error {
	if server.ID == uuid.Nil || trimString(server.ServerKey) == "" || defaultServerProvider(server.ProviderType) == "" || defaultServerStatus(server.Status) == "" {
		return errs.ErrInvalidArgument
	}
	if err := validateSecretReference(server.SecretStoreRef); err != nil {
		return err
	}
	return nil
}

func validateKubernetesCluster(cluster entity.KubernetesCluster) error {
	if cluster.ID == uuid.Nil || cluster.FleetScopeID == uuid.Nil || trimString(cluster.ClusterKey) == "" {
		return errs.ErrInvalidArgument
	}
	if defaultClusterStatus(cluster.Status) == "" || defaultClusterHealth(cluster.LastHealthStatus) == "" {
		return errs.ErrInvalidArgument
	}
	if err := validateSecretReference(cluster.SecretStoreRef); err != nil {
		return err
	}
	return nil
}

func validateSecretReference(reference string) error {
	trimmed := trimString(reference)
	if trimmed == "" {
		return nil
	}
	if strings.Contains(trimmed, "\n") || strings.Contains(trimmed, "-----BEGIN") || strings.Contains(trimmed, "apiVersion:") {
		return errs.ErrInvalidArgument
	}
	return nil
}
