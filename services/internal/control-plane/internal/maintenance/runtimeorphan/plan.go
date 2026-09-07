// Package runtimeorphan реализует только операторскую очистку disposable orphan.
package runtimeorphan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ErrGuard = errors.New("orphan maintenance guard rejected")
var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Cluster struct {
	Version  int    `json:"version"`
	UID      string `json:"clusterUID"`
	Endpoint string `json:"apiEndpoint"`
	CA       string `json:"caSHA256"`
}

type Namespace struct {
	Name    string    `json:"name"`
	UID     string    `json:"uid"`
	Created time.Time `json:"created"`
	Profile string    `json:"profile"`
}

type Descriptor struct {
	Name           string    `json:"name"`
	UID            string    `json:"uid"`
	RV             string    `json:"resourceVersion"`
	Created        time.Time `json:"created"`
	MetadataDigest string    `json:"metadataDigest"`
	Operation      string    `json:"operation"`
	SecretRef      string    `json:"secretRef"`
	Key            string    `json:"key"`
	Revision       int64     `json:"revision"`
	Generation     int64     `json:"generation"`
	ContentDigest  string    `json:"contentDigest"`
}

type Writer struct {
	UID        string `json:"uid"`
	RV         string `json:"resourceVersion"`
	Replicas   int32  `json:"replicas"`
	SpecDigest string `json:"specDigest"`
}

type Snapshot struct {
	Cluster Cluster    `json:"cluster"`
	System  Namespace  `json:"system"`
	Runtime Namespace  `json:"runtime"`
	Secret  Descriptor `json:"secret"`
	Writer  Writer     `json:"writer"`
}

type Plan struct {
	Version  int      `json:"version"`
	Source   string   `json:"source"`
	Tree     string   `json:"tree"`
	Snapshot Snapshot `json:"snapshot"`
	State    string   `json:"state"`
	Restored bool     `json:"restored"`
}

type Boundary interface {
	Snapshot(context.Context, string) (Snapshot, error)
	References(context.Context, Descriptor) (bool, error)
	Consumers(context.Context, Descriptor) (bool, error)
	Pause(context.Context, Writer) error
	WaitStopped(context.Context, Writer) error
	Delete(context.Context, Descriptor) error
	Absent(context.Context, Descriptor) (bool, error)
	Restore(context.Context, Writer) error
}

func Digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// MetadataDescriptor не читает Secret data, type или immutable.
func MetadataDescriptor(m *metav1.PartialObjectMetadata) (Descriptor, error) {
	if m == nil || m.Namespace != "kodex-runtime" || m.UID == "" || m.ResourceVersion == "" || m.DeletionTimestamp != nil || m.Labels["runtime-secrets.kodex.dev/managed"] != "true" || len(m.OwnerReferences) != 0 || len(m.Finalizers) != 0 {
		return Descriptor{}, ErrGuard
	}
	prefix := "runtime-secrets.kodex.dev/"
	revision, err := strconv.ParseInt(m.Annotations[prefix+"revision"], 10, 64)
	if err != nil || revision < 1 {
		return Descriptor{}, ErrGuard
	}
	generation, err := strconv.ParseInt(m.Annotations[prefix+"claim-generation"], 10, 64)
	if err != nil || generation < 1 {
		return Descriptor{}, ErrGuard
	}
	d := Descriptor{Name: m.Name, UID: string(m.UID), RV: m.ResourceVersion, Created: m.CreationTimestamp.Time,
		Operation: m.Annotations[prefix+"operation-ref"], SecretRef: m.Annotations[prefix+"secret-ref"],
		Key: m.Annotations[prefix+"secret-key"], Revision: revision, Generation: generation,
		ContentDigest: m.Annotations[prefix+"content-sha256"]}
	name, err := runtimesecret.VersionedKubernetesName(d.SecretRef, revision)
	if err != nil || name != d.Name || !regexp.MustCompile(`^secop_[A-Za-z0-9_-]+$`).MatchString(d.Operation) || d.Key != "value" || !hex64.MatchString(d.ContentDigest) || d.Created.IsZero() {
		return Descriptor{}, ErrGuard
	}
	copy := m.DeepCopy()
	copy.ManagedFields = nil
	d.MetadataDigest = Digest(copy.ObjectMeta)
	return d, nil
}

