package runtimeorphan

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const metadataAccept = "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1"

type Kubernetes struct {
	Client     kubernetes.Interface
	HTTP       *http.Client
	Endpoint   string
	Identity   Cluster
	System     Namespace
	Runtime    Namespace
	OwnerCheck func(context.Context, Descriptor) (bool, error)
}

func NewKubernetes(config *rest.Config, identity Cluster) (*Kubernetes, error) {
	if config.Insecure || !strings.HasPrefix(config.Host, "https://") {
		return nil, ErrGuard
	}
	config = rest.CopyConfig(config)
	config.Timeout = 20 * time.Second
	h, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, ErrGuard
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c, err := kubernetes.NewForConfigAndClient(config, h)
	if err != nil {
		return nil, ErrGuard
	}
	return &Kubernetes{Client: c, HTTP: h, Endpoint: strings.TrimRight(config.Host, "/"), Identity: identity}, nil
}

func (k *Kubernetes) secretRequest(ctx context.Context, method, name string, body []byte) (*http.Response, error) {
	u := k.Endpoint + "/api/v1/namespaces/kodex-runtime/secrets/" + url.PathEscape(name)
	r, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, ErrGuard
	}
	r.Header.Set("Accept", metadataAccept)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := k.HTTP.Do(r)
	if err != nil {
		return nil, ErrGuard
	}
	return response, nil
}

func (k *Kubernetes) Metadata(ctx context.Context, name string) (*metav1.PartialObjectMetadata, bool, error) {
	r, err := k.secretRequest(ctx, http.MethodGet, name, nil)
	if err != nil {
		return nil, false, err
	}
	defer r.Body.Close()
	if r.StatusCode == http.StatusNotFound {
		var status metav1.Status
		if json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&status) != nil || status.Kind != "Status" || status.Reason != metav1.StatusReasonNotFound || status.Details == nil || status.Details.Name != name {
			return nil, false, ErrGuard
		}
		return nil, true, nil
	}
	if r.StatusCode != http.StatusOK {
		return nil, false, ErrGuard
	}
	var m metav1.PartialObjectMetadata
	if json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&m) != nil || m.Kind != "PartialObjectMetadata" || m.APIVersion != "meta.k8s.io/v1" {
		return nil, false, ErrGuard
	}
	return &m, false, nil
}

func namespace(m metav1.ObjectMeta, name string) (Namespace, error) {
	p := m.Labels["kodex.dev/profile"]
	if m.Name != name || m.UID == "" || m.CreationTimestamp.IsZero() || m.DeletionTimestamp != nil ||
		m.Labels["app.kubernetes.io/part-of"] != "kodex" || m.Labels["kodex.dev/local-profile"] != "hot-reload" || (p != "web-only" && p != "web-with-mattermost") {
		return Namespace{}, ErrGuard
	}
	return Namespace{Name: name, UID: string(m.UID), Created: m.CreationTimestamp.Time, Profile: p}, nil
}

func (k *Kubernetes) Namespaces(ctx context.Context) (Namespace, Namespace, error) {
	s, err := k.Client.CoreV1().Namespaces().Get(ctx, "kodex-system", metav1.GetOptions{})
	if err != nil {
		return Namespace{}, Namespace{}, ErrGuard
	}
	r, err := k.Client.CoreV1().Namespaces().Get(ctx, "kodex-runtime", metav1.GetOptions{})
	if err != nil {
		return Namespace{}, Namespace{}, ErrGuard
	}
	system, err := namespace(s.ObjectMeta, "kodex-system")
	if err != nil {
		return Namespace{}, Namespace{}, err
	}
	runtime, err := namespace(r.ObjectMeta, "kodex-runtime")
	return system, runtime, err
}

func writer(d *appsv1.Deployment) (Writer, error) {
	if d.UID == "" || d.ResourceVersion == "" || d.DeletionTimestamp != nil || len(d.OwnerReferences) != 0 || d.Spec.Replicas == nil ||
		d.Spec.Template.Spec.ServiceAccountName != "secret-broker" || d.Spec.Template.Labels["kodex.dev/local-profile"] != "hot-reload" || d.Spec.Template.Labels["kodex.dev/environment"] != "staging" {
		return Writer{}, ErrGuard
	}
	spec := d.Spec.DeepCopy()
	spec.Replicas = nil
	return Writer{UID: string(d.UID), RV: d.ResourceVersion, Replicas: *d.Spec.Replicas, SpecDigest: Digest(spec)}, nil
}

