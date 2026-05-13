package fleet

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	fleetv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/fleet/v1"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMapFleetError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code codes.Code
		want error
	}{
		{name: "invalid argument", code: codes.InvalidArgument, want: errs.ErrInvalidArgument},
		{name: "forbidden", code: codes.PermissionDenied, want: errs.ErrForbidden},
		{name: "rejected", code: codes.FailedPrecondition, want: errs.ErrPlacementRejected},
		{name: "missing dependency state", code: codes.NotFound, want: errs.ErrPreconditionFailed},
		{name: "conflict", code: codes.Aborted, want: errs.ErrConflict},
		{name: "unavailable", code: codes.Unavailable, want: errs.ErrDependencyUnavailable},
	}
	for _, item := range cases {
		item := item
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()

			got := mapFleetError(status.Error(item.code, item.name))
			if !errors.Is(got, item.want) {
				t.Fatalf("mapFleetError(%s) = %v, want %v", item.code, got, item.want)
			}
		})
	}
}

func TestResolvePlacementRequestPreservesConstraintJSON(t *testing.T) {
	t.Parallel()

	projectID := mustUUID("00000000-0000-0000-0000-000000000001")
	repositoryID := mustUUID("00000000-0000-0000-0000-000000000002")
	preferredScopeID := mustUUID("00000000-0000-0000-0000-000000000003")
	request := resolvePlacementRequest(runtimeservice.PlacementResolutionRequest{
		ProjectID:                &projectID,
		RepositoryIDs:            []uuid.UUID{repositoryID},
		ServiceKeys:              []string{"api"},
		RuntimeMode:              enum.RuntimeModeFullEnv,
		RuntimeProfile:           "go-backend",
		PreferredFleetScopeID:    &preferredScopeID,
		PlacementConstraintsJSON: []byte(`{"regions":["eu-1"],"allow_degraded":true}`),
		RuntimeRequirementsJSON:  []byte(`{"capacity_classes":["standard"]}`),
		Meta: value.CommandMeta{
			CommandID: uuid.MustParse("00000000-0000-0000-0000-000000000004"),
			Actor:     value.Actor{Type: "service", ID: "agent-manager"},
		},
	})

	if request.GetProjectId() != projectID.String() || request.GetRepositoryId() != repositoryID.String() || request.GetServiceKey() != "api" {
		t.Fatalf("scope refs = %s/%s/%s, want project/repository/service", request.GetProjectId(), request.GetRepositoryId(), request.GetServiceKey())
	}
	if request.GetPreferredFleetScopeId() != preferredScopeID.String() {
		t.Fatalf("preferred fleet scope = %s, want %s", request.GetPreferredFleetScopeId(), preferredScopeID)
	}
	if request.GetPlacementConstraintsJson() != `{"regions":["eu-1"],"allow_degraded":true}` {
		t.Fatalf("placement json = %s", request.GetPlacementConstraintsJson())
	}
	if request.GetRuntimeRequirementsJson() != `{"capacity_classes":["standard"]}` {
		t.Fatalf("requirements json = %s", request.GetRuntimeRequirementsJson())
	}
	if request.GetRuntimeMode() != fleetv1.RuntimeMode_RUNTIME_MODE_FULL_ENV {
		t.Fatalf("runtime mode = %s, want full env", request.GetRuntimeMode())
	}
}

func mustUUID(value string) uuid.UUID {
	return uuid.MustParse(value)
}
