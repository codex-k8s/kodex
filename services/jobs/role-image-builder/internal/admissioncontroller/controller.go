// Package admissioncontroller materializes a bounded supply-chain Job graph without receiving phase registry, signing or owner credentials.
// Пакет admissioncontroller материализует ограниченный граф supply-chain Jobs, не получая реквизиты фаз для реестра, подписи или владельца.
package admissioncontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const (
	// orchestratedLabel marks Kubernetes objects selected by this controller; it is not domain authorization evidence.
	// orchestratedLabel помечает выбираемые контроллером объекты Kubernetes, но не доказывает доменные полномочия.
	orchestratedLabel = "kodex.dev/image-admission-orchestrated"
	// idLabel binds a phase Job and its workspace to the same 32-character rendered operation identifier.
	// idLabel связывает Job этапа и рабочий том с одним 32-символьным идентификатором сформированной операции.
	idLabel = "kodex.dev/image-admission-id"
	// phaseLabel selects the closed phase whose command and service account must match the managed Job contract.
	// phaseLabel выбирает закрытый этап, команда и service account которого должны соответствовать контракту управляемого Job.
	phaseLabel = "kodex.dev/image-admission-phase"
	// runIDAnnotation carries the timestamp/revision run binding checked during adoption and recovery.
	// runIDAnnotation переносит привязку запуска ко времени и ревизии, проверяемую при принятии существующего ресурса и восстановлении.
	runIDAnnotation = "kodex.dev/admission-run-id"
	// proofHoldLabel marks reserved executable-proof claim/promotion Jobs rather than granting permission to run them.
	// proofHoldLabel помечает зарезервированные Jobs claim и promotion для проверки исполнения, не выдавая право их запуска.
	proofHoldLabel = "kodex.dev/executable-proof-hold"
	// proofAttempt names the annotation holding the fixed first proof-reservation attempt.
	// proofAttempt задаёт имя аннотации фиксированной первой попытки резервирования для проверки.
	proofAttempt = "kodex.dev/executable-proof-attempt"
	// proofReservation names the annotation binding run, operation, phase and attempt by a deterministic digest.
	// proofReservation задаёт имя аннотации, связывающей запуск, операцию, этап и попытку детерминированным дайджестом.
	proofReservation = "kodex.dev/executable-proof-reservation"
	// policyName is the fixed policy base name, also used as the prefix of digest-suffixed policy names.
	// policyName задаёт фиксированное базовое имя политики и префикс имён с суффиксом дайджеста.
	policyName = "kodex-image-admission-policy"
	// recoveryUIDAnnotation persists the exact failed admit Job UID before that Job is deleted for receipt recovery.
	// recoveryUIDAnnotation сохраняет точный UID неуспешного admit Job до его удаления для восстановления квитанции.
	recoveryUIDAnnotation = "kodex.dev/admission-recovery-uid"
	// recoveryAfterAnnotation persists the earliest next recovery time beside the predecessor UID in the workspace.
	// recoveryAfterAnnotation сохраняет самое раннее время следующего восстановления рядом с UID предшественника на рабочем томе.
	recoveryAfterAnnotation = "kodex.dev/admission-recovery-after"
)

var (
	// revisionPattern accepts exactly 40 lowercase hexadecimal characters; nonzero revision checks are applied separately.
	// revisionPattern допускает ровно 40 шестнадцатеричных символов нижнего регистра; ненулевая ревизия проверяется отдельно.
	revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
	// idPattern restricts rendered operation labels to 32 lowercase hexadecimal characters.
	// idPattern ограничивает метки сформированной операции 32 шестнадцатеричными символами нижнего регистра.
	idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
	// uidPattern checks UUID-shaped lowercase hexadecimal syntax without resolving or authenticating any Kubernetes object.
	// uidPattern проверяет UUID-подобный синтаксис нижнего регистра без поиска или аутентификации объекта Kubernetes.
	uidPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
	// phaseOrder orders admission work; promotion is reconciled separately and is not a fifth sequential admission step.
	// phaseOrder упорядочивает работу допуска; продвижение согласуется отдельно и не является пятым последовательным этапом допуска.
	phaseOrder = []string{"claim", "scan", "sign", "admit"}
	// phaseAccounts assigns each closed phase its expected Kubernetes service account, not its application credentials.
	// phaseAccounts назначает каждому закрытому этапу ожидаемый Kubernetes service account, а не прикладные реквизиты.
	phaseAccounts = map[string]string{
		"claim": "image-admission", "scan": "kodex-image-scanner",
		"sign": "kodex-image-signer", "admit": "image-admission", "promote": "image-promotion",
	}
)

// Controller coordinates managed Kubernetes Jobs/PVCs with a renderer, policy projection and authoritative work source.
// Attempt timestamps are process-local backoff hints; durable recovery cursors live in PVC annotations, and domain authority stays external.
// Controller координирует управляемые Jobs/PVC Kubernetes с renderer, проекцией политики и авторитетным источником работы.
// Время попыток служит локальной подсказкой задержки; устойчивые курсоры восстановления находятся в аннотациях PVC, а доменные полномочия — вне контроллера.
type Controller struct {
	client               kubernetes.Interface
	renderer             Renderer
	config               Config
	logger               *slog.Logger
	now                  func() time.Time
	lastAdmissionAttempt time.Time
	lastPromotionAttempt time.Time
	leader               *fixedLeaderLease
}

// WorkAvailability reports independent admission/promotion availability without carrying a claim or permission to execute a candidate.
// WorkAvailability сообщает независимую доступность допуска и продвижения, не передавая claim или право исполнения кандидата.
type WorkAvailability struct {
	AdmissionAvailable bool
	PromotionAvailable bool
}

// WorkSource reads authoritative supply-chain availability instead of inferring pending owner work from Kubernetes inventory.
// WorkSource читает авторитетную доступность supply-chain работы, а не выводит ожидающую работу владельца из списка Kubernetes.
type WorkSource interface {
	// GetAvailability returns both work flags under the caller's context; an error prevents starting new idle work.
	// GetAvailability возвращает оба признака работы с контекстом вызывающего; ошибка запрещает запуск новой работы в простое.
	GetAvailability(context.Context) (WorkAvailability, error)
}

