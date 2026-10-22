// Package providercredential coordinates private credential materialization and bounded catalog
// observation; account/task authority is established by the owning transport and domain services.
// Package providercredential координирует приватную materialization учётных данных и ограниченное
// наблюдение каталога; полномочия account/task устанавливают transport и доменные сервисы-владельцы.
package providercredential

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

const (
	// maximumAPIKeyBytes bounds API-key input at 16 KiB before JSON encoding or materialization; it is not a
	// provider-specific token-format validator.
	// maximumAPIKeyBytes ограничивает API-key вход 16 КиБ до JSON кодирования и materialization;
	// provider-специфичный формат token не проверяется.
	maximumAPIKeyBytes = 16 << 10
	// minimumAPIKeyBytes rejects trivially short API-key material below eight bytes without assuming a
	// particular provider key prefix.
	// minimumAPIKeyBytes отклоняет API-key material короче восьми байтов без предположения о конкретном
	// prefix provider ключа.
	minimumAPIKeyBytes = 8
	// defaultDeviceAuthTTL is the default 15-minute pending device-authorization lifetime; constructor
	// validation permits only a bounded configured TTL.
	// defaultDeviceAuthTTL задаёт pending device-authorization срок 15 минут по умолчанию; constructor
	// допускает только ограниченный настроенный TTL.
	defaultDeviceAuthTTL = 15 * time.Minute
	// deviceAuthFinishBudget bounds terminal credential publication and attempt completion using an
	// uncanceled lifecycle-derived context, independent of the expired polling deadline.
	// deviceAuthFinishBudget ограничивает terminal credential публикацию и завершение attempt через
	// неотменённый lifecycle-derived context независимо от истёкшего polling дедлайна.
	deviceAuthFinishBudget = 30 * time.Second
	// deviceAuthCleanupBudget gives authorization fencing, local worker join and durable cleanup
	// receipt/recovery a 40-second upper budget without extending an earlier caller deadline.
	// deviceAuthCleanupBudget задаёт верхний бюджет 40 секунд для authorization fencing, local worker join и
	// durable cleanup receipt/recovery без продления раннего caller дедлайна.
	deviceAuthCleanupBudget = 40 * time.Second
)

var (
	// ErrInvalidInput reports malformed materializer configuration inputs or unsupported capability/state
	// shapes without returning credential bytes.
	// ErrInvalidInput сообщает о повреждённых входных данных materializer либо неподдерживаемых
	// capability/state формах без возврата credential байтов.
	ErrInvalidInput = errors.New("provider credential materializer input is invalid")
	// ErrNotFound maps authoritative absence of a durable device-authorization attempt to the service-facing
	// result.
	// ErrNotFound преобразует авторитетное отсутствие durable device-authorization attempt в service
	// результат.
	ErrNotFound = errors.New("provider credential materializer attempt is not found")
	// ErrConflict reports a mismatched materialization, expired/replaced attempt or contradictory durable
	// result; wrapped storage errors can retain their internal provenance.
	// ErrConflict сообщает о несовпадающей materialization, истёкшей/заменённой attempt или противоречивом
	// durable результате; обёрнутые storage ошибки могут сохранять внутреннее происхождение.
	ErrConflict = errors.New("provider credential materializer state conflicts")
)

