package httptransport

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

type OpenAPIContract struct {
	path string
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
	return &OpenAPIContract{path: path}, nil
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