// InCluster creates a timeout-configured Kubernetes client from the Pod identity and delegates dependency validation to New.
// It configures no leader, starts no loop and does not create workloads.
// InCluster создаёт клиент Kubernetes с таймаутом из идентичности Pod и передаёт проверку зависимостей в New.
// Он не настраивает лидера, не запускает цикл и не создаёт рабочие нагрузки.
func InCluster(config Config, renderer Renderer, logger *slog.Logger) (*Controller, error) {
	client, err := inClusterClient(config.RequestTimeout)
	if err != nil {
		return nil, err
	}
	return New(client, renderer, config, logger)
}

// New rejects missing dependencies or invalid configuration and initializes the local clock without external operations.
// Leadership configuration and execution are separate explicit steps owned by the composition root.
// New отклоняет отсутствующие зависимости и неверную конфигурацию и задаёт локальные часы без внешних операций.
// Настройка лидерства и запуск остаются отдельными явными шагами корня композиции.
func New(client kubernetes.Interface, renderer Renderer, config Config, logger *slog.Logger) (*Controller, error) {
	if client == nil || renderer == nil || logger == nil || config.WorkSource == nil || config.Validate() != nil {
		return nil, errors.New("image admission controller configuration is invalid")
	}
	return &Controller{client: client, renderer: renderer, config: config, logger: logger, now: time.Now}, nil
}

// runReconciler performs bounded reconciliation cycles, checking a configured leader's fresh holder before each cycle.
// It logs only degraded/restored transitions, retries cycle errors on its ticker and exits on context or holder failure.
// runReconciler выполняет ограниченные циклы согласования, перед каждым проверяя свежего держателя настроенного лидера.
// Он пишет только переходы ухудшения и восстановления, повторяет ошибочные циклы по ticker и завершается при отмене или утрате держателя.
func (controller *Controller) runReconciler(ctx context.Context) error {
	ticker := time.NewTicker(controller.config.ReconcileInterval)
	defer ticker.Stop()
	degraded := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		request, cancel := context.WithTimeout(ctx, controller.config.RequestTimeout)
		if controller.leader != nil {
			if err := controller.leader.checkHolder(request); err != nil {
				cancel()
				return err
			}
		}
		err := controller.Reconcile(request)
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) && !degraded {
			degraded = true
			controller.logger.WarnContext(ctx, "image admission orchestration degraded", "error_class", "kubernetes_api")
		} else if err == nil && degraded {
			degraded = false
			controller.logger.InfoContext(ctx, "image admission orchestration restored")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Check reads direct Kubernetes infrastructure and the configured immutable policy projection, never the work-source service.
// It validates an optional pinned lease and proof deadline, then probes labelled Jobs/PVCs without acquiring leadership or writing.
// Check читает прямую инфраструктуру Kubernetes и настроенную immutable-проекцию политики, не обращаясь к сервису источника работы.
// Он проверяет необязательную закреплённую аренду и срок проверки, затем читает помеченные Jobs/PVC без захвата лидерства или записи.
func (controller *Controller) Check(ctx context.Context) error {
	if controller.leader != nil {
		if _, err := controller.leader.check(ctx); err != nil {
			return err
		}
	}
	if err := controller.validateProofHold(controller.now().UTC()); err != nil {
		return err
	}
	policy, err := controller.client.CoreV1().ConfigMaps(controller.config.Namespace).Get(ctx, controller.config.PolicyConfigMap, metav1.GetOptions{})
	if err != nil {
		return errors.New("read image admission policy")
	}
	if _, err := validatePolicy(policy, controller.config.PolicyConfigMap); err != nil {
		return err
	}
	if _, err := controller.client.BatchV1().Jobs(controller.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: orchestratedSelector(), Limit: 1}); err != nil {
		return errors.New("check image admission Kubernetes access")
	}
	if _, err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: orchestratedSelector(), Limit: 1}); err != nil {
		return errors.New("check image admission workspace access")
	}
	return nil
}

// Reconcile validates policy/inventory, cleans admission orphans, advances admission and independently schedules promotion.
// It uses bounded list-page requests but does not follow continuation tokens; Run supplies the leader guard and time budget.
// Partial Kubernetes effects may precede an error, so idempotent adoption and later cycles handle recovery, not a cross-resource transaction.
// Reconcile проверяет политику и список ресурсов, удаляет осиротевшие Jobs допуска, продвигает допуск и независимо планирует продвижение.
// Он задаёт ограниченные страницы списков, но не следует continuation tokens; проверку лидера и бюджет времени предоставляет Run.
// Часть эффектов Kubernetes может предшествовать ошибке, поэтому восстановление обеспечивают идемпотентное принятие и последующие циклы, не общая транзакция ресурсов.
func (controller *Controller) Reconcile(ctx context.Context) error {
	now := controller.now().UTC()
	if err := controller.validateProofHold(now); err != nil {
		return err
	}
	policy, err := controller.client.CoreV1().ConfigMaps(controller.config.Namespace).Get(ctx, controller.config.PolicyConfigMap, metav1.GetOptions{})
	if err != nil {
		return errors.New("read image admission owner policy")
	}
	revision, err := validatePolicy(policy, controller.config.PolicyConfigMap)
	if err != nil {
		return err
	}
	jobs, err := controller.client.BatchV1().Jobs(controller.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: orchestratedSelector(), Limit: 512})
	if err != nil {
		return errors.New("list image admission jobs")
	}
	workspaces, err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: orchestratedSelector(), Limit: 32})
	if err != nil {
		return errors.New("list image admission workspaces")
	}
	if err := validateManagedInventory(jobs.Items, workspaces.Items, controller.config.Namespace); err != nil {
		return err
	}
	orphanCleanupPending, err := controller.deleteOrphanAdmissionJobs(ctx, jobs.Items, workspaces.Items)
	if err != nil {
		return err
	}
	if !orphanCleanupPending {
		if err := controller.reconcileAdmissions(ctx, policy, revision, jobs.Items, workspaces.Items, now); err != nil {
			return err
		}
	}
	if err := controller.reconcilePromotions(ctx, policy, revision, jobs.Items, now); err != nil {
		return err
	}
	return nil
}

