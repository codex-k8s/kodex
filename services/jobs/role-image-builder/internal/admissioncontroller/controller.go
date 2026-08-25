// Package admissioncontroller автоматически материализует ограниченную цепочку
// supply-chain Jobs, не получая registry, signing или owner credentials фаз.
package admissioncontroller

import (
	"context"
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
	"k8s.io/client-go/kubernetes"
)

const (
	orchestratedLabel = "kodex.dev/image-admission-orchestrated"
	idLabel           = "kodex.dev/image-admission-id"
	phaseLabel        = "kodex.dev/image-admission-phase"
	runIDAnnotation   = "kodex.dev/admission-run-id"
	policyName        = "kodex-image-admission-policy"
)

var (
	revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
	idPattern       = regexp.MustCompile(`^[a-f0-9]{32}$`)
	phaseOrder      = []string{"claim", "scan", "sign", "admit"}
	phaseAccounts   = map[string]string{
		"claim": "image-admission", "scan": "kodex-image-scanner",
		"sign": "kodex-image-signer", "admit": "image-admission", "promote": "image-promotion",
	}
)

type Controller struct {
	client               kubernetes.Interface
	renderer             Renderer
	config               Config
	logger               *slog.Logger
	now                  func() time.Time
	lastAdmissionAttempt time.Time
	lastPromotionAttempt time.Time
}

func InCluster(config Config, renderer Renderer, logger *slog.Logger) (*Controller, error) {
	client, err := inClusterClient(config.RequestTimeout)
	if err != nil {
		return nil, err
	}
	return New(client, renderer, config, logger)
}

func New(client kubernetes.Interface, renderer Renderer, config Config, logger *slog.Logger) (*Controller, error) {
	if client == nil || renderer == nil || logger == nil || config.Validate() != nil {
		return nil, errors.New("image admission controller configuration is invalid")
	}
	return &Controller{client: client, renderer: renderer, config: config, logger: logger, now: time.Now}, nil
}

func (controller *Controller) Run(ctx context.Context) error {
	ticker := time.NewTicker(controller.config.ReconcileInterval)
	defer ticker.Stop()
	degraded := false
	for {
		request, cancel := context.WithTimeout(ctx, controller.config.RequestTimeout)
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

// Check проверяет только прямую Kubernetes infrastructure и immutable local
// policy projection; соседний control-plane не участвует в Pod readiness.
func (controller *Controller) Check(ctx context.Context) error {
	policy, err := controller.client.CoreV1().ConfigMaps(controller.config.Namespace).Get(ctx, controller.config.PolicyConfigMap, metav1.GetOptions{})
	if err != nil {
		return errors.New("read image admission policy")
	}
	if _, err := validatePolicy(policy); err != nil {
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

func (controller *Controller) Reconcile(ctx context.Context) error {
	policy, err := controller.client.CoreV1().ConfigMaps(controller.config.Namespace).Get(ctx, controller.config.PolicyConfigMap, metav1.GetOptions{})
	if err != nil {
		return errors.New("read image admission owner policy")
	}
	revision, err := validatePolicy(policy)
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
	now := controller.now().UTC()
	if err := controller.reconcileAdmissions(ctx, policy, revision, jobs.Items, workspaces.Items, now); err != nil {
		return err
	}
	if err := controller.reconcilePromotions(ctx, policy, revision, jobs.Items, now); err != nil {
		return err
	}
	return nil
}

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
		phase, terminal, failed := nextAdmissionPhase(workspace.Labels[idLabel], jobs)
		if terminal || failed {
			if err := controller.deleteWorkspace(ctx, workspace); err != nil {
				return err
			}
			controller.lastAdmissionAttempt = now
			continue
		}
		active = true
		if phase != "" {
			if err := controller.ensurePhase(ctx, policy, runID, phase); err != nil {
				return err
			}
		}
	}
	if active || len(workspaces) != 0 || now.Sub(controller.lastAdmissionAttempt) < controller.config.RetryInterval {
		return nil
	}
	runID := makeRunID(now, revision)
	if err := controller.ensurePhase(ctx, policy, runID, "claim"); err != nil {
		return err
	}
	controller.lastAdmissionAttempt = now
	return nil
}

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
	if now.Sub(controller.lastPromotionAttempt) < controller.config.RetryInterval {
		return nil
	}
	if err := controller.ensurePhase(ctx, policy, makeRunID(now, revision), "promote"); err != nil {
		return err
	}
	controller.lastPromotionAttempt = now
	return nil
}

func (controller *Controller) ensurePhase(ctx context.Context, policy *corev1.ConfigMap, runID, phase string) error {
	rendered, err := controller.renderer.Render(ctx, policy, controller.config.Environment, runID, phase)
	if err != nil {
		return err
	}
	if err := prepareRendered(rendered, controller.config.Namespace, runID, phase); err != nil {
		return err
	}
	if rendered.PVC != nil {
		if err := controller.ensureWorkspace(ctx, rendered.PVC); err != nil {
			return err
		}
	}
	return controller.ensureJob(ctx, rendered.Job, runID, phase)
}

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

func (controller *Controller) ensureJob(ctx context.Context, job *batchv1.Job, runID, phase string) error {
	_, err := controller.client.BatchV1().Jobs(controller.config.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := controller.client.BatchV1().Jobs(controller.config.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
		if getErr != nil || existing.Annotations[runIDAnnotation] != runID || !validManagedJob(existing, controller.config.Namespace, phase) {
			return errors.New("existing image admission job conflicts")
		}
		return nil
	}
	if err != nil {
		return errors.New("create image admission job")
	}
	return nil
}

func (controller *Controller) deleteWorkspace(ctx context.Context, workspace *corev1.PersistentVolumeClaim) error {
	uid := workspace.UID
	err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Delete(ctx, workspace.Name,
		metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("delete terminal image admission workspace")
	}
	return nil
}

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
			return "", false, true
		}
		if !jobSucceeded(job) {
			return "", false, false
		}
	}
	return "", true, false
}

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

