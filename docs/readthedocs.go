package docs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

const (
	readTheDocsDefaultURL = "https://app.readthedocs.org"
	// readTheDocsPageSize is the search API's default and maximum page size.
	readTheDocsPageSize = 50
	// readTheDocsMaxBody bounds a response; a full page of results with whole
	// section texts fits comfortably.
	readTheDocsMaxBody = 4 << 20
	// readTheDocsSnippetMax caps one section's text. Sections are complete
	// documentation passages, so this is longer than a web-search snippet.
	readTheDocsSnippetMax = 1000
)

// ReadTheDocs searches documentation hosted on Read the Docs through its
// server-side search API. Every result is a section of one project's pages,
// scoped with Read the Docs' own project:<slug>[/<version>] query filter.
type ReadTheDocs struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewReadTheDocs creates a Read the Docs backend for one instance. token is
// optional: it unlocks private projects on Business instances and lifts the
// anonymous rate limit.
func NewReadTheDocs(baseURL, token string) *ReadTheDocs {
	return &ReadTheDocs{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		client:  httpx.Default(),
	}
}

// Search sends query to the search API as written. Read the Docs answers a
// query that names no project with an empty page, so that case fails fast
// with the syntax instead.
func (r *ReadTheDocs) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if !strings.Contains(query, "project:") {
		return nil, fmt.Errorf("readthedocs: %w — ketch docs \"project:<slug> %s\" or --library <slug>", ErrScopeRequired, query)
	}
	return r.search(ctx, query, limit, 0)
}

// GetDocs searches within one project. library is a project slug, optionally
// followed by /<version>. tokens bounds the total snippet text at roughly
// four characters per token, matching the Context7 budget.
func (r *ReadTheDocs) GetDocs(ctx context.Context, library, query string, tokens int) ([]Result, error) {
	library = strings.Trim(strings.TrimSpace(library), "/")
	if library == "" {
		return nil, fmt.Errorf("readthedocs: %w — library must be a project slug, optionally slug/version", ErrScopeRequired)
	}
	return r.search(ctx, "project:"+library+" "+query, 0, tokens*4)
}

var readTheDocsSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ResolveLibrary looks up one project by its exact slug. Read the Docs has no
// public project search, so the name is normalised to slug form (lower case,
// spaces and underscores to hyphens) and fetched directly; a name that cannot
// be a slug, such as a query carrying filters, yields no matches without a
// request. A slug/version suffix is kept in the match ID.
func (r *ReadTheDocs) ResolveLibrary(ctx context.Context, name string, _ int) ([]LibraryMatch, error) {
	slug, version, _ := strings.Cut(normalizeSlug(name), "/")
	if !readTheDocsSlug.MatchString(slug) || (version != "" && !readTheDocsSlug.MatchString(version)) {
		return nil, nil
	}
	req, err := r.newRequest(ctx, fmt.Sprintf("%s/api/v3/projects/%s/?fields=slug,name,default_version,urls", r.baseURL, url.PathEscape(slug)))
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("readthedocs project request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if err := readTheDocsStatus(resp.StatusCode); err != nil {
		return nil, err
	}
	var project struct {
		Slug           string `json:"slug"`
		Name           string `json:"name"`
		DefaultVersion string `json:"default_version"`
		URLs           struct {
			Documentation string `json:"documentation"`
		} `json:"urls"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, readTheDocsMaxBody)).Decode(&project); err != nil {
		return nil, fmt.Errorf("failed to decode readthedocs project response: %w", err)
	}
	if project.Slug == "" {
		return nil, nil
	}
	match := LibraryMatch{ID: project.Slug, Title: project.Name, Description: project.URLs.Documentation, Versions: []string{project.DefaultVersion}}
	if version != "" {
		match.ID += "/" + version
		match.Versions = []string{version}
	}
	return []LibraryMatch{match}, nil
}

// normalizeSlug turns a human library name into Read the Docs slug form.
func normalizeSlug(name string) string {
	name = strings.NewReplacer(" ", "-", "_", "-").Replace(strings.ToLower(strings.TrimSpace(name)))
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return strings.Trim(name, "-")
}

type readTheDocsSearchResponse struct {
	Results []readTheDocsPage `json:"results"`
}

type readTheDocsPage struct {
	Project struct {
		Slug string `json:"slug"`
	} `json:"project"`
	Version struct {
		Slug string `json:"slug"`
	} `json:"version"`
	Title  string             `json:"title"`
	Domain string             `json:"domain"`
	Path   string             `json:"path"`
	Blocks []readTheDocsBlock `json:"blocks"`
}

// readTheDocsBlock is a page section (title, id) or a Sphinx domain object
// such as a function or class (name, id). Both carry plain-text content.
type readTheDocsBlock struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// results maps a page to one Result per block, or one for the page itself
// when the API attached no blocks.
func (p readTheDocsPage) results() []Result {
	base := Result{Library: p.Project.Slug, Version: p.Version.Slug, Title: p.Title, URL: p.Domain + p.Path, Source: "readthedocs"}
	if len(p.Blocks) == 0 {
		return []Result{base}
	}
	out := make([]Result, 0, len(p.Blocks))
	for _, b := range p.Blocks {
		res := base
		title := b.Title
		if title == "" {
			title = b.Name
		}
		if title != "" && title != p.Title {
			res.Title = title
			res.Breadcrumb = p.Title + " › " + title
		}
		res.Snippet = collapseSnippet(b.Content, readTheDocsSnippetMax)
		if b.ID != "" {
			res.URL += "#" + b.ID
		}
		out = append(out, res)
	}
	return out
}

// collapseSnippet flattens whitespace and caps the text at max runes.
func collapseSnippet(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > max {
		return strings.TrimSpace(string(runes[:max])) + "…"
	}
	return s
}

// search runs one query. limit bounds the result count and charBudget the
// total snippet text; either is ignored when zero, and the first result is
// always kept so a budget never yields an empty page.
func (r *ReadTheDocs) search(ctx context.Context, q string, limit, charBudget int) ([]Result, error) {
	pageSize := readTheDocsPageSize
	if limit > 0 && limit < pageSize {
		pageSize = limit
	}
	req, err := r.newRequest(ctx, fmt.Sprintf("%s/api/v3/search/?q=%s&page_size=%d", r.baseURL, url.QueryEscape(q), pageSize))
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("readthedocs request failed: %w", err)
	}
	defer resp.Body.Close()
	if err := readTheDocsStatus(resp.StatusCode); err != nil {
		return nil, err
	}
	var body readTheDocsSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, readTheDocsMaxBody)).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to decode readthedocs response: %w", err)
	}

	var results []Result
	used := 0
	for _, page := range body.Results {
		for _, res := range page.results() {
			if limit > 0 && len(results) >= limit {
				return results, nil
			}
			if charBudget > 0 && len(results) > 0 && used+len(res.Snippet) > charBudget {
				return results, nil
			}
			used += len(res.Snippet)
			results = append(results, res)
		}
	}
	return results, nil
}

func (r *ReadTheDocs) newRequest(ctx context.Context, u string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if r.token != "" {
		req.Header.Set("Authorization", "Token "+r.token)
	}
	return req, nil
}

// readTheDocsStatus maps a failing status to an error carrying the fix.
func readTheDocsStatus(code int) error {
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return errors.New("readthedocs: API token rejected (ketch config set readthedocs_api_token <token>)")
	case http.StatusForbidden:
		return errors.New("readthedocs: request blocked (HTTP 403) — authenticate with ketch config set readthedocs_api_token <token>")
	case http.StatusTooManyRequests:
		return errors.New("readthedocs: rate limited (HTTP 429) — a token lifts the limit: ketch config set readthedocs_api_token <token>")
	default:
		return fmt.Errorf("readthedocs returned status %d", code)
	}
}

// ProbeReadTheDocs checks that the instance answers the search API with JSON.
// It searches Read the Docs' own documentation project because a probe that
// names no project is answered with an empty page by any host.
func ProbeReadTheDocs(ctx context.Context, client *http.Client, baseURL, token string) (health.Status, string) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return health.StatusMisconfigured, "readthedocs_url not set (ketch config set readthedocs_url " + readTheDocsDefaultURL + ")"
	}
	headers := map[string]string{"Accept": "application/json"}
	if token != "" {
		headers["Authorization"] = "Token " + token
	}
	resp, err := health.Get(ctx, client, baseURL+"/api/v3/search/?q=project:docs+search&page_size=1", headers)
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		var body struct {
			Results []json.RawMessage `json:"results"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
			return health.StatusMisconfigured, fmt.Sprintf("returned non-JSON response — is %s a Read the Docs instance?", baseURL)
		}
		return health.StatusOK, ""
	case http.StatusUnauthorized:
		return health.StatusMisconfigured, "API token rejected (ketch config set readthedocs_api_token <token>)"
	case http.StatusForbidden:
		return health.StatusMisconfigured, "request blocked (HTTP 403) — set readthedocs_api_token to authenticate"
	case http.StatusTooManyRequests:
		return health.StatusMisconfigured, "rate limited (HTTP 429) — set readthedocs_api_token to lift the limit"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

func readTheDocsProvider() Provider {
	instance := config.Scalar("readthedocs_url")
	instance.Default = readTheDocsDefaultURL
	token := config.Scalar("readthedocs_api_token")
	token.Secret = true
	token.GateDoctor = true // an explicitly configured token must pass doctor
	setup := "readthedocs: instance URL not set (ketch config set readthedocs_url " + readTheDocsDefaultURL + ")"
	return Provider{
		ID:           "readthedocs",
		Name:         "Read the Docs",
		Setup:        setup,
		LibrarySetup: setup,
		Settings:     []config.Setting{instance, token},
		Usable:       func(c *config.Config) bool { return strings.TrimSpace(c.String("readthedocs_url")) != "" },
		New: func(c *config.Config) (Searcher, error) {
			return NewReadTheDocs(c.String("readthedocs_url"), c.String("readthedocs_api_token")), nil
		},
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return ProbeReadTheDocs(ctx, client, c.String("readthedocs_url"), c.String("readthedocs_api_token"))
		},
	}
}
