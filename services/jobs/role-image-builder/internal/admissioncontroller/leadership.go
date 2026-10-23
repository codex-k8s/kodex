package admissioncontroller

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	coordinationclient "k8s.io/client-go/kubernetes/typed/coordination/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	// LeaderLeaseName selects the single precreated coordination lease; controllers may update but never create it.
	// LeaderLeaseName выбирает единственную заранее созданную координационную аренду; контроллеры могут менять, но не создавать её.
	LeaderLeaseName = "image-admission-controller-leader"
	// leaderLeaseUnavailable is the safe diagnostic for missing/unreadable lease state or unconfigured leadership.
	// leaderLeaseUnavailable — безопасная диагностика отсутствующей или недоступной аренды либо ненастроенного лидерства.
	leaderLeaseUnavailable = "image admission leader lease is unavailable"
	// leaderLeaseChanged reports replacement or invalid identity/shape of the pinned coordination object.
	// leaderLeaseChanged сообщает о замене либо неверной идентичности или форме закреплённого объекта координации.
	leaderLeaseChanged = "image admission leader lease identity changed"
	// leaderLeaseUpdateFailed hides Kubernetes update details behind one stable failure diagnostic.
	// leaderLeaseUpdateFailed скрывает подробности обновления Kubernetes за одной устойчивой диагностикой сбоя.
	leaderLeaseUpdateFailed = "image admission leader lease update failed"
	// leaderElectionStopped identifies loss or end of leadership without treating it as a successful worker completion.
	// leaderElectionStopped обозначает потерю или окончание лидерства, не считая это успешным завершением worker.
	leaderElectionStopped = "image admission leadership stopped"
)

// ValidPodUID checks the configured Downward API UID syntax only; it neither resolves a Pod nor grants authority.
// ValidPodUID проверяет только синтаксис настроенного UID из Downward API; Pod не ищется и полномочия не выдаются.
func ValidPodUID(identity string) bool { return uidPattern.MatchString(identity) }

// fixedLeaderLease pins one precreated Kubernetes Lease UID and caches the last record for versioned updates.
// It coordinates materialization only; domain claims, fences and grants remain owned by the control plane.
// fixedLeaderLease закрепляет UID заранее созданной Kubernetes Lease и хранит последний объект для версионированного обновления.
// Он координирует только материализацию; доменные claims, fences и grants остаются у управляющего сервиса.
type fixedLeaderLease struct {
	client   coordinationclient.LeaseInterface
	uid      types.UID
	identity string
	observed *coordinationv1.Lease
}