// Store exposes exact provider credential, device-attempt and cleanup-fence/receipt operations. The
// transport/task owner supplies fresh authority; storage enforces object identity and durability rather
// than trusting caller-selected references.
// Store предоставляет точные provider credential, device-attempt и cleanup-fence/receipt операции.
// Transport/task owner обеспечивает свежие полномочия; хранилище проверяет identity объекта и
// durability, не доверяя выбранным caller ссылкам.
type Store interface {
	// Check probes the same storage dependency used for materialization readiness without creating provider
	// credentials.
	// Check проверяет storage зависимость рабочего materialization пути для readiness без создания provider
	// credentials.
	Check(context.Context) error
	// CreateProviderAuthorizationAttempt persists pending metadata and returns whether this call created it;
	// an existing exact attempt is reused instead of authorizing a second local worker.
	// CreateProviderAuthorizationAttempt сохраняет pending metadata и сообщает, создал ли их этот вызов;
	// существующая точная attempt используется повторно без разрешения второго local worker.
	CreateProviderAuthorizationAttempt(context.Context, kubernetesstore.ProviderAuthorizationAttempt) (kubernetesstore.ProviderAuthorizationAttempt, bool, error)
	// GetProviderAuthorizationAttempt reads the durable state for one materializer attempt reference,
	// distinguishing absence from conflict or API failure.
	// GetProviderAuthorizationAttempt читает durable состояние одной materializer attempt reference, отличая
	// отсутствие от конфликта и API ошибки.
	GetProviderAuthorizationAttempt(context.Context, string) (kubernetesstore.ProviderAuthorizationAttempt, error)
	// CompleteProviderAuthorizationAttempt performs a fenced pending-to-terminal update or exact terminal
	// replay; a competing transition must not be overwritten.
	// CompleteProviderAuthorizationAttempt выполняет защищённый pending-to-terminal переход либо точный
	// terminal повтор; конкурирующий переход не перезаписывается.
	CompleteProviderAuthorizationAttempt(context.Context, kubernetesstore.ProviderAuthorizationAttempt) (kubernetesstore.ProviderAuthorizationAttempt, error)
	// CreateProviderCredential publishes immutable credential JSON for one account and attempt and returns
	// only exact metadata; callers retain responsibility for clearing input buffers.
	// CreateProviderCredential публикует immutable credential JSON одного account и attempt и возвращает
	// только точные metadata; входные буферы обнуляет вызывающий.
	CreateProviderCredential(context.Context, string, string, []byte) (kubernetesstore.ProviderCredentialDescriptor, error)
	// ReadProviderCredentialExact resolves exact account/object/content pins before returning an independent
	// private credential copy that the caller must clear.
	// ReadProviderCredentialExact разрешает точные account/object/content привязки до возврата независимой
	// приватной credential копии, которую обнуляет вызывающий.
	ReadProviderCredentialExact(context.Context, string, kubernetesstore.ProviderCredentialDescriptor) ([]byte, error)
	// DiscardProviderAuthorizationAttempt compensates one exact authorization snapshot using durable name
	// fences and any separately discovered produced credential, not a broad reference-only delete.
	// DiscardProviderAuthorizationAttempt компенсирует один точный authorization снимок через durable name
	// fences и отдельно обнаруженный produced credential, а не широкое удаление только по ссылке.
	DiscardProviderAuthorizationAttempt(context.Context, string, string, string, string, string) error
	// DiscardProviderCredential compensates only the exact account/attempt-bound immutable credential; a
	// replacement must not be silently removed.
	// DiscardProviderCredential компенсирует только точный account/attempt-bound immutable credential;
	// замена не удаляется молча.
	DiscardProviderCredential(context.Context, string, string, kubernetesstore.ProviderCredentialDescriptor) error
	// CleanupProviderCredential binds a background task/generation to an exact descriptor and returns a
	// durable terminal result or a distinct replacement target.
	// CleanupProviderCredential связывает background task/generation с точным descriptor и возвращает
	// durable terminal результат либо отдельную replacement цель.
	CleanupProviderCredential(context.Context, string, string, int64, kubernetesstore.ProviderCredentialDescriptor) (kubernetesstore.ProviderCredentialCleanupResult, error)
	// ObserveProviderAuthorizationCleanup reads only metadata and exact produced descriptors to select a
	// later cleanup target without polling OAuth or changing state.
	// ObserveProviderAuthorizationCleanup читает только metadata и точные produced descriptors для выбора
	// последующей cleanup цели без OAuth polling и изменения состояния.
	ObserveProviderAuthorizationCleanup(context.Context, kubernetesstore.ProviderAuthorizationCleanupTarget) (kubernetesstore.ProviderAuthorizationCleanupObservation, error)
	// FenceProviderAuthorization blocks delayed producers by replacing the exact pending snapshot or
	// reserving confirmed absence; definitive snapshot rejection remains distinguishable from an unknown
	// write outcome.
	// FenceProviderAuthorization блокирует запоздавших producers заменой точного pending снимка либо
	// резервированием подтверждённого отсутствия; доказанный отказ снимка отличается от неопределённого
	// исхода записи.
	FenceProviderAuthorization(context.Context, kubernetesstore.ProviderAuthorizationCleanupTarget) error
	// FenceAuthorizationCredentialName reserves the deterministic result name only after the pending fence
	// and reports an existing valid credential as a separate exact cleanup target.
	// FenceAuthorizationCredentialName резервирует детерминированное имя результата только после pending
	// fence и сообщает существующий корректный credential как отдельную точную cleanup цель.
	FenceAuthorizationCredentialName(context.Context, kubernetesstore.ProviderAuthorizationCleanupTarget) (*kubernetesstore.ProviderCredentialDescriptor, error)
	// ReadProviderCleanupReceipt reads a canonical terminal record bound to one full cleanup identity,
	// returning absence separately from invalid or conflicting state.
	// ReadProviderCleanupReceipt читает canonical terminal запись, связанную с полной cleanup identity,
	// отдельно возвращая отсутствие и неверное либо противоречивое состояние.
	ReadProviderCleanupReceipt(context.Context, kubernetesstore.ProviderCleanupReceiptIdentity) (kubernetesstore.ProviderCredentialCleanupResult, bool, error)
	// CompleteProviderCleanupReceipt durably records the metadata-only result for one exact cleanup identity
	// and accepts only identical replay without overwriting another outcome.
	// CompleteProviderCleanupReceipt durable сохраняет результат только с metadata для одной точной cleanup
	// identity и допускает лишь идентичный повтор без перезаписи другого исхода.
	CompleteProviderCleanupReceipt(context.Context, kubernetesstore.ProviderCleanupReceiptIdentity, *kubernetesstore.ProviderCredentialDescriptor) (kubernetesstore.ProviderCredentialCleanupResult, error)
}