func (k *Kubernetes) Snapshot(ctx context.Context, name string) (Snapshot, error) {
	hpas, err := k.Client.AutoscalingV2().HorizontalPodAutoscalers("kodex-system").List(ctx, metav1.ListOptions{Limit: 100})
	if err != nil || hpas.Continue != "" {
		return Snapshot{}, ErrGuard
	}
	for _, hpa := range hpas.Items {
		if hpa.Spec.ScaleTargetRef.Name == "secret-broker" {
			return Snapshot{}, ErrGuard
		}
	}
	s, r, err := k.Namespaces(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	m, absent, err := k.Metadata(ctx, name)
	if err != nil || absent {
		return Snapshot{}, ErrGuard
	}
	d, err := MetadataDescriptor(m)
	if err != nil {
		return Snapshot{}, err
	}
	deployment, err := k.Client.AppsV1().Deployments("kodex-system").Get(ctx, "secret-broker", metav1.GetOptions{})
	if err != nil {
		return Snapshot{}, ErrGuard
	}
	w, err := writer(deployment)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Cluster: k.Identity, System: s, Runtime: r, Secret: d, Writer: w}, nil
}

func (k *Kubernetes) References(ctx context.Context, d Descriptor) (bool, error) {
	if k.OwnerCheck == nil {
		return false, ErrGuard
	}
	return k.OwnerCheck(ctx, d)
}

// Перебираются все Pod/controller/ServiceAccount без label/tenant фильтра.
func (k *Kubernetes) Consumers(ctx context.Context, d Descriptor) (bool, error) {
	paths := []string{"/api/v1/namespaces/kodex-runtime/pods", "/api/v1/namespaces/kodex-runtime/serviceaccounts"}
	for _, kind := range []string{"deployments", "replicasets", "statefulsets", "daemonsets"} {
		paths = append(paths, "/apis/apps/v1/namespaces/kodex-runtime/"+kind)
	}
	for _, kind := range []string{"jobs", "cronjobs"} {
		paths = append(paths, "/apis/batch/v1/namespaces/kodex-runtime/"+kind)
	}
	for _, path := range paths {
		cursor := ""
		count := 0
		for {
			r, err := http.NewRequestWithContext(ctx, http.MethodGet, k.Endpoint+path+"?limit=100&continue="+url.QueryEscape(cursor), nil)
			if err != nil {
				return false, ErrGuard
			}
			r.Header.Set("Accept", "application/json")
			response, err := k.HTTP.Do(r)
			if err != nil {
				return false, ErrGuard
			}
			var page struct {
				Kind     string `json:"kind"`
				Metadata struct {
					Continue string `json:"continue"`
				} `json:"metadata"`
				Items []json.RawMessage `json:"items"`
			}
			err = json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&page)
			response.Body.Close()
			if err != nil || response.StatusCode != 200 || !strings.HasSuffix(page.Kind, "List") || page.Items == nil {
				return false, ErrGuard
			}
			for _, item := range page.Items {
				var object any
				if json.Unmarshal(item, &object) != nil {
					return false, ErrGuard
				}
				if references(object, d.Name, d.UID) {
					return true, nil
				}
			}
			count += len(page.Items)
			if count > 10000 {
				return false, ErrGuard
			}
			if page.Metadata.Continue == "" {
				break
			}
			if page.Metadata.Continue == cursor {
				return false, ErrGuard
			}
			cursor = page.Metadata.Continue
		}
	}
	return false, nil
}

func references(v any, names ...string) bool {
	switch x := v.(type) {
	case string:
		for _, name := range names {
			if x == name {
				return true
			}
		}
	case []any:
		for _, item := range x {
			if references(item, names...) {
				return true
			}
		}
	case map[string]any:
		for _, item := range x {
			if references(item, names...) {
				return true
			}
		}
	}
	return false
}