// validateProofHold requires an enabled proof hold to expire strictly after now and no more than thirty minutes ahead.
// Disabled proof holds skip the deadline check; no Job is changed by this predicate.
// validateProofHold требует, чтобы включённая задержка проверки истекала строго после now и не позднее чем через тридцать минут.
// При выключенной задержке срок не проверяется; Jobs этим предикатом не меняются.
func (controller *Controller) validateProofHold(now time.Time) error {
	if !controller.config.HoldProofJobs {
		return nil
	}
	if !controller.config.ProofHoldUntil.After(now) || controller.config.ProofHoldUntil.After(now.Add(30*time.Minute)) {
		return errors.New("image admission proof hold deadline is invalid")
	}
	return nil
}

// validateManagedInventory allows at most one workspace and one nonterminal promotion and validates every listed Job/run shape.
// Individual workspace contents are checked later; this does not verify unseen pages or authoritative owner state.
// validateManagedInventory допускает не более одного рабочего тома и одного незавершённого продвижения и проверяет форму всех перечисленных Jobs и запусков.
// Содержимое отдельных томов проверяется позже; невидимые страницы и авторитетное состояние владельца здесь не проверяются.
func validateManagedInventory(jobs []batchv1.Job, workspaces []corev1.PersistentVolumeClaim, namespace string) error {
	if len(workspaces) > 1 {
		return errors.New("multiple active image admission workspaces are forbidden")
	}
	activePromotions := 0
	for index := range jobs {
		job := &jobs[index]
		phase := job.Labels[phaseLabel]
		if !slicesContains(phases, phase) || !validManagedJob(job, namespace, phase) ||
			!validRunIDShape(job.Annotations[runIDAnnotation]) {
			return errors.New("managed image admission job is invalid")
		}
		if phase == "promote" && !jobTerminal(job) {
			activePromotions++
		}
	}
	if activePromotions > 1 {
		return errors.New("multiple active image promotion jobs are forbidden")
	}
	return nil
}

// reconcileAdmissions advances a single managed workspace, preserves deleting workspaces and recovers failed admit receipts.
// Terminal chains are cleaned before local backoff; a new claim requires no workspace, no pause, elapsed retry and available owner work.
// reconcileAdmissions продвигает один управляемый рабочий том, сохраняет удаляемые тома и восстанавливает квитанции неуспешного admit.
// Завершённые цепочки очищаются до локальной задержки; новый claim требует отсутствия тома и паузы, истёкшего retry и доступной работы владельца.
func (controller *Controller) reconcileAdmissions(ctx context.Context, policy *corev1.ConfigMap, revision string, jobs []batchv1.Job, workspaces []corev1.PersistentVolumeClaim, now time.Time) error {
	active := false
	for index := range workspaces {
		workspace := &workspaces[index]
		if !validManagedWorkspace(workspace, controller.config.Namespace) {
			return errors.New("managed image admission workspace is invalid")
		}
		runID := workspace.Annotations[runIDAnnotation]
		if !validRunID(runID, revision) {
			return errors.New("managed image admission run is invalid")
		}
		if workspace.DeletionTimestamp != nil {
			continue
		}
		phase, terminal, failed := nextAdmissionPhase(workspace.Labels[idLabel], jobs)
		if workspace.Annotations[recoveryUIDAnnotation] != "" && !terminal {
			active = true
			if err := controller.recoverFailedAdmission(ctx, policy, workspace, runID, jobs, now); err != nil {
				return err
			}
			continue
		}
		if failed {
			var failedAdmit *batchv1.Job
			for j := range jobs {
				if jobs[j].Labels[idLabel] == workspace.Labels[idLabel] && jobs[j].Labels[phaseLabel] == "admit" && jobFailed(&jobs[j]) {
					failedAdmit = &jobs[j]
					break
				}
			}
			if failedAdmit != nil {
				active = true
				if err := controller.recoverFailedAdmission(ctx, policy, workspace, runID, jobs, now); err != nil {
					return err
				}
				continue
			}
		}
		if terminal || failed {
			if err := controller.deleteAdmissionJobs(ctx, workspace.Labels[idLabel], jobs); err != nil {
				return err
			}
			if err := controller.deleteWorkspace(ctx, workspace); err != nil {
				return err
			}
			controller.lastAdmissionAttempt = now
			continue
		}
		active = true
		if phase != "" {
			if err := controller.ensureAdmissionPhase(ctx, policy, workspace, runID, phase, jobs); err != nil {
				return err
			}
		}
	}
	if active || len(workspaces) != 0 || controller.config.PauseNewRuns || now.Sub(controller.lastAdmissionAttempt) < controller.config.RetryInterval {
		return nil
	}
	availability, err := controller.config.WorkSource.GetAvailability(ctx)
	if err != nil {
		return errors.New("read image supply work availability")
	}
	if !availability.AdmissionAvailable {
		return nil
	}
	runID := makeRunID(now, revision)
	if err := controller.ensurePhase(ctx, policy, runID, "claim"); err != nil {
		return err
	}
	controller.lastAdmissionAttempt = now
	return nil
}

