// Package migrations embeds the goose SQL migrations so the API binary can apply them
// (`api migrate up`) without the goose CLI being installed in the container.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
