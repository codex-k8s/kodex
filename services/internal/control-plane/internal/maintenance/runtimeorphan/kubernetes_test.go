package runtimeorphan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

func TestMetadataDescriptorCanonicalKey(t *testing.T) {
	name, _ := runtimesecret.VersionedKubernetesName("sec_fixturekey", 1)
	m := metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kodex-runtime", UID: "uid", ResourceVersion: "1", CreationTimestamp: metav1.Now(), Labels: map[string]string{"runtime-secrets.kodex.dev/managed": "true"}, Annotations: map[string]string{
		"runtime-secrets.kodex.dev/operation-ref": "secop_fixture", "runtime-secrets.kodex.dev/claim-generation": "1",
		"runtime-secrets.kodex.dev/secret-ref": "sec_fixturekey", "runtime-secrets.kodex.dev/secret-key": "value",
		"runtime-secrets.kodex.dev/revision": "1", "runtime-secrets.kodex.dev/content-sha256": strings.Repeat("a", 64)}}}
	if _, err := MetadataDescriptor(&m); err != nil {
		t.Fatal(err)
	}
	m.Annotations["runtime-secrets.kodex.dev/secret-key"] = "foreign"
	if _, err := MetadataDescriptor(&m); err == nil {
		t.Fatal("foreign key accepted")
	}
}

func TestMetadataOnlyWire(t *testing.T) {
	for _, method := range []string{"GET", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept") != metadataAccept {
					t.Error("metadata fallback")
				}
				if method == "DELETE" {
					var options metav1.DeleteOptions
					if json.NewDecoder(r.Body).Decode(&options) != nil || options.Preconditions == nil || *options.Preconditions.UID != "uid" || *options.Preconditions.ResourceVersion != "rv" {
						t.Error("missing exact preconditions")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Secret","data":{"value":"fixture-sentinel"}}`))
			}))
			defer server.Close()
			k := &Kubernetes{Endpoint: server.URL, HTTP: server.Client()}
			if method == "GET" {
				if _, _, err := k.Metadata(t.Context(), "name"); err == nil {
					t.Fatal("full Secret accepted")
				}
			} else if k.Delete(t.Context(), Descriptor{Name: "name", UID: "uid", RV: "rv"}) == nil {
				t.Fatal("unexpected DELETE response accepted")
			}
		})
	}
}

