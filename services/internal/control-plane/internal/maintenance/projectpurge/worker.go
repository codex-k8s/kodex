// Package projectpurge removes verified project-owned external resources before the owner finalizes database deletion.
// Package projectpurge удаляет подтверждённые внешние ресурсы проекта до окончательного удаления данных владельцем из БД.
package projectpurge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// managedLabel names the Kubernetes label that must mark a PVC as platform-managed before deletion; the label alone is insufficient proof of project ownership.
	// managedLabel задаёт метку Kubernetes, подтверждающую управление PVC платформой перед удалением; одной метки недостаточно для доказательства владения проектом.
	managedLabel = "runtime.kodex.dev/managed"
	// sessionHashLabel names the label binding a managed PVC to the expected session hash, checked alongside the derived name and ownership annotations.
	// sessionHashLabel задаёт метку привязки управляемого PVC к ожидаемому хешу сессии, проверяемую вместе с выведенным именем и аннотациями владения.
	sessionHashLabel = "runtime.kodex.dev/session-hash"
	// organizationHashAnnotation names the required organization hash annotation, preventing a matching session/name from authorizing cross-organization deletion.
	// organizationHashAnnotation задаёт обязательную аннотацию хеша организации, не позволяя совпадению session/name разрешить удаление другой организации.
	organizationHashAnnotation = "runtime.kodex.dev/organization-hash"
	// projectHashAnnotation names the project hash annotation; an empty-project hash requires additional server-owned moved-session lineage instead of a caller assertion.
	// projectHashAnnotation задаёт аннотацию хеша проекта; хеш пустого проекта требует дополнительного серверного lineage перенесённой сессии, а не заявления вызывающего.
	projectHashAnnotation = "runtime.kodex.dev/project-hash"
)

