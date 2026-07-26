// Package data holds the generated service catalog dataset embedded into the
// server binary. Regenerate it with internal/servicecatalog/gen; never edit
// services.tsv by hand.
package data

import _ "embed"

// ServicesTSV is the pruned v2fly/domain-list-community snapshot parsed by
// internal/servicecatalog.
//
//go:embed services.tsv
var ServicesTSV string
