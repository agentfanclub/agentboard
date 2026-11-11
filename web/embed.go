// Package web holds the board's templates and static files, embedded into the
// binary so it runs from any directory.
package web

import "embed"

//go:embed static templates
var FS embed.FS
