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

// metadataAccept requests the meta.k8s.io/v1 partial metadata representation for Secret GET and
// DELETE responses without advertising a full-object fallback.
// metadataAccept запрашивает представление meta.k8s.io/v1 с частичными metadata для ответов GET и
// DELETE Secret без объявления запасного пути полного объекта.
const metadataAccept = "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1"

// Kubernetes implements operator maintenance with typed and raw clients, configured cluster
// identity, prepared namespaces and an explicit owner-reference callback.
// Kubernetes реализует операторское обслуживание через типизированный и HTTP clients, заданную
// identity кластера, подготовленные namespace и явный callback owner references.
type Kubernetes struct {
	Client     kubernetes.Interface
	HTTP       *http.Client
	Endpoint   string
	Identity   Cluster
	System     Namespace
	Runtime    Namespace
	OwnerCheck func(context.Context, Descriptor) (bool, error)
}

// NewKubernetes copies a nonnil TLS REST config, rejects insecure or non-HTTPS transport, sets a
// twenty-second request timeout and disables HTTP redirects for both clients.
// NewKubernetes копирует ненулевую TLS REST config, отклоняет insecure либо не-HTTPS transport,
// задаёт двадцатисекундный request timeout и запрещает HTTP redirects обоих clients.
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

// secretRequest addresses one escaped Secret name only in kodex-runtime, binds the supplied
// context and requests metadata responses for every HTTP method.
// secretRequest адресует одно экранированное имя Secret только в kodex-runtime, привязывает
// переданный контекст и запрашивает metadata ответы для каждого HTTP метода.
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

// Metadata accepts a bounded PartialObjectMetadata response or a typed NotFound status naming the
// requested object; full Secret and unexpected statuses are rejected.
// Metadata принимает ограниченный ответ PartialObjectMetadata либо типизированный NotFound status
// с именем запрошенного объекта; полный Secret и неожиданные статусы отклоняются.
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

// namespace requires the exact namespace name, nonempty UID/creation instant and approved
// hot-reload profile labels while rejecting a deleting namespace.
// namespace требует точное имя namespace, непустые UID и момент создания и утверждённые labels
// профиля hot-reload, отклоняя удаляемый namespace.
func namespace(m metav1.ObjectMeta, name string) (Namespace, error) {
	p := m.Labels["kodex.dev/profile"]
	if m.Name != name || m.UID == "" || m.CreationTimestamp.IsZero() || m.DeletionTimestamp != nil ||
		m.Labels["app.kubernetes.io/part-of"] != "kodex" || m.Labels["kodex.dev/local-profile"] != "hot-reload" || (p != "web-only" && p != "web-with-mattermost") {
		return Namespace{}, ErrGuard
	}
	return Namespace{Name: name, UID: string(m.UID), Created: m.CreationTimestamp.Time, Profile: p}, nil
}

// Namespaces reads and validates the fixed system/runtime namespace identities through the typed
// client without changing cluster resources.
// Namespaces читает и проверяет фиксированные identity system и runtime namespace через
// типизированный client без изменения ресурсов кластера.
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

// writer checks broker Deployment identity, ownership, service account and local template labels,
// then hashes its spec with replicas removed for pause/restore comparison.
// writer проверяет identity и ownership broker Deployment, service account и local template
// labels, затем хеширует spec без replicas для сравнения при pause и restore.
func writer(d *appsv1.Deployment) (Writer, error) {
	if d.UID == "" || d.ResourceVersion == "" || d.DeletionTimestamp != nil || len(d.OwnerReferences) != 0 || d.Spec.Replicas == nil ||
		d.Spec.Template.Spec.ServiceAccountName != "secret-broker" || d.Spec.Template.Labels["kodex.dev/local-profile"] != "hot-reload" || d.Spec.Template.Labels["kodex.dev/environment"] != "staging" {
		return Writer{}, ErrGuard
	}
	spec := d.Spec.DeepCopy()
	spec.Replicas = nil
	return Writer{UID: string(d.UID), RV: d.ResourceVersion, Replicas: *d.Spec.Replicas, SpecDigest: Digest(spec)}, nil
}

