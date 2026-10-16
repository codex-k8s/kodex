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
	LeaderLeaseName         = "image-admission-controller-leader"
	leaderLeaseUnavailable  = "image admission leader lease is unavailable"
	leaderLeaseChanged      = "image admission leader lease identity changed"
	leaderLeaseUpdateFailed = "image admission leader lease update failed"
	leaderElectionStopped   = "image admission leadership stopped"
)

// ValidPodUID проверяет формат Downward API identity, но не назначает authority.
func ValidPodUID(identity string) bool { return uidPattern.MatchString(identity) }

// Lease координирует локальную материализацию. Доменные claim/fence и grants
// по-прежнему принадлежат control-plane, а не этому lock.
type fixedLeaderLease struct {
	client   coordinationclient.LeaseInterface
	uid      types.UID
	identity string
	observed *coordinationv1.Lease
}

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

func leaseUID(lease *coordinationv1.Lease) types.UID {
	if lease == nil {
		return ""
	}
	return lease.UID
}

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

func (*fixedLeaderLease) Create(context.Context, resourcelock.LeaderElectionRecord) error {
	return errors.New("image admission leader lease creation is forbidden")
}
func (*fixedLeaderLease) RecordEvent(string)    {}
func (lock *fixedLeaderLease) Identity() string { return lock.identity }
func (*fixedLeaderLease) Describe() string      { return "kodex-system/" + LeaderLeaseName }

type electionTiming struct{ lease, renew, retry time.Duration }

func (controller *Controller) Run(ctx context.Context) error {
	return controller.runLeader(ctx, electionTiming{45 * time.Second, 20 * time.Second, 5 * time.Second})
}

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
		// Не отдавать lock до join reconcile. После остановки он истекает сам.
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
		// Синхронный run обеспечивает join: worker не завершается до остановки
		// reconcile. Election loop независимо отменяет leaderCtx при потере lock.
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