// DeviceAuthorization returns pending user-facing verification metadata, expiry and exact materializer
// API pins; it contains no raw provider auth JSON.
// DeviceAuthorization возвращает pending пользовательские verification metadata, срок и точные
// materializer API-привязки без сырого provider auth JSON.
type DeviceAuthorization struct {
	MaterializerAttemptRef, VerificationURI, UserCode  string
	MaterializerAttemptUID, MaterializerAttemptVersion string
	ExpiresAt                                          time.Time
}

// DiscardMaterialization selects either an exact authorization attempt snapshot or an exact produced
// credential for compensation; mixed target forms are rejected.
// DiscardMaterialization выбирает либо точный authorization attempt снимок, либо точный produced
// credential для компенсации; смешанные формы цели отклоняются.
type DiscardMaterialization struct {
	AttemptRef, AccountRef                         string
	MaterializerAttemptRef, MaterializerAttemptUID string
	MaterializerAttemptVersion                     string
	Credential                                     *kubernetesstore.ProviderCredentialDescriptor
}

// deviceWorker owns one local device session, its completion signal and a once-closed discard signal;
// durable attempt/fence state remains in the store, not this in-memory worker.
// deviceWorker владеет одной local device сессией, сигналом завершения и однократно закрываемым discard
// сигналом; durable attempt/fence состояние принадлежит store, не worker в памяти.
type deviceWorker struct {
	session DeviceAuthorizationSession
	done    chan struct{}
	discard chan struct{}
	once    sync.Once
}

// Service coordinates private provider materialization with durable attempts and cleanup state while
// tracking only local device workers under a mutex. It is not the account authority owner; its lifecycle
// and shutdown ordering are supplied by composition.
// Service координирует приватную provider materialization с durable attempts и cleanup состоянием,
// учитывая только local device workers под mutex. Он не владеет account полномочиями; lifecycle и
// порядок shutdown обеспечивает composition.
type Service struct {
	lifecycle context.Context
	store     Store
	appServer AppServer
	deviceTTL time.Duration

	mu       sync.Mutex
	sessions map[string]*deviceWorker
	workers  sync.WaitGroup
}

