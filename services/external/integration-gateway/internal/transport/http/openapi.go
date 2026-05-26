package httptransport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// OpenAPIValidator validates incoming requests against the public OpenAPI contract.
type OpenAPIValidator struct {
	router routers.Router
}

// NewOpenAPIValidator loads and validates the OpenAPI document.
func NewOpenAPIValidator(ctx context.Context, specPath string) (*OpenAPIValidator, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(strings.TrimSpace(specPath))
	if err != nil {
		return nil, fmt.Errorf("load integration-gateway OpenAPI spec: %w", err)
	}
	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("validate integration-gateway OpenAPI spec: %w", err)
	}
	runtimeDoc := *doc
	runtimeDoc.Servers = nil
	router, err := gorillamux.NewRouter(&runtimeDoc)
	if err != nil {
		return nil, fmt.Errorf("build integration-gateway OpenAPI router: %w", err)
	}
	return &OpenAPIValidator{router: router}, nil
}

// Middleware validates only public API routes and leaves service endpoints to their own handlers.
func (v *OpenAPIValidator) Middleware(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		if skipOpenAPIValidation(req.URL.Path) {
			next.ServeHTTP(w, req)
			return
		}
		body := requestBodyFromContext(req.Context())
		validationReq := requestForValidation(req, body)
		route, pathParams, err := v.router.FindRoute(validationReq)
		if err != nil {
			WriteSafeError(w, req, openAPIRouteError(err))
			return
		}
		input := &openapi3filter.RequestValidationInput{
			Request:    validationReq,
			PathParams: pathParams,
			Route:      route,
			Options: &openapi3filter.Options{
				AuthenticationFunc: noopAuthentication,
				MultiError:         true,
			},
		}
		if err := openapi3filter.ValidateRequest(req.Context(), input); err != nil {
			WriteSafeError(w, req, WrapSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "request does not match OpenAPI contract", false, err))
			return
		}
		req.Body = ioNopCloser(body)
		next.ServeHTTP(w, req)
	})
}

func skipOpenAPIValidation(path string) bool {
	return strings.HasPrefix(path, "/health/") ||
		path == "/metrics" ||
		strings.HasPrefix(path, "/openapi/")
}

func openAPIRouteError(err error) *SafeError {
	switch {
	case errors.Is(err, routers.ErrPathNotFound):
		return NewSafeError(stdhttp.StatusNotFound, CodeInvalidRequest, "route is not found", false)
	case errors.Is(err, routers.ErrMethodNotAllowed):
		return NewSafeError(stdhttp.StatusMethodNotAllowed, CodeInvalidRequest, "method is not allowed", false)
	default:
		return WrapSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "route is invalid", false, err)
	}
}

func noopAuthentication(context.Context, *openapi3filter.AuthenticationInput) error {
	return nil
}

func requestForValidation(req *stdhttp.Request, body []byte) *stdhttp.Request {
	validationReq := req.Clone(req.Context())
	validationReq.URL.Scheme = "https"
	if validationReq.URL.Host == "" {
		validationReq.URL.Host = req.Host
	}
	if validationReq.URL.Host == "" {
		validationReq.URL.Host = "integration.example.local"
	}
	validationReq.Body = ioNopCloser(body)
	return validationReq
}

func ioNopCloser(body []byte) *nopReadCloser {
	return &nopReadCloser{Reader: bytes.NewReader(body)}
}

type nopReadCloser struct {
	*bytes.Reader
}

func (n *nopReadCloser) Close() error {
	return nil
}
