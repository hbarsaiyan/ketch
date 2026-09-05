package search

import (
	"context"
	"net/http"
	"slices"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// Provider owns the wiring and health policy for one search backend.
type Provider struct {
	ID         string
	Name       string
	Hidden     bool
	Usable     func(*config.Config) bool
	Configured func(*config.Config) bool
	New        func(*config.Config) (Searcher, error)
	Probe      func(context.Context, *http.Client, *config.Config) (health.Status, string)
}

// Required reports whether a failing health check must fail doctor. Selection
// and explicitly configured credentials gate health independently of usability.
func (p Provider) Required(cfg *config.Config) bool {
	return cfg.Backend == p.ID || (p.Configured != nil && p.Configured(cfg))
}

var providers = []Provider{
	braveProvider(),
	ddgProvider(),
	searxngProvider(),
	exaProvider(),
	firecrawlProvider(),
	keenableProvider(),
	tavilyProvider(),
	parallelProvider(),
	serpbaseProvider(),
}

// Providers returns the descriptors in their stable presentation order.
func Providers() []Provider { return slices.Clone(providers) }

// AvailableBackends returns the implemented provider IDs in registry order.
func AvailableBackends() []string {
	var names []string
	for _, p := range providers {
		if !p.Hidden {
			names = append(names, p.ID)
		}
	}
	return names
}

// ProviderNames returns human-readable names in registry order.
func ProviderNames() []string {
	var names []string
	for _, p := range providers {
		if !p.Hidden {
			names = append(names, p.Name)
		}
	}
	return names
}
