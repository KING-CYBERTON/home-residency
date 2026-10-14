// Package gen holds the server interface and models generated from api/openapi.yaml.
// Regenerate with `make generate`; CI fails if the committed output is stale.
package gen

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 -config ../../../oapi-codegen.yaml ../../../api/openapi.yaml