// New validates lifecycle/dependencies and a one-minute to one-hour device TTL, initializes local worker
// tracking and starts no provider process or background work.
// New проверяет lifecycle/зависимости и device TTL от минуты до часа, создаёт local worker учёт и не
// запускает provider процесс или background работу.
func New(lifecycle context.Context, store Store, appServer AppServer, deviceTTL time.Duration) (*Service, error) {
	if lifecycle == nil || store == nil || appServer == nil || deviceTTL < time.Minute || deviceTTL > time.Hour {
		return nil, errors.New("provider credential materializer configuration is invalid")
	}
	return &Service{lifecycle: lifecycle, store: store, appServer: appServer, deviceTTL: deviceTTL, sessions: map[string]*deviceWorker{}}, nil
}

// Check joins storage and app-server readiness errors so both dependencies are probed rather than
// short-circuiting after the first failure.
// Check объединяет storage и app-server readiness ошибки, проверяя обе зависимости без остановки после
// первого отказа.
func (service *Service) Check(ctx context.Context) error {
	return errors.Join(service.store.Check(ctx), service.appServer.Check(ctx))
}

// StartDeviceAuthorization derives stable account/attempt references and reuses an exact unexpired
// pending record across replicas. If absent, it starts a session and persists pending metadata,
// registers a worker only for a newly created record and closes a losing session; fresh authority must
// already be checked upstream.
// StartDeviceAuthorization выводит стабильные account/attempt ссылки и повторно использует точную
// неистёкшую pending запись между репликами. При отсутствии запускает сессию и сохраняет pending
// metadata, регистрирует worker только для новой записи и закрывает проигравшую сессию; свежие
// полномочия проверяются заранее выше.
func (service *Service) StartDeviceAuthorization(
	ctx context.Context,
	attemptRef, accountRef string,
) (DeviceAuthorization, error) {
	materializerRef, homeName, err := deviceAuthorizationReferences(attemptRef, accountRef)
	if err != nil {
		return DeviceAuthorization{}, err
	}
	stored, err := service.store.GetProviderAuthorizationAttempt(ctx, materializerRef)
	if err == nil {
		if stored.AttemptRef != attemptRef || stored.AccountRef != accountRef || stored.State != "PENDING" {
			return DeviceAuthorization{}, ErrConflict
		}
		return service.activeDeviceAuthorization(ctx, stored)
	}
	if !errors.Is(err, kubernetesstore.ErrProviderAttemptNotFound) {
		return DeviceAuthorization{}, errors.Join(ErrConflict, err)
	}
	session, err := service.appServer.StartDeviceAuthorization(ctx, materializerRef, homeName)
	if err != nil {
		return DeviceAuthorization{}, err
	}
	expiresAt := time.Now().UTC().Add(service.deviceTTL)
	wanted := kubernetesstore.ProviderAuthorizationAttempt{
		AttemptRef: attemptRef, AccountRef: accountRef, MaterializerAttemptRef: materializerRef,
		State: "PENDING", VerificationURI: session.VerificationURI(), UserCode: session.UserCode(), ExpiresAt: expiresAt,
	}
	stored, created, err := service.store.CreateProviderAuthorizationAttempt(ctx, wanted)
	if err != nil {
		_ = session.Close()
		return DeviceAuthorization{}, errors.Join(ErrConflict, err)
	}
	if !created {
		_ = session.Close()
		return service.activeDeviceAuthorization(ctx, stored)
	}
	worker := &deviceWorker{session: session, done: make(chan struct{}), discard: make(chan struct{})}
	service.mu.Lock()
	service.sessions[materializerRef] = worker
	service.mu.Unlock()
	service.workers.Add(1)
	go service.waitForDeviceAuthorization(stored, worker)
	return castDeviceAuthorization(stored), nil
}

