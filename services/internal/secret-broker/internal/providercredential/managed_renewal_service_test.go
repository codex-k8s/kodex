package providercredential

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	kstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// renewalServiceFixture models reservation and publication in memory with start/write counters and
// independent lost-ACK modes; embedded production ports are not used for external operations.
// renewalServiceFixture моделирует резервирование и публикацию в памяти со счётчиками start/write и
// независимыми режимами потери ACK; встроенные production ports не используются для внешних операций.
type renewalServiceFixture struct {
	Store
	AppServer
	receipt                          kstore.ProviderRenewalReceipt
	produced                         *kstore.ProviderCredentialDescriptor
	starts, writes                   int
	loseBeginACK, losePublicationACK bool
}

// Check reports synthetic fixture readiness without reading credentials, opening a process or checking
// any external endpoint.
// Check сообщает синтетическую готовность fixture без чтения credentials, открытия процесса и проверки
// внешнего endpoint.
func (*renewalServiceFixture) Check(context.Context) error { return nil }

// ReadProviderCredentialExact returns fresh synthetic managed-auth bytes for the fixture account; it
// intentionally does not model real descriptor validation or ownership.
// ReadProviderCredentialExact возвращает свежие синтетические managed-auth байты fixture account;
// реальные проверки descriptor и владения намеренно не моделируются.
func (fixture *renewalServiceFixture) ReadProviderCredentialExact(context.Context, string, kstore.ProviderCredentialDescriptor) ([]byte, error) {
	return renewalFixture("fixture-success"), nil
}

// BeginProviderCredentialRenewal records one RUNNING reservation before optionally returning a synthetic
// lost ACK; repeats return the existing receipt without a new-effect grant.
// BeginProviderCredentialRenewal записывает одно RUNNING резервирование до необязательной синтетической
// потери ACK; повторы возвращают существующую квитанцию без разрешения нового эффекта.
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

// ReadProviderCredentialRenewal returns the stored fixture receipt unchanged so service observation can
// be tested independently of Kubernetes serialization and CAS.
// ReadProviderCredentialRenewal возвращает сохранённую fixture квитанцию без изменений для проверки
// service observation отдельно от Kubernetes сериализации и CAS.
func (fixture *renewalServiceFixture) ReadProviderCredentialRenewal(context.Context, kstore.ProviderRenewalIdentity) (kstore.ProviderRenewalReceipt, error) {
	return fixture.receipt, nil
}

// CompleteProviderCredentialRenewal replaces the fixture's receipt with SUCCEEDED and the supplied
// descriptor; production exact-result and CAS checks are deliberately outside this fake.
// CompleteProviderCredentialRenewal заменяет fixture квитанцию на SUCCEEDED с переданным descriptor;
// production проверки точного результата и CAS намеренно находятся вне этой подмены.
func (fixture *renewalServiceFixture) CompleteProviderCredentialRenewal(_ context.Context, receipt kstore.ProviderRenewalReceipt, produced kstore.ProviderCredentialDescriptor) (kstore.ProviderRenewalReceipt, error) {
	fixture.receipt = receipt
	fixture.receipt.State, fixture.receipt.Produced = kstore.ProviderRenewalSucceeded, &produced
	return fixture.receipt, nil
}

// RecoverProviderRenewalCredential returns fixture publication presence and metadata without
// incrementing refresh or write counters.
// RecoverProviderRenewalCredential возвращает наличие и metadata fixture публикации без увеличения
// счётчиков refresh и write.
func (fixture *renewalServiceFixture) RecoverProviderRenewalCredential(context.Context, kstore.ProviderRenewalIdentity) (kstore.ProviderCredentialDescriptor, bool, error) {
	if fixture.produced == nil {
		return kstore.ProviderCredentialDescriptor{}, false, nil
	}
	return *fixture.produced, true, nil
}

// CreateProviderCredential increments the publication counter, validates synthetic managed-auth shape
// and saves a descriptor before optionally losing the publication ACK, making read-only recovery
// observable.
// CreateProviderCredential увеличивает счётчик публикаций, проверяет синтетическую managed-auth форму и
// сохраняет descriptor до необязательной потери publication ACK, позволяя наблюдать read-only recovery.
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

// renewManagedCredential counts one synthetic native start and invokes the durable sink with fixture
// credentials, without executing a process or contacting OAuth.
// renewManagedCredential считает один синтетический native запуск и вызывает durable sink с fixture
// credentials без процесса и обращения к OAuth.
func (fixture *renewalServiceFixture) renewManagedCredential(ctx context.Context, _ []byte, persist func(context.Context, []byte) error) error {
	fixture.starts++
	return persist(ctx, renewalFixture("fixture-success"))
}

// TestManagedRenewalServiceNeverRepeatsEffectAfterLostACK repeats the request in normal,
// reservation-ACK-loss and publication-ACK-loss modes and checks at most one start/publication, zero
// starts on unknown reservation and no write during observation.
// TestManagedRenewalServiceNeverRepeatsEffectAfterLostACK повторяет запрос при нормальном ходе, потере
// reservation ACK и publication ACK, проверяя максимум один start/publication, отсутствие запуска при
// неопределённом резервировании и отсутствие write при наблюдении.
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

// TestManagedRenewalServiceStopsAtLifecycleCancellation pre-cancels the service lifecycle and requires
// refusal before either native refresh or credential publication counters change.
// TestManagedRenewalServiceStopsAtLifecycleCancellation заранее отменяет service lifecycle и требует
// отказа до изменения счётчиков native refresh или credential публикации.
func TestManagedRenewalServiceStopsAtLifecycleCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fixture := &renewalServiceFixture{}
	service := &Service{lifecycle: ctx, store: fixture, appServer: fixture}
	if _, err := service.RenewManagedCredential(t.Context(), kstore.ProviderRenewalIdentity{ExpiresAt: time.Now().Add(time.Minute)}); err == nil || fixture.starts != 0 || fixture.writes != 0 {
		t.Fatal("cancelled service started renewal")
	}
}
