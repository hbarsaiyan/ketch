package docs

import (
	"errors"
	"fmt"
	"strings"

	config "github.com/1broseidon/ketch/internal/configbase"
)

// ErrUnknownBackend reports a backend name that is not a known docs backend.
// Callers classify errors wrapping it as validation failures; any other
// NewFromConfig error is a missing precondition (API key, unimplemented
// backend).
var ErrUnknownBackend = errors.New("unknown docs backend")

// NewFromConfig builds the docs Searcher for backend, resolving API keys from
// cfg exactly as the `ketch docs` CLI does. Both the CLI and the MCP server
// call this — it is the single owner of the backend switch.
//
// The "local" FTS5 backend is a stub whose Search always errors, so it is
// rejected here — at construction time — instead of failing on first use.
func NewFromConfig(cfg *config.Config, backend string) (Searcher, error) {
	switch backend {
	case "context7":
		if cfg.Context7APIKey == "" {
			return nil, fmt.Errorf("context7: API key not set (get one then: ketch config set context7_api_key <key>)")
		}
		return NewContext7(cfg.Context7APIKey), nil
	case "local":
		// Recognized but planned-only: kept out of config.AvailableDocBackends
		// so no surface advertises it, yet rejected as a precondition (not an
		// unknown backend) because the name is reserved for the FTS5 stub.
		return nil, fmt.Errorf("docs backend %q not yet implemented (use context7)", backend)
	default:
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownBackend, backend, strings.Join(AvailableBackends(), ", "))
	}
}