// activeDeviceAuthorization returns pending metadata only before its expiry; otherwise it clears
// verification fields and attempts durable EXPIRED completion before returning conflict, tolerating a
// competing terminal transition.
// activeDeviceAuthorization возвращает pending metadata только до истечения срока; иначе очищает
// verification поля и пытается durable завершить EXPIRED перед возвратом conflict, допуская
// конкурирующий terminal переход.
func (service *Service) activeDeviceAuthorization(
	ctx context.Context,
	stored kubernetesstore.ProviderAuthorizationAttempt,
) (DeviceAuthorization, error) {
	if time.Now().UTC().Before(stored.ExpiresAt) {
		return castDeviceAuthorization(stored), nil
	}
	stored.State = "EXPIRED"
	stored.SafeFailureCode = "DEVICE_AUTHORIZATION_EXPIRED"
	stored.VerificationURI = ""
	stored.UserCode = ""
	_, err := service.store.CompleteProviderAuthorizationAttempt(ctx, stored)
	if err != nil && !errors.Is(err, kubernetesstore.ErrProviderAttemptConflict) {
		return DeviceAuthorization{}, errors.Join(ErrConflict, err)
	}
	return DeviceAuthorization{}, ErrConflict
}

// Discard rejects mixed/incomplete compensation targets, stops and joins a matching local device worker
// before exact attempt compensation, or delegates an exact credential discard. It does not select or
// delete arbitrary account material.
// Discard отклоняет смешанные/неполные compensation цели, останавливает и дожидается соответствующего
// local device worker до exact attempt компенсации либо передаёт точный credential discard. Произвольный
// account material не выбирается и не удаляется.
func (service *Service) Discard(ctx context.Context, target DiscardMaterialization) error {
	if target.MaterializerAttemptRef != "" {
		if target.Credential != nil || target.MaterializerAttemptUID == "" || target.MaterializerAttemptVersion == "" {
			return ErrInvalidInput
		}
		if err := service.stopAndJoinDeviceWorker(ctx, target.MaterializerAttemptRef); err != nil {
			return err
		}
		return service.store.DiscardProviderAuthorizationAttempt(ctx, target.AttemptRef, target.AccountRef,
			target.MaterializerAttemptRef, target.MaterializerAttemptUID, target.MaterializerAttemptVersion)
	}
	if target.Credential == nil {
		return ErrInvalidInput
	}
	return service.store.DiscardProviderCredential(ctx, target.AttemptRef, target.AccountRef, *target.Credential)
}

// stopAndJoinDeviceWorker snapshots one local registration, signals discard once, closes its session
// and waits for its done signal or the caller's cancellation. It performs no durable fence, receipt or
// map removal and adds no deadline; each caller retains its exact authority and storage ordering.
// stopAndJoinDeviceWorker читает одну local регистрацию, однократно сигнализирует discard, закрывает
// сессию и ждёт done либо отмены caller. Durable fence, receipt и удаление из map здесь не выполняются,
// дедлайн не добавляется; каждый caller сохраняет точный порядок authority и storage действий.
func (service *Service) stopAndJoinDeviceWorker(ctx context.Context, materializerRef string) error {
	service.mu.Lock()
	worker := service.sessions[materializerRef]
	service.mu.Unlock()
	if worker == nil {
		return nil
	}
	worker.once.Do(func() { close(worker.discard) })
	_ = worker.session.Close()
	select {
	case <-worker.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CleanupProviderCredential forwards task, account, generation and descriptor unchanged to storage,
// translating only the invalid-cleanup sentinel into service invalid input.
// CleanupProviderCredential передаёт task, account, generation и descriptor в storage без изменений,
// преобразуя только invalid-cleanup sentinel в service invalid input.
func (service *Service) CleanupProviderCredential(
	ctx context.Context,
	taskRef, accountRef string,
	leaseGeneration int64,
	descriptor kubernetesstore.ProviderCredentialDescriptor,
) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	receipt, err := service.store.CleanupProviderCredential(ctx, taskRef, accountRef, leaseGeneration, descriptor)
	if errors.Is(err, kubernetesstore.ErrProviderCredentialCleanupInvalid) {
		return kubernetesstore.ProviderCredentialCleanupResult{}, ErrInvalidInput
	}
	return receipt, err
}

// ObserveAuthorizationCleanup delegates metadata-only cleanup discovery to storage; it neither starts
// OAuth polling nor fences or joins local workers.
// ObserveAuthorizationCleanup передаёт metadata-only cleanup discovery в storage; OAuth polling, fencing
// и join local workers не выполняются.
func (service *Service) ObserveAuthorizationCleanup(ctx context.Context, target kubernetesstore.ProviderAuthorizationCleanupTarget) (kubernetesstore.ProviderAuthorizationCleanupObservation, error) {
	return service.store.ObserveProviderAuthorizationCleanup(ctx, target)
}

// CleanupAuthorization uses a bounded exact identity and existing terminal receipt first, then durably
// fences producers before local worker shutdown/join. Only after a completed join does it fence the
// result name and persist the terminal receipt with any distinct produced credential.
// CleanupAuthorization сначала использует ограниченную точную identity и существующую terminal
// квитанцию, затем durable блокирует producers до local worker остановки/join. Только после завершённого
// join блокирует result имя и сохраняет terminal квитанцию с отдельным produced credential при наличии.
func (service *Service) CleanupAuthorization(ctx context.Context, target kubernetesstore.ProviderAuthorizationCleanupTarget) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	ctx, cancel := context.WithTimeout(ctx, deviceAuthCleanupBudget)
	defer cancel()
	identity, err := kubernetesstore.AuthorizationCleanupReceiptIdentity(target)
	if err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	if result, found, err := service.store.ReadProviderCleanupReceipt(ctx, identity); err != nil || found {
		return result, err
	}
	// CAS fence закрывает новых producers на всех репликах до локального join.
	if err := service.store.FenceProviderAuthorization(ctx, target); err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	if err := service.stopAndJoinDeviceWorker(ctx, target.MaterializerAttemptRef); err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	produced, err := service.store.FenceAuthorizationCredentialName(ctx, target)
	if err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	return service.store.CompleteProviderCleanupReceipt(ctx, identity, produced)
}

