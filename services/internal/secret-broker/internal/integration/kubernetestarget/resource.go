package kubernetestarget

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"net/http"
	"net/url"
	"strings"
)

// ResourceAddress is a transport locator resolved from an immutable owner snapshot and API discovery.
// It proves neither resource ownership nor authorization; callers must obtain a fresh owner permit
// and verify the selected Kubernetes identity before sending the constructed request.
// ResourceAddress — transport locator из immutable owner snapshot и API discovery.
// Не доказывает владение или полномочия; перед отправкой нужны свежий owner permit
// и проверка выбранной Kubernetes identity.
type ResourceAddress = managedkubernetes.ResourceAddress

// ReadRequest constructs a bodyless exact-origin GET without adding credentials or accepting raw URLs.
// Namespaced and cluster-wide resources are explicit; a collection has no implicit all-namespace mode.
// ReadRequest создаёт bodyless exact-origin GET без credentials и без caller URL.
// Namespaced и cluster-wide ресурсы явные; collection не получает implicit all-namespace mode.
func (credential Credential) ReadRequest(ctx context.Context, address ResourceAddress) (*http.Request, error) {
	if ctx == nil || validateTarget(credential.target) != nil || address.Validate() != nil {
		return nil, managedkubernetes.ErrInvalidResourceAddress
	}
	base, err := url.Parse(credential.target.ServerURL)
	if err != nil {
		return nil, managedkubernetes.ErrInvalidResourceAddress
	}
	parts := []string{"api", address.Version}
	if address.Group != "" {
		parts = []string{"apis", address.Group, address.Version}
	}
	if address.Namespaced && !address.AllNamespaces {
		parts = append(parts, "namespaces", address.Namespace)
	}
	parts = append(parts, address.Resource)
	if address.Name != "" {
		parts = append(parts, address.Name)
	}
	if address.Subresource != "" {
		parts = append(parts, address.Subresource)
	}
	base.Path = "/" + strings.Join(parts, "/")
	base.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, managedkubernetes.ErrInvalidResourceAddress
	}
	request.Header.Set("Accept", "application/json")
	return request, nil
}

// DiscoveryRequest constructs a registered non-resource GET on the selected private target.
// It does not follow serverRelativeURL, attach credentials or replace non-resource SSAR/owner admission.
// DiscoveryRequest строит registered non-resource GET выбранного private target.
// Не следует serverRelativeURL, не добавляет credentials и не заменяет non-resource SSAR/owner admission.
func (credential Credential) DiscoveryRequest(ctx context.Context, address managedkubernetes.DiscoveryAddress) (*http.Request, error) {
	if ctx == nil || validateTarget(credential.target) != nil {
		return nil, managedkubernetes.ErrInvalidResourceAddress
	}
	relative, err := address.RelativeURL()
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(credential.target.ServerURL)
	if err != nil {
		return nil, managedkubernetes.ErrInvalidResourceAddress
	}
	base.Path, base.RawPath, base.RawQuery = relative.Path, "", relative.RawQuery
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, managedkubernetes.ErrInvalidResourceAddress
	}
	request.Header.Set("Accept", "application/json")
	return request, nil
}
