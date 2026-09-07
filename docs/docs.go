package docs

import (
	"context"
	"errors"
)

// ErrNotFound reports a docs resource that is permanently absent upstream —
// typically a Context7 library ID that resolves to a 404. It is detected in
// this package (never by string matching at the surfaces) so both the CLI and
// the MCP server classify it as not-found (exit 3 / [not_found]) rather than
// a retryable upstream failure.
var ErrNotFound = errors.New("not found")

// ErrScopeRequired reports a query the provider cannot run without a library
// scope (Read the Docs answers an unscoped query with nothing). Both surfaces
// classify it as a validation failure: the caller must add --library or the
// provider's own scope syntax, and retrying unchanged will not help.
var ErrScopeRequired = errors.New("a library scope is required")

// Result is a single docs search result.
type Result struct {
	Library    string `json:"library"`
	Version    string `json:"version,omitempty"`
	Title      string `json:"title"`
	Breadcrumb string `json:"breadcrumb,omitempty"`
	Snippet    string `json:"snippet"`
	URL        string `json:"url"`
	Source     string `json:"source"` // "context7" | "local"
}

// Searcher is the interface for docs backends.
type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}
