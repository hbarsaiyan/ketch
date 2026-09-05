package code

import (
	"errors"
	"fmt"
	"strings"

	config "github.com/1broseidon/ketch/internal/configbase"
)

// ErrUnknownBackend reports a backend name that is not a known code search
// backend. Callers classify errors wrapping it as validation failures; any
// other NewFromConfig error is a missing precondition (e.g. token).
var ErrUnknownBackend = errors.New("unknown code backend")

// NewFromConfig builds the code Searcher for backend, resolving tokens and
// instance URLs from cfg exactly as the `ketch code` CLI does. Both the CLI
// and the MCP server call this — it is the single owner of the backend switch.
func NewFromConfig(cfg *config.Config, backend string) (Searcher, error) {
	switch backend {
	case "sourcegraph":
		return NewSourcegraph(cfg.String("sourcegraph_url")), nil
	case "grepapp":
		return NewGrepApp(), nil
	case "github":
		token, _ := cfg.ResolveGithubToken()
		if token == "" {
			return nil, fmt.Errorf(`github code search: no token found.
  - explicit:   ketch config set github_token <token>
  - env var:    export GITHUB_TOKEN=<token>
  - or run:     gh auth login`)
		}
		return NewGitHub(token), nil
	default:
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownBackend, backend, strings.Join(AvailableBackends(), ", "))
	}
}
