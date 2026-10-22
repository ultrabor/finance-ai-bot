package httpapi

// Типы запросов и ответов генерируются из api/openapi.yaml: make generate.
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config oapi-codegen.yaml ../../../api/openapi.yaml