// recoverFailedAdmission reloads exact workspace/Job identities and persists a UID/deadline cursor before deleting a failed admit.
// After retry it recreates only that predecessor-bound receipt path, or closes resources on verified owner-terminal recovery.
// The recovery budget is bounded by workspace creation plus 24 hours; API effects are ordered, not one atomic transaction.
// recoverFailedAdmission перечитывает точные идентичности тома и Job и сохраняет курсор UID и срока до удаления неуспешного admit.
// После задержки восстанавливается только связанный с предшественником путь квитанции либо ресурсы закрываются по проверенному завершению владельца.
// Бюджет восстановления ограничен 24 часами от создания тома; эффекты API упорядочены, но не объединены в одну транзакцию.
func (controller *Controller) recoverFailedAdmission(ctx context.Context, policy *corev1.ConfigMap, workspace *corev1.PersistentVolumeClaim, runID string, jobs []batchv1.Job, now time.Time) error {
	freshWorkspace, err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Get(ctx, workspace.Name, metav1.GetOptions{})
	if err != nil || workspace.UID == "" || freshWorkspace.UID != workspace.UID || freshWorkspace.Annotations[runIDAnnotation] != runID || !validManagedWorkspace(freshWorkspace, controller.config.Namespace) {
		return errors.New("image admission recovery workspace conflicts")
	}
	uid := freshWorkspace.Annotations[recoveryUIDAnnotation]
	deadline := freshWorkspace.Annotations[recoveryAfterAnnotation]
	if (uid == "") != (deadline == "") || (uid != "" && !uidPattern.MatchString(uid)) {
		return errors.New("image admission recovery cursor is invalid")
	}
	if deadline != "" {
		after, err := time.Parse(time.RFC3339Nano, deadline)
		if err != nil || after.After(now.Add(controller.config.RetryInterval)) {
			return errors.New("image admission recovery deadline is invalid")
		}
		if now.Before(after) {
			return nil
		}
	}
	name := "mc-admit-" + workspace.Labels[idLabel] + "-admit"
	previous, err := controller.client.BatchV1().Jobs(controller.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if uid != "" && (apierrors.IsNotFound(err) || err == nil && jobFailed(previous)) {
		if closed, proofErr := controller.cleanupOwnerTerminalRecovery(ctx, freshWorkspace, runID, jobs); proofErr != nil || closed {
			return proofErr
		}
	}
	if err == nil {
		if !validManagedJob(previous, controller.config.Namespace, "admit") || previous.Annotations[runIDAnnotation] != runID || !uidPattern.MatchString(string(previous.UID)) {
			return errors.New("image admission recovery job conflicts")
		}
		if uid != "" && string(previous.UID) != uid && previous.Annotations[failedPredecessorUID] != uid {
			return errors.New("image admission recovery lineage conflicts")
		}
		if !jobFailed(previous) {
			if uid == "" || previous.Annotations[failedPredecessorUID] != uid || len(previous.Spec.Template.Spec.Containers[0].Command) != 4 || previous.Spec.Template.Spec.Containers[0].Command[3] != "ADMIT_PREDECESSOR_FAILED" {
				return errors.New("image admission recovery receipt job conflicts")
			}
			if jobSucceeded(previous) {
				if err := controller.deleteAdmissionJobs(ctx, workspace.Labels[idLabel], jobs); err != nil {
					return err
				}
				if err := controller.deleteWorkspace(ctx, freshWorkspace); err != nil {
					return err
				}
				controller.lastAdmissionAttempt = now
			}
			return nil
		}
		updated := freshWorkspace.DeepCopy()
		if updated.Annotations == nil {
			updated.Annotations = map[string]string{}
		}
		updated.Annotations[recoveryUIDAnnotation] = string(previous.UID)
		updated.Annotations[recoveryAfterAnnotation] = now.Add(controller.config.RetryInterval).UTC().Format(time.RFC3339Nano)
		if freshWorkspace.CreationTimestamp.IsZero() || now.Add(controller.config.RetryInterval).After(freshWorkspace.CreationTimestamp.Add(24*time.Hour)) {
			return errors.New("image admission recovery budget exceeded")
		}
		if _, err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
			return errors.New("persist image admission recovery cursor")
		}
		return controller.deleteAdmissionJob(ctx, previous, "delete failed image admission for receipt recovery")
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("read image admission recovery job")
	}
	if uid == "" {
		return errors.New("image admission recovery predecessor is absent")
	}
	predecessor := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{UID: types.UID(uid), Labels: map[string]string{phaseLabel: "admit"}}}
	return controller.ensurePhaseWithFailure(ctx, policy, runID, "admit", predecessor)
}

// reconcilePromotions orders retained promotions by creation time and waits for the newest one to become terminal and leave backoff.
// Pause/local retry and authoritative availability gate a fresh promotion independently of the admission workspace chain.
// reconcilePromotions упорядочивает сохранённые продвижения по времени создания и ждёт завершения новейшего и окончания задержки.
// Пауза, локальный retry и авторитетная доступность ограничивают новое продвижение независимо от цепочки рабочего тома допуска.
func (controller *Controller) reconcilePromotions(ctx context.Context, policy *corev1.ConfigMap, revision string, jobs []batchv1.Job, now time.Time) error {
	promotions := make([]batchv1.Job, 0)
	for index := range jobs {
		if jobs[index].Labels[phaseLabel] == "promote" {
			promotions = append(promotions, jobs[index])
		}
	}
	sort.Slice(promotions, func(left, right int) bool {
		return promotions[left].CreationTimestamp.Time.After(promotions[right].CreationTimestamp.Time)
	})
	if len(promotions) != 0 {
		latest := &promotions[0]
		if !validManagedJob(latest, controller.config.Namespace, "promote") {
			return errors.New("managed image promotion job is invalid")
		}
		if !jobTerminal(latest) {
			return nil
		}
		terminalAt := latest.CreationTimestamp.Time
		if terminalAt.IsZero() || terminalAt.Before(controller.lastPromotionAttempt) {
			terminalAt = controller.lastPromotionAttempt
		}
		if now.Sub(terminalAt) < controller.config.RetryInterval {
			return nil
		}
	}
	if controller.config.PauseNewRuns || now.Sub(controller.lastPromotionAttempt) < controller.config.RetryInterval {
		return nil
	}
	availability, err := controller.config.WorkSource.GetAvailability(ctx)
	if err != nil {
		return errors.New("read image supply work availability")
	}
	if !availability.PromotionAvailable {
		return nil
	}
	if err := controller.ensurePhase(ctx, policy, makeRunID(now, revision), "promote"); err != nil {
		return err
	}
	controller.lastPromotionAttempt = now
	return nil
}