func eligible(s Snapshot) bool {
	return s.Cluster.Version == 1 && s.Cluster.UID != "" && hex64.MatchString(s.Cluster.CA) &&
		s.System.Name == "kodex-system" && s.Runtime.Name == "kodex-runtime" &&
		s.System.UID != "" && s.Runtime.UID != "" && s.System.UID != s.Runtime.UID &&
		(s.System.Profile == "web-only" || s.System.Profile == "web-with-mattermost") &&
		s.Runtime.Profile == s.System.Profile && !s.Secret.Created.IsZero() &&
		s.Secret.Created.Before(s.System.Created) && !s.Secret.Created.Before(s.Runtime.Created) &&
		s.Writer.UID != "" && s.Writer.RV != "" && s.Writer.Replicas > 0 && hex64.MatchString(s.Writer.SpecDigest)
}

func noReferences(ctx context.Context, b Boundary, d Descriptor) error {
	refs, err := b.References(ctx, d)
	if err != nil || refs {
		return ErrGuard
	}
	refs, err = b.Consumers(ctx, d)
	if err != nil || refs {
		return ErrGuard
	}
	return nil
}

func Prepare(ctx context.Context, b Boundary, source, tree, name string) (Plan, error) {
	s, err := b.Snapshot(ctx, name)
	if err != nil || !eligible(s) {
		return Plan{}, ErrGuard
	}
	if err = noReferences(ctx, b, s.Secret); err != nil {
		return Plan{}, err
	}
	return Plan{Version: 1, Source: source, Tree: tree, Snapshot: s, State: "PLANNED"}, nil
}

// Apply сохраняет UNKNOWN до DELETE. Возобновление никогда не повторяет эффект.
func Apply(ctx context.Context, b Boundary, p *Plan, save func() error) (result error) {
	if p.Version != 1 || !eligible(p.Snapshot) {
		return ErrGuard
	}
	if p.State == "COMPLETE" && p.Restored {
		return nil
	}
	if p.State != "PLANNED" && p.State != "PAUSE_UNKNOWN" && p.State != "PAUSED" && p.State != "DELETE_UNKNOWN" && p.State != "DELETED" && p.State != "COMPLETE" {
		return ErrGuard
	}
	if p.State == "PLANNED" {
		current, err := b.Snapshot(ctx, p.Snapshot.Secret.Name)
		if err != nil || current != p.Snapshot {
			return ErrGuard
		}
		if err = noReferences(ctx, b, p.Snapshot.Secret); err != nil {
			return err
		}
		p.State = "PAUSE_UNKNOWN"
		if err = save(); err != nil {
			return err
		}
		// Даже неоднозначный scale получает guarded restoration ниже.
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if err := b.Restore(cleanup, p.Snapshot.Writer); err != nil {
				result = errors.Join(result, ErrGuard)
				return
			}
			p.Restored = true
			result = errors.Join(result, save())
		}()
		if err = b.Pause(ctx, p.Snapshot.Writer); err != nil {
			return ErrGuard
		}
		if err = b.WaitStopped(ctx, p.Snapshot.Writer); err != nil {
			return ErrGuard
		}
		p.State = "PAUSED"
		if err = save(); err != nil {
			return err
		}
		current, err = b.Snapshot(ctx, p.Snapshot.Secret.Name)
		if err != nil {
			return ErrGuard
		}
		if current.Writer.UID != p.Snapshot.Writer.UID || current.Writer.SpecDigest != p.Snapshot.Writer.SpecDigest || current.Writer.Replicas != 0 {
			return ErrGuard
		}
		current.Writer = p.Snapshot.Writer
		if current != p.Snapshot {
			return ErrGuard
		}
		if err = noReferences(ctx, b, p.Snapshot.Secret); err != nil {
			return err
		}
		p.State = "DELETE_UNKNOWN"
		if err = save(); err != nil {
			return err
		}
		if err = b.Delete(ctx, p.Snapshot.Secret); err != nil {
			return ErrGuard
		}
	} else {
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if err := b.Restore(cleanup, p.Snapshot.Writer); err != nil {
				result = errors.Join(result, ErrGuard)
				return
			}
			p.Restored = true
			result = errors.Join(result, save())
		}()
		if p.State == "PAUSE_UNKNOWN" || p.State == "PAUSED" {
			return ErrGuard
		}
	}
	absent, err := b.Absent(ctx, p.Snapshot.Secret)
	if err != nil || !absent {
		return ErrGuard
	}
	if err = noReferences(ctx, b, p.Snapshot.Secret); err != nil {
		return err
	}
	p.State = "DELETED"
	if err = save(); err != nil {
		return err
	}
	p.State = "COMPLETE"
	return save()
}