// Snapshot rejects broker-targeting HPA or truncated HPA lists, then reads namespace identities,
// eligible Secret metadata and broker writer under the configured cluster identity.
// Snapshot отклоняет HPA с целью broker и усечённые HPA списки, затем читает namespace identities,
// проверенные Secret metadata и broker writer под заданной identity кластера.
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

// References delegates exact descriptor reference lookup to the required owner callback and fails
// closed when that callback is absent.
// References передаёт поиск references точного descriptor обязательному owner callback и закрыто
// отказывает при отсутствии callback.
func (k *Kubernetes) References(ctx context.Context, d Descriptor) (bool, error) {
	if k.OwnerCheck == nil {
		return false, ErrGuard
	}
	return k.OwnerCheck(ctx, d)
}

// Consumers scans eight runtime Pod/service-account/controller groups without label filters, using
// bounded pages and a per-group item limit while rejecting malformed or immediately repeated
// cursors.
// Consumers сканирует восемь runtime групп Pod, service account и controllers без label фильтров,
// ограничивая страницы и число объектов каждой группы и отклоняя повреждённые либо немедленно
// повторённые cursors.
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

// references recursively compares every JSON string value with the supplied Secret name and UID,
// conservatively matching beyond recognized Secret-reference fields.
// references рекурсивно сравнивает каждое строковое JSON значение с переданными Secret name и UID,
// консервативно находя совпадения и вне известных полей Secret references.
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

// scale guards writer UID/spec and optional planned resource version, patches replicas under
// current UID/version tests and verifies a fresh read; restoration changes only a currently paused
// writer.
// scale проверяет writer UID, spec и при необходимости planned resource version, меняет replicas
// под test текущих UID и version и проверяет свежее чтение; восстановление меняет только
// приостановленный writer.
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

// Pause scales the exact planned writer to zero while requiring its original resource version as
// well as unchanged UID and spec.
// Pause масштабирует точный planned writer до нуля с требованием исходной resource version и
// неизменных UID и spec.
func (k *Kubernetes) Pause(ctx context.Context, w Writer) error { return k.scale(ctx, w, 0, true) }

// Restore restores the planned replica count using current version fencing and unchanged writer
// UID/spec, allowing an already restored count without another patch.
// Restore восстанавливает planned число replicas под текущим version fencing и неизменными writer
// UID и spec, допуская уже восстановленное число без нового patch.
func (k *Kubernetes) Restore(ctx context.Context, w Writer) error {
	return k.scale(ctx, w, w.Replicas, false)
}

// WaitStopped polls until the exact writer remains at zero replicas and no broker-service-account
// Pods remain, rejecting truncated Pod lists and context cancellation.
// WaitStopped ожидает нулевых replicas точного writer и отсутствия Pods broker service account,
// отклоняя усечённые Pod списки и отмену контекста.
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

// Delete sends one exact UID/resource-version deletion and accepts only successful Status or
// matching partial metadata; an accepted response alone does not prove absence.
// Delete отправляет одно удаление с точными UID и resource version и принимает только успешный
// Status либо совпадающие частичные metadata; один принятый ответ ещё не доказывает отсутствие.
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

// Absent first rechecks both prepared namespace identities, then uses the metadata-only read path
// to determine whether the exact name is absent.
// Absent сначала повторно проверяет обе подготовленные namespace identities, затем определяет
// отсутствие точного имени через metadata-only путь чтения.
func (k *Kubernetes) Absent(ctx context.Context, d Descriptor) (bool, error) {
	s, r, err := k.Namespaces(ctx)
	if err != nil || !s.Equal(k.System) || !r.Equal(k.Runtime) {
		return false, ErrGuard
	}
	_, absent, err := k.Metadata(ctx, d.Name)
	return absent, err
}