func TestOrphanActualAPI(t *testing.T) {
	if os.Getenv("KODEX_ORPHAN_API_FIXTURE") != "1" {
		t.Skip("disposable API fixture not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	config, err := clientcmd.BuildConfigFromFlags("", "/etc/rancher/k3s/k3s.yaml")
	if err != nil {
		t.Fatal(err)
	}
	k, err := NewKubernetes(config, Cluster{Version: 1, UID: "fixture", CA: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{"app.kubernetes.io/part-of": "kodex", "kodex.dev/local-profile": "hot-reload", "kodex.dev/profile": "web-only"}
	createNS := func(name string) {
		if _, err := k.Client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	createNS("kodex-runtime")
	name, _ := runtimesecret.VersionedKubernetesName("sec_orphanfixture", 1)
	secret, err := k.Client.CoreV1().Secrets("kodex-runtime").Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"runtime-secrets.kodex.dev/managed": "true"}, Annotations: map[string]string{
		"runtime-secrets.kodex.dev/operation-ref": "secop_fixture", "runtime-secrets.kodex.dev/claim-generation": "1",
		"runtime-secrets.kodex.dev/secret-ref": "sec_orphanfixture", "runtime-secrets.kodex.dev/secret-key": "value",
		"runtime-secrets.kodex.dev/revision": "1", "runtime-secrets.kodex.dev/content-sha256": strings.Repeat("a", 64)}}, Data: map[string][]byte{"value": []byte("disposable-fixture-only")}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	createNS("kodex-system")
	if _, err = k.Client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "identity", Labels: map[string]string{"kodex.dev/capability": "identity"}}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = k.Client.CoreV1().ServiceAccounts("kodex-system").Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "secret-broker"}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	one := int32(1)
	_, err = k.Client.AppsV1().Deployments("kodex-system").Create(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "secret-broker"}, Spec: appsv1.DeploymentSpec{Replicas: &one, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "fixture-broker"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "fixture-broker", "kodex.dev/local-profile": "hot-reload", "kodex.dev/environment": "staging"}}, Spec: corev1.PodSpec{ServiceAccountName: "secret-broker", Containers: []corev1.Container{{Name: "broker", Image: "fixture.invalid/not-run"}}}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	k.OwnerCheck = func(context.Context, Descriptor) (bool, error) { return false, nil }
	p, err := Prepare(ctx, k, "source", "tree", name)
	if err != nil {
		t.Fatal(err)
	}
	// API metadata → private JSON → новый reader перед любым Apply.
	p, store := persistedPlan(t, p)
	k.System = p.Snapshot.System
	k.Runtime = p.Snapshot.Runtime
	system, runtimeNamespace, err := k.Namespaces(ctx)
	if err != nil || !system.Equal(p.Snapshot.System) || !runtimeNamespace.Equal(p.Snapshot.Runtime) {
		t.Fatal("namespace receipt roundtrip rejected")
	}
	wrong := p.Snapshot.Secret
	wrong.UID = "other-uid"
	if k.Delete(ctx, wrong) == nil {
		t.Fatal("wrong UID delete")
	}
	wrong = p.Snapshot.Secret
	wrong.RV = "1"
	if k.Delete(ctx, wrong) == nil {
		t.Fatal("wrong RV delete")
	}
	metadata, absent, err := k.Metadata(ctx, name)
	if err != nil || absent || metadata.UID != secret.UID {
		t.Fatal("negative DELETE changed object")
	}
	if err = Apply(ctx, k, &p, func() error { return store.Save(p, false) }); err != nil {
		t.Fatal(err)
	}
	if p.State != "COMPLETE" || !p.Restored {
		t.Fatal("incomplete receipt")
	}
	p, err = store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, k, &p, func() error { return store.Save(p, false) }); err != nil {
		t.Fatal(err)
	}
	// Scoped reset должен удалить runtime и system, сохранив identity.
	runtime, err := k.Client.CoreV1().Namespaces().Get(ctx, "kodex-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.Labels["kodex.dev/local-profile"] = "foreign"
	if _, err = k.Client.CoreV1().Namespaces().Update(ctx, runtime, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if k.Reset(ctx) == nil {
		t.Fatal("foreign runtime namespace accepted")
	}
	if _, err = k.Client.CoreV1().Namespaces().Get(ctx, "kodex-system", metav1.GetOptions{}); err != nil {
		t.Fatal("system deleted before upfront runtime guard")
	}
	runtime, err = k.Client.CoreV1().Namespaces().Get(ctx, "kodex-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.Labels["kodex.dev/local-profile"] = "hot-reload"
	if _, err = k.Client.CoreV1().Namespaces().Update(ctx, runtime, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = k.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = k.Client.CoreV1().Namespaces().Get(ctx, "identity", metav1.GetOptions{}); err != nil {
		t.Fatal("identity lost")
	}
	createNS("kodex-system")
	if err = k.Reset(ctx); err != nil {
		t.Fatal("absent runtime reset", err)
	}
}

func TestConsumerGroups(t *testing.T) {
	for _, group := range []string{"pods", "serviceaccounts", "deployments", "replicasets", "statefulsets", "daemonsets", "jobs", "cronjobs"} {
		t.Run(group, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/"+group) {
					_, _ = w.Write([]byte(`{"kind":"FixtureList","items":[{"spec":{"nested":{"secretName":"target"}}}]}`))
				} else {
					_, _ = w.Write([]byte(`{"kind":"FixtureList","items":[]}`))
				}
			}))
			defer s.Close()
			k := &Kubernetes{Endpoint: s.URL, HTTP: s.Client()}
			found, err := k.Consumers(t.Context(), Descriptor{Name: "target", UID: "uid"})
			if err != nil || !found {
				t.Fatal("consumer omitted")
			}
		})
	}
}
