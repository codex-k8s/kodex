package httptransport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

type OpenAPIContract struct {
	path   string
	router routers.Router
}

func LoadOpenAPIContract(ctx context.Context, specPath string) (*OpenAPIContract, error) {
	path := strings.TrimSpace(specPath)
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load staff-gateway OpenAPI spec: %w", err)
	}
	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("validate staff-gateway OpenAPI spec: %w", err)
	}
	runtimeDoc := *doc
	runtimeDoc.Servers = nil
	router, err := gorillamux.NewRouter(&runtimeDoc)
	if err != nil {
		return nil, fmt.Errorf("build staff-gateway OpenAPI router: %w", err)
	}
	return &OpenAPIContract{path: path, router: router}, nil
}

func (c *OpenAPIContract) Ready() bool {
	return c != nil && strings.TrimSpace(c.path) != ""
}

func (c *OpenAPIContract) Read() ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("staff-gateway OpenAPI spec is not loaded")
	}
	return os.ReadFile(c.path)
}

func (c *OpenAPIContract) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if skipOpenAPIValidation(req.URL.Path) {
			next.ServeHTTP(w, req)
			return
		}
		body, safeErr := bodyForOpenAPIValidation(req)
		if safeErr != nil {
			WriteSafeError(w, req, safeErr)
			return
		}
		validationReq := cloneRequestForOpenAPI(req, body)
		route, pathParams, err := c.router.FindRoute(validationReq)
		if err != nil {
			WriteSafeError(w, req, openAPIRouteError(err))
			return
		}
		input := &openapi3filter.RequestValidationInput{
			Request:    validationReq,
			PathParams: pathParams,
			Route:      route,
			Options: &openapi3filter.Options{
				AuthenticationFunc: allowOpenAPIAuthentication,
				MultiError:         true,
			},
		}
		if err := openapi3filter.ValidateRequest(req.Context(), input); err != nil {
			WriteSafeError(w, req, WrapSafeError(http.StatusBadRequest, CodeInvalidRequest, "request does not match OpenAPI contract", false, err))
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, req)
	})
}

func skipOpenAPIValidation(path string) bool {
	return strings.HasPrefix(path, "/health/") ||
		path == "/metrics" ||
		strings.HasPrefix(path, "/openapi/")
}

func bodyForOpenAPIValidation(req *http.Request) ([]byte, *SafeError) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	defer req.Body.Close()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, WrapSafeError(http.StatusBadRequest, CodeInvalidRequest, "request body is invalid", false, err)
	}
	return body, nil
}

func cloneRequestForOpenAPI(req *http.Request, body []byte) *http.Request {
	validationReq := req.Clone(req.Context())
	validationReq.URL.Scheme = "https"
	if validationReq.URL.Host == "" {
		validationReq.URL.Host = req.Host
	}
	if validationReq.URL.Host == "" {
		validationReq.URL.Host = "staff-gateway"
	}
	validationReq.Body = io.NopCloser(bytes.NewReader(body))
	return validationReq
}

func openAPIRouteError(err error) *SafeError {
	if errors.Is(err, routers.ErrPathNotFound) {
		return NewSafeError(http.StatusNotFound, CodeInvalidRequest, "route is not found", false)
	}
	if errors.Is(err, routers.ErrMethodNotAllowed) {
		return NewSafeError(http.StatusMethodNotAllowed, CodeInvalidRequest, "method is not allowed", false)
	}
	return WrapSafeError(http.StatusBadRequest, CodeInvalidRequest, "route is invalid", false, err)
}

func allowOpenAPIAuthentication(context.Context, *openapi3filter.AuthenticationInput) error {
	return nil
}
