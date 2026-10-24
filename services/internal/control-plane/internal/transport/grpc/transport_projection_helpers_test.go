package grpc

import (
	"reflect"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	digestfixture "github.com/codex-k8s/kodex/services/internal/control-plane/testdata/digests"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestContextBindingResultRequiresOwnerIdentityAndVersion checks malformed owner results retain
// the exact Internal status and valid bindings preserve all fields through the shared caster.
// TestContextBindingResultRequiresOwnerIdentityAndVersion проверяет прежний точный status Internal
// для неверных результатов владельца и сохранение всех полей допустимой связи общим caster.
func TestContextBindingResultRequiresOwnerIdentityAndVersion(t *testing.T) {
	for _, input := range []*entity.AgentContextBinding{nil, {}, {Version: 1}, {Ref: "binding", Version: 0}, {Ref: "binding", Version: -1}} {
		got, err := castContextBindingResult(input)
		if got != nil || status.Code(err) != codes.Internal || status.Convert(err).Message() != "context binding result is incomplete" {
			t.Fatal("malformed binding result changed its public failure contract")
		}
	}
	input := &entity.AgentContextBinding{Ref: "binding", Version: 7, AgentRef: "agent", ResourceRef: "resource", RevisionRef: "revision", Digest: "digest"}
	got, err := castContextBindingResult(input)
	want := &cp.AgentContextBinding{Ref: "binding", Version: 7, AgentRef: "agent", ResourceRef: "resource", RevisionRef: "revision", Digest: "digest"}
	if err != nil || !proto.Equal(got, want) {
		t.Fatal("valid binding lost exact projection fields")
	}
}

// TestSecretImpactConsumerPreservesOwnerPinsAndSlicePresence checks direct and draft impact's
// common projection retains outer/nested scope pins and nil versus empty revision slices.
// TestSecretImpactConsumerPreservesOwnerPinsAndSlicePresence проверяет сохранение внешних и
// вложенных scope pins и различия nil и пустого среза ревизий общей проекцией direct/draft impact.
func TestSecretImpactConsumerPreservesOwnerPinsAndSlicePresence(t *testing.T) {
	for _, scope := range []struct {
		name string
		kind cp.RuntimeResourceScopeKind
	}{{"PROJECT", cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT}, {"ORGANIZATION", cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION}} {
		for _, revisions := range [][]int64{nil, {}, {2, 9}} {
			input := entity.RuntimeSecretImpactConsumer{EnvironmentRef: "environment", EnvironmentVersion: 3, EnvironmentVersionRef: "environment-version",
				ScopeKind: scope.name, OrganizationRef: "organization", ProjectRef: "project", SecretRevisions: revisions,
				Consumer: entity.RuntimeEnvironmentConsumer{AgentRef: "agent", AgentVersion: 4, BindingRef: "binding", BindingVersion: 5, VersionRef: "source-version", ScopeKind: scope.name, OrganizationRef: "nested-organization", ProjectRef: "nested-project"}}
			want := &cp.RuntimeSecretImpactConsumer{EnvironmentRef: "environment", EnvironmentVersion: 3, EnvironmentVersionRef: "environment-version",
				ScopeKind: scope.kind, OrganizationRef: "organization", ProjectRef: "project", SecretRevisions: revisions,
				Consumer: &cp.RuntimeEnvironmentConsumer{AgentRef: "agent", AgentVersion: 4, BindingRef: "binding", BindingVersion: 5, VersionRef: "source-version", ScopeKind: scope.kind, OrganizationRef: "nested-organization", ProjectRef: "nested-project"}}
			got := castRuntimeSecretImpactConsumer(input)
			if !proto.Equal(got, want) || !reflect.DeepEqual(got.SecretRevisions, revisions) {
				t.Fatal("secret impact consumer lost owner pins or revision slice presence")
			}
		}
	}
}

// TestDigestCastersPreserveExactTextGrammar checks both digest fields through real usage and
// publication casters, independently specifying acceptance of lowercase hexadecimal text.
// TestDigestCastersPreserveExactTextGrammar проверяет оба digest-поля настоящими usage и
// publication casters с независимо заданным допуском строчного шестнадцатеричного текста.
func TestDigestCastersPreserveExactTextGrammar(t *testing.T) {
	cases := digestfixture.SHA256TextCases()
	if len(cases) != 14 {
		t.Fatal("synthetic digest corpus is incomplete")
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			for _, field := range []string{"context", "catalog"} {
				account := providerUsageReadFixture()
				if field == "context" {
					account.Usage.ContextDigest = test.Text
				} else {
					account.Usage.CatalogDigest = test.Text
					account.Usage.CatalogRevision = "mcat_" + test.Text
				}
				if _, err := castProviderAccountUsageRead(account); (err == nil) != test.Valid {
					t.Fatalf("usage %s digest acceptance changed", field)
				}
			}
			for _, kind := range []string{"AGENT_INSTRUCTIONS", "PROMPT_TEMPLATE"} {
				for _, field := range []string{"plan", "target"} {
					result := publicationImpactResultFixture(kind)
					if field == "plan" {
						result.RevisionImpactPlan.Digest = test.Text
					} else {
						result.RevisionImpactPlan.TargetDigest = test.Text
						result.ManagedRevision.Digest = test.Text
					}
					err := validatePublishedImpactResult(result, kind, "rvip_fixture", "source_fixture")
					if (err == nil) != test.Valid {
						t.Fatalf("%s %s digest acceptance changed", kind, field)
					}
				}
			}
		})
	}
}