// ensurePhase delegates ordinary phase materialization with no failed-predecessor binding or implied owner approval.
// ensurePhase передаёт обычную материализацию этапа без привязки неуспешного предшественника или подразумеваемого одобрения владельца.
func (controller *Controller) ensurePhase(ctx context.Context, policy *corev1.ConfigMap, runID, phase string) error {
	return controller.ensurePhaseWithFailure(ctx, policy, runID, phase, nil)
}

// ensureAdmissionPhase materializes a normal phase, except admit reloads a failed scan/sign predecessor before binding recovery.
// The fresh Job must retain its exact UID/run/phase and failed state; stale inventory alone cannot authorize that recovery path.
// ensureAdmissionPhase материализует обычный этап, но для admit перечитывает неуспешный scan/sign перед привязкой восстановления.
// Свежий Job обязан сохранить точные UID, запуск, этап и ошибочное состояние; одного старого списка для пути восстановления недостаточно.
func (controller *Controller) ensureAdmissionPhase(ctx context.Context, policy *corev1.ConfigMap, workspace *corev1.PersistentVolumeClaim, runID, phase string, jobs []batchv1.Job) error {
	if phase != "admit" {
		return controller.ensurePhase(ctx, policy, runID, phase)
	}
	for index := range jobs {
		previous := &jobs[index]
		previousPhase := previous.Labels[phaseLabel]
		if previous.Labels[idLabel] != workspace.Labels[idLabel] ||
			(previousPhase != "scan" && previousPhase != "sign") || !jobFailed(previous) {
			continue
		}
		fresh, err := controller.client.BatchV1().Jobs(controller.config.Namespace).Get(ctx, previous.Name, metav1.GetOptions{})
		if err != nil || previous.UID == "" || fresh.UID != previous.UID ||
			fresh.Annotations[runIDAnnotation] != runID || !jobFailed(fresh) ||
			!validManagedJob(fresh, controller.config.Namespace, previousPhase) {
			return errors.New("failed image admission predecessor conflicts")
		}
		return controller.ensurePhaseWithFailure(ctx, policy, runID, phase, fresh)
	}
	return controller.ensurePhase(ctx, policy, runID, phase)
}

// ensurePhaseWithFailure renders and locally validates a phase, adds optional predecessor/proof bindings, then ensures PVC before Job.
// The renderer owns its output; this path does not issue domain grants or roll back a PVC if later Job creation fails.
// ensurePhaseWithFailure формирует и локально проверяет этап, добавляет необязательные привязки предшественника и проверки и обеспечивает PVC до Job.
// Renderer владеет своим результатом; путь не выдаёт доменные grants и не откатывает PVC при последующей ошибке создания Job.
func (controller *Controller) ensurePhaseWithFailure(ctx context.Context, policy *corev1.ConfigMap, runID, phase string, previous *batchv1.Job) error {
	rendered, err := controller.renderer.Render(ctx, policy, controller.config.Environment, runID, phase)
	if err != nil {
		return err
	}
	if err := prepareRendered(rendered, controller.config.Namespace, runID, phase); err != nil {
		return err
	}
	if previous != nil {
		code := strings.ToUpper(previous.Labels[phaseLabel]) + "_PREDECESSOR_FAILED"
		rendered.Job.Spec.Template.Spec.Containers[0].Command = append(rendered.Job.Spec.Template.Spec.Containers[0].Command, code)
		rendered.Job.Annotations[failedPredecessorUID] = string(previous.UID)
		if !validManagedJob(rendered.Job, controller.config.Namespace, phase) {
			return errors.New("failed image admission recovery is invalid")
		}
	}
	if controller.config.HoldProofJobs && (phase == "claim" || phase == "promote") {
		prepareProofReservation(rendered.Job, runID, phase)
	}
	if rendered.PVC != nil {
		if err := controller.ensureWorkspace(ctx, rendered.PVC); err != nil {
			return err
		}
	}
	return controller.ensureJob(ctx, rendered.Job, runID, phase)
}

// ensureWorkspace creates the rendered PVC or adopts an existing same-name PVC only after run and managed-shape checks.
// Adoption checks selected contract fields rather than byte-for-byte spec equality and does not replace the existing object.
// ensureWorkspace создаёт сформированный PVC либо принимает одноимённый существующий после проверки запуска и управляемой формы.
// При принятии проверяются выбранные поля контракта, а не полное равенство спецификации; существующий объект не заменяется.
func (controller *Controller) ensureWorkspace(ctx context.Context, workspace *corev1.PersistentVolumeClaim) error {
	_, err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Create(ctx, workspace, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Get(ctx, workspace.Name, metav1.GetOptions{})
		if getErr != nil || existing.Annotations[runIDAnnotation] != workspace.Annotations[runIDAnnotation] || !validManagedWorkspace(existing, controller.config.Namespace) {
			return errors.New("existing image admission workspace conflicts")
		}
		return nil
	}
	if err != nil {
		return errors.New("create image admission workspace")
	}
	return nil
}

