package providercredential

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	kstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

type renewalServiceFixture struct {
	Store
	AppServer
	receipt                          kstore.ProviderRenewalReceipt
	produced                         *kstore.ProviderCredentialDescriptor
	starts, writes                   int
	loseBeginACK, losePublicationACK bool
}

func (*renewalServiceFixture) Check(context.Context) error { return nil }

func (fixture *renewalServiceFixture) ReadProviderCredentialExact(context.Context, string, kstore.ProviderCredentialDescriptor) ([]byte, error) {
	return renewalFixture("fixture-success"), nil
}
func (fixture *renewalServiceFixture) BeginProviderCredentialRenewal(_ context.Context, identity kstore.ProviderRenewalIdentity) (kstore.ProviderRenewalReceipt, bool, error) {
	if fixture.receipt.UID != "" {
		return fixture.receipt, false, nil
	}
	fixture.receipt = kstore.ProviderRenewalReceipt{Identity: identity, State: kstore.ProviderRenewalRunning, UID: "synthetic-uid", ResourceVersion: "1"}
	if fixture.loseBeginACK {
		return kstore.ProviderRenewalReceipt{}, false, errors.New("synthetic lost ACK")
	}
	return fixture.receipt, true, nil
}
func (fixture *renewalServiceFixture) ReadProviderCredentialRenewal(context.Context, kstore.ProviderRenewalIdentity) (kstore.ProviderRenewalReceipt, error) {
	return fixture.receipt, nil
}
func (fixture *renewalServiceFixture) CompleteProviderCredentialRenewal(_ context.Context, receipt kstore.ProviderRenewalReceipt, produced kstore.ProviderCredentialDescriptor) (kstore.ProviderRenewalReceipt, error) {
	fixture.receipt = receipt
	fixture.receipt.State, fixture.receipt.Produced = kstore.ProviderRenewalSucceeded, &produced
	return fixture.receipt, nil
}
func (fixture *renewalServiceFixture) RecoverProviderRenewalCredential(context.Context, kstore.ProviderRenewalIdentity) (kstore.ProviderCredentialDescriptor, bool, error) {
	if fixture.produced == nil {
		return kstore.ProviderCredentialDescriptor{}, false, nil
	}
	return *fixture.produced, true, nil
}
func (fixture *renewalServiceFixture) CreateProviderCredential(_ context.Context, _, _ string, raw []byte) (kstore.ProviderCredentialDescriptor, error) {
	fixture.writes++
	if _, err := parseManagedRenewalAuth(raw); err != nil {
		return kstore.ProviderCredentialDescriptor{}, err
	}
	fixture.produced = &kstore.ProviderCredentialDescriptor{SecretName: "synthetic-result", SecretUID: "synthetic-uid", SecretResourceVersion: "2", ContentSHA256: strings.Repeat("a", 64)}
	if fixture.losePublicationACK {
		return kstore.ProviderCredentialDescriptor{}, errors.New("synthetic lost publication ACK")
	}
	return *fixture.produced, nil
}
func (fixture *renewalServiceFixture) renewManagedCredential(ctx context.Context, _ []byte, persist func(context.Context, []byte) error) error {
	fixture.starts++
	return persist(ctx, renewalFixture("fixture-success"))
}

func TestManagedRenewalServiceNeverRepeatsEffectAfterLostACK(t *testing.T) {
	for _, mode := range []string{"normal", "begin-ack", "publication-ack"} {
		t.Run(mode, func(t *testing.T) {
			fixture := &renewalServiceFixture{loseBeginACK: mode == "begin-ack", losePublicationACK: mode == "publication-ack"}
			service := &Service{lifecycle: t.Context(), store: fixture, appServer: fixture}
			identity := kstore.ProviderRenewalIdentity{ExpiresAt: time.Now().Add(time.Minute)}
			for range 2 {
				result, err := service.RenewManagedCredential(t.Context(), identity)
				if mode == "begin-ack" {
					if !errors.Is(err, errManagedRenewalUnknown) {
						t.Fatal("unknown reservation became success")
					}
				} else if err != nil || result.State != kstore.ProviderRenewalSucceeded || result.Credential == nil {
					t.Fatal("published result did not rejoin")
				}
			}
			wantStarts := 1
			if mode == "begin-ack" {
				wantStarts = 0
			}
			if fixture.starts != wantStarts || fixture.writes != wantStarts {
				t.Fatal("lost ACK repeated native refresh or publication")
			}
			starts, writes := fixture.starts, fixture.writes
			_, _ = service.ObserveManagedRenewal(t.Context(), identity)
			if fixture.starts != starts || fixture.writes != writes {
				t.Fatal("READ rejoin performed a write")
			}
		})
	}
}

func TestManagedRenewalServiceStopsAtLifecycleCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fixture := &renewalServiceFixture{}
	service := &Service{lifecycle: ctx, store: fixture, appServer: fixture}
	if _, err := service.RenewManagedCredential(t.Context(), kstore.ProviderRenewalIdentity{ExpiresAt: time.Now().Add(time.Minute)}); err == nil || fixture.starts != 0 || fixture.writes != 0 {
		t.Fatal("cancelled service started renewal")
	}
}
