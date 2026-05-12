// Package kubernetes contains fleet-manager Kubernetes API health probes.
package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	fleetservice "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultTimeout = 5 * time.Second
	userAgent      = "kodex-fleet-manager"
)

// Checker verifies Kubernetes API connectivity without exposing kubeconfig values.
type Checker struct {
	resolver secretresolver.Resolver
	timeout  time.Duration
}

// NewChecker creates a Kubernetes API checker backed by secretresolver.
func NewChecker(resolver secretresolver.Resolver, timeout time.Duration) (*Checker, error) {
	if resolver == nil {
		return nil, secretresolver.ErrUnsupportedStoreType
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Checker{resolver: resolver, timeout: timeout}, nil
}

// CheckClusterConnectivity resolves a kubeconfig secret in memory and probes ServerVersion.
func (c *Checker) CheckClusterConnectivity(ctx context.Context, target fleetservice.ConnectivityCheckTarget) (fleetservice.ConnectivityCheckResult, error) {
	ref := secretresolver.SecretRef{StoreType: target.SecretStoreType, StoreRef: target.SecretStoreRef}
	secret, err := c.resolver.Resolve(ctx, ref)
	if err != nil {
		return secretResolverFailure(err), nil
	}
	defer secret.Clear()

	kubeconfig := secret.Bytes()
	defer clear(kubeconfig)
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return failedResult(enum.ConnectivityCheckStatusFailed, "invalid_cluster_secret", "Cluster access secret is not a valid kubeconfig"), nil
	}
	config.Timeout = c.timeout
	config.UserAgent = userAgent

	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return failedResult(enum.ConnectivityCheckStatusFailed, "kubernetes_client_init_failed", "Kubernetes API client could not be initialized"), nil
	}
	startedAt := time.Now()
	version, err := client.ServerVersion()
	latency := time.Since(startedAt).Milliseconds()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return failedResultWithLatency(enum.ConnectivityCheckStatusTimedOut, "kubernetes_api_timeout", "Kubernetes API check timed out", latency), nil
		}
		return kubernetesAPIFailure(err, latency), nil
	}
	if version == nil {
		return failedResultWithLatency(enum.ConnectivityCheckStatusFailed, "kubernetes_version_unavailable", "Kubernetes API version response was empty", latency), nil
	}
	summary, err := json.Marshal(map[string]string{"probe": "server_version", "kubernetes_version": version.GitVersion})
	if err != nil {
		return failedResultWithLatency(enum.ConnectivityCheckStatusFailed, "kubernetes_summary_failed", "Kubernetes API health summary could not be encoded", latency), nil
	}
	return fleetservice.ConnectivityCheckResult{
		Status:         enum.ConnectivityCheckStatusSucceeded,
		HealthStatus:   enum.ClusterHealthStatusHealthy,
		CapacityStatus: enum.CapacityStatusUnknown,
		SummaryJSON:    summary,
		LatencyMS:      &latency,
	}, nil
}

func secretResolverFailure(err error) fleetservice.ConnectivityCheckResult {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return failedResult(enum.ConnectivityCheckStatusTimedOut, "secret_resolve_timed_out", "Cluster access secret resolve timed out")
	case errors.Is(err, secretresolver.ErrInvalidRef):
		return failedResult(enum.ConnectivityCheckStatusFailed, "invalid_secret_ref", "Cluster access secret reference is invalid")
	case errors.Is(err, secretresolver.ErrUnsupportedStoreType):
		return failedResult(enum.ConnectivityCheckStatusFailed, "unsupported_secret_store", "Cluster access secret store is not supported")
	case errors.Is(err, secretresolver.ErrSecretNotFound):
		return failedResult(enum.ConnectivityCheckStatusFailed, "secret_not_found", "Cluster access secret was not found")
	default:
		return failedResult(enum.ConnectivityCheckStatusFailed, "secret_unavailable", "Cluster access secret is unavailable")
	}
}

func kubernetesAPIFailure(err error, latency int64) fleetservice.ConnectivityCheckResult {
	switch {
	case apierrors.IsUnauthorized(err):
		return failedResultWithLatency(enum.ConnectivityCheckStatusFailed, "kubernetes_api_auth_failed", "Kubernetes API authentication failed", latency)
	case apierrors.IsForbidden(err):
		return failedResultWithLatency(enum.ConnectivityCheckStatusFailed, "kubernetes_api_forbidden", "Kubernetes API access is forbidden", latency)
	case apierrors.IsTooManyRequests(err):
		return failedResultWithLatency(enum.ConnectivityCheckStatusFailed, "kubernetes_api_rate_limited", "Kubernetes API rate limit was reached", latency)
	default:
		return failedResultWithLatency(enum.ConnectivityCheckStatusFailed, "kubernetes_api_unreachable", "Kubernetes API connectivity check failed", latency)
	}
}

func failedResult(status enum.ConnectivityCheckStatus, code string, message string) fleetservice.ConnectivityCheckResult {
	return fleetservice.ConnectivityCheckResult{
		Status:         status,
		HealthStatus:   enum.ClusterHealthStatusUnhealthy,
		CapacityStatus: enum.CapacityStatusUnknown,
		SummaryJSON:    []byte("{}"),
		ErrorCode:      code,
		ErrorMessage:   message,
	}
}

func failedResultWithLatency(status enum.ConnectivityCheckStatus, code string, message string, latency int64) fleetservice.ConnectivityCheckResult {
	result := failedResult(status, code, message)
	if latency >= 0 {
		result.LatencyMS = &latency
	}
	return result
}
