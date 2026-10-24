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

// ErrGuard marks operator orphan-maintenance guard rejection with one fixed error rather than
// exposing source, cluster or resource details.
// ErrGuard обозначает отказ guard операторского orphan maintenance одной фиксированной ошибкой
// вместо раскрытия source, cluster либо resource деталей.
var ErrGuard = errors.New("orphan maintenance guard rejected")

// hex64 checks exactly sixty-four lowercase hexadecimal characters for stored cluster, metadata
// and writer digest guards.
// hex64 проверяет ровно шестьдесят четыре строчных hex символа для сохраняемых cluster, metadata и
// writer digest guards.
var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Cluster pins the operator-selected cluster contract version, identity, API endpoint and CA
// digest in a maintenance receipt.
// Cluster фиксирует выбранные оператором contract version кластера, identity, API endpoint и CA
// digest в maintenance receipt.
type Cluster struct {
	Version  int    `json:"version"`
	UID      string `json:"clusterUID"`
	Endpoint string `json:"apiEndpoint"`
	CA       string `json:"caSHA256"`
}

// Namespace records exact namespace name/UID, creation instant and local profile without inferring
// authority merely from its name.
// Namespace хранит точные namespace name и UID, момент создания и local profile без вывода
// полномочий только из имени.
type Namespace struct {
	Name    string    `json:"name"`
	UID     string    `json:"uid"`
	Created time.Time `json:"created"`
	Profile string    `json:"profile"`
}

// Equal compares all namespace identity/profile fields and the exact creation instant after UTC
// conversion removes location and monotonic representation differences.
// Equal сравнивает все namespace identity и profile поля и точный момент создания после UTC
// преобразования, устраняющего различия location и monotonic представления.
func (n Namespace) Equal(other Namespace) bool {
	n.Created = n.Created.UTC()
	other.Created = other.Created.UTC()
	return n == other
}

// Descriptor stores metadata-only Secret identity, resource version, creation time,
// metadata/content digests and owner operation/revision/generation pins without Secret data.
// Descriptor хранит metadata-only Secret identity, resource version, время создания, metadata и
// content digests и owner operation, revision и generation pins без Secret data.
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

// Writer pins the secret-broker writer UID/resource version, desired replicas and spec digest
// excluding replicas for guarded pause/restore.
// Writer фиксирует UID и resource version secret-broker writer, desired replicas и spec digest без
// replicas для guarded pause и restore.
type Writer struct {
	UID        string `json:"uid"`
	RV         string `json:"resourceVersion"`
	Replicas   int32  `json:"replicas"`
	SpecDigest string `json:"specDigest"`
}

// Snapshot binds one cluster, system/runtime namespace identities, exact Secret descriptor and
// writer state for operator plan comparison.
// Snapshot связывает один cluster, system и runtime namespace identities, точный Secret descriptor
// и writer state для сравнения операторского плана.
type Snapshot struct {
	Cluster Cluster    `json:"cluster"`
	System  Namespace  `json:"system"`
	Runtime Namespace  `json:"runtime"`
	Secret  Descriptor `json:"secret"`
	Writer  Writer     `json:"writer"`
}

// Equal compares every snapshot field after normalizing only the system/runtime namespace and
// Secret creation timestamps to UTC.
// Equal сравнивает каждое поле snapshot после нормализации только времени создания system и
// runtime namespace и Secret в UTC.
func (s Snapshot) Equal(other Snapshot) bool {
	s.System.Created, other.System.Created = s.System.Created.UTC(), other.System.Created.UTC()
	s.Runtime.Created, other.Runtime.Created = s.Runtime.Created.UTC(), other.Runtime.Created.UTC()
	s.Secret.Created, other.Secret.Created = s.Secret.Created.UTC(), other.Secret.Created.UTC()
	return s == other
}

// Plan persists source/tree locators, the exact preparation snapshot, lifecycle state and
// restoration result for one bounded orphan operation.
// Plan сохраняет source и tree locators, точный preparation snapshot, lifecycle state и
// restoration result одной ограниченной orphan операции.
type Plan struct {
	Version  int      `json:"version"`
	Source   string   `json:"source"`
	Tree     string   `json:"tree"`
	Snapshot Snapshot `json:"snapshot"`
	State    string   `json:"state"`
	Restored bool     `json:"restored"`
}

