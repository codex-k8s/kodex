package runtimeorphan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fakeBoundary models only in-memory orphan snapshots, references, consumers and effect counters,
// including lost acknowledgements and restoration failure.
// fakeBoundary моделирует только in-memory orphan snapshots, references, consumers и счётчики
// эффектов, включая потерю подтверждения и ошибку восстановления.
type fakeBoundary struct {
	s                                                            Snapshot
	refs, consumers, paused, absent, lostACK, drift, restoreFail bool
	deletes, restores                                            int
	beforeDelete                                                 func()
	snapshotChange                                               func(*Snapshot)
}

// fixture builds one eligible synthetic namespace epoch, Secret descriptor and broker writer with
// no external cluster or owner access.
// fixture создаёт одну допустимую синтетическую эпоху namespace, Secret descriptor и broker writer
// без доступа к внешнему кластеру либо owner.
func fixture() *fakeBoundary {
	now := time.Unix(1788738000, 0).UTC()
	return &fakeBoundary{s: Snapshot{Cluster: Cluster{Version: 1, UID: "cluster", CA: strings.Repeat("a", 64)},
		System:  Namespace{Name: "kodex-system", UID: "system", Created: now, Profile: "web-only"},
		Runtime: Namespace{Name: "kodex-runtime", UID: "runtime", Created: now.Add(-2 * time.Hour), Profile: "web-only"},
		Secret:  Descriptor{Name: "secret", UID: "uid", RV: "1", Created: now.Add(-time.Hour)},
		Writer:  Writer{UID: "writer", RV: "2", Replicas: 1, SpecDigest: strings.Repeat("b", 64)}}}
}

// Snapshot returns the synthetic snapshot with paused writer state and optional injected drift for
// guard tests.
// Snapshot возвращает синтетический snapshot с приостановленным writer и необязательным внедрённым
// дрейфом для guard тестов.
func (f *fakeBoundary) Snapshot(context.Context, string) (Snapshot, error) {
	s := f.s
	if f.paused {
		s.Writer.Replicas = 0
	}
	if f.drift {
		s.Secret.RV = "different"
	}
	if f.snapshotChange != nil {
		f.snapshotChange(&s)
	}
	return s, nil
}

// persistedPlan round-trips a plan through a private temporary receipt, closing and reopening its
// store to simulate a fresh process reader.
// persistedPlan проводит план через private временный receipt с закрытием и повторным открытием
// store для имитации reader нового процесса.
func persistedPlan(t *testing.T, p Plan) (Plan, *Store) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plan.json")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(p, true); err != nil {
		store.Close()
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	p, err = store.Read()
	if err != nil {
		t.Fatal(err)
	}
	return p, store
}

// TestPlanReceiptTimestampRoundTrip checks API-style timestamp persistence and unchanged instants
// across a fresh reader, requiring durable DELETE_UNKNOWN before the simulated effect.
// TestPlanReceiptTimestampRoundTrip проверяет сохранение timestamp в формате API и неизменность
// моментов через новый reader, требуя durable DELETE_UNKNOWN до имитируемого эффекта.
func TestPlanReceiptTimestampRoundTrip(t *testing.T) {
	for _, offset := range []int{0, 4 * 60 * 60} {
		t.Run(time.FixedZone("Local", offset).String()+time.Duration(offset).String(), func(t *testing.T) {
			f := fixture()
			// Используем настоящий Kubernetes JSON decoder, затем локальную зону
			// producer; receipt декодируется стандартным time.Time.
			for _, created := range []*time.Time{&f.s.System.Created, &f.s.Runtime.Created, &f.s.Secret.Created} {
				raw, err := json.Marshal(metav1.NewTime(*created))
				if err != nil {
					t.Fatal(err)
				}
				var decoded metav1.Time
				if err = json.Unmarshal(raw, &decoded); err != nil {
					t.Fatal(err)
				}
				*created = decoded.Time.In(time.FixedZone("Local", offset))
			}
			p, err := Prepare(t.Context(), f, "source", "tree", "secret")
			if err != nil {
				t.Fatal(err)
			}
			p, store := persistedPlan(t, p)
			if f.s == p.Snapshot || !f.s.System.Equal(p.Snapshot.System) || !f.s.Runtime.Equal(p.Snapshot.Runtime) || !f.s.Equal(p.Snapshot) {
				t.Fatal("receipt representation did not preserve the exact instant")
			}
			f.beforeDelete = func() {
				durable, err := store.Read()
				if err != nil || durable.State != "DELETE_UNKNOWN" || !f.paused {
					t.Fatal("effect before durable UNKNOWN")
				}
			}
			if err = Apply(t.Context(), f, &p, func() error { return store.Save(p, false) }); err != nil || f.deletes != 1 || !p.Restored {
				t.Fatal("saved plan did not apply", err)
			}
		})
	}
}

