// Package managedkubernetes owns shared strict value contracts for the managed Kubernetes boundary.
// Пакет managedkubernetes содержит общие строгие value contracts управляемой Kubernetes boundary.
package managedkubernetes

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ErrInvalidResourceAddress is deliberately independent of resource names and input bytes.
// ErrInvalidResourceAddress намеренно не содержит resource names либо input bytes.
var ErrInvalidResourceAddress = errors.New("invalid managed Kubernetes resource address")

// ResourceAddress is a transport locator resolved from an immutable owner snapshot and discovery.
// It proves neither ownership nor authorization. Owner, gateway and broker use the same validator
// before taking their separate fresh permission, protected-resource and concurrency decisions.
// ResourceAddress — transport locator из immutable owner snapshot и discovery.
// Не доказывает ownership либо authorization. Owner, gateway и broker используют один validator
// до отдельных fresh permission, protected-resource и concurrency решений.
type ResourceAddress struct {
	Group           string `json:"group"`
	Version         string `json:"version"`
	Resource        string `json:"resource"`
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	Subresource     string `json:"subresource"`
	UID             string `json:"uid"`
	ResourceVersion string `json:"resourceVersion"`
	Namespaced      bool   `json:"namespaced"`
	AllNamespaces   bool   `json:"allNamespaces"`
}

// Validate rejects path traversal, implicit namespaces and raw proxy/token/interactive routes.
// Interactive routes require their separately registered UID-aware transport, not this locator.
// Validate запрещает traversal, implicit namespaces и raw proxy/token/interactive routes.
// Interactive routes требуют отдельный registered UID-aware transport, не этот locator.
func (address ResourceAddress) Validate() error {
	if address.AllNamespaces && (!address.Namespaced || address.Namespace != "" || address.Name != "" || address.UID != "" || address.ResourceVersion != "" || address.Subresource != "") {
		return ErrInvalidResourceAddress
	}
	if (address.Group != "" && len(validation.IsDNS1123Subdomain(address.Group)) != 0) ||
		len(validation.IsDNS1035Label(address.Version)) != 0 || len(validation.IsDNS1035Label(address.Resource)) != 0 ||
		(address.Namespaced && !address.AllNamespaces && len(validation.IsDNS1123Label(address.Namespace)) != 0) ||
		(!address.Namespaced && address.Namespace != "") ||
		(address.Name != "" && (!validPathName(address.Name) || address.Name == "." || address.Name == "..")) {
		return ErrInvalidResourceAddress
	}
	if address.Subresource != "" {
		if address.Name == "" {
			return ErrInvalidResourceAddress
		}
		switch address.Subresource {
		case "status", "scale", "log", "eviction", "approval", "binding", "ephemeralcontainers":
		default:
			return ErrInvalidResourceAddress
		}
	}
	if address.UID != "" {
		id, err := uuid.Parse(address.UID)
		if err != nil || id == uuid.Nil || id.String() != address.UID || address.Name == "" {
			return ErrInvalidResourceAddress
		}
	}
	if address.ResourceVersion != "" && (len(address.ResourceVersion) > 256 || !utf8.ValidString(address.ResourceVersion) ||
		strings.ContainsAny(address.ResourceVersion, " \t\r\n\x00") || address.Name == "") {
		return ErrInvalidResourceAddress
	}
	return nil
}

// validPathName accepts bounded Kubernetes names that remain one decoded URL segment.
// Unicode RBAC names are preserved; callers must encode URL paths rather than concatenate raw URLs.
// validPathName принимает bounded Kubernetes names, остающиеся одним decoded URL segment.
// Unicode RBAC names сохраняются; caller кодирует URL path, не соединяет raw URL.
func validPathName(name string) bool {
	if len(name) == 0 || len(name) > 4096 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\%") {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
