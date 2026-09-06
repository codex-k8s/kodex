// Package filetransfer проверяет узкий relative descriptor без выдачи authority.
package filetransfer

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	HeaderTimeout = runtimecontract.MaximumArtifactTransferDuration + 15*time.Second
	TotalTimeout  = 2*runtimecontract.MaximumArtifactTransferDuration + 30*time.Second
)

var reference = regexp.MustCompile(`^[a-z][a-z0-9]*_[A-Za-z0-9_-]{8,80}$`)
var digest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var errDescriptor = errors.New("runtime file descriptor is invalid")

// CatalogRequest проверяет только форму и immutable RuntimeRevision scope.
// Текущие права и exact entry/revision разрешает controller через владельца.
func CatalogRequest(input runtimecontract.RunnerInput, request *http.Request) (string, error) {
	u := request.URL
	if request.Method != http.MethodGet || request.ContentLength > 0 || len(request.TransferEncoding) > 0 || u == nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" || u.RawPath != "" || len(u.RawQuery) > 1024 ||
		input.Mode != runtimecontract.RunnerModeTurn || input.ProjectRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 || input.FileCatalog == nil || input.FileCatalog.Validate() != nil {
		return "", errDescriptor
	}
	prefix := "/v1/executions/" + input.LeaseRef + "/artifacts/"
	if !reference.MatchString(input.LeaseRef) || !strings.HasPrefix(u.Path, prefix) {
		return "", errDescriptor
	}
	artifact := strings.TrimPrefix(u.Path, prefix)
	if !strings.HasPrefix(artifact, "art_") || !reference.MatchString(artifact) {
		return "", errDescriptor
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 4 {
		return "", errDescriptor
	}
	for _, key := range []string{"purpose", "entry_ref", "revision", "digest"} {
		if len(q[key]) != 1 || q.Get(key) == "" {
			return "", errDescriptor
		}
	}
	revision, err := strconv.ParseInt(q.Get("revision"), 10, 64)
	if err != nil || revision < 1 || revision > 9007199254740991 || strconv.FormatInt(revision, 10) != q.Get("revision") || !slices.Contains(input.FileCatalog.Purposes, q.Get("purpose")) || !strings.HasPrefix(q.Get("entry_ref"), "vfe_") || !reference.MatchString(q.Get("entry_ref")) || !digest.MatchString(q.Get("digest")) {
		return "", errDescriptor
	}
	return q.Get("digest"), nil
}

func NewClient(transport *http.Transport) *http.Client {
	fileTransport := transport.Clone()
	fileTransport.ResponseHeaderTimeout = HeaderTimeout
	fileTransport.MaxResponseHeaderBytes = 16 << 10
	return &http.Client{Transport: fileTransport, Timeout: TotalTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
