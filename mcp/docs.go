package mcp

import (
	"context"

	"github.com/1broseidon/ketch/docs"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DocsInput is the input schema for the "docs" tool.
type DocsInput struct {
	Query   string `json:"query" jsonschema:"the docs search query, or a library name when resolve is true"`
	Backend string `json:"backend,omitempty" jsonschema:"docs backend (default: the configured backend); context7 is the only implemented backend"`
	Library string `json:"library,omitempty" jsonschema:"Context7 library ID to fetch docs from directly, skipping the resolve step; requires the context7 backend"`
	Tokens  int    `json:"tokens,omitempty" jsonschema:"Context7 token budget when library is set (default 4000)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max number of results (default: the configured limit)"`
	Resolve bool   `json:"resolve,omitempty" jsonschema:"resolve a library name to Context7 library IDs instead of searching docs"`
}

// DocsOutput is the output schema for the "docs" tool. Results is populated
// for a normal or library-scoped search; Matches is populated when Resolve
// is set. Exactly one of the two is non-empty for a given call. The result
// objects match the CLI's `ketch docs --json` (which emits a bare array; MCP
// structured content needs the object wrapper).
type DocsOutput struct {
	Results []docs.Result       `json:"results,omitempty"`
	Matches []docs.LibraryMatch `json:"matches,omitempty"`
}

func (s *Server) registerDocsTool() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name: "docs",
		Description: "Search library documentation using Context7. Supports resolving a library name to a Context7 library ID, and fetching docs directly from a known library ID." +
			errTaxonomy,
		Annotations: readOnlyOpenWorld(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in DocsInput) (*mcpsdk.CallToolResult, DocsOutput, error) {
		if in.Query == "" {
			return nil, DocsOutput{}, errf(kindValidation, "query is required")
		}
		backend := in.Backend
		if backend == "" {
			backend = s.cfg.DocsBackend
		}
		limit := in.Limit
		if limit <= 0 {
			limit = s.cfg.Limit
		}
		tokens := in.Tokens
		if tokens <= 0 {
			tokens = 4000
		}

		if in.Resolve {
			return s.docsResolve(ctx, in.Query, limit)
		}

		if in.Library != "" {
			// library is a Context7 concept; with any other backend it would
			// previously be dropped silently and the query re-routed. Reject
			// loudly instead — same rule as the CLI.
			if backend != "context7" {
				return nil, DocsOutput{}, errf(kindValidation, "library requires the context7 backend (got %q)", backend)
			}
			return s.docsForLibrary(ctx, in.Query, in.Library, tokens)
		}

		searcher, err := docs.NewFromConfig(s.cfg, backend)
		if err != nil {
			return nil, DocsOutput{}, backendErrf(err, docs.ErrUnknownBackend)
		}

		results, err := searcher.Search(ctx, in.Query, limit)
		if err != nil {
			return nil, DocsOutput{}, upstreamErrf(err, "docs search failed")
		}

		return nil, DocsOutput{Results: results}, nil
	})
}

// docsResolve maps a free-form library name to Context7 library IDs,
// returning at most limit matches.
func (s *Server) docsResolve(ctx context.Context, query string, limit int) (*mcpsdk.CallToolResult, DocsOutput, error) {
	c7, err := s.context7()
	if err != nil {
		return nil, DocsOutput{}, err
	}
	matches, err := c7.ResolveLibrary(ctx, query, limit)
	if err != nil {
		return nil, DocsOutput{}, upstreamErrf(err, "resolve failed")
	}
	return nil, DocsOutput{Matches: matches}, nil
}

// docsForLibrary fetches docs for a known Context7 library ID.
func (s *Server) docsForLibrary(ctx context.Context, query, library string, tokens int) (*mcpsdk.CallToolResult, DocsOutput, error) {
	c7, err := s.context7()
	if err != nil {
		return nil, DocsOutput{}, err
	}
	results, err := c7.GetDocs(ctx, library, query, tokens)
	if err != nil {
		return nil, DocsOutput{}, upstreamErrf(err, "docs fetch failed")
	}
	return nil, DocsOutput{Results: results}, nil
}

// context7 builds the Context7 client, classifying a missing API key as a
// precondition failure.
func (s *Server) context7() (*docs.Context7, error) {
	if s.cfg.String("context7_api_key") == "" {
		return nil, errf(kindPrecondition, "context7: API key not set (set with: ketch config set context7_api_key <key>)")
	}
	return docs.NewContext7(s.cfg.String("context7_api_key")), nil
}
