// Package apispec embeds the OpenAPI document so the server can serve it and derive
// route permissions (x-roles) from it at startup.
package apispec

import _ "embed"

//go:embed openapi.yaml
var YAML []byte
