package repository

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// PublisherStore owns durable credential issuance, pinned readback intents and forward-only
// snapshot/rotation records; external Secret delivery and consumer acceptance remain separate effects
// checked before promotion.
// PublisherStore владеет durable credential issuance, закреплёнными readback intents и forward-only
// snapshot/rotation записями; внешняя Secret доставка и consumer принятие остаются отдельными эффектами
// до promotion.
type PublisherStore interface {
	// LoadPublishedCredential returns the prior durable issuance result for an idempotency key,
	// distinguishing absence from storage failure without issuing new material.
	// LoadPublishedCredential возвращает прежний durable issuance результат по idempotency key, отличая
	// отсутствие от storage ошибки без выпуска нового material.
	LoadPublishedCredential(
		context.Context,
		string,
	) (model.PublishedCredential, bool, error)
	// SavePublishedCredential records or rejoins an identical issuance result; a conflicting idempotency
	// input must not overwrite the previously accepted receipt or generations.
	// SavePublishedCredential сохраняет либо повторно получает идентичный issuance результат; конфликтующий
	// idempotency вход не перезаписывает принятые receipt и поколения.
	SavePublishedCredential(
		context.Context,
		model.PublishedCredential,
	) (model.PublishedCredential, error)
	// PinReadbackIntent durably fixes the exact expected target/material/served-state intent and returns its
	// authoritative stored form before independent challenge issuance.
	// PinReadbackIntent durable закрепляет exact expected target/material/served-state intent и возвращает
	// авторитетную сохранённую форму до независимой выдачи challenge.
	PinReadbackIntent(context.Context, model.ReadbackIntent) (model.ReadbackIntent, error)
	// LoadSnapshotHistory reads the persisted forward-only revision/digest chain used when constructing a
	// successor publication; it does not infer history from the current Kubernetes Secret.
	// LoadSnapshotHistory читает сохранённую forward-only revision/digest цепочку для successor публикации,
	// не выводя историю из текущего Kubernetes Secret.
	LoadSnapshotHistory(context.Context) (model.AuthoritySnapshotHistory, error)
	// LoadSnapshotPublication resolves an existing publication by source revision and exact
	// publication-input digest, returning explicit presence rather than silently creating a new intent.
	// LoadSnapshotPublication разрешает существующую публикацию по source revision и exact publication-input
	// digest, возвращая явное наличие без неявного создания нового intent.
	LoadSnapshotPublication(
		context.Context,
		uint64,
		string,
	) (model.AuthoritySnapshotPublication, bool, error)
	// LoadSnapshotPredecessor requires the exact source revision/digest and registry digest to read a stored
	// predecessor with its publication phase, avoiding mutable-latest reconstruction.
	// LoadSnapshotPredecessor требует exact source revision/digest и registry digest для чтения сохранённого
	// predecessor с publication phase без восстановления по mutable-latest.
	LoadSnapshotPredecessor(
		context.Context,
		uint64,
		string,
		string,
	) (model.AuthoritySnapshotPredecessor, bool, error)
	// PrepareRotation reserves one immutable forward-only rotation intent before any external CAS delivery;
	// persistence of intent alone is not completed delivery or promotion.
	// PrepareRotation резервирует один immutable forward-only rotation intent до внешней CAS доставки;
	// сохранение intent не означает завершение доставки и promotion.
	PrepareRotation(
		context.Context,
		model.AuthorityRotationIntent,
	) error
	// LoadOrPrepareRotationOperation creates or rejoins the registry/base-bound rotation operation with
	// server-assigned phase deadlines; retry must not extend an existing operation's validity.
	// LoadOrPrepareRotationOperation создаёт либо повторно получает registry/base-bound rotation operation с
	// server-assigned фазовыми дедлайнами; retry не продлевает действующую operation.
	LoadOrPrepareRotationOperation(
		context.Context,
		model.AuthorityRotationOperation,
	) (model.AuthorityRotationOperation, error)
	// LoadRotationOperation reads the exact operation ID with explicit absence, preserving stored base,
	// status and deadlines for restart/rejoin.
	// LoadRotationOperation читает exact operation ID с явным отсутствием, сохраняя stored base, status и
	// дедлайны для restart/rejoin.
	LoadRotationOperation(
		context.Context,
		string,
	) (model.AuthorityRotationOperation, bool, error)
	// PrepareRotationPhase binds a specific operation phase to its immutable publication intent before
	// delivery, rather than allowing the caller to skip a phase by overwriting current state.
	// PrepareRotationPhase связывает конкретную operation phase с immutable publication intent до доставки,
	// не позволяя пропускать фазу перезаписью current state.
	PrepareRotationPhase(
		context.Context,
		model.AuthorityRotationOperation,
		string,
		model.AuthorityRotationIntent,
	) error
	// AdvanceRotationOperation records phase progress against the exact operation and verified publication
	// result, returning the authoritative operation state rather than a caller-computed next status.
	// AdvanceRotationOperation фиксирует phase progress по exact operation и проверенному publication
	// результату, возвращая авторитетное operation состояние вместо вычисленного caller next status.
	AdvanceRotationOperation(
		context.Context,
		model.AuthorityRotationOperation,
		string,
		model.AuthoritySnapshotPublication,
	) (model.AuthorityRotationOperation, error)
	// BeginRotationDelivery durably marks the transition into external delivery for the pinned intent,
	// separating prepared intent from side effects in progress.
	// BeginRotationDelivery durable отмечает вход во внешнюю доставку для pinned intent, отделяя
	// подготовленный intent от выполняющихся side effects.
	BeginRotationDelivery(
		context.Context,
		model.AuthorityRotationIntent,
	) error
	// MarkRotationDelivered records completed CAS/readback delivery for the intent without substituting for
	// subsequent consumer admission or snapshot promotion.
	// MarkRotationDelivered фиксирует завершённую CAS/readback доставку intent, не заменяя последующий
	// consumer admission и snapshot promotion.
	MarkRotationDelivered(
		context.Context,
		model.AuthorityRotationIntent,
	) error
	// AppendSnapshot appends the exact publication to durable history with its expected readback count and
	// returns the stored publication; divergent predecessors must not mutate accepted history.
	// AppendSnapshot добавляет exact публикацию в durable историю с ожидаемым readback count и возвращает
	// сохранённую публикацию; другой predecessor не меняет принятую историю.
	AppendSnapshot(
		context.Context,
		model.AuthoritySnapshotPublication,
		int,
	) (model.AuthoritySnapshotPublication, error)
	// SnapshotPublicationReady checks the exact publication against the required workload/role/generation
	// target set, not merely a numeric count or the publisher's success flag.
	// SnapshotPublicationReady сверяет exact публикацию с обязательным workload/role/generation target
	// набором, не только числом и publisher success признаком.
	SnapshotPublicationReady(
		context.Context,
		model.AuthoritySnapshotPublication,
		[]model.SnapshotReadbackTarget,
	) error
	// PublisherReady checks readiness of the publisher-owned persistence path without claiming that a
	// particular external Secret or consumer snapshot has already been accepted.
	// PublisherReady проверяет готовность publisher-owned persistence пути без утверждения о принятии
	// конкретного внешнего Secret и consumer snapshot.
	PublisherReady(context.Context) error
}