// TestReceiptMetadataDriftRemainsClosed checks that pinned metadata drift before or after writer
// pause prevents deletion despite a persisted receipt round trip.
// TestReceiptMetadataDriftRemainsClosed проверяет, что дрейф закреплённых metadata до либо после
// pause writer запрещает удаление и после сохранения и чтения receipt.
func TestReceiptMetadataDriftRemainsClosed(t *testing.T) {
	changes := map[string]func(*Snapshot){
		"system_instant":  func(s *Snapshot) { s.System.Created = s.System.Created.Add(time.Nanosecond) },
		"runtime_instant": func(s *Snapshot) { s.Runtime.Created = s.Runtime.Created.Add(time.Nanosecond) },
		"secret_instant":  func(s *Snapshot) { s.Secret.Created = s.Secret.Created.Add(time.Nanosecond) },
		"system_uid":      func(s *Snapshot) { s.System.UID += "-changed" },
		"runtime_uid":     func(s *Snapshot) { s.Runtime.UID += "-changed" },
		"profile":         func(s *Snapshot) { s.System.Profile = "web-with-mattermost" },
		"secret_uid":      func(s *Snapshot) { s.Secret.UID += "-changed" },
		"secret_rv":       func(s *Snapshot) { s.Secret.RV += "0" },
		"metadata_digest": func(s *Snapshot) { s.Secret.MetadataDigest = strings.Repeat("c", 64) },
		"operation":       func(s *Snapshot) { s.Secret.Operation += "-changed" },
		"content_digest":  func(s *Snapshot) { s.Secret.ContentDigest = strings.Repeat("d", 64) },
		"writer_uid":      func(s *Snapshot) { s.Writer.UID += "-changed" },
		"writer_spec":     func(s *Snapshot) { s.Writer.SpecDigest = strings.Repeat("c", 64) },
		"cluster":         func(s *Snapshot) { s.Cluster.UID += "-changed" },
	}
	for name, change := range changes {
		for _, afterPause := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/before_pause", true: "/after_pause"}[afterPause], func(t *testing.T) {
				f := fixture()
				p, _ := Prepare(t.Context(), f, "source", "tree", "secret")
				p, store := persistedPlan(t, p)
				f.snapshotChange = func(s *Snapshot) {
					if f.paused == afterPause {
						change(s)
					}
				}
				if Apply(t.Context(), f, &p, func() error { return store.Save(p, false) }) == nil || f.deletes != 0 {
					t.Fatal("metadata drift allowed DELETE")
				}
			})
		}
	}
}

// TestNamespaceIdentityAndInstant checks namespace name, UID, profile and creation-instant drift,
// plus writer resource-version rejection before pause.
// TestNamespaceIdentityAndInstant проверяет дрейф namespace name, UID, profile и момента создания,
// а также отклонение writer resource version до pause.
func TestNamespaceIdentityAndInstant(t *testing.T) {
	n := fixture().s.System
	for name, change := range map[string]func(*Namespace){
		"name":    func(other *Namespace) { other.Name += "-changed" },
		"uid":     func(other *Namespace) { other.UID += "-changed" },
		"profile": func(other *Namespace) { other.Profile = "web-with-mattermost" },
		"instant": func(other *Namespace) { other.Created = other.Created.Add(time.Nanosecond) },
	} {
		t.Run(name, func(t *testing.T) {
			other := n
			change(&other)
			if n.Equal(other) {
				t.Fatal("namespace drift accepted")
			}
		})
	}
	f := fixture()
	p, _ := Prepare(t.Context(), f, "source", "tree", "secret")
	f.s.Writer.RV += "0"
	if Apply(t.Context(), f, &p, func() error { return nil }) == nil || f.paused || f.deletes != 0 {
		t.Fatal("writer resourceVersion drift accepted before pause")
	}
}

