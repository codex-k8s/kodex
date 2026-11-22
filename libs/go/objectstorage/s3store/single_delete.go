package s3store

import (
	"context"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// exactDeleteHTTPClient derives a redirect-closed view of the resolved HTTP client, sharing its
// existing transport, TLS policy and connection pool. Unknown client implementations remain
// usable for ordinary operations but cannot advertise one physical DELETE attempt.
// exactDeleteHTTPClient создаёт redirect-closed view resolved HTTP client, разделяя existing
// transport, TLS policy и connection pool. Unknown client implementations доступны ordinary
// operations, но не подтверждают одну physical DELETE attempt.
func exactDeleteHTTPClient(client aws.HTTPClient) (aws.HTTPClient, aws.HTTPClient) {
	trustedSDKTransport := false
	if buildable, ok := client.(*awshttp.BuildableClient); ok {
		client = buildable.Freeze()
		trustedSDKTransport = true
	}
	concrete, ok := client.(*http.Client)
	if !ok {
		return client, nil
	}
	if !trustedSDKTransport && concrete.Transport != nil {
		if _, ok := concrete.Transport.(*http.Transport); !ok {
			return client, nil
		}
	}
	exact := *concrete
	exact.CheckRedirect = rejectExactDeleteRedirect
	return client, &exactDeleteClient{client: &exact}
}

// exactDeleteClient prevents the HTTP/2 transport from silently retrying a bodyless DELETE after
// REFUSED_STREAM or GOAWAY. The request remains an empty entity; its body cannot be reset.
// exactDeleteClient запрещает HTTP/2 transport незаметно повторять bodyless DELETE после
// REFUSED_STREAM либо GOAWAY. Entity остаётся пустой; её body невозможно сбросить для replay.
type exactDeleteClient struct{ client *http.Client }

// Do accepts only the SDK's empty DELETE and clones its request without changing signed headers.
// The same configured client/transport handles IO; ordinary operations never use this view.
// Do принимает только empty DELETE от SDK и копирует request без изменения signed headers.
// IO выполняет тот же configured client/transport; ordinary operations не используют этот view.
func (client *exactDeleteClient) Do(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodDelete || request.ContentLength != 0 ||
		(request.Body != nil && request.Body != http.NoBody) {
		return nil, objectstorage.ErrUnavailable
	}
	clone := request.Clone(request.Context())
	clone.Body = exactDeleteEmptyBody{}
	clone.GetBody = nil
	return client.client.Do(clone)
}

// exactDeleteEmptyBody is immediately exhausted, but deliberately differs from nil/http.NoBody.
// exactDeleteEmptyBody немедленно исчерпана, но намеренно отличается от nil/http.NoBody.
type exactDeleteEmptyBody struct{}

// Read returns EOF without bytes, blocking or an alternate source of payload.
// Read возвращает EOF без bytes, блокировки либо другого source payload.
func (exactDeleteEmptyBody) Read([]byte) (int, error) { return 0, io.EOF }

// Close owns no resource and does not turn the body into a replayable sentinel.
// Close не владеет ресурсом и не превращает body в replayable sentinel.
func (exactDeleteEmptyBody) Close() error { return nil }

// rejectExactDeleteRedirect returns the first response without another physical request.
// rejectExactDeleteRedirect возвращает первый response без дополнительного physical request.
func rejectExactDeleteRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

// CheckDeleteOnce refuses unsupported HTTP clients before an owner consumes durable admission.
// CheckDeleteOnce отклоняет unsupported HTTP clients до расходования owner durable admission.
func (store *Store) CheckDeleteOnce(ctx context.Context) error {
	if ctx.Err() != nil || store == nil || store.singleDeleteHTTP == nil {
		return objectstorage.ErrUnavailable
	}
	return nil
}