// ObserveDeviceAuthorization maps missing attempts to not-found and durably expires elapsed pending
// state instead of polling a provider. A competing completion triggers authoritative reread rather than
// returning stale expiry metadata.
// ObserveDeviceAuthorization преобразует отсутствие attempt в not-found и durable завершает истёкшее
// pending состояние вместо provider polling. Конкурирующее завершение вызывает авторитетное
// перечитывание вместо выдачи устаревших expiry metadata.
func (service *Service) ObserveDeviceAuthorization(
	ctx context.Context,
	materializerAttemptRef string,
) (kubernetesstore.ProviderAuthorizationAttempt, error) {
	stored, err := service.store.GetProviderAuthorizationAttempt(ctx, materializerAttemptRef)
	if errors.Is(err, kubernetesstore.ErrProviderAttemptNotFound) {
		return kubernetesstore.ProviderAuthorizationAttempt{}, ErrNotFound
	}
	if err != nil {
		return kubernetesstore.ProviderAuthorizationAttempt{}, err
	}
	if stored.State == "PENDING" && !time.Now().UTC().Before(stored.ExpiresAt) {
		stored.State = "EXPIRED"
		stored.SafeFailureCode = "DEVICE_AUTHORIZATION_EXPIRED"
		stored.VerificationURI = ""
		stored.UserCode = ""
		stored, err = service.store.CompleteProviderAuthorizationAttempt(ctx, stored)
		if err != nil && !errors.Is(err, kubernetesstore.ErrProviderAttemptConflict) {
			return kubernetesstore.ProviderAuthorizationAttempt{}, err
		}
		if errors.Is(err, kubernetesstore.ErrProviderAttemptConflict) {
			return service.store.GetProviderAuthorizationAttempt(ctx, materializerAttemptRef)
		}
	}
	return stored, nil
}

