package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// personalInvocationFixture observes the real loop helper's owner/adapter order without live HTTP.
// personalInvocationFixture наблюдает owner/adapter порядок рабочего helper без живого HTTP.
type personalInvocationFixture struct {
	t        *testing.T
	response *cp.BeginPersonalIntegrationInvocationResponse
	err      error
	steps    []string
	request  *cp.BeginPersonalIntegrationInvocationRequest
}

// BeginPersonalIntegrationInvocation captures the exact requested lease and can lose its ACK.
// BeginPersonalIntegrationInvocation сохраняет exact запрошенный lease и может потерять его ACK.
func (fixture *personalInvocationFixture) BeginPersonalIntegrationInvocation(_ context.Context, request *cp.BeginPersonalIntegrationInvocationRequest, _ ...grpc.CallOption) (*cp.BeginPersonalIntegrationInvocationResponse, error) {
	fixture.steps = append(fixture.steps, "begin")
	fixture.request = proto.Clone(request).(*cp.BeginPersonalIntegrationInvocationRequest)
	return fixture.response, fixture.err
}

// Execute observes only the admitted immutable credential and records a single synthetic effect.
// Execute наблюдает лишь допущенную immutable credential и фиксирует один synthetic effect.
func (fixture *personalInvocationFixture) Execute(_ context.Context, request integration.Request) (integration.Result, error) {
	fixture.steps = append(fixture.steps, "execute")
	if request.Credential == nil || request.Credential.Ref != "icr_personalfixture" {
		fixture.t.Fatal("execution selected a shared or missing credential")
	}
	return integration.Result{}, nil
}

// Recover records read-only recovery separately; it cannot be replaced by Execute.
// Recover отдельно фиксирует read-only recovery; она не подменяется Execute.
func (fixture *personalInvocationFixture) Recover(_ context.Context, request integration.Request) (integration.Result, error) {
	fixture.steps = append(fixture.steps, "recover")
	if request.Credential == nil || request.Credential.Ref != "icr_personalfixture" {
		fixture.t.Fatal("recovery selected a shared or missing credential")
	}
	return integration.Result{}, nil
}