// source defines the owner-side purge/recovery metadata port used before external deletion and finalization; the processor receives no direct database handle through this boundary.
// source задаёт порт метаданных владельца для очистки/восстановления перед внешним удалением и финализацией; прямое соединение с БД процессор через эту границу не получает.
type source interface {
	// ClaimProjectPurgeRecovery claims a bounded page of exact recovery work for one generated worker owner; claim generation/deadline are assigned by the repository.
	// ClaimProjectPurgeRecovery захватывает ограниченную страницу точных работ восстановления для сгенерированного владельца worker; поколение и срок захвата назначает репозиторий.
	ClaimProjectPurgeRecovery(context.Context, string, int32) ([]platform.ProjectPurgeRecoveryClaim, error)
	// PinProjectPurgeRecovery persists the exact observed storage receipt under the recovery owner/claim before deletion, so a replacement worker cannot target a later object version.
	// PinProjectPurgeRecovery сохраняет точную наблюдаемую квитанцию хранилища под owner/claim восстановления до удаления, чтобы заменивший worker не выбрал позднюю версию объекта.
	PinProjectPurgeRecovery(context.Context, string, platform.ProjectPurgeRecoveryClaim, objectstorage.Receipt) error
	// FinishProjectPurgeRecovery records the bounded recovery outcome for the exact owner/claim after effects or absence observation; a receipt failure leaves recovery to owner reconciliation.
	// FinishProjectPurgeRecovery фиксирует ограниченный результат восстановления для точных owner/claim после действия или наблюдения отсутствия; при отказе квитанции восстановлением занимается согласование владельца.
	FinishProjectPurgeRecovery(context.Context, string, platform.ProjectPurgeRecoveryClaim, string) error
	// PromoteDueProjectPurges advances a bounded set of due owner-managed project purges without selecting arbitrary projects from processor input.
	// PromoteDueProjectPurges продвигает ограниченный набор наступивших очисток проектов владельца без выбора произвольных проектов входом процессора.
	PromoteDueProjectPurges(context.Context, int32) error
	// ListPendingProjectPurges lists a bounded page of authoritative pending candidates whose project versions and ownership are used by later checks.
	// ListPendingProjectPurges перечисляет ограниченную страницу авторитетных ожидающих кандидатов, версии и владение которых используются последующими проверками.
	ListPendingProjectPurges(context.Context, int32) ([]platform.ProjectPurgeCandidate, error)
	// ProjectPurgeInventory returns the candidate-bound external object/PVC inventory; the processor must validate identities and consumers before applying effects.
	// ProjectPurgeInventory возвращает привязанный к кандидату inventory внешних объектов/PVC; перед действиями процессор проверяет identity и потребителей.
	ProjectPurgeInventory(context.Context, platform.ProjectPurgeCandidate) ([]platform.ProjectPurgeExternalItem, error)
	// ProjectPurgeObjectIsShared checks whether another retained owner reference makes deletion of the exact object key ambiguous; errors must block the effect.
	// ProjectPurgeObjectIsShared проверяет, делает ли другая удержанная ссылка владельца удаление точного ключа объекта неоднозначным; ошибка должна блокировать действие.
	ProjectPurgeObjectIsShared(context.Context, platform.ProjectPurgeCandidate, string) (bool, error)
	// ProjectPurgeArchiveIsOwned confirms candidate ownership of an archive item through server-managed lineage before the processor uses the archive store.
	// ProjectPurgeArchiveIsOwned подтверждает владение кандидата архивным элементом через серверный lineage до обращения процессора к архивному хранилищу.
	ProjectPurgeArchiveIsOwned(context.Context, platform.ProjectPurgeCandidate, platform.ProjectPurgeExternalItem) (bool, error)
	// ProjectPurgeSubjectArtifactIsOwned confirms exact subject-artifact lineage for a project candidate; a subject-prefixed storage key alone cannot authorize deletion.
	// ProjectPurgeSubjectArtifactIsOwned подтверждает точный lineage артефакта субъекта для кандидата проекта; одного ключа с префиксом субъекта недостаточно для разрешения удаления.
	ProjectPurgeSubjectArtifactIsOwned(context.Context, platform.ProjectPurgeCandidate, platform.ProjectPurgeExternalItem) (bool, error)
	// ProjectPurgeGlobalPVCIsOwned confirms that a formerly system-scoped session PVC belongs to the project being purged, independently of its empty-project annotation.
	// ProjectPurgeGlobalPVCIsOwned подтверждает принадлежность ранее системного PVC сессии очищаемому проекту независимо от аннотации пустого проекта.
	ProjectPurgeGlobalPVCIsOwned(context.Context, platform.ProjectPurgeCandidate, string) (bool, error)
	// FinalizeProjectPurge asks the owner to finalize one candidate against the exact processed inventory after verified external absence; it is the database deletion boundary.
	// FinalizeProjectPurge передаёт владельцу финализацию кандидата по точному обработанному inventory после проверенного отсутствия внешних ресурсов; это граница удаления из БД.
	FinalizeProjectPurge(context.Context, platform.ProjectPurgeCandidate, []platform.ProjectPurgeExternalItem) error
}

// Processor holds the owner metadata port, separate artifact/archive stores and a namespace-scoped Kubernetes client for verified purge effects; it owns no goroutine until Run executes it.
// Processor хранит порт метаданных владельца, отдельные хранилища артефактов/архивов и клиент Kubernetes с namespace для проверенной очистки; до Run собственные goroutines не создаёт.
type Processor struct {
	source     source
	objects    objectstorage.Store
	archives   objectstorage.Store
	kubernetes kubernetes.Interface
	namespace  string
}

// New wires explicitly supplied purge dependencies and namespace without probing them, starting work or changing readiness.
// New связывает явно переданные зависимости очистки и namespace без их проверки, запуска работ и изменения готовности.
func New(source source, objects, archives objectstorage.Store, kubernetes kubernetes.Interface, namespace string) *Processor {
	return &Processor{source: source, objects: objects, archives: archives, kubernetes: kubernetes, namespace: namespace}
}