func validManagedWorkspace(workspace *corev1.PersistentVolumeClaim, namespace string) bool {
	if workspace == nil || workspace.Namespace != namespace || workspace.Labels[orchestratedLabel] != "true" ||
		!idPattern.MatchString(workspace.Labels[idLabel]) || workspace.Name != "mc-admit-"+workspace.Labels[idLabel] ||
		workspace.Spec.Resources.Requests.Storage().String() != "2Gi" || len(workspace.Spec.AccessModes) != 1 ||
		workspace.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		return false
	}
	return true
}

func validManagedJob(job *batchv1.Job, namespace, phase string) bool {
	if job == nil || job.Namespace != namespace || job.Labels[orchestratedLabel] != "true" ||
		job.Labels[phaseLabel] != phase || !idPattern.MatchString(job.Labels[idLabel]) ||
		job.Name != "mc-admit-"+job.Labels[idLabel]+"-"+phase || job.Spec.Template.Spec.ServiceAccountName != phaseAccounts[phase] ||
		job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken ||
		job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever || len(job.Spec.Template.Spec.Containers) != 1 ||
		job.Spec.Template.Spec.Containers[0].Name != phase || len(job.Spec.Template.Spec.Containers[0].Command) != 3 ||
		job.Spec.Template.Spec.Containers[0].Command[2] != phase {
		return false
	}
	return true
}

func validatePolicy(policy *corev1.ConfigMap) (string, error) {
	if policy == nil || policy.Name != policyName || policy.Namespace != "kodex-system" || policy.Immutable == nil || !*policy.Immutable ||
		policy.Labels["kodex.dev/owner-intent"] != "true" {
		return "", errors.New("image admission owner policy is invalid")
	}
	revision := policy.Data["orchestrationRevision"]
	if !revisionPattern.MatchString(revision) || strings.Trim(revision, "0") == "" {
		return "", errors.New("image admission orchestration revision is invalid")
	}
	return revision, nil
}

func makeRunID(now time.Time, revision string) string {
	return "v" + now.UTC().Format("20060102150405") + "-" + revision
}

func validRunID(runID, revision string) bool {
	return validRunIDShape(runID) && strings.HasSuffix(runID, "-"+revision)
}

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

func jobFailed(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return job.Status.Active == 0 && job.Status.Failed > 0
}

func jobTerminal(job *batchv1.Job) bool { return jobSucceeded(job) || jobFailed(job) }

func orchestratedSelector() string {
	return labels.Set{orchestratedLabel: "true"}.AsSelector().String()
}

func slicesContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
