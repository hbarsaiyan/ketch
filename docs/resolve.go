package docs

import "context"

// Resolution reports how a bare-query search chose its library: the ID it
// fetched from and the resolver's other candidates in rank order. An agent
// that was handed the wrong library retries with --library instead of
// guessing at names.
type Resolution struct {
	Library    string         `json:"library"`
	Candidates []LibraryMatch `json:"candidates,omitempty"`
}

// Resolving is the optional capability of a docs provider whose bare-query
// search first chooses a library. Consumers assert it, as they do
// LibraryResolver, so the choice reaches the output.
type Resolving interface {
	SearchResolved(ctx context.Context, query string, limit int) ([]Result, *Resolution, error)
}

// Query runs a bare query through s and reports the library resolution when
// the provider makes one. Providers without that capability search directly.
func Query(ctx context.Context, s Searcher, query string, limit int) ([]Result, *Resolution, error) {
	if r, ok := s.(Resolving); ok {
		return r.SearchResolved(ctx, query, limit)
	}
	results, err := s.Search(ctx, query, limit)
	return results, nil, err
}