func (k *Kubernetes) scale(ctx context.Context, w Writer, replicas int32, exactRV bool) error {
	d, err := k.Client.AppsV1().Deployments("kodex-system").Get(ctx, "secret-broker", metav1.GetOptions{})
	if err != nil {
		return ErrGuard
	}
	current, err := writer(d)
	if err != nil || current.UID != w.UID || current.SpecDigest != w.SpecDigest || (exactRV && current.RV != w.RV) {
		return ErrGuard
	}
	if current.Replicas == replicas {
		return nil
	}
	if !exactRV && current.Replicas != 0 {
		return ErrGuard
	}
	patch, _ := json.Marshal([]map[string]any{{"op": "test", "path": "/metadata/uid", "value": current.UID}, {"op": "test", "path": "/metadata/resourceVersion", "value": current.RV}, {"op": "replace", "path": "/spec/replicas", "value": replicas}})
	r, err := http.NewRequestWithContext(ctx, http.MethodPatch, k.Endpoint+"/apis/apps/v1/namespaces/kodex-system/deployments/secret-broker", bytes.NewReader(patch))
	if err != nil {
		return ErrGuard
	}
	r.Header.Set("Content-Type", "application/json-patch+json")
	response, err := k.HTTP.Do(r)
	if err != nil {
		return ErrGuard
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		return ErrGuard
	}
	read, err := k.Client.AppsV1().Deployments("kodex-system").Get(ctx, "secret-broker", metav1.GetOptions{})
	if err != nil {
		return ErrGuard
	}
	after, err := writer(read)
	if err != nil || after.UID != w.UID || after.SpecDigest != w.SpecDigest || after.Replicas != replicas {
		return ErrGuard
	}
	return nil
}
func (k *Kubernetes) Pause(ctx context.Context, w Writer) error { return k.scale(ctx, w, 0, true) }
func (k *Kubernetes) Restore(ctx context.Context, w Writer) error {
	return k.scale(ctx, w, w.Replicas, false)
}
func (k *Kubernetes) WaitStopped(ctx context.Context, w Writer) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		d, err := k.Client.AppsV1().Deployments("kodex-system").Get(ctx, "secret-broker", metav1.GetOptions{})
		if err != nil {
			return ErrGuard
		}
		current, err := writer(d)
		if err != nil || current.UID != w.UID || current.SpecDigest != w.SpecDigest || current.Replicas != 0 {
			return ErrGuard
		}
		pods, err := k.Client.CoreV1().Pods("kodex-system").List(ctx, metav1.ListOptions{FieldSelector: "spec.serviceAccountName=secret-broker", Limit: 100})
		if err != nil || pods.Continue != "" {
			return ErrGuard
		}
		if len(pods.Items) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ErrGuard
		case <-ticker.C:
		}
	}
}

func (k *Kubernetes) Delete(ctx context.Context, d Descriptor) error {
	body, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": map[string]string{"uid": d.UID, "resourceVersion": d.RV}})
	r, err := k.secretRequest(ctx, http.MethodDelete, d.Name, body)
	if err != nil {
		return ErrGuard
	}
	defer r.Body.Close()
	if r.StatusCode != 200 && r.StatusCode != 202 {
		return ErrGuard
	}
	var result struct {
		Kind       string            `json:"kind"`
		APIVersion string            `json:"apiVersion"`
		Status     string            `json:"status"`
		Metadata   metav1.ObjectMeta `json:"metadata"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&result) != nil {
		return ErrGuard
	}
	if result.Kind == "Status" && result.APIVersion == "v1" && result.Status == "Success" {
		return nil
	}
	if result.Kind != "PartialObjectMetadata" || result.APIVersion != "meta.k8s.io/v1" || string(result.Metadata.UID) != d.UID {
		return ErrGuard
	}
	return nil
}
func (k *Kubernetes) Absent(ctx context.Context, d Descriptor) (bool, error) {
	s, r, err := k.Namespaces(ctx)
	if err != nil || !s.Equal(k.System) || !r.Equal(k.Runtime) {
		return false, ErrGuard
	}
	_, absent, err := k.Metadata(ctx, d.Name)
	return absent, err
}