// ensureJob creates the rendered Job or verifies a same-name existing Job's run, managed shape, command and predecessor UID.
// It never patches a conflicting Job into compliance and does not compare every Pod-spec field during adoption.
// ensureJob создаёт сформированный Job либо проверяет запуск, управляемую форму, команду и UID предшественника одноимённого объекта.
// Конфликтующий Job не исправляется обновлением; при принятии сравниваются не все поля спецификации Pod.
func (controller *Controller) ensureJob(ctx context.Context, job *batchv1.Job, runID, phase string) error {
	_, err := controller.client.BatchV1().Jobs(controller.config.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := controller.client.BatchV1().Jobs(controller.config.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
		if getErr != nil || existing.Annotations[runIDAnnotation] != runID || !validManagedJob(existing, controller.config.Namespace, phase) ||
			strings.Join(existing.Spec.Template.Spec.Containers[0].Command, "\x00") != strings.Join(job.Spec.Template.Spec.Containers[0].Command, "\x00") ||
			existing.Annotations[failedPredecessorUID] != job.Annotations[failedPredecessorUID] {
			return errors.New("existing image admission job conflicts")
		}
		return nil
	}
	if err != nil {
		return errors.New("create image admission job")
	}
	return nil
}

// deleteWorkspace requests deletion of the named PVC with its supplied UID precondition and treats NotFound as success.
// Kubernetes deletion/finalizers may remain asynchronous; this function does not wait for physical volume removal.
// deleteWorkspace запрашивает удаление именованного PVC с переданной предпосылкой UID и считает NotFound успехом.
// Удаление Kubernetes и finalizers могут оставаться асинхронными; физического удаления тома функция не ждёт.
func (controller *Controller) deleteWorkspace(ctx context.Context, workspace *corev1.PersistentVolumeClaim) error {
	uid := workspace.UID
	err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Delete(ctx, workspace.Name,
		metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("delete terminal image admission workspace")
	}
	return nil
}

// deleteAdmissionJobs deletes listed Jobs for the exact operation ID while preserving promotion and other operations.
// Caller-validated inventory supplies the deletion targets; the first failure stops later deletions without undoing earlier ones.
// deleteAdmissionJobs удаляет перечисленные Jobs точного идентификатора операции, сохраняя продвижение и другие операции.
// Цели берутся из проверенного вызывающим списка; первая ошибка останавливает следующие удаления без отмены предыдущих.
func (controller *Controller) deleteAdmissionJobs(ctx context.Context, id string, jobs []batchv1.Job) error {
	for index := range jobs {
		job := &jobs[index]
		if job.Labels[idLabel] != id || job.Labels[phaseLabel] == "promote" {
			continue
		}
		if err := controller.deleteAdmissionJob(ctx, job, "delete terminal image admission job"); err != nil {
			return err
		}
	}
	return nil
}

// deleteOrphanAdmissionJobs validates and deletes non-promotion Jobs whose operation has no listed workspace.
// Its pending flag suppresses new admission for this cycle even after successful delete requests; promotion remains independent.
// deleteOrphanAdmissionJobs проверяет и удаляет Jobs кроме продвижения, если для операции нет перечисленного рабочего тома.
// Флаг ожидания подавляет новый допуск в этом цикле даже после успешных запросов удаления; продвижение остаётся независимым.
func (controller *Controller) deleteOrphanAdmissionJobs(ctx context.Context, jobs []batchv1.Job, workspaces []corev1.PersistentVolumeClaim) (bool, error) {
	workspaceIDs := make(map[string]struct{}, len(workspaces))
	for index := range workspaces {
		workspaceIDs[workspaces[index].Labels[idLabel]] = struct{}{}
	}
	cleanupPending := false
	for index := range jobs {
		job := &jobs[index]
		phase := job.Labels[phaseLabel]
		if phase == "promote" {
			continue
		}
		if _, exists := workspaceIDs[job.Labels[idLabel]]; exists {
			continue
		}
		if !validManagedJob(job, controller.config.Namespace, phase) || !validRunIDShape(job.Annotations[runIDAnnotation]) {
			return false, errors.New("orphan image admission job is invalid")
		}
		if err := controller.deleteAdmissionJob(ctx, job, "delete orphan image admission job"); err != nil {
			return false, err
		}
		cleanupPending = true
	}
	return cleanupPending, nil
}

// deleteAdmissionJob requires a nonempty UID, requests background cascading deletion with that UID and accepts NotFound.
// It uses the caller's safe failure message and does not wait for garbage collection or revoke domain grants itself.
// deleteAdmissionJob требует непустой UID, запрашивает фоновое каскадное удаление с этим UID и принимает NotFound.
// Используется безопасный текст ошибки вызывающего; сборка мусора не ожидается, доменные grants самим методом не отзываются.
func (controller *Controller) deleteAdmissionJob(ctx context.Context, job *batchv1.Job, message string) error {
	if job.UID == "" {
		return errors.New("managed image admission job UID is invalid")
	}
	uid := job.UID
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{
		Preconditions:     &metav1.Preconditions{UID: &uid},
		PropagationPolicy: &propagation,
	}
	if err := controller.client.BatchV1().Jobs(controller.config.Namespace).Delete(ctx, job.Name, options); err != nil && !apierrors.IsNotFound(err) {
		return errors.New(message)
	}
	return nil
}

// nextAdmissionPhase returns the missing next phase, terminal success, terminal failure or an active wait for one operation.
// A failed scan/sign routes to an admit receipt instead of continuing the normal chain; promotion is excluded from this state machine.
// nextAdmissionPhase возвращает отсутствующий следующий этап, итоговый успех, итоговую ошибку либо активное ожидание одной операции.
// Неуспешный scan/sign направляется к квитанции admit вместо обычной цепочки; продвижение не входит в эту машину состояний.
func nextAdmissionPhase(id string, jobs []batchv1.Job) (string, bool, bool) {
	byPhase := map[string]*batchv1.Job{}
	for index := range jobs {
		if jobs[index].Labels[idLabel] == id && jobs[index].Labels[phaseLabel] != "promote" {
			job := &jobs[index]
			byPhase[job.Labels[phaseLabel]] = job
		}
	}
	for _, phase := range phaseOrder {
		job := byPhase[phase]
		if job == nil {
			return phase, false, false
		}
		if jobFailed(job) {
			if phase == "scan" || phase == "sign" {
				admit := byPhase["admit"]
				switch {
				case admit == nil:
					return "admit", false, false
				case jobFailed(admit):
					return "", false, true
				case jobSucceeded(admit):
					return "", true, false
				default:
					return "", false, false
				}
			}
			return "", false, true
		}
		if !jobSucceeded(job) {
			return "", false, false
		}
	}
	return "", true, false
}

// prepareRendered decorates the rendered Job/optional PVC in place with managed/run metadata, then checks selected contracts.
// Failure does not undo those in-memory edits; no Kubernetes write or owner approval occurs here.
// prepareRendered меняет сформированные Job и необязательный PVC на месте, добавляя метки управления и запуска, затем проверяет выбранные контракты.
// Ошибка не отменяет эти изменения в памяти; запись Kubernetes или одобрение владельца здесь не выполняются.
func prepareRendered(rendered Rendered, namespace, runID, phase string) error {
	if rendered.Job == nil || !validRunIDShape(runID) || !slicesContains(phases, phase) {
		return errors.New("rendered image admission phase is invalid")
	}
	decorate := func(meta *metav1.ObjectMeta) {
		if meta.Labels == nil {
			meta.Labels = map[string]string{}
		}
		if meta.Annotations == nil {
			meta.Annotations = map[string]string{}
		}
		meta.Labels[orchestratedLabel] = "true"
		meta.Annotations[runIDAnnotation] = runID
	}
	decorate(&rendered.Job.ObjectMeta)
	if rendered.PVC != nil {
		decorate(&rendered.PVC.ObjectMeta)
		if !validManagedWorkspace(rendered.PVC, namespace) {
			return errors.New("rendered image admission workspace is invalid")
		}
	}
	if !validManagedJob(rendered.Job, namespace, phase) {
		return errors.New("rendered image admission job is invalid")
	}
	return nil
}

// validManagedWorkspace checks namespace, managed ID/name, exactly 2Gi requested storage and one ReadWriteOnce access mode.
// It does not verify UID, run annotation, storage class or the entire PVC spec; callers enforce additional lifecycle bindings.
// validManagedWorkspace проверяет namespace, управляемые ID и имя, запрос ровно 2Gi и один режим доступа ReadWriteOnce.
// UID, аннотация запуска, storage class и полная спецификация PVC не проверяются; дополнительные lifecycle-привязки обеспечивает вызывающий.
func validManagedWorkspace(workspace *corev1.PersistentVolumeClaim, namespace string) bool {
	if workspace == nil || workspace.Namespace != namespace || workspace.Labels[orchestratedLabel] != "true" ||
		!idPattern.MatchString(workspace.Labels[idLabel]) || workspace.Name != "mc-admit-"+workspace.Labels[idLabel] ||
		workspace.Spec.Resources.Requests.Storage().String() != "2Gi" || len(workspace.Spec.AccessModes) != 1 ||
		workspace.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		return false
	}
	return true
}