// TestPersonalInvocationExecutionBoundary qualifies the actual adapter-dispatch helper, including
// lost ACK, denial, malformed authority and read-only recovery. Owner outcomes are synthetic;
// this test does not claim database, JWT, mounted-secret or external provider acceptance.
// TestPersonalInvocationExecutionBoundary проверяет настоящий adapter-dispatch helper, включая
// потерянный ACK, отказ, malformed authority и read-only recovery. Owner outcomes синтетические;
// тест не подтверждает DB, JWT, mounted-secret либо внешний provider.
func TestPersonalInvocationExecutionBoundary(t *testing.T) {
	for _, scenario := range []struct {
		name                string
		configure           func(*personalInvocationFixture, *cp.IntegrationInvocationClaim)
		steps               []string
		executed, wantError bool
	}{
		{"object check", func(f *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			configureConfluencePersonalClaim(c)
			f.response.ResourcePermissionProfile = c.ResourcePermissionProfile
		}, []string{"begin", "execute"}, true, false},
		{"old owner missing object check", func(_ *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			configureConfluencePersonalClaim(c)
			c.ResourcePermissionProfile = 0
		}, nil, false, true},
		{"old begin missing object check", func(_ *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			configureConfluencePersonalClaim(c)
		}, []string{"begin"}, false, true},
		{"unknown object check", func(_ *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			c.ResourcePermissionProfile = cp.IntegrationCredentialResourcePermissionProfile(99)
		}, nil, false, true},
		{"profile on wrong operation", func(_ *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			configureConfluencePersonalClaim(c)
			c.Operation = "confluence.page.create"
		}, nil, false, true},
		{"unknown claim wire", func(_ *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			c.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 1})
		}, nil, false, true},
		{"denial with object authority", func(f *personalInvocationFixture, _ *cp.IntegrationInvocationClaim) {
			f.response = &cp.BeginPersonalIntegrationInvocationResponse{ResourcePermissionProfile: cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_CONFLUENCE_SERVER_PAGE_UPDATE_V1}
		}, []string{"begin"}, false, true},
		{"admitted", func(*personalInvocationFixture, *cp.IntegrationInvocationClaim) {}, []string{"begin", "execute"}, true, false},
		{"recovery", func(f *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			c.WorkMode = cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY
			f.response.WorkMode = c.WorkMode
		}, []string{"begin", "recover"}, true, false},
		{"revoked", func(f *personalInvocationFixture, _ *cp.IntegrationInvocationClaim) {
			f.response = &cp.BeginPersonalIntegrationInvocationResponse{}
		}, []string{"begin"}, false, false},
		{"lost ACK", func(f *personalInvocationFixture, _ *cp.IntegrationInvocationClaim) {
			f.response = nil
			f.err = errors.New("synthetic lost admission acknowledgment")
		}, []string{"begin"}, false, true},
		{"missing response", func(f *personalInvocationFixture, _ *cp.IntegrationInvocationClaim) { f.response = nil }, []string{"begin"}, false, true},
		{"shared fallback", func(_ *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			c.CredentialRevision = &cp.IntegrationCredentialRevision{Ref: "icr_sharedfixture"}
		}, nil, false, true},
		{"materialization fallback", func(_ *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			c.CredentialMaterializationRef = "shared-fixture"
		}, nil, false, true},
		{"denial with descriptor", func(f *personalInvocationFixture, _ *cp.IntegrationInvocationClaim) { f.response.Admitted = false }, []string{"begin"}, false, true},
		{"wrong mode", func(f *personalInvocationFixture, _ *cp.IntegrationInvocationClaim) {
			f.response.WorkMode = cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY
		}, []string{"begin"}, false, true},
		{"missing descriptor", func(f *personalInvocationFixture, _ *cp.IntegrationInvocationClaim) {
			f.response.CredentialRevision = nil
		}, []string{"begin"}, false, true},
		{"unknown response", func(f *personalInvocationFixture, _ *cp.IntegrationInvocationClaim) {
			f.response.ProtoReflect().SetUnknown([]byte{0x78, 1})
		}, []string{"begin"}, false, true},
		{"expired lease", func(_ *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			c.Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		}, nil, false, true},
		{"missing begin and completion budget", func(_ *personalInvocationFixture, c *cp.IntegrationInvocationClaim) {
			c.Lease.ExpiresAt = timestamppb.New(time.Now().Add(2500 * time.Millisecond))
		}, nil, false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			claim := &cp.IntegrationInvocationClaim{InvocationRef: "inv_personalfixture", CredentialAdmissionRequired: true,
				WorkMode: cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE,
				Lease:    &cp.WorkLease{Ref: "lea_personalfixture", Fence: "fnc_personalfixture", Generation: 1, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}}
			fixture := &personalInvocationFixture{t: t, response: &cp.BeginPersonalIntegrationInvocationResponse{Admitted: true, WorkMode: claim.WorkMode,
				CredentialRevision: &cp.IntegrationCredentialRevision{Ref: "icr_personalfixture", Revision: 1, SecretRef: "kodex-system/kodex-integration-credentials#synthetic-personal",
					SecretUid: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "synthetic-version", ContentSha256: strings.Repeat("a", 64), CreatedAt: timestamppb.New(time.Now())}}}
			scenario.configure(fixture, claim)
			original := proto.Clone(claim)
			result, err := executeInvocationClaim(t.Context(), fixture, fixture, claim, Config{InstanceID: "personal-fixture", RequestTimeout: time.Second, OperationTimeout: time.Second})
			if (err != nil) != scenario.wantError || result.executed != scenario.executed || !reflect.DeepEqual(fixture.steps, scenario.steps) {
				t.Fatalf("unexpected work path: steps=%v executed=%v error=%v", fixture.steps, result.executed, err)
			}
			if !proto.Equal(claim, original) {
				t.Fatal("worker overwrote its immutable owner claim")
			}
			if fixture.request != nil && (fixture.request.InvocationRef != claim.InvocationRef || fixture.request.LeaseRef != claim.Lease.Ref || fixture.request.Fence != claim.Lease.Fence || fixture.request.Generation != claim.Lease.Generation || fixture.request.WorkloadInstance != "personal-fixture") {
				t.Fatal("admission request lost its exact lease or workload")
			}
		})
	}
}

// configureConfluencePersonalClaim supplies inert typed pins for the mandatory object-check path.
// configureConfluencePersonalClaim задаёт неактивные typed pins обязательной проверки объекта.
func configureConfluencePersonalClaim(claim *cp.IntegrationInvocationClaim) {
	claim.DefinitionKey, claim.CapabilityKey, claim.Operation = "confluence-server-dc", "confluence.page.update", "confluence.page.update"
	claim.Risk = cp.IntegrationRisk_INTEGRATION_RISK_WRITE
	claim.ApprovalPolicy = cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_HUMAN_EACH_EFFECT
	claim.ResourceScope = &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_CONFLUENCE_SPACE}
	claim.ResourcePermissionProfile = cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_CONFLUENCE_SERVER_PAGE_UPDATE_V1
}