// MaterializeAPIKey validates reference/key bounds and unsafe separators, serializes a private API-key
// auth record and publishes immutable material before returning descriptor and a fixed account label.
// Encoded bytes are cleared; the original input and immutable Go string copies are not erased here.
// MaterializeAPIKey проверяет границы reference/key и опасные разделители, сериализует приватную API-key
// auth запись и публикует immutable material до возврата descriptor и фиксированной account метки.
// Encoded байты обнуляются; исходный вход и immutable Go string копии здесь не стираются.
func (service *Service) MaterializeAPIKey(
	ctx context.Context,
	attemptRef, accountRef string,
	apiKey []byte,
) (kubernetesstore.ProviderCredentialDescriptor, string, error) {
	if _, _, err := deviceAuthorizationReferences(attemptRef, accountRef); err != nil ||
		len(apiKey) < minimumAPIKeyBytes || len(apiKey) > maximumAPIKeyBytes ||
		len(strings.TrimSpace(string(apiKey))) != len(apiKey) || strings.ContainsAny(string(apiKey), "\r\n\x00") {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", ErrInvalidInput
	}
	encoded, err := json.Marshal(struct {
		APIKey   string `json:"OPENAI_API_KEY"`
		AuthMode string `json:"auth_mode"`
	}{APIKey: string(apiKey), AuthMode: "apikey"})
	if err != nil {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", errors.New("encode provider API key authorization")
	}
	defer clear(encoded)
	descriptor, err := service.store.CreateProviderCredential(ctx, attemptRef, accountRef, encoded)
	if errors.Is(err, kubernetesstore.ErrProviderCredentialConflict) {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", ErrConflict
	}
	if err != nil {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", err
	}
	return descriptor, "API key account", nil
}

// Close snapshots and removes local worker registrations, closes each session and waits for all
// registered workers. Composition must prevent concurrent new starts; this method does not itself cancel
// the supplied lifecycle or add a shutdown timeout.
// Close снимает снимок и удаляет регистрации local workers, закрывает каждую сессию и ждёт всех
// зарегистрированных workers. Composition запрещает новые конкурирующие starts; метод сам не отменяет
// переданный lifecycle и не добавляет shutdown таймаут.
func (service *Service) Close() error {
	service.mu.Lock()
	sessions := make([]*deviceWorker, 0, len(service.sessions))
	for _, worker := range service.sessions {
		sessions = append(sessions, worker)
	}
	service.sessions = map[string]*deviceWorker{}
	service.mu.Unlock()
	var result error
	for _, worker := range sessions {
		result = errors.Join(result, worker.session.Close())
	}
	service.workers.Wait()
	return result
}

// waitForDeviceAuthorization rereads the exact pending identity before provider Wait, honors discard
// before publication, maps polling/materialization failures and uses an independent bounded finish
// context for terminal persistence. It always clears returned auth bytes, closes the session and signals
// local completion; persistence errors are not retried here.
// waitForDeviceAuthorization перечитывает точную pending identity до provider Wait, учитывает discard до
// публикации, классифицирует polling/materialization ошибки и использует отдельный ограниченный finish
// context для terminal сохранения. Полученные auth байты всегда обнуляются, сессия закрывается, local
// completion сигнализируется; ошибки сохранения здесь не повторяются.
func (service *Service) waitForDeviceAuthorization(
	attempt kubernetesstore.ProviderAuthorizationAttempt,
	worker *deviceWorker,
) {
	defer service.workers.Done()
	defer close(worker.done)
	defer func() {
		service.mu.Lock()
		delete(service.sessions, attempt.MaterializerAttemptRef)
		service.mu.Unlock()
		_ = worker.session.Close()
	}()
	session := worker.session
	wait, cancel := context.WithDeadline(service.lifecycle, attempt.ExpiresAt)
	// Cleanup мог установить fence между Create pending и регистрацией worker.
	// Свежий owner read закрывает запуск такого локального producer.
	current, readErr := service.store.GetProviderAuthorizationAttempt(wait, attempt.MaterializerAttemptRef)
	if readErr != nil || current.State != "PENDING" || current.AttemptRef != attempt.AttemptRef || current.AccountRef != attempt.AccountRef ||
		current.SecretUID != attempt.SecretUID || current.MaterializerAttemptRef != attempt.MaterializerAttemptRef {
		cancel()
		return
	}
	authJSON, masked, err := session.Wait(wait)
	cancel()
	select {
	case <-worker.discard:
		clear(authJSON)
		return
	default:
	}
	terminal := attempt
	terminal.VerificationURI = ""
	terminal.UserCode = ""
	if err == nil {
		finish, finishCancel := context.WithTimeout(context.WithoutCancel(service.lifecycle), deviceAuthFinishBudget)
		descriptor, materializeErr := service.store.CreateProviderCredential(finish, attempt.AttemptRef, attempt.AccountRef, authJSON)
		clear(authJSON)
		if materializeErr == nil {
			terminal.State = "AUTHORIZED"
			terminal.ExternalAccountMasked = masked
			terminal.Credential = &descriptor
		} else {
			terminal.State = "FAILED"
			terminal.SafeFailureCode = "CREDENTIAL_MATERIALIZATION_FAILED"
		}
		_, _ = service.store.CompleteProviderAuthorizationAttempt(finish, terminal)
		finishCancel()
		return
	}
	clear(authJSON)
	if errors.Is(err, context.DeadlineExceeded) {
		terminal.State = "EXPIRED"
		terminal.SafeFailureCode = "DEVICE_AUTHORIZATION_EXPIRED"
	} else if errors.Is(err, errCredentialMaterialization) {
		terminal.State = "FAILED"
		terminal.SafeFailureCode = "CREDENTIAL_MATERIALIZATION_FAILED"
	} else {
		terminal.State = "FAILED"
		terminal.SafeFailureCode = "DEVICE_AUTHORIZATION_FAILED"
	}
	finish, finishCancel := context.WithTimeout(context.WithoutCancel(service.lifecycle), deviceAuthFinishBudget)
	_, _ = service.store.CompleteProviderAuthorizationAttempt(finish, terminal)
	finishCancel()
}