// validManagedJob checks identity labels/name, the phase account, disabled token automount, one container and closed commands/proof bindings.
// Callers supply a known phase; this local predicate is not full Pod admission and does not verify every security/resource field.
// validManagedJob проверяет метки и имя, учётную запись этапа, выключенный automount токена, один контейнер и закрытые команды и привязки проверки.
// Известный этап задаёт вызывающий; локальный предикат не является полным допуском Pod и не проверяет все поля защиты и ресурсов.
func validManagedJob(job *batchv1.Job, namespace, phase string) bool {
	if job == nil || job.Namespace != namespace || job.Labels[orchestratedLabel] != "true" ||
		job.Labels[phaseLabel] != phase || !idPattern.MatchString(job.Labels[idLabel]) ||
		job.Name != "mc-admit-"+job.Labels[idLabel]+"-"+phase || job.Spec.Template.Spec.ServiceAccountName != phaseAccounts[phase] ||
		job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken ||
		job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever || len(job.Spec.Template.Spec.Containers) != 1 ||
		job.Spec.Template.Spec.Containers[0].Name != phase || !validPhaseCommand(job, phase) || !validProofReservation(job, phase) {
		return false
	}
	return true
}

// failedPredecessorUID names the annotation tying an admit recovery command to the exact failed predecessor object.
// failedPredecessorUID задаёт аннотацию, связывающую команду восстановления admit с точным объектом неуспешного предшественника.
const failedPredecessorUID = "kodex.dev/admission-failed-predecessor-uid"

// validPhaseCommand accepts the exact shell/script/phase triple or an admit-only fourth closed failure code with predecessor UID.
// The caller must first establish that the Job has one container; command validity alone does not grant execution authority.
// validPhaseCommand допускает точную тройку shell, скрипта и этапа либо четвёртый закрытый код ошибки только для admit с UID предшественника.
// Вызывающий сначала обязан установить наличие одного контейнера; корректность команды сама не выдаёт полномочия исполнения.
func validPhaseCommand(job *batchv1.Job, phase string) bool {
	command := job.Spec.Template.Spec.Containers[0].Command
	if len(command) < 3 || command[0] != "/bin/sh" || command[1] != "/opt/kodex/image-admission.sh" || command[2] != phase {
		return false
	}
	if len(command) == 3 {
		_, present := job.Annotations[failedPredecessorUID]
		return !present
	}
	return phase == "admit" && len(command) == 4 &&
		(command[3] == "SCAN_PREDECESSOR_FAILED" || command[3] == "SIGN_PREDECESSOR_FAILED" || command[3] == "ADMIT_PREDECESSOR_FAILED") &&
		uidPattern.MatchString(job.Annotations[failedPredecessorUID])
}

// prepareProofReservation marks the already decorated Job as suspended with attempt 1 and its deterministic reservation digest.
// It assumes initialized metadata maps and does not persist, resume or approve the Job.
// prepareProofReservation помечает уже дополненный Job как приостановленный с попыткой 1 и детерминированным дайджестом резервирования.
// Предполагаются созданные карты метаданных; сохранение, возобновление и одобрение Job не выполняются.
func prepareProofReservation(job *batchv1.Job, runID, phase string) {
	attempt := "1"
	job.Labels[proofHoldLabel] = "true"
	job.Annotations[proofAttempt] = attempt
	job.Annotations[proofReservation] = proofReservationDigest(runID, job.Labels[idLabel], phase, attempt)
	held := true
	job.Spec.Suspend = &held
}

