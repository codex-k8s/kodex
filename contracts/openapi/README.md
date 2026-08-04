# OpenAPI

Размещайте внешние HTTP API по пути
`contracts/openapi/<gateway>/v<version>/openapi.yaml`.

Owner API `control-api-gateway` находится в
[`control-api-gateway/v1/openapi.yaml`](control-api-gateway/v1/openapi.yaml) и
генерирует Go server/models через `make gen-control-api-gateway-openapi-go`.
