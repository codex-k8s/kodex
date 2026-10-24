package rpcprincipal

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

// TestTrustedSTTUserDelegationUsesVerifiedCredentialAndClosedOperations checks the three closed
// trusted-STT operations use verified user identity and reject altered profile, operation,
// permission, peer, project or missing bearer before owner resolution.
// TestTrustedSTTUserDelegationUsesVerifiedCredentialAndClosedOperations проверяет verified user
// identity трёх закрытых trusted STT операций и отказ изменённым profile, операции, permission,
// peer, проекту либо отсутствующему bearer до разрешения владельца.
func TestTrustedSTTUserDelegationUsesVerifiedCredentialAndClosedOperations(t *testing.T) {
	for _, operation := range []string{platformrepo.TrustedSTTAuthorityOperation, platformrepo.TrustedSTTCatalogAuthorityOperation, platformrepo.TrustedSTTPolicyOperation} {
		owner, credentials := &ownerFixture{}, &credentialFixture{}
		service, err := New(owner, credentials)
		if err != nil {
			t.Fatal(err)
		}
		input := Input{Admission: serviceidentity.Admission{
			RPCProfile: transportprofile.TrustedCluster, TargetSPIFFEID: target,
			Peer:        serviceidentity.PeerIdentity{SPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"},
			OperationID: operation, Permission: operation, ActorMode: serviceidentity.UserActor,
		}, Authorization: "Bearer synthetic", RequestDigestSHA256: strings.Repeat("a", 64)}
		principal, err := service.Resolve(t.Context(), input)
		if err != nil || principal.CallerWorkload != "stt-tts-service" || principal.CredentialRevision != 9 ||
			owner.input.ExternalActorID != "verified-subject" || owner.input.RPCProfile != transportprofile.TrustedCluster || credentials.calls != 1 {
			t.Fatal("trusted STT did not resolve verified user")
		}
		for _, mutate := range []func(*Input){
			func(i *Input) { i.Admission.RPCProfile = "service-v1" },
			func(i *Input) { i.Admission.OperationID = "project.read" },
			func(i *Input) { i.Admission.Permission = "organization.manage" },
			func(i *Input) { i.Admission.Peer.SPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/email-bridge" },
			func(i *Input) { i.ProjectRef = "prj_untrusted"; i.Admission.ProjectRequired = true },
			func(i *Input) { i.Authorization = "" },
		} {
			candidate := input
			mutate(&candidate)
			if _, err := service.Resolve(t.Context(), candidate); err == nil || owner.calls != 1 {
				t.Fatal("invalid trusted delegation reached owner")
			}
		}
	}
}

// TestTrustedSTTBrokerProjectionRequiresFreshUserCredential checks broker credential projection
// invokes user verification and cannot reuse service authority when the bearer credential is
// absent.
// TestTrustedSTTBrokerProjectionRequiresFreshUserCredential проверяет вызов user verification
// проекцией credentials broker и невозможность использовать service authority при отсутствующем
// bearer credential.
func TestTrustedSTTBrokerProjectionRequiresFreshUserCredential(t *testing.T) {
	owner, credentials := &ownerFixture{}, &credentialFixture{}
	service, err := New(owner, credentials)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Admission: serviceidentity.Admission{
		RPCProfile: transportprofile.TrustedCluster, TargetSPIFFEID: target,
		Peer:        serviceidentity.PeerIdentity{SPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/secret-broker"},
		OperationID: platformrepo.TrustedSTTCredentialOperation, Permission: platformrepo.TrustedSTTCredentialOperation, ActorMode: serviceidentity.UserActor,
	}, Authorization: "Bearer synthetic", RequestDigestSHA256: strings.Repeat("a", 64)}
	principal, err := service.Resolve(t.Context(), input)
	if err != nil || principal.CallerWorkload != "secret-broker" || credentials.calls != 1 || owner.input.ExternalActorID != "verified-subject" {
		t.Fatal("broker projection did not resolve user credential")
	}
	input.Authorization = ""
	if _, err := service.Resolve(t.Context(), input); err == nil || owner.calls != 1 {
		t.Fatal("broker projection omitted user credential")
	}
}

// ownerFixture captures proof inputs and call counts and supplies configured domain authority or
// service generation for hermetic principal-resolution tests.
// ownerFixture сохраняет proof inputs и счётчики вызовов и предоставляет заданные доменные
// полномочия либо service generation для герметичных тестов principal resolution.
type ownerFixture struct {
	input      platformrepo.ProofPrincipalInput
	calls      int
	generation uint64
	resolved   *platformrepo.ProofAuthority
}

// ResolveProofAuthority records exact proof input and returns either injected owner authority or
// stable synthetic actor/organization with the requested project locator.
// ResolveProofAuthority записывает точный proof input и возвращает заданные полномочия владельца
// либо устойчивые синтетические actor и организацию с запрошенным locator проекта.
func (owner *ownerFixture) ResolveProofAuthority(_ context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	owner.input = input
	owner.calls++
	if owner.resolved != nil {
		return *owner.resolved, nil
	}
	return platformrepo.ProofAuthority{ActorID: "resolved-actor", OrganizationID: "resolved-org", ProjectID: input.ProjectRef}, nil
}

// ResolveServiceCredentialGeneration returns the configured synthetic service credential
// generation without consulting user credentials or external identity providers.
// ResolveServiceCredentialGeneration возвращает заданное синтетическое поколение service
// credentials без обращения к пользовательским credentials либо внешним identity providers.
func (owner *ownerFixture) ResolveServiceCredentialGeneration(context.Context, string) (uint64, error) {
	return owner.generation, nil
}

// credentialFixture counts bearer verification calls while supplying a fixed synthetic external
// identity and session revision.
// credentialFixture подсчитывает проверки bearer с предоставлением фиксированной синтетической
// внешней identity и ревизии сессии.
type credentialFixture struct{ calls int }

// VerifyToken counts verification and returns the fixed synthetic verified subject, organization
// and session generation independently of real credentials.
// VerifyToken подсчитывает проверки и возвращает фиксированные синтетические проверенные subject,
// организацию и поколение сессии независимо от реальных credentials.
func (verifier *credentialFixture) VerifyToken(context.Context, string) (oidcverifier.Principal, error) {
	verifier.calls++
	return oidcverifier.Principal{Subject: "verified-subject", OrganizationID: "verified-org", SessionRevision: 9}, nil
}

// TestServiceGenerationComesFromOwnerWithoutUserCredential checks service identity uses owner
// generation without user verification, rejects bearer input and refuses to downgrade task
// delegation to service resolution.
// TestServiceGenerationComesFromOwnerWithoutUserCredential проверяет поколение владельца для
// service identity без user verification, отказ bearer input и запрет понижения task delegation до
// service resolution.
func TestServiceGenerationComesFromOwnerWithoutUserCredential(t *testing.T) {
	owner := &ownerFixture{generation: 7}
	credentials := &credentialFixture{}
	service, err := New(owner, credentials)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Admission: serviceidentity.Admission{TargetSPIFFEID: target, Peer: serviceidentity.PeerIdentity{SPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/automation-scheduler"}, OperationID: "schedule.claim", Permission: "schedule.claim", ActorMode: serviceidentity.ServiceActor}, RequestDigestSHA256: strings.Repeat("a", 64)}
	principal, err := service.Resolve(t.Context(), input)
	if err != nil || principal.CredentialRevision != 7 || credentials.calls != 0 || owner.input.ExternalActorID != "kodex-system-subject" {
		t.Fatal("service owner resolution failed")
	}
	input.Authorization = "Bearer synthetic"
	if _, err = service.Resolve(t.Context(), input); err == nil || owner.calls != 1 {
		t.Fatal("service accepted caller credential")
	}
	input.Authorization = ""
	input.Admission.ActorMode = serviceidentity.TaskActor
	if _, err = service.Resolve(t.Context(), input); err == nil {
		t.Fatal("task delegation downgraded")
	}
}

// TestUserResolutionPreservesVerifiedSubjectAndProjectBoundary checks gateway user resolution
// preserves verified subject and session generation and rejects an omitted required project before
// a second owner call.
// TestUserResolutionPreservesVerifiedSubjectAndProjectBoundary проверяет сохранение verified
// subject и поколения сессии при gateway user resolution и отказ отсутствующему обязательному
// проекту до повторного вызова владельца.
func TestUserResolutionPreservesVerifiedSubjectAndProjectBoundary(t *testing.T) {
	owner := &ownerFixture{}
	credentials := &credentialFixture{}
	service, err := New(owner, credentials)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Admission: serviceidentity.Admission{TargetSPIFFEID: target, Peer: serviceidentity.PeerIdentity{SPIFFEID: gateway}, OperationID: "project.read", Permission: "project.read", ActorMode: serviceidentity.UserActor, ProjectRequired: true}, ProjectRef: "project-locator", Authorization: "Bearer synthetic", RequestDigestSHA256: strings.Repeat("b", 64)}
	principal, err := service.Resolve(t.Context(), input)
	if err != nil || principal.ActorID != "resolved-actor" || principal.CredentialRevision != 9 || owner.input.ExternalActorID != "verified-subject" || owner.input.ProjectRef != input.ProjectRef {
		t.Fatal("verified identity was not resolved by owner")
	}
	input.ProjectRef = ""
	if _, err = service.Resolve(t.Context(), input); err == nil || owner.calls != 1 {
		t.Fatal("required project boundary omitted")
	}
}