// References returns the configured in-memory owner-reference flag without contacting an owner
// database.
// References возвращает настроенный in-memory признак owner references без обращения к базе owner.
func (f *fakeBoundary) References(context.Context, Descriptor) (bool, error) { return f.refs, nil }

// Consumers returns the configured in-memory consumer flag without listing any Kubernetes
// resources.
// Consumers возвращает настроенный in-memory признак consumer без получения Kubernetes ресурсов.
func (f *fakeBoundary) Consumers(context.Context, Descriptor) (bool, error) { return f.consumers, nil }

// Pause marks only the simulated writer as paused without issuing a Kubernetes scale operation.
// Pause отмечает только имитируемый writer как приостановленный без Kubernetes операции scale.
func (f *fakeBoundary) Pause(context.Context, Writer) error { f.paused = true; return nil }

// WaitStopped completes the simulated stop barrier immediately without polling an external API.
// WaitStopped немедленно завершает имитируемый барьер остановки без опроса внешнего API.
func (f *fakeBoundary) WaitStopped(context.Context, Writer) error { return nil }

// Delete counts the simulated deletion, runs the pre-effect receipt assertion and can lose its
// acknowledgement after marking the object absent.
// Delete учитывает имитируемое удаление, вызывает проверку receipt до эффекта и может потерять
// подтверждение после отметки отсутствия объекта.
func (f *fakeBoundary) Delete(context.Context, Descriptor) error {
	if f.beforeDelete != nil {
		f.beforeDelete()
	}
	f.deletes++
	f.absent = true
	if f.lostACK {
		return errors.New("lost ACK")
	}
	return nil
}

// Absent returns the in-memory absence flag for resumed-outcome checks without reading a real
// Secret.
// Absent возвращает in-memory признак отсутствия для проверки возобновлённого исхода без чтения
// реального Secret.
func (f *fakeBoundary) Absent(context.Context, Descriptor) (bool, error) { return f.absent, nil }

// Restore counts restoration and either returns the injected failure or clears the synthetic
// paused writer state.
// Restore учитывает восстановление и либо возвращает внедрённую ошибку, либо снимает синтетический
// признак приостановки writer.
func (f *fakeBoundary) Restore(context.Context, Writer) error {
	f.restores++
	if f.restoreFail {
		return ErrGuard
	}
	f.paused = false
	return nil
}

// TestOrphanEffectAndLostACK checks one delete across successful and lost-acknowledgement
// outcomes, then resolves the persisted plan without repeating deletion.
// TestOrphanEffectAndLostACK проверяет одно удаление при успешном исходе и потере подтверждения,
// затем разрешает сохранённый план без повторного удаления.
func TestOrphanEffectAndLostACK(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "lost_ACK"}[lost], func(t *testing.T) {
			f := fixture()
			f.lostACK = lost
			p, err := Prepare(t.Context(), f, "source", "tree", "secret")
			if err != nil {
				t.Fatal(err)
			}
			var durable Plan
			save := func() error { durable = p; return nil }
			f.beforeDelete = func() {
				if durable.State != "DELETE_UNKNOWN" || !f.paused {
					t.Fatal("effect before durable fence")
				}
			}
			err = Apply(t.Context(), f, &p, save)
			if (err != nil) != lost || f.deletes != 1 || f.paused {
				t.Fatal("effect/restoration mismatch")
			}
			if err = Apply(t.Context(), f, &p, save); err != nil {
				t.Fatal(err)
			}
			if f.deletes != 1 || p.State != "COMPLETE" || !p.Restored {
				t.Fatal("replayed effect")
			}
		})
	}
}