// Run returns a lifecycle-owned minute worker that lazily retries in-cluster Kubernetes client creation and bounds each cycle to five minutes. Cleanup failure is logged and retried while the project remains pending; control-plane readiness is not coupled to this optional cleanup client.
// Run возвращает принадлежащий жизненному циклу минутный worker с повторной ленивой настройкой in-cluster клиента Kubernetes и пятиминутным пределом цикла. Отказ очистки журналируется и повторяется, проект остаётся ожидающим; готовность control-plane не связана с этим клиентом очистки.
func Run(source source, objects, archives objectstorage.Store, namespace string, logger *slog.Logger) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		var processor *Processor
		for {
			if processor == nil {
				config, err := rest.InClusterConfig()
				if err == nil {
					var client kubernetes.Interface
					client, err = kubernetes.NewForConfig(config)
					if err == nil {
						processor = New(source, objects, archives, client, namespace)
					}
				}
				if err != nil {
					logger.WarnContext(ctx, "project purge Kubernetes client unavailable", "error_class", "kubernetes_client")
				}
			}
			if processor != nil {
				cycle, cancel := context.WithTimeout(ctx, 5*time.Minute)
				if err := processor.Cycle(cycle); err != nil && !errors.Is(err, context.Canceled) {
					logFailures(ctx, logger, err)
				}
				cancel()
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
}

// Cycle handles up to four recovery claims before promoting due purges and processing up to four project inventories, preventing recovery starvation behind large projects. It joins per-item failures and lets later independent candidates proceed; earlier external effects are not rolled back.
// Cycle обрабатывает до четырёх захватов восстановления перед продвижением наступивших очисток и до четырёх inventory проектов, предотвращая голодание восстановления за большими проектами. Объединяет ошибки элементов и продолжает независимых кандидатов; прежние внешние действия не откатываются.
func (processor *Processor) Cycle(ctx context.Context) error {
	// Отдельная короткая очередь не голодает за большими inventory проектов.
	owner := uuid.NewString()
	claims, recoveryErr := processor.source.ClaimProjectPurgeRecovery(ctx, owner, 4)
	var failures []error
	if recoveryErr != nil {
		failures = append(failures, &failure{stage: "recovery_claim", cause: recoveryErr})
	}
	for _, claim := range claims {
		if err := processor.recoverObject(ctx, owner, claim); err != nil {
			failures = append(failures, &failure{stage: "recovery_object", projectRef: claim.ProjectRef, cause: err})
		}
	}
	if err := processor.source.PromoteDueProjectPurges(ctx, 8); err != nil {
		return errors.Join(append(failures, err)...)
	}
	candidates, err := processor.source.ListPendingProjectPurges(ctx, 4)
	if err != nil {
		return errors.Join(append(failures, err)...)
	}
	for _, candidate := range candidates {
		if err := processor.process(ctx, candidate); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// failure retains an internal stage, project ref and cause for wrapping while exposing a fixed public message; logFailures deliberately excludes the cause contents.
// failure сохраняет внутренние stage, project ref и причину для оборачивания, наружу выдавая фиксированное сообщение; logFailures намеренно исключает содержимое причины.
type failure struct {
	stage, projectRef string
	cause             error
}

// Error returns a fixed purge-stage failure message without resource keys or provider diagnostics.
// Error возвращает фиксированное сообщение отказа этапа очистки без ключей ресурсов и диагностики провайдера.
func (item *failure) Error() string { return "project purge stage failed" }

// Unwrap preserves the cause for errors.Is/As and cancellation handling; the error chain must not be dumped into purge logs.
// Unwrap сохраняет причину для errors.Is/As и обработки отмены; цепочка ошибки не должна целиком выводиться в журнал очистки.
func (item *failure) Unwrap() error { return item.cause }

// logFailures recursively separates joined failures and logs only the fixed class plus a typed stage/project projection. Raw storage errors, credentials and object coordinates are not serialized.
// logFailures рекурсивно разделяет объединённые отказы и пишет лишь фиксированный класс с типизированной stage/project проекцией. Сырые ошибки хранилища, учётные данные и координаты объектов не сериализуются.
func logFailures(ctx context.Context, logger *slog.Logger, err error) {
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range group.Unwrap() {
			logFailures(ctx, logger, child)
		}
		return
	}
	attributes := []any{"error_class", "purge_cycle"}
	var item *failure
	if errors.As(err, &item) {
		attributes = append(attributes, "safe_stage", item.stage, "project_ref", item.projectRef)
	}
	logger.WarnContext(ctx, "project purge cycle failed", attributes...)
}

// process reads authoritative inventory, checks every PVC consumer before any external effect, deletes only supported object/PVC kinds and finalizes against the exact inventory. A failed item blocks finalization; previously deleted resources are handled by idempotent replay rather than rollback.
// process читает авторитетный inventory, проверяет всех потребителей PVC до внешних действий, удаляет лишь поддержанные object/PVC и финализирует по точному inventory. Ошибка элемента блокирует финализацию; прежние удаления обрабатываются идемпотентным повтором, а не откатом.
func (processor *Processor) process(ctx context.Context, candidate platform.ProjectPurgeCandidate) error {
	items, err := processor.source.ProjectPurgeInventory(ctx, candidate)
	if err != nil {
		return &failure{stage: "inventory", projectRef: candidate.ProjectRef, cause: err}
	}
	if err := processor.checkPVCConsumers(ctx, items); err != nil {
		return &failure{stage: "pvc_consumers", projectRef: candidate.ProjectRef, cause: err}
	}
	for _, item := range items {
		switch item.Kind {
		case "OBJECT":
			if err := processor.deleteObject(ctx, candidate, item); err != nil {
				return &failure{stage: "object", projectRef: candidate.ProjectRef, cause: err}
			}
		case "PVC":
			if err := processor.deletePVC(ctx, candidate, item); err != nil {
				return &failure{stage: "pvc", projectRef: candidate.ProjectRef, cause: err}
			}
		default:
			return errors.New("project purge inventory kind is invalid")
		}
	}
	if err := processor.source.FinalizeProjectPurge(ctx, candidate, items); err != nil {
		return &failure{stage: "finalize", projectRef: candidate.ProjectRef, cause: err}
	}
	return nil
}

// deleteObject validates the project/subject/archive key boundary and server lineage, refuses shared objects, reads the exact version and confirms absence after deleting that version from the selected store. NotFound is replay success, while mismatched ownership/readback blocks deletion.
// deleteObject проверяет project/subject/archive границу ключа и серверный lineage, отвергает общие объекты, читает точную версию и подтверждает её отсутствие после удаления в выбранном хранилище. NotFound означает успешный повтор; несовпадение владения/чтения блокирует удаление.
func (processor *Processor) deleteObject(ctx context.Context, candidate platform.ProjectPurgeCandidate, item platform.ProjectPurgeExternalItem) error {
	artifactPrefix := "organizations/" + candidate.OrganizationRef + "/projects/" + candidate.ProjectRef + "/artifacts/"
	archivePrefix := "session-archive/v1/" + candidate.OrganizationRef + "/"
	if !objectstorage.ValidKey(item.Target) {
		return errors.New("project purge object escaped project scope")
	}
	store := processor.objects
	switch {
	case strings.HasPrefix(item.Target, artifactPrefix):
	case strings.HasPrefix(item.Target, "organizations/"+candidate.OrganizationRef+"/subjects/"):
		owned, err := processor.source.ProjectPurgeSubjectArtifactIsOwned(ctx, candidate, item)
		if err != nil || !owned {
			return errors.New("project purge artifact ownership is unconfirmed")
		}
	case strings.HasPrefix(item.Target, archivePrefix):
		owned, err := processor.source.ProjectPurgeArchiveIsOwned(ctx, candidate, item)
		if err != nil || !owned || processor.archives == nil {
			return errors.New("project purge archive ownership is unconfirmed")
		}
		store = processor.archives
	default:
		return errors.New("project purge object escaped project scope")
	}
	shared, err := processor.source.ProjectPurgeObjectIsShared(ctx, candidate, item.Target)
	if err != nil || shared {
		return errors.New("project purge object ownership is ambiguous")
	}
	receipt, err := store.Head(ctx, item.Target, item.Version)
	if errors.Is(err, objectstorage.ErrNotFound) {
		return nil
	}
	if err != nil || receipt.Key != item.Target || item.Version != "" && receipt.VersionID != item.Version {
		return errors.New("project purge object readback failed")
	}
	if err := store.Delete(ctx, item.Target, receipt.VersionID); err != nil && !errors.Is(err, objectstorage.ErrNotFound) {
		return errors.New("project purge object deletion failed")
	}
	if _, err := store.Head(ctx, item.Target, receipt.VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
		return errors.New("project purge object remains after deletion")
	}
	return nil
}

// checkPVCConsumers validates all inventory PVC identities and scans one paginated namespace Pod snapshot before any deletion. Nonterminal consumers block even if their PVC is already absent; terminal Pods are ignored and list failure blocks the whole candidate.
// checkPVCConsumers проверяет identity всех PVC inventory и один постраничный снимок Pods namespace до удалений. Неитоговый потребитель блокирует даже уже отсутствующий PVC; итоговые Pods пропускаются, отказ списка блокирует кандидата целиком.
func (processor *Processor) checkPVCConsumers(ctx context.Context, items []platform.ProjectPurgeExternalItem) error {
	names := make(map[string]bool)
	for _, item := range items {
		if item.Kind != "PVC" {
			continue
		}
		name, err := processor.pvcName(item)
		if err != nil {
			return err
		}
		names[name] = true
	}
	if len(names) == 0 {
		return nil
	}
	continueToken := ""
	for {
		page, err := processor.kubernetes.CoreV1().Pods(processor.namespace).List(ctx, metav1.ListOptions{Limit: 500, Continue: continueToken})
		if err != nil {
			return errors.New("project purge PVC consumers unavailable")
		}
		for _, pod := range page.Items {
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			for _, volume := range pod.Spec.Volumes {
				if volume.PersistentVolumeClaim != nil && names[volume.PersistentVolumeClaim.ClaimName] {
					return errors.New("project purge PVC is in use")
				}
			}
		}
		continueToken = page.Continue
		if continueToken == "" {
			break
		}
	}
	return nil
}

// deletePVC verifies the canonical name, managed/session/organization/project markers and any required moved-system lineage, then deletes with the observed UID precondition. It polls until absence under the caller context and refuses a replacement UID instead of deleting the replacement.
// deletePVC сверяет каноническое имя, managed/session/organization/project признаки и необходимый lineage перенесённой системной сессии, затем удаляет с наблюдаемым UID precondition. Проверяет отсутствие под контекстом вызывающего и отвергает новый UID вместо удаления замены.
func (processor *Processor) deletePVC(ctx context.Context, candidate platform.ProjectPurgeCandidate, item platform.ProjectPurgeExternalItem) error {
	name, err := processor.pvcName(item)
	if err != nil {
		return err
	}
	pvcs := processor.kubernetes.CoreV1().PersistentVolumeClaims(processor.namespace)
	pvc, err := pvcs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("project purge PVC read failed")
	}
	if pvc.Labels[managedLabel] != "true" || pvc.Labels[sessionHashLabel] != shortHash(item.Version) ||
		pvc.Annotations[organizationHashAnnotation] != shortHash(candidate.OrganizationRef) {
		return errors.New("project purge PVC ownership mismatch")
	}
	if pvc.Annotations[projectHashAnnotation] != shortHash(candidate.ProjectRef) {
		if pvc.Annotations[projectHashAnnotation] != shortHash("") {
			return errors.New("project purge PVC ownership mismatch")
		}
		owned, err := processor.source.ProjectPurgeGlobalPVCIsOwned(ctx, candidate, item.Version)
		if err != nil || !owned {
			return errors.New("project purge global PVC ownership is unconfirmed")
		}
	}
	uid := pvc.UID
	if err := pvcs.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("project purge PVC deletion failed")
	}
	for {
		after, err := pvcs.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return errors.New("project purge PVC deletion readback failed")
		}
		if after.UID != uid {
			return errors.New("project purge PVC was replaced")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// pvcName derives the canonical session PVC name, requires it to match the inventory target and rejects an unset namespace. Consumer preflight and deletion share this identity guard; Kubernetes labels, tenant lineage and UID preconditions remain separate checks.
// pvcName выводит каноническое имя PVC сессии, требует совпадение с целью inventory и отклоняет ненастроенный namespace. Проверка потребителей и удаление делят эту проверку identity; метки Kubernetes, lineage арендатора и UID preconditions проверяются отдельно.
func (processor *Processor) pvcName(item platform.ProjectPurgeExternalItem) (string, error) {
	name, err := runtimecontract.SessionPVCName(item.Version)
	if err != nil || name != item.Target || processor.namespace == "" {
		return "", errors.New("project purge PVC identity is invalid")
	}
	return name, nil
}

// shortHash encodes the first eight SHA-256 bytes as the fixed metadata label/annotation fingerprint; the truncated hash is not a credential or independent ownership proof.
// shortHash кодирует первые восемь байтов SHA-256 как фиксированный отпечаток меток/аннотаций; усечённый хеш не является учётными данными и самостоятельным доказательством владения.
func shortHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}