// TestPersonalResourcePermissionDispatch closes old, mismatched and unknown profile tuples
// before an effect; a compatible Begin keeps the typed profile through RequestFromInvocation.
// TestPersonalResourcePermissionDispatch закрывает старые, несовпавшие и неизвестные profile
// tuples до effect; совместимый Begin сохраняет typed profile через RequestFromInvocation.
func TestPersonalResourcePermissionDispatch(t *testing.T) {
	for _, registered := range []struct{ definition, operation string }{
		{"jira-server-dc", "jira.issue.create"},
		{"jira-server-dc", "jira.issue.comment.write"},
		{"jira-server-dc", "jira.issue.transition.apply"},
		{"jira-server-dc", "jira.issue.search"},
		{"jira-server-dc", "jira.issue.read"},
		{"jira-server-dc", "jira.issue.transition.list"},
		{"confluence-server-dc", "confluence.page.search"},
		{"confluence-server-dc", "confluence.page.read"},
		{"gitlab", "gitlab.issue.create"},
		{"gitlab", "gitlab.branch.create"},
		{"gitlab", "gitlab.merge_request.create"},
		{"gitlab", "gitlab.repository.file.read"},
		{"gitlab", "gitlab.issue.read"},
		{"gitlab", "gitlab.merge_request.read"},
		{"gitlab", "gitlab.pipeline.read"},
	} {
		definition, operation := registered.definition, registered.operation
		for _, scenario := range []string{"ready", "old claim", "old begin", "wrong scope", "foreign profile", "unknown profile", "shared profile"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				contract, exists := integrationpackage.CredentialResourcePermissionContractFor(definition, operation)
				if !exists {
					t.Fatal("registered resource contract missing")
				}
				profile := cp.IntegrationCredentialResourcePermissionProfile(cp.IntegrationCredentialResourcePermissionProfile_value["INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_"+contract.Profile])
				claim := &cp.IntegrationInvocationClaim{InvocationRef: "inv_personalfixture", CredentialAdmissionRequired: true,
					DefinitionKey: definition, CapabilityKey: operation, Operation: operation,
					Risk: cp.IntegrationRisk(cp.IntegrationRisk_value["INTEGRATION_RISK_"+contract.Risk]), ApprovalPolicy: cp.IntegrationApprovalPolicy(cp.IntegrationApprovalPolicy_value["INTEGRATION_APPROVAL_POLICY_"+contract.ApprovalPolicy]),
					ResourceScope:             &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind(cp.IntegrationResourceKind_value["INTEGRATION_RESOURCE_KIND_"+contract.ResourceKind])},
					ResourcePermissionProfile: profile, WorkMode: cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE,
					Lease: &cp.WorkLease{Ref: "lea_personalfixture", Fence: "fnc_personalfixture", Generation: 1, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}}
				fixture := &personalInvocationFixture{t: t, response: &cp.BeginPersonalIntegrationInvocationResponse{Admitted: true,
					WorkMode: claim.WorkMode, ResourcePermissionProfile: profile,
					CredentialRevision: &cp.IntegrationCredentialRevision{Ref: "icr_personalfixture", Revision: 1, SecretRef: "kodex-system/kodex-integration-credentials#synthetic-personal",
						SecretUid: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "synthetic-version", ContentSha256: strings.Repeat("a", 64), CreatedAt: timestamppb.New(time.Now())}}}
				switch scenario {
				case "old claim":
					claim.ResourcePermissionProfile = 0
				case "old begin":
					fixture.response.ResourcePermissionProfile = 0
				case "wrong scope":
					claim.ResourceScope.Kind = cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_UNSPECIFIED
				case "foreign profile":
					claim.ResourcePermissionProfile = cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_CONFLUENCE_SERVER_PAGE_UPDATE_V1
				case "unknown profile":
					claim.ResourcePermissionProfile = cp.IntegrationCredentialResourcePermissionProfile(99)
				case "shared profile":
					claim.CredentialAdmissionRequired = false
				}
				result, err := executeInvocationClaim(t.Context(), fixture, fixture, claim, Config{InstanceID: "personal-fixture", RequestTimeout: time.Second, OperationTimeout: time.Second})
				if scenario == "ready" {
					if err != nil || !result.executed || !reflect.DeepEqual(fixture.steps, []string{"begin", "execute"}) {
						t.Fatal("compatible Jira profile failed dispatch", err)
					}
					material := integration.RequestFromInvocation(result.claim)
					if material.ResourcePermissionProfile != contract.Profile || !material.PersonalCredentialAdmission {
						t.Fatal("typed resource permission profile was lost in adapter request")
					}
				} else if err == nil || result.executed || len(fixture.steps) > 1 || len(fixture.steps) == 1 && fixture.steps[0] != "begin" {
					t.Fatal("incompatible resource permission profile reached an effect", err)
				}
			})
		}
	}
}