// validProofReservation requires no reservation metadata for ordinary unsuspended Jobs or exact claim/promotion attempt-1 binding.
// Reserved Jobs need a nonnil Suspend pointer but may already be resumed; the digest is consistency data, not a signed owner decision.
// validProofReservation требует отсутствия метаданных резервирования у обычных неприостановленных Jobs либо точную привязку попытки 1 для claim/promotion.
// У зарезервированного Job нужен ненулевой указатель Suspend, но он может быть уже возобновлён; дайджест — данные согласованности, не подписанное решение владельца.
func validProofReservation(job *batchv1.Job, phase string) bool {
	held := job.Labels[proofHoldLabel]
	if held == "" {
		return job.Annotations[proofAttempt] == "" && job.Annotations[proofReservation] == "" &&
			(job.Spec.Suspend == nil || !*job.Spec.Suspend)
	}
	if held != "true" || (phase != "claim" && phase != "promote") || job.Spec.Suspend == nil ||
		job.Annotations[proofAttempt] != "1" {
		return false
	}
	return job.Annotations[proofReservation] == proofReservationDigest(job.Annotations[runIDAnnotation], job.Labels[idLabel], phase, "1")
}

// proofReservationDigest hashes four ordered NUL-separated identifiers and returns lowercase hexadecimal SHA-256.
// This deterministic binding provides no secret authentication or server-owned approval by itself.
// proofReservationDigest хеширует четыре упорядоченных идентификатора с NUL-разделителями и возвращает SHA-256 нижнего регистра.
// Детерминированная привязка сама не предоставляет секретную аутентификацию или серверное одобрение.
func proofReservationDigest(runID, operationID, phase, attempt string) string {
	digest := sha256.Sum256([]byte(runID + "\x00" + operationID + "\x00" + phase + "\x00" + attempt))
	return hex.EncodeToString(digest[:])
}

// validatePolicy requires the configured immutable owner-intent ConfigMap in kodex-system and a nonzero 40-hex revision.
// Digest-suffixed names additionally verify the payload hash; the existing fixed-name path does not perform that hash check.
// The ConfigMap/label is a local policy projection, not proof of a user-supplied authorization context.
// validatePolicy требует настроенный immutable ConfigMap с owner-intent в kodex-system и ненулевую 40-символьную hex-ревизию.
// Имена с суффиксом дайджеста дополнительно проверяют хеш содержимого; существующий путь фиксированного имени этой проверки не делает.
// ConfigMap и метка служат локальной проекцией политики, а не доказательством переданного пользователем контекста полномочий.
func validatePolicy(policy *corev1.ConfigMap, expectedName string) (string, error) {
	if policy == nil || policy.Name != expectedName || !validPolicyName(policy.Name) || policy.Namespace != "kodex-system" || policy.Immutable == nil || !*policy.Immutable ||
		policy.Labels["kodex.dev/owner-intent"] != "true" {
		return "", errors.New("image admission owner policy is invalid")
	}
	if policy.Name != policyName && !versionedPolicyMatches(policy) {
		return "", errors.New("image admission policy digest is invalid")
	}
	revision := policy.Data["orchestrationRevision"]
	if !revisionPattern.MatchString(revision) || strings.Trim(revision, "0") == "" {
		return "", errors.New("image admission orchestration revision is invalid")
	}
	return revision, nil
}

// makeRunID concatenates a UTC second-resolution timestamp and the supplied revision without adding randomness or validating either.
// makeRunID соединяет время UTC с точностью до секунды и переданную ревизию без случайности и проверки значений.
func makeRunID(now time.Time, revision string) string {
	return "v" + now.UTC().Format("20060102150405") + "-" + revision
}

// validRunID checks the run's syntactic shape and exact revision suffix, not object existence or permission.
// validRunID проверяет синтаксическую форму запуска и точный суффикс ревизии, а не существование объекта или полномочия.
func validRunID(runID, revision string) bool {
	return validRunIDShape(runID) && strings.HasSuffix(runID, "-"+revision)
}

// validRunIDShape requires v, fourteen decimal digits, a hyphen and forty lowercase hexadecimal digits.
// It does not parse a real calendar instant or reject an all-zero revision; policy validation handles the latter separately.
// validRunIDShape требует v, четырнадцать десятичных цифр, дефис и сорок шестнадцатеричных символов нижнего регистра.
// Реальная календарная дата и ненулевая ревизия не проверяются; последнее отдельно проверяет политика.
func validRunIDShape(runID string) bool {
	if len(runID) != 56 || runID[0] != 'v' || runID[15] != '-' || !revisionPattern.MatchString(runID[16:]) {
		return false
	}
	for _, character := range runID[1:15] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// jobSucceeded treats a positive succeeded count or a true Complete condition as success in caller-supplied nonnil Job state.
// jobSucceeded считает успехом положительный счётчик succeeded либо true-условие Complete в переданном ненулевом Job.
func jobSucceeded(job *batchv1.Job) bool {
	if job.Status.Succeeded > 0 {
		return true
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// jobFailed accepts a true Failed condition or, as a fallback, zero active Pods with a positive failed count.
// This local heuristic is broader than checking only Kubernetes' terminal JobFailed condition.
// jobFailed принимает true-условие Failed либо, как запасной признак, ноль активных Pod при положительном счётчике failed.
// Локальная эвристика шире проверки только терминального условия JobFailed Kubernetes.
func jobFailed(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return job.Status.Active == 0 && job.Status.Failed > 0
}

// jobTerminal combines the local success/failure predicates without contacting Kubernetes or validating the Job's identity.
// jobTerminal объединяет локальные признаки успеха и ошибки без обращения к Kubernetes или проверки идентичности Job.
func jobTerminal(job *batchv1.Job) bool { return jobSucceeded(job) || jobFailed(job) }

// orchestratedSelector renders the exact managed-label equality used for this controller's Job and PVC list requests.
// orchestratedSelector формирует точное равенство метки управления для запросов списка Jobs и PVC этого контроллера.
func orchestratedSelector() string {
	return labels.Set{orchestratedLabel: "true"}.AsSelector().String()
}

// slicesContains reports an exact string match in the caller's slice without mutation or normalization.
// slicesContains сообщает о точном совпадении строки в срезе вызывающего без изменения или нормализации.
func slicesContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
