package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"strings"
)

func managedGitSourceView(configuration *cp.ManagedConfigurationSet) (*generated.ManagedConfigurationGitSource, error) {
	value := configuration.GetGitSource()
	if value == nil {
		return nil, nil
	}
	if (configuration.GetKind() != cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE && configuration.GetKind() != cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION) ||
		!opaqueHTTPReference.MatchString(value.Ref) || !opaqueHTTPReference.MatchString(value.ConnectionRef) || !validManagedVersion(value.Version) || !validManagedVersion(value.Generation) ||
		!validGitSourceLocation(value.RepositoryRef, value.RefName, value.Path) || (value.ProviderKey != "github" && value.ProviderKey != "gitlab") {
		return nil, errManagedConfigurationShape
	}
	switch value.State {
	case cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_QUEUED,
		cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_CLAIMED,
		cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_READY,
		cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_SYNC_BLOCKED,
		cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_DETACHED:
	default:
		return nil, errManagedConfigurationShape
	}
	detached := value.State == cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_DETACHED
	if (configuration.ManagedBy == cp.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_UI) != detached ||
		!detached && (configuration.Source != value.Ref || configuration.SourceRevision != value.AcceptedCommitSha) {
		return nil, errManagedConfigurationShape
	}
	result := &generated.ManagedConfigurationGitSource{Ref: value.Ref, Version: value.Version, Generation: value.Generation, ConnectionRef: value.ConnectionRef,
		ProviderKey: generated.ManagedConfigurationGitSourceProviderKey(value.ProviderKey), RepositoryRef: value.RepositoryRef, RefName: value.RefName, Path: value.Path,
		State: generated.ManagedConfigurationGitSourceState(strings.TrimPrefix(value.State.String(), "MANAGED_CONFIGURATION_SOURCE_STATE_"))}
	blocked := value.State == cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_SYNC_BLOCKED
	if blocked {
		if value.FailureCode < cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_UNAVAILABLE || value.FailureCode > cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_RESPONSE_INVALID {
			return nil, errManagedConfigurationShape
		}
		failure := generated.ManagedConfigurationGitSourceFailureCode(strings.TrimPrefix(value.FailureCode.String(), "MANAGED_CONFIGURATION_SOURCE_FAILURE_"))
		result.FailureCode = &failure
	} else if value.FailureCode != cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_UNSPECIFIED {
		return nil, errManagedConfigurationShape
	}
	hasPin := value.AcceptedCommitSha != "" || value.AcceptedContentSha256 != "" || value.AcceptedRevisionRef != "" || value.SyncedAt != nil
	if hasPin {
		commit := value.AcceptedCommitSha
		if (len(commit) != 40 && len(commit) != 64) || strings.Trim(commit, "0123456789abcdef") != "" || !validManagedDigest(value.AcceptedContentSha256) ||
			!opaqueHTTPReference.MatchString(value.AcceptedRevisionRef) || value.SyncedAt == nil || value.SyncedAt.CheckValid() != nil {
			return nil, errManagedConfigurationShape
		}
		result.AcceptedCommitSha = &value.AcceptedCommitSha
		result.AcceptedContentSha256 = &value.AcceptedContentSha256
		result.AcceptedRevisionRef = &value.AcceptedRevisionRef
		synced := value.SyncedAt.AsTime()
		result.SyncedAt = &synced
	} else if value.State == cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_READY {
		return nil, errManagedConfigurationShape
	}
	return result, nil
}