// validLeaderLease requires the exact name/namespace/UID/label, a resource version and nondeleted ordinary lease state.
// It bounds supplied duration/transitions and requires valid holder/timestamps, but does not prove current leadership.
// validLeaderLease требует точные имя, namespace, UID и метку, версию ресурса и неудалённое состояние обычной аренды.
// Заданные длительность и переходы ограничены, держатель и время проверяются, но текущее лидерство этим не доказывается.
func validLeaderLease(lease *coordinationv1.Lease, uid types.UID) bool {
	if lease == nil {
		return false
	}
	if lease.Spec.LeaseTransitions != nil && *lease.Spec.LeaseTransitions < 0 {
		return false
	}
	if lease.Spec.LeaseDurationSeconds != nil && (*lease.Spec.LeaseDurationSeconds <= 0 || *lease.Spec.LeaseDurationSeconds > 60) {
		return false
	}
	if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity != "" &&
		(lease.Spec.LeaseDurationSeconds == nil || lease.Spec.RenewTime == nil || lease.Spec.AcquireTime == nil) {
		return false
	}
	return lease.Name == LeaderLeaseName && lease.Namespace == "kodex-system" &&
		lease.UID == uid && uid != "" && lease.ResourceVersion != "" && lease.DeletionTimestamp == nil &&
		lease.Labels["app.kubernetes.io/name"] == "image-admission-controller" &&
		lease.Spec.Strategy == nil && lease.Spec.PreferredHolder == nil &&
		(lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" || uidPattern.MatchString(*lease.Spec.HolderIdentity))
}

// ConfigureLeadership accepts one Pod-UID identity and pins a valid existing lease through a read-only lookup.
// It rejects reconfiguration, creates no Kubernetes object and does not itself acquire the lease.
// ConfigureLeadership принимает одну идентичность Pod UID и закрепляет корректную существующую аренду чтением.
// Повторная настройка отклоняется; объекты Kubernetes не создаются и аренда самим вызовом не захватывается.
func (controller *Controller) ConfigureLeadership(ctx context.Context, identity string) error {
	if controller.leader != nil || !ValidPodUID(identity) {
		return errors.New("image admission leader identity is invalid")
	}
	client := controller.client.CoordinationV1().Leases(controller.config.Namespace)
	lease, err := client.Get(ctx, LeaderLeaseName, metav1.GetOptions{})
	if err != nil || !validLeaderLease(lease, leaseUID(lease)) {
		return errors.New(leaderLeaseUnavailable)
	}
	controller.leader = &fixedLeaderLease{client: client, uid: lease.UID, identity: identity}
	return nil
}

// leaseUID safely extracts a lease UID, returning the invalid empty identity for a missing object.
// leaseUID безопасно извлекает UID аренды, возвращая недопустимую пустую идентичность при отсутствии объекта.
func leaseUID(lease *coordinationv1.Lease) types.UID {
	if lease == nil {
		return ""
	}
	return lease.UID
}

// check reloads the fixed lease and verifies its pinned UID/shape without changing the cached election record.
// Read failures and replacement are projected to closed diagnostics instead of leaking API errors.
// check перечитывает фиксированную аренду и проверяет закреплённые UID и форму, не меняя кеш выборного объекта.
// Сбой чтения и замена переводятся в закрытую диагностику без раскрытия ошибок API.
func (lock *fixedLeaderLease) check(ctx context.Context) (*coordinationv1.Lease, error) {
	lease, err := lock.client.Get(ctx, LeaderLeaseName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.New(leaderLeaseUnavailable)
	}
	if !validLeaderLease(lease, lock.uid) {
		return nil, errors.New(leaderLeaseChanged)
	}
	return lease, nil
}

// checkHolder requires a fresh valid lease to name this lock's identity before guarded reconciliation.
// This holder check is not a replacement for domain-owned claim generations or fencing.
// checkHolder требует, чтобы свежая корректная аренда называла идентичность этого lock перед защищённым согласованием.
// Проверка держателя не заменяет принадлежащие домену поколения claims или fencing.
func (lock *fixedLeaderLease) checkHolder(ctx context.Context) error {
	lease, err := lock.check(ctx)
	if err != nil {
		return err
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != lock.identity {
		return errors.New(leaderElectionStopped)
	}
	return nil
}

// Get caches a freshly verified lease and returns its leader-election record plus JSON observation bytes.
// The cached resource version is subsequently used by Update for the Kubernetes server's optimistic concurrency check.
// Get сохраняет свежепроверенную аренду и возвращает запись выборов лидера вместе с JSON-байтами наблюдения.
// Сохранённая версия ресурса затем используется Update для оптимистической проверки конкурентности сервером Kubernetes.
func (lock *fixedLeaderLease) Get(ctx context.Context) (*resourcelock.LeaderElectionRecord, []byte, error) {
	lease, err := lock.check(ctx)
	if err != nil {
		return nil, nil, err
	}
	lock.observed = lease
	record := resourcelock.LeaseSpecToLeaderElectionRecord(&lease.Spec)
	encoded, err := json.Marshal(record)
	return record, encoded, err
}

// Update copies the last valid observed object, replaces its election spec and submits the preserved resource version.
// It relies on server-side CAS, validates the returned UID/shape and updates the cache only after successful validation.
// Update копирует последний корректный наблюдаемый объект, заменяет спецификацию выборов и передаёт сохранённую версию ресурса.
// Используется серверный CAS; UID и форма ответа проверяются, а кеш обновляется только после успешной проверки.
func (lock *fixedLeaderLease) Update(ctx context.Context, record resourcelock.LeaderElectionRecord) error {
	if lock.observed == nil || !validLeaderLease(lock.observed, lock.uid) {
		return errors.New(leaderLeaseChanged)
	}
	lease := lock.observed.DeepCopy()
	lease.Spec = resourcelock.LeaderElectionRecordToLeaseSpec(&record)
	updated, err := lock.client.Update(ctx, lease, metav1.UpdateOptions{})
	if err != nil {
		return errors.New(leaderLeaseUpdateFailed)
	}
	if !validLeaderLease(updated, lock.uid) {
		return errors.New(leaderLeaseChanged)
	}
	lock.observed = updated
	return nil
}

// Create always rejects runtime lease creation; installation must supply the exact coordination object in advance.
// Create всегда отклоняет создание аренды во время работы; установка обязана заранее предоставить точный объект координации.
func (*fixedLeaderLease) Create(context.Context, resourcelock.LeaderElectionRecord) error {
	return errors.New("image admission leader lease creation is forbidden")
}

// RecordEvent deliberately emits no Kubernetes Event and grants no extra event-write capability to the elector.
// RecordEvent намеренно не создаёт Kubernetes Event и не требует дополнительного права записи событий для выборов.
func (*fixedLeaderLease) RecordEvent(string) {}

// Identity returns the configured Pod UID used as the election holder, not a user or domain authorization identity.
// Identity возвращает настроенный Pod UID держателя выборов, а не пользовательскую идентичность или доменные полномочия.
func (lock *fixedLeaderLease) Identity() string { return lock.identity }

// Describe returns the fixed namespace/name label for election diagnostics without reading the cluster.
// Describe возвращает фиксированные namespace и имя для диагностики выборов без чтения кластера.
func (*fixedLeaderLease) Describe() string { return "kodex-system/" + LeaderLeaseName }

// electionTiming groups lease lifetime, renew deadline and retry interval; client-go validates their relationship.
// electionTiming объединяет срок аренды, срок продления и интервал повторов; их соотношение проверяет client-go.
type electionTiming struct{ lease, renew, retry time.Duration }

// Run starts the leader-gated reconciler with 45-second lease, 20-second renew and 5-second retry budgets.
// It blocks until cancellation or leadership failure; an external supervisor owns any restart.
// Run запускает согласование под лидерством с арендой 45 секунд, продлением 20 секунд и повтором через 5 секунд.
// Он блокируется до отмены или сбоя лидерства; повторный запуск принадлежит внешнему supervisor.
func (controller *Controller) Run(ctx context.Context) error {
	return controller.runLeader(ctx, electionTiming{45 * time.Second, 20 * time.Second, 5 * time.Second})
}

// runLeader waits for acquisition, runs reconciliation synchronously under the leader context, then joins the election loop.
// Cancellation never releases the lease early; natural expiry and domain-owned fencing handle takeover separately.
// Parent cancellation takes precedence in the returned error; ordinary leadership end is reported as failure, not success.
// runLeader ждёт захвата, синхронно выполняет согласование с контекстом лидера, затем ожидает завершения цикла выборов.
// Отмена не освобождает аренду досрочно; естественное истечение и доменный fencing отдельно обеспечивают смену лидера.
// Отмена родителя имеет приоритет в возвращаемой ошибке; обычное окончание лидерства считается сбоем, а не успехом.
func (controller *Controller) runLeader(ctx context.Context, timing electionTiming) error {
	if controller.leader == nil {
		return errors.New(leaderLeaseUnavailable)
	}
	electionCtx, cancelElection := context.WithCancel(ctx)
	defer cancelElection()
	started := make(chan context.Context)
	stopped := make(chan struct{})
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: controller.leader, Name: LeaderLeaseName,
		LeaseDuration: timing.lease, RenewDeadline: timing.renew, RetryPeriod: timing.retry,
		// Keep the lease until natural expiry rather than releasing it before reconciliation has joined.
		// Сохранять аренду до естественного истечения, не освобождая её до завершения согласования.
		ReleaseOnCancel: false,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaderCtx context.Context) {
				select {
				case started <- leaderCtx:
				case <-stopped:
				}
			},
			OnStoppedLeading: func() {},
		},
	})
	if err != nil {
		return errors.New("image admission leader timing is invalid")
	}
	go func() { elector.Run(electionCtx); close(stopped) }()
	select {
	case leaderCtx := <-started:
		controller.logger.InfoContext(ctx, "image admission leadership acquired")
		// Synchronous execution joins reconciliation; the independent election loop cancels leaderCtx on lease loss.
		// Синхронное исполнение ожидает согласование; независимый цикл выборов отменяет leaderCtx при потере аренды.
		err = controller.runReconciler(leaderCtx)
		cancelElection()
		<-stopped
		controller.logger.InfoContext(ctx, leaderElectionStopped)
	case <-stopped:
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return errors.New(leaderElectionStopped)
}