// deviceAuthorizationReferences validates generic bounded references and hashes NUL-separated
// attempt/account into stable materializer and private-home names; prefixes and domain ownership are not
// established by this hash.
// deviceAuthorizationReferences проверяет общие ограниченные ссылки и хеширует разделённые NUL
// attempt/account в стабильные materializer и private-home имена; prefix и доменное владение хешем не
// устанавливаются.
func deviceAuthorizationReferences(attemptRef, accountRef string) (string, string, error) {
	if !validReference(attemptRef) || !validReference(accountRef) {
		return "", "", ErrInvalidInput
	}
	digest := sha256.Sum256([]byte(attemptRef + "\x00" + accountRef))
	suffix := hex.EncodeToString(digest[:16])
	return "pmat_" + suffix, "auth-" + suffix, nil
}

// validReference accepts eight to 96 ASCII reference bytes beginning with a letter and continuing with
// letters, digits, underscore or hyphen; it validates syntax, not resource existence.
// validReference допускает ASCII-ссылки длиной восемь–96 байтов с буквой в начале и дальнейшими буквами,
// цифрами, underscore или hyphen; проверяется синтаксис, не существование ресурса.
func validReference(value string) bool {
	if len(value) < 8 || len(value) > 96 {
		return false
	}
	for index, character := range value {
		if index == 0 && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
		if index > 0 && (character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-') {
			continue
		}
		if index > 0 {
			return false
		}
	}
	return true
}

// castDeviceAuthorization copies only pending verification metadata and exact materializer
// UID/resourceVersion into the service reply, excluding credentials and failure internals.
// castDeviceAuthorization копирует только pending verification metadata и точные materializer
// UID/resourceVersion в service ответ без credentials и внутренних failure данных.
func castDeviceAuthorization(value kubernetesstore.ProviderAuthorizationAttempt) DeviceAuthorization {
	return DeviceAuthorization{
		MaterializerAttemptRef: value.MaterializerAttemptRef, VerificationURI: value.VerificationURI,
		UserCode: value.UserCode, ExpiresAt: value.ExpiresAt,
		MaterializerAttemptUID: value.SecretUID, MaterializerAttemptVersion: value.ResourceVersion,
	}
}

// DefaultDeviceAuthorizationTTL exposes the service's default pending device-authorization lifetime
// without reading environment or changing existing attempts.
// DefaultDeviceAuthorizationTTL возвращает default pending device-authorization срок сервиса без чтения
// окружения и изменения существующих attempts.
func DefaultDeviceAuthorizationTTL() time.Duration { return defaultDeviceAuthTTL }
