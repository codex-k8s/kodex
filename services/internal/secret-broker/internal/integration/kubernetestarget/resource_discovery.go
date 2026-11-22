package kubernetestarget

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	strictjson "sigs.k8s.io/json"
)

// resourceDiscoveryFailed hides upstream discovery paths, schemas, bodies and transport errors.
// resourceDiscoveryFailed скрывает upstream discovery paths, schemas, bodies и transport errors.
const resourceDiscoveryFailed = "managed Kubernetes resource discovery failed"

// discoverResource uses the selected identity to resolve one exact GVR/subresource and immutable
// OpenAPI group document. Remote discovery URLs are validated as metadata and never executed;
// the actual requests are reconstructed by the closed DiscoveryAddress constructor.
// It grants no resource effect or result credit and does not cache an authorization decision.
// discoverResource выбранной identity разрешает один exact GVR/subresource и immutable OpenAPI
// group document. Remote discovery URLs проверяются как metadata и никогда не исполняются;
// actual requests заново строит закрытый DiscoveryAddress constructor. Resource effect/result
// credit не выдаётся, authorization decision не кэшируется.
func (credential Credential) discoverResource(ctx context.Context, subjectDigest string, address ResourceAddress, operation integrationpackage.KubernetesOperation) (*registeredResourceSchema, error) {
	resources := managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryResources, Group: address.Group, Version: address.Version}
	index := managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryOpenAPIIndex}
	if ctx == nil || address.Validate() != nil || operation.Execution != integrationpackage.KubernetesResource && !(operation.Execution == integrationpackage.KubernetesStream && watchOperation(operation.Key)) ||
		operation.Subresource != address.Subresource || credential.Authorize(ctx, subjectDigest,
		[]AccessRequirement{{Discovery: resources, Verb: "get"}, {Discovery: index, Verb: "get"}}) != nil {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	client, closeIdle, err := credential.NewClient(30 * time.Second)
	if err != nil {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	defer closeIdle()
	request, err := credential.DiscoveryRequest(ctx, resources)
	if err != nil {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	raw, status, err := readResourceHTTPDocument(client, request, resourceResponseBytes)
	if err != nil || status != http.StatusOK {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	defer clear(raw)
	if _, err = decodeResourceJSON(raw, resourceResponseBytes, 64); err != nil {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	var discovered metav1.APIResourceList
	strictErrors, err := strictjson.UnmarshalStrict(raw, &discovered)
	if err != nil || len(strictErrors) != 0 || discovered.Kind != "APIResourceList" || discovered.APIVersion != "v1" ||
		discovered.GroupVersion != resourceGroupVersion(address) || len(discovered.APIResources) > 4096 {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	verb := operation.Verb
	if watchOperation(operation.Key) {
		// Stream handlers own their full RBAC set; watch discovery still requires the native watch verb.
		// Stream handlers владеют полным RBAC set; watch discovery всё равно требует native watch verb.
		verb = "watch"
	}
	registered, err := selectDiscoveredResource(discovered.APIResources, address, verb)
	if err != nil {
		return nil, err
	}
	request, err = credential.DiscoveryRequest(ctx, index)
	if err != nil {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	indexRaw, status, err := readResourceHTTPDocument(client, request, resourceResponseBytes)
	if err != nil || status != http.StatusOK {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	defer clear(indexRaw)
	indexDocument, err := decodeResourceJSON(indexRaw, resourceResponseBytes, 64)
	if err != nil {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	immutableHash, err := resourceSchemaIndexHash(indexDocument, address)
	if err != nil {
		return nil, err
	}
	schemaAddress := managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryOpenAPISchema, Group: address.Group, Version: address.Version, ImmutableSchemaHash: immutableHash}
	if credential.Authorize(ctx, subjectDigest, []AccessRequirement{{Discovery: schemaAddress, Verb: "get"}}) != nil {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	request, err = credential.DiscoveryRequest(ctx, schemaAddress)
	if err != nil {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	schemaRaw, status, err := readResourceHTTPDocument(client, request, resourceSchemaBytes)
	if err != nil || status != http.StatusOK {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	defer clear(schemaRaw)
	result, err := compileRegisteredResourceSchema(schemaRaw, registered, immutableHash)
	if err != nil || ctx.Err() != nil {
		return nil, errors.New(resourceDiscoveryFailed)
	}
	result.unversionedRead = resourceRegistrationReadOnly(discovered.APIResources, address)
	return result, nil
}

// resourceRegistrationReadOnly admits unversioned observations only for an explicitly read-only
// discovered API view. Missing UID/RV on an ordinary writable object remains malformed metadata.
// resourceRegistrationReadOnly допускает unversioned observations только для явно read-only
// discovered API view. Отсутствие UID/RV у ordinary writable object остаётся malformed metadata.
func resourceRegistrationReadOnly(resources []metav1.APIResource, address ResourceAddress) bool {
	name := address.Resource
	if address.Subresource != "" {
		name += "/" + address.Subresource
	}
	for _, resource := range resources {
		if resource.Name != name {
			continue
		}
		for _, verb := range resource.Verbs {
			if verb != "get" && verb != "list" && verb != "watch" {
				return false
			}
		}
		return len(resource.Verbs) != 0
	}
	return false
}

// selectDiscoveredResource binds exact plural/subresource, scope and operation verb to one
// discovery entry. An item's body GVK may differ for a registered subresource such as Scale;
// its RBAC and request address remain the original parent GVR/subresource.
// selectDiscoveredResource связывает exact plural/subresource, scope и operation verb с одной
// discovery entry. Body GVK может отличаться для registered subresource вроде Scale;
// RBAC/request address остаётся исходным parent GVR/subresource.
func selectDiscoveredResource(resources []metav1.APIResource, address ResourceAddress, verb string) (schema.GroupVersionKind, error) {
	name := address.Resource
	if address.Subresource != "" {
		name += "/" + address.Subresource
	}
	var selected *metav1.APIResource
	for index := range resources {
		resource := &resources[index]
		if resource.Name == name {
			if selected != nil {
				return schema.GroupVersionKind{}, errors.New(resourceDiscoveryFailed)
			}
			selected = resource
		}
	}
	if selected == nil || selected.Namespaced != address.Namespaced || selected.Kind == "" || len(selected.Kind) > 512 {
		return schema.GroupVersionKind{}, errors.New(resourceDiscoveryFailed)
	}
	allowed := false
	for _, candidate := range selected.Verbs {
		if candidate == verb {
			allowed = true
		}
	}
	if !allowed {
		return schema.GroupVersionKind{}, errors.New(resourceDiscoveryFailed)
	}
	group, version := address.Group, address.Version
	if selected.Group != "" {
		group = selected.Group
	}
	if selected.Version != "" {
		version = selected.Version
	}
	return schema.GroupVersionKind{Group: group, Version: version, Kind: selected.Kind}, nil
}

// resourceSchemaIndexHash accepts only the selected canonical path and one bounded hex hash.
// Userinfo, external origins, query duplicates, encoded paths and redirect locators are refused.
// resourceSchemaIndexHash допускает только selected canonical path и один bounded hex hash.
// Userinfo, external origins, query duplicates, encoded paths и redirect locators запрещены.
func resourceSchemaIndexHash(document map[string]any, address ResourceAddress) (string, error) {
	paths, ok := document["paths"].(map[string]any)
	if !ok || len(paths) > 4096 {
		return "", errors.New(resourceDiscoveryFailed)
	}
	key := "api/" + address.Version
	if address.Group != "" {
		key = "apis/" + address.Group + "/" + address.Version
	}
	entry, ok := paths[key].(map[string]any)
	if !ok || len(entry) != 1 {
		return "", errors.New(resourceDiscoveryFailed)
	}
	raw, ok := entry["serverRelativeURL"].(string)
	location, err := url.Parse(raw)
	if !ok || err != nil || location.IsAbs() || location.Host != "" || location.User != nil || location.Opaque != "" ||
		location.Fragment != "" || location.RawPath != "" || location.Path != "/openapi/v3/"+key {
		return "", errors.New(resourceDiscoveryFailed)
	}
	query, err := url.ParseQuery(location.RawQuery)
	if err != nil || len(query) != 1 || len(query["hash"]) != 1 {
		return "", errors.New(resourceDiscoveryFailed)
	}
	hash := query.Get("hash")
	probe := managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryOpenAPISchema, Group: address.Group, Version: address.Version, ImmutableSchemaHash: hash}
	if _, err = probe.RelativeURL(); err != nil {
		return "", errors.New(resourceDiscoveryFailed)
	}
	return hash, nil
}

// readResourceHTTPDocument performs one owned HTTP call and bounds the private response.
// It never returns upstream error bodies or redirect URLs; callers clear successful private bytes.
// readResourceHTTPDocument выполняет один owned HTTP call и ограничивает private response.
// Upstream error bodies/redirect URLs не возвращаются; caller очищает successful private bytes.
func readResourceHTTPDocument(client *http.Client, request *http.Request, maximum int) ([]byte, int, error) {
	response, err := client.Do(request)
	if err != nil || response == nil || response.Body == nil {
		return nil, 0, errors.New(resourceDiscoveryFailed)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotFound {
		return nil, response.StatusCode, errors.New(resourceDiscoveryFailed)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") {
		return nil, response.StatusCode, errors.New(resourceDiscoveryFailed)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, int64(maximum)+1))
	if err != nil || len(raw) == 0 || len(raw) > maximum || request.Context().Err() != nil {
		clear(raw)
		return nil, response.StatusCode, errors.New(resourceDiscoveryFailed)
	}
	return raw, response.StatusCode, nil
}

// resourceGroupVersion formats only already validated API coordinates, never a caller URL.
// resourceGroupVersion форматирует только ранее проверенные API coordinates, не caller URL.
func resourceGroupVersion(address ResourceAddress) string {
	if address.Group == "" {
		return address.Version
	}
	return address.Group + "/" + address.Version
}

// cloneResourceObject detaches a validated JSON object's mutable maps before reconstruction.
// It uses the existing bounded JSON boundary rather than a reflective copy or SDK materializer.
// cloneResourceObject отделяет mutable maps validated JSON object до reconstruction.
// Использует существующую bounded JSON boundary, не reflective copy либо SDK materializer.
func cloneResourceObject(object map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(object)
	if err != nil {
		return nil, errors.New(resourceJSONInvalid)
	}
	defer clear(raw)
	return decodeResourceJSON(raw, resourceResponseBytes, 64)
}