// TestOrphanGuardsBeforeEffect checks reference, consumer, namespace epoch/profile, object
// identity/version and receipt-save failures prevent the simulated irreversible effect.
// TestOrphanGuardsBeforeEffect проверяет, что references, consumers, эпоха и профиль namespace,
// identity и version объекта и ошибка сохранения receipt запрещают имитируемый необратимый эффект.
func TestOrphanGuardsBeforeEffect(t *testing.T) {
	for _, kind := range []string{"refs", "consumers", "epoch", "profile", "UID_RV", "save"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture()
			p, _ := Prepare(t.Context(), f, "source", "tree", "secret")
			save := func() error { return nil }
			switch kind {
			case "refs":
				f.refs = true
			case "consumers":
				f.consumers = true
			case "epoch":
				p.Snapshot.Secret.Created = p.Snapshot.System.Created
			case "profile":
				p.Snapshot.Runtime.Profile = "foreign"
			case "UID_RV":
				f.drift = true
			case "save":
				save = func() error { return ErrGuard }
			}
			if Apply(t.Context(), f, &p, save) == nil || f.deletes != 0 {
				t.Fatal("guard allowed effect")
			}
		})
	}
}

// TestUnknownPresentNeverRetries checks an uncertain deletion with the object still present fails
// closed and restores the writer without retrying delete.
// TestUnknownPresentNeverRetries проверяет, что неопределённое удаление при сохранённом объекте
// закрыто отклоняется и восстанавливает writer без повторного delete.
func TestUnknownPresentNeverRetries(t *testing.T) {
	f := fixture()
	p, _ := Prepare(t.Context(), f, "s", "t", "secret")
	p.State = "DELETE_UNKNOWN"
	if Apply(t.Context(), f, &p, func() error { return nil }) == nil || f.deletes != 0 || f.restores != 1 {
		t.Fatal("unknown repeated")
	}
}

// TestRestorationCanRecoverWithoutDelete checks a later apply can retry guarded restoration after
// its earlier failure without issuing a second deletion.
// TestRestorationCanRecoverWithoutDelete проверяет, что последующий apply повторяет guarded
// восстановление после его ошибки без второго удаления.
func TestRestorationCanRecoverWithoutDelete(t *testing.T) {
	f := fixture()
	f.restoreFail = true
	p, _ := Prepare(t.Context(), f, "s", "t", "secret")
	if Apply(t.Context(), f, &p, func() error { return nil }) == nil {
		t.Fatal("lost restoration hidden")
	}
	f.restoreFail = false
	if Apply(t.Context(), f, &p, func() error { return nil }) != nil || f.deletes != 1 || !p.Restored {
		t.Fatal("restoration retry repeated delete")
	}
}

// TestPrivateReceipt checks exclusive initial creation, competing lock rejection, symlink refusal
// and private-mode enforcement using temporary filesystem fixtures.
// TestPrivateReceipt проверяет исключительное начальное создание, отклонение конкурирующего lock,
// symlink и неверного private mode на временных filesystem fixtures.
func TestPrivateReceipt(t *testing.T) {
	dir := t.TempDir()
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("chmod")
	}
	path := filepath.Join(dir, "plan.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := Plan{Version: 1, State: "DELETE_UNKNOWN"}
	if s.Save(p, true) != nil || s.Save(p, true) == nil {
		t.Fatal("exclusive create")
	}
	if _, err = OpenStore(path); err == nil {
		t.Fatal("concurrent receipt writer")
	}
	read, err := s.Read()
	if err != nil || read.State != "DELETE_UNKNOWN" {
		t.Fatal("durable receipt")
	}
	link := filepath.Join(dir, "link")
	if os.Symlink(path, link) != nil {
		t.Fatal("symlink")
	}
	other, err := OpenStore(link)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err = other.Read(); err == nil {
		t.Fatal("symlink accepted")
	}
	if os.Chmod(path, 0644) != nil {
		t.Fatal("chmod")
	}
	if _, err = s.Read(); err == nil {
		t.Fatal("public receipt accepted")
	}
}