// Boundary separates authoritative snapshot/reference/consumer reads, guarded writer pause/delete
// and restoration; caller-provided fields do not replace these checks.
// Boundary разделяет авторитетные snapshot, reference и consumer чтения, guarded writer pause и
// delete и восстановление; поля caller не заменяют эти проверки.
type Boundary interface {
	// Snapshot reads current exact cluster/namespace/Secret/writer metadata for comparison with a
	// stored operator plan.
	// Snapshot читает текущие точные cluster, namespace, Secret и writer metadata для сравнения с
	// сохранённым операторским планом.
	Snapshot(context.Context, string) (Snapshot, error)
	// References checks authoritative owner references to the exact Secret descriptor before an
	// irreversible effect.
	// References проверяет авторитетные owner references точного Secret descriptor до необратимого
	// эффекта.
	References(context.Context, Descriptor) (bool, error)
	// Consumers checks actual Kubernetes consumers of the exact Secret identity without restricting
	// the scan to friendly labels.
	// Consumers проверяет фактических Kubernetes consumers точной Secret identity без ограничения
	// сканирования удобными labels.
	Consumers(context.Context, Descriptor) (bool, error)
	// Pause pauses only the exact planned writer under its identity/spec/version preconditions.
	// Pause приостанавливает только точный planned writer под его identity, spec и version
	// preconditions.
	Pause(context.Context, Writer) error
	// WaitStopped waits for the exact paused writer and all broker-service-account Pods to stop before
	// deletion is considered.
	// WaitStopped ожидает остановки точного paused writer и всех Pods broker service account до
	// рассмотрения удаления.
	WaitStopped(context.Context, Writer) error
	// Delete attempts one deletion of the exact Secret UID/resource version after the plan has durably
	// recorded an uncertain outcome.
	// Delete пытается однократно удалить точные Secret UID и resource version после durable фиксации
	// неопределённого исхода планом.
	Delete(context.Context, Descriptor) error
	// Absent reads authoritative absence while preserving the prepared namespace identities instead of
	// treating an arbitrary 404 as proof.
	// Absent читает авторитетное отсутствие с сохранением подготовленных namespace identities вместо
	// признания произвольного404 доказательством.
	Absent(context.Context, Descriptor) (bool, error)
	// Restore restores only the originally planned writer replica count while retaining identity and
	// spec guards.
	// Restore восстанавливает только исходное planned число replicas writer с сохранением identity и
	// spec guards.
	Restore(context.Context, Writer) error
}

// Digest hashes JSON serialization with lowercase SHA-256; it does not validate values and
// currently hashes nil bytes if JSON marshaling fails.
// Digest хеширует JSON сериализацию строчным SHA-256; значения не проверяет и при ошибке JSON
// marshaling сейчас хеширует nil bytes.
func Digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// MetadataDescriptor validates managed runtime Secret metadata, canonical name/key and positive
// revision/generation, then hashes metadata excluding managed fields without reading Secret
// data/type/immutable.
// MetadataDescriptor проверяет managed runtime Secret metadata, canonical name и key и
// положительные revision и generation, затем хеширует metadata без managed fields и без чтения
// Secret data, type либо immutable.
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

// eligible requires the fixed local namespace pair/profile and Secret creation epoch plus nonempty
// cluster/writer identities and digest-shaped pins; this is not live reference verification.
// eligible требует фиксированные local namespace pair и profile и эпоху создания Secret, непустые
// cluster и writer identities и digest-shaped pins; live references эта функция не проверяет.
func eligible(s Snapshot) bool {
	return s.Cluster.Version == 1 && s.Cluster.UID != "" && hex64.MatchString(s.Cluster.CA) &&
		s.System.Name == "kodex-system" && s.Runtime.Name == "kodex-runtime" &&
		s.System.UID != "" && s.Runtime.UID != "" && s.System.UID != s.Runtime.UID &&
		(s.System.Profile == "web-only" || s.System.Profile == "web-with-mattermost") &&
		s.Runtime.Profile == s.System.Profile && !s.Secret.Created.IsZero() &&
		s.Secret.Created.Before(s.System.Created) && !s.Secret.Created.Before(s.Runtime.Created) &&
		s.Writer.UID != "" && s.Writer.RV != "" && s.Writer.Replicas > 0 && hex64.MatchString(s.Writer.SpecDigest)
}

// noReferences requires both authoritative owner references and actual Kubernetes consumers to be
// absent, mapping either error or positive result to the guard failure.
// noReferences требует отсутствия и авторитетных owner references, и фактических Kubernetes
// consumers с отображением ошибки либо положительного результата любого чтения в отказ guard.
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

// Prepare reads and checks an eligible exact snapshot plus absence of references/consumers,
// returning PLANNED while recording source/tree strings supplied by the operator.
// Prepare читает и проверяет eligible точный snapshot и отсутствие references и consumers,
// возвращая PLANNED с записью source и tree строк оператора.
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

// Apply persists pause/delete uncertainty before effects, rechecks exact metadata and references,
// attempts deletion only from PLANNED and resolves resumed outcomes by absence readback with
// guarded restoration; completed restored receipts return immediately.
// Apply сохраняет неопределённость pause и delete до эффектов, повторно проверяет точные metadata
// и references, пытается удалить только из PLANNED и разрешает resumed исход чтением отсутствия с
// guarded restoration; completed restored receipts возвращаются сразу.
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
		if err != nil || !current.Equal(p.Snapshot) {
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
		if !current.Equal(p.Snapshot) {
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
