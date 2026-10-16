// Package projectpurge удаляет внешние следы проекта до удаления его строк в БД.
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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	managedLabel               = "runtime.kodex.dev/managed"
	sessionHashLabel           = "runtime.kodex.dev/session-hash"
	organizationHashAnnotation = "runtime.kodex.dev/organization-hash"
	projectHashAnnotation      = "runtime.kodex.dev/project-hash"
)

type source interface {
	PromoteDueProjectPurges(context.Context, int32) error
	ListPendingProjectPurges(context.Context, int32) ([]platform.ProjectPurgeCandidate, error)
	ProjectPurgeInventory(context.Context, platform.ProjectPurgeCandidate) ([]platform.ProjectPurgeExternalItem, error)
	ProjectPurgeObjectIsShared(context.Context, platform.ProjectPurgeCandidate, string) (bool, error)
	ProjectPurgeArchiveIsOwned(context.Context, platform.ProjectPurgeCandidate, platform.ProjectPurgeExternalItem) (bool, error)
	FinalizeProjectPurge(context.Context, platform.ProjectPurgeCandidate, []platform.ProjectPurgeExternalItem) error
}

type Processor struct {
	source     source
	objects    objectstorage.Store
	archives   objectstorage.Store
	kubernetes kubernetes.Interface
	namespace  string
}

func New(source source, objects, archives objectstorage.Store, kubernetes kubernetes.Interface, namespace string) *Processor {
	return &Processor{source: source, objects: objects, archives: archives, kubernetes: kubernetes, namespace: namespace}
}

// Run не связывает readiness control-plane с Kubernetes API: сбой очистки
// оставляет проект в PURGE_PENDING и повторяется без потери квитанции.
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

func (processor *Processor) Cycle(ctx context.Context) error {
	if err := processor.source.PromoteDueProjectPurges(ctx, 8); err != nil {
		return err
	}
	candidates, err := processor.source.ListPendingProjectPurges(ctx, 4)
	if err != nil {
		return err
	}
	var failures []error
	for _, candidate := range candidates {
		if err := processor.process(ctx, candidate); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

type failure struct {
	stage, projectRef string
	cause             error
}

func (item *failure) Error() string { return "project purge stage failed" }
func (item *failure) Unwrap() error { return item.cause }

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

func (processor *Processor) deleteObject(ctx context.Context, candidate platform.ProjectPurgeCandidate, item platform.ProjectPurgeExternalItem) error {
	artifactPrefix := "organizations/" + candidate.OrganizationRef + "/projects/" + candidate.ProjectRef + "/artifacts/"
	archivePrefix := "session-archive/v1/" + candidate.OrganizationRef + "/"
	if !objectstorage.ValidKey(item.Target) {
		return errors.New("project purge object escaped project scope")
	}
	store := processor.objects
	switch {
	case strings.HasPrefix(item.Target, artifactPrefix):
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

// PURGE_PENDING закрывает новые claims/materialization. Один свежий paginated
// snapshot проверяет ВСЕ inventory PVC до любых external effects, включая
// отсутствующие PVC с ещё живым потребителем. Не делаем O(sessions*pods) READ.
func (processor *Processor) checkPVCConsumers(ctx context.Context, items []platform.ProjectPurgeExternalItem) error {
	names := make(map[string]bool)
	for _, item := range items {
		if item.Kind != "PVC" {
			continue
		}
		name, err := runtimecontract.SessionPVCName(item.Version)
		if err != nil || name != item.Target || processor.namespace == "" {
			return errors.New("project purge PVC identity is invalid")
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

func (processor *Processor) deletePVC(ctx context.Context, candidate platform.ProjectPurgeCandidate, item platform.ProjectPurgeExternalItem) error {
	name, err := runtimecontract.SessionPVCName(item.Version)
	if err != nil || name != item.Target || processor.namespace == "" {
		return errors.New("project purge PVC identity is invalid")
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
		pvc.Annotations[organizationHashAnnotation] != shortHash(candidate.OrganizationRef) ||
		pvc.Annotations[projectHashAnnotation] != shortHash(candidate.ProjectRef) {
		return errors.New("project purge PVC ownership mismatch")
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

func shortHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}