// SecretMaterial holds the actual version/digest and key-value contents of a trusted delivery read; Data
// can contain private credentials and must not be logged or treated as safe metadata-only output.
// SecretMaterial хранит фактические version/digest и key-value содержимое trusted delivery чтения; Data
// может содержать private credentials и не логируется и не считается safe metadata-only результатом.
type SecretMaterial struct {
	Version uint64
	Data    map[string]string
	Digest  string
}

// SecretDelivery exposes exact registry-target versioned material reads, first creation and
// expected-version CAS; callers cannot use it as an unrestricted secret-store path API.
// SecretDelivery предоставляет чтение versioned material exact registry целей, первое создание и
// expected-version CAS; это не API произвольных secret-store путей.
type SecretDelivery interface {
	// ReadVersioned reads one registered resource name and returns material with explicit presence; it must
	// not redirect an unknown locator to another secret backend.
	// ReadVersioned читает одно зарегистрированное resource имя и возвращает material с явным наличием;
	// неизвестный locator не перенаправляется в другой secret backend.
	ReadVersioned(context.Context, string) (SecretMaterial, bool, error)
	// CreateVersioned creates the initial material for a registered target and returns actual version/digest
	// readback; it is not permission to overwrite an existing generation.
	// CreateVersioned создаёт начальный material зарегистрированной цели и возвращает actual version/digest
	// readback; это не разрешение перезаписи существующего поколения.
	CreateVersioned(context.Context, string, map[string]string) (SecretMaterial, error)
	// WriteVersionedCAS updates only the registered resource at the supplied nonzero expected version,
	// returning actual stored material or a closed conflict/failure.
	// WriteVersionedCAS обновляет только зарегистрированный ресурс с переданной ненулевой expected version,
	// возвращая actual stored material либо закрытый conflict/failure.
	WriteVersionedCAS(
		context.Context,
		string,
		uint64,
		map[string]string,
	) (SecretMaterial, error)
}

// SnapshotDelivery publishes and reads the preconfigured snapshot target independently of persistence
// history and owns any underlying client cleanup.
// SnapshotDelivery публикует и читает заранее настроенную snapshot цель независимо от persistence
// истории и владеет cleanup underlying клиента.
type SnapshotDelivery interface {
	// Publish delivers the exact prepared snapshot publication and returns its observed publication
	// metadata, rather than marking consumers ready solely because a write was attempted.
	// Publish доставляет exact подготовленную snapshot публикацию и возвращает наблюдённые publication
	// metadata без признания consumers готовыми только по попытке записи.
	Publish(
		context.Context,
		model.AuthoritySnapshotPublication,
	) (model.AuthoritySnapshotPublication, error)
	// Read returns the actually served configured snapshot publication for exact readback, not a
	// caller-supplied cached copy.
	// Read возвращает фактически served настроенную snapshot публикацию для exact readback, не переданную
	// caller cached копию.
	Read(
		context.Context,
	) (model.AuthoritySnapshotPublication, error)
	// Close releases snapshot delivery client resources after users have stopped; lifecycle ordering is
	// supplied by the owning composition.
	// Close освобождает ресурсы snapshot delivery клиента после остановки потребителей; порядок lifecycle
	// обеспечивает owning composition.
	Close()
}

// AuthorityGraphLifecycle separates complete graph publication from exact readiness/readback of its
// resulting publication; declaring either method does not bypass domain promotion rules.
// AuthorityGraphLifecycle разделяет полную graph публикацию и exact readiness/readback её результата;
// методы не обходят доменные promotion правила.
type AuthorityGraphLifecycle interface {
	// Publish performs the configured complete authority-material graph publication and returns the
	// resulting snapshot metadata, preserving delivery/history ownership inside the implementation.
	// Publish выполняет настроенную полную authority-material graph публикацию и возвращает snapshot
	// metadata с сохранением delivery/history владения внутри реализации.
	Publish(context.Context) (model.AuthoritySnapshotPublication, error)
	// Ready verifies that the exact supplied publication and its required graph consumers are ready before
	// the caller can treat publication as activated.
	// Ready проверяет готовность exact переданной публикации и обязательных graph consumers до признания
	// публикации активированной.
	Ready(context.Context, model.AuthoritySnapshotPublication) error
}
