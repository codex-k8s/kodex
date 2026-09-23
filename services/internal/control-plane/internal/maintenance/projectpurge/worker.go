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
	FinalizeProjectPurge(context.Context, platform.ProjectPurgeCandidate, []platform.ProjectPurgeExternalItem) error
}

type Processor struct {
	source     source
	objects    objectstorage.Store
	kubernetes kubernetes.Interface
	namespace  string
}

func New(source source, objects objectstorage.Store, kubernetes kubernetes.Interface, namespace string) *Processor {
	return &Processor{source: source, objects: objects, kubernetes: kubernetes, namespace: namespace}
}

// Run не связывает readiness control-plane с Kubernetes API: сбой очистки
// оставляет проект в PURGE_PENDING и повторяется без потери квитанции.
func Run(source source, objects objectstorage.Store, namespace string, logger *slog.Logger) serviceruntime.Worker {
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
						processor = New(source, objects, client, namespace)
					}
				}
				if err != nil {
					logger.WarnContext(ctx, "project purge Kubernetes client unavailable", "error_class", "kubernetes_client")
				}
			}
			if processor != nil {
				cycle, cancel := context.WithTimeout(ctx, 5*time.Minute)
				if err := processor.Cycle(cycle); err != nil && !errors.Is(err, context.Canceled) {
					logger.WarnContext(ctx, "project purge cycle failed", "error_class", "purge_cycle")
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
	var failed bool
	for _, candidate := range candidates {
		if err := processor.process(ctx, candidate); err != nil {
			failed = true
		}
	}
	if failed {
		return errors.New("project purge external cleanup is incomplete")
	}
	return nil
}

func (processor *Processor) process(ctx context.Context, candidate platform.ProjectPurgeCandidate) error {
	items, err := processor.source.ProjectPurgeInventory(ctx, candidate)
	if err != nil {
		return err
	}
	for _, item := range items {
		switch item.Kind {
		case "OBJECT":
			if err := processor.deleteObject(ctx, candidate, item); err != nil {
				return err
			}
		case "PVC":
			if err := processor.deletePVC(ctx, candidate, item); err != nil {
				return err
			}
		default:
			return errors.New("project purge inventory kind is invalid")
		}
	}
	return processor.source.FinalizeProjectPurge(ctx, candidate, items)
}

func (processor *Processor) deleteObject(ctx context.Context, candidate platform.ProjectPurgeCandidate, item platform.ProjectPurgeExternalItem) error {
	artifactPrefix := "organizations/" + candidate.OrganizationRef + "/projects/" + candidate.ProjectRef + "/artifacts/"
	archivePrefix := "session-archive/v1/" + candidate.OrganizationRef + "/" + candidate.ProjectRef + "/"
	if !objectstorage.ValidKey(item.Target) ||
		!strings.HasPrefix(item.Target, artifactPrefix) && !strings.HasPrefix(item.Target, archivePrefix) {
		return errors.New("project purge object escaped project scope")
	}
	shared, err := processor.source.ProjectPurgeObjectIsShared(ctx, candidate, item.Target)
	if err != nil || shared {
		return errors.New("project purge object ownership is ambiguous")
	}
	receipt, err := processor.objects.Head(ctx, item.Target, item.Version)
	if errors.Is(err, objectstorage.ErrNotFound) {
		return nil
	}
	if err != nil || receipt.Key != item.Target || item.Version != "" && receipt.VersionID != item.Version {
		return errors.New("project purge object readback failed")
	}
	if err := processor.objects.Delete(ctx, item.Target, receipt.VersionID); err != nil && !errors.Is(err, objectstorage.ErrNotFound) {
		return errors.New("project purge object deletion failed")
	}
	if _, err := processor.objects.Head(ctx, item.Target, receipt.VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
		return errors.New("project purge object remains after deletion")
	}
	return nil
}

func (processor *Processor) deletePVC(ctx context.Context, candidate platform.ProjectPurgeCandidate, item platform.ProjectPurgeExternalItem) error {
	name, err := runtimecontract.SessionPVCName(item.Version)
	if err != nil || name != item.Target || processor.namespace == "" {
		return errors.New("project purge PVC identity is invalid")
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
				if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == name {
					return errors.New("project purge PVC is in use")
				}
			}
		}
		continueToken = page.Continue
		if continueToken == "" {
			break
		}
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
		_, err := pvcs.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return errors.New("project purge PVC deletion readback failed")
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
