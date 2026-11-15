// Package examples embeds the sample project that `blast init` writes out, so
// the files in this directory are the only copy.
package examples

import "embed"

// Acme is the ten-customer SaaS used throughout the documentation.
//
//go:embed acme
var Acme embed.FS
