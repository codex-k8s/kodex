package kubernetes

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

// sshIdentityHTTPResponse keeps only the fixed protocol envelope and a bounded private body; callers own clearing the body regardless of status or error.
// sshIdentityHTTPResponse сохраняет только fixed protocol envelope и bounded private body; caller обязан очистить body независимо от status либо error.
type sshIdentityHTTPResponse struct {
	status int
	json   bool
	body   []byte
}

// sshIdentityHTTP executes one purpose request over the prevalidated SDK client's existing TLS/auth transport without SDK body logging, response error formatting, replay buffers or application retries; it takes exclusive ownership of wire even on rejection.
// sshIdentityHTTP выполняет один purpose request через existing TLS/auth transport проверенного SDK client без SDK body logging, response error formatting, replay buffers либо application retries; exclusive ownership wire передаётся даже при отказе.
func (store *Store) sshIdentityHTTP(ctx context.Context, client *rest.RESTClient, method, name string, wire []byte) (sshIdentityHTTPResponse, error) {
	body := newPrivateIdentityRequestBody(wire)
	defer body.Close()
	if ctx == nil || ctx.Err() != nil || client == nil || client.Client == nil || client.Client.Transport == nil {
		return sshIdentityHTTPResponse{}, sshrepository.ErrIdentityRejected
	}
	operation, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var request *rest.Request
	switch method {
	case http.MethodPost:
		if name != "" {
			return sshIdentityHTTPResponse{}, sshrepository.ErrIdentityRejected
		}
		options := metav1.CreateOptions{FieldManager: "kodex-secret-broker-ssh-identity"}
		request = client.Post().Namespace(store.namespace).Resource("secrets").VersionedParams(&options, scheme.ParameterCodec)
	case http.MethodGet:
		if name == "" || len(wire) != 0 {
			return sshIdentityHTTPResponse{}, sshrepository.ErrIdentityRejected
		}
		request = client.Get().Namespace(store.namespace).Resource("secrets").Name(name)
	case http.MethodDelete:
		if name == "" {
			return sshIdentityHTTPResponse{}, sshrepository.ErrIdentityRejected
		}
		request = client.Delete().Namespace(store.namespace).Resource("secrets").Name(name)
	default:
		return sshIdentityHTTPResponse{}, sshrepository.ErrIdentityRejected
	}
	if limiter := client.GetRateLimiter(); limiter != nil {
		if err := limiter.Wait(operation); err != nil {
			return sshIdentityHTTPResponse{}, sshrepository.ErrIdentityRejected
		}
	}
	return sshIdentitySendHTTP(operation, client, method, request.URL(), body, len(wire))
}

// sshIdentitySendHTTP executes only SDK-built private URLs and borrows the caller-owned synchronized body; it preserves the existing single-attempt, redirect-denying, bounded response contract for material and namespace reads.
// sshIdentitySendHTTP выполняет лишь SDK-built private URLs и заимствует caller-owned synchronized body; сохраняет single-attempt, redirect-denying и bounded response contract для material и namespace reads.
func sshIdentitySendHTTP(operation context.Context, client *rest.RESTClient, method string, target *url.URL, body *privateIdentityRequestBody, length int) (sshIdentityHTTPResponse, error) {
	var input io.Reader
	if length > 0 {
		input = body
	}
	outbound, err := http.NewRequestWithContext(operation, method, target.String(), input)
	if err != nil {
		return sshIdentityHTTPResponse{}, sshrepository.ErrIdentityRejected
	}
	outbound.GetBody = nil
	outbound.ContentLength = int64(length)
	outbound.Header.Set("Accept", "application/json")
	if input != nil {
		outbound.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Client.Do(outbound)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil || response == nil || response.Body == nil {
		return sshIdentityHTTPResponse{}, sshrepository.ErrIdentityUnknown
	}
	acknowledged, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil {
		return sshIdentityHTTPResponse{status: response.StatusCode, body: acknowledged}, sshrepository.ErrIdentityUnknown
	}
	contentType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	return sshIdentityHTTPResponse{status: response.StatusCode, json: mediaErr == nil && contentType == "application/json", body: acknowledged}, nil
}
