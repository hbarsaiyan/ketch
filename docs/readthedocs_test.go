package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

const readTheDocsSearchFixture = `{"count":2,"next":null,"previous":null,"projects":[{"slug":"requests","versions":[{"slug":"latest"}]}],"query":"session retries","results":[
 {"type":"page","project":{"slug":"requests","alias":null},"version":{"slug":"latest"},"title":"Advanced Usage","domain":"https://requests.readthedocs.io","path":"/en/latest/user/advanced/",
  "highlights":{"title":[]},
  "blocks":[
   {"type":"section","id":"example-automatic-retries","title":"Example: Automatic Retries","content":"By default, Requests does not retry\n\n  failed connections.   However, it is possible.","highlights":{"content":["<span>retries</span>"]}},
   {"type":"domain","role":"py:class","name":"requests.adapters.HTTPAdapter","id":"requests.adapters.HTTPAdapter","content":"The built-in HTTP Adapter for urllib3.","highlights":{}}
  ]},
 {"type":"page","project":{"slug":"requests","alias":null},"version":{"slug":"latest"},"title":"Developer Interface","domain":"https://requests.readthedocs.io","path":"/en/latest/api/","highlights":{"title":[]},"blocks":[]}
]}`

// newTestReadTheDocs points a backend at handler.
func newTestReadTheDocs(t *testing.T, token string, handler http.HandlerFunc) *ReadTheDocs {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewReadTheDocs(server.URL+"/", token)
}

func TestReadTheDocsSearchMapsSectionsDomainObjectsAndBarePages(t *testing.T) {
	r := newTestReadTheDocs(t, "", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v3/search/" {
			t.Errorf("path = %s", req.URL.Path)
		}
		if got := req.URL.Query().Get("q"); got != "project:requests session retries" {
			t.Errorf("q = %q", got)
		}
		if got := req.URL.Query().Get("page_size"); got != "50" {
			t.Errorf("page_size = %q, want the API maximum when no limit is set", got)
		}
		if req.Header.Get("Authorization") != "" {
			t.Error("anonymous search must not send an Authorization header")
		}
		if req.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", req.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, readTheDocsSearchFixture)
	})
	results, err := r.Search(context.Background(), "project:requests session retries", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3 (two blocks + one bare page): %+v", len(results), results)
	}
	want := Result{
		Library: "requests", Version: "latest", Title: "Example: Automatic Retries",
		Breadcrumb: "Advanced Usage › Example: Automatic Retries",
		Snippet:    "By default, Requests does not retry failed connections. However, it is possible.",
		URL:        "https://requests.readthedocs.io/en/latest/user/advanced/#example-automatic-retries",
		Source:     "readthedocs",
	}
	if results[0] != want {
		t.Errorf("section result:\n got %+v\nwant %+v", results[0], want)
	}
	if results[1].Title != "requests.adapters.HTTPAdapter" || results[1].Snippet != "The built-in HTTP Adapter for urllib3." || !strings.HasSuffix(results[1].URL, "#requests.adapters.HTTPAdapter") {
		t.Errorf("domain object result = %+v", results[1])
	}
	if results[2].Title != "Developer Interface" || results[2].Breadcrumb != "" || results[2].URL != "https://requests.readthedocs.io/en/latest/api/" || results[2].Snippet != "" {
		t.Errorf("bare page result = %+v", results[2])
	}
}

// A query that names no project never reaches the network: Read the Docs
// would answer it with an empty page, which hides the fix.
func TestReadTheDocsSearchRequiresProjectFilter(t *testing.T) {
	r := newTestReadTheDocs(t, "", func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") })
	_, err := r.Search(context.Background(), "session retries", 5)
	if !errors.Is(err, ErrScopeRequired) || !strings.Contains(err.Error(), "project:<slug>") || !strings.Contains(err.Error(), "--library") {
		t.Fatalf("err = %v, want ErrScopeRequired with the project filter syntax", err)
	}
}

func TestReadTheDocsSearchRespectsLimitAndPageSize(t *testing.T) {
	r := newTestReadTheDocs(t, "", func(w http.ResponseWriter, req *http.Request) {
		if got := req.URL.Query().Get("page_size"); got != "2" {
			t.Errorf("page_size = %q, want the limit", got)
		}
		fmt.Fprint(w, readTheDocsSearchFixture)
	})
	results, err := r.Search(context.Background(), "project:requests session", 2)
	if err != nil || len(results) != 2 {
		t.Fatalf("results = %d, err = %v; want 2", len(results), err)
	}
}

// --library scopes the query with the project filter (version included) and
// tokens caps the snippet text, always keeping the first result.
func TestReadTheDocsGetDocsScopesAndBudgets(t *testing.T) {
	r := newTestReadTheDocs(t, "", func(w http.ResponseWriter, req *http.Request) {
		if got := req.URL.Query().Get("q"); got != "project:requests/stable retries" {
			t.Errorf("q = %q", got)
		}
		fmt.Fprint(w, readTheDocsSearchFixture)
	})
	results, err := r.GetDocs(context.Background(), "/requests/stable/", "retries", 25) // 100 chars: fits the 81-char first snippet only
	if err != nil || len(results) != 1 || results[0].Title != "Example: Automatic Retries" {
		t.Fatalf("results = %+v, err = %v; want just the first section", results, err)
	}
	if _, err := r.GetDocs(context.Background(), " / ", "retries", 100); !errors.Is(err, ErrScopeRequired) {
		t.Fatalf("empty library must be rejected as a scope error, got %v", err)
	}
}

func TestReadTheDocsResolveLibraryIsAnExactSlugLookup(t *testing.T) {
	var requests []string
	r := newTestReadTheDocs(t, "", func(w http.ResponseWriter, req *http.Request) {
		requests = append(requests, req.URL.Path)
		switch req.URL.Path {
		case "/api/v3/projects/flask/":
			fmt.Fprint(w, `{"slug":"flask","name":"Flask","default_version":"stable","urls":{"documentation":"https://flask.palletsprojects.com/en/stable/"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	ctx := context.Background()
	matches, err := r.ResolveLibrary(ctx, " Flask ", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := LibraryMatch{ID: "flask", Title: "Flask", Description: "https://flask.palletsprojects.com/en/stable/", Versions: []string{"stable"}}
	if len(matches) != 1 || matches[0].ID != want.ID || matches[0].Title != want.Title || matches[0].Description != want.Description || !slices.Equal(matches[0].Versions, want.Versions) {
		t.Errorf("matches = %+v, want %+v", matches, want)
	}
	matches, err = r.ResolveLibrary(ctx, "Flask/2.3.x", 5)
	if err != nil || len(matches) != 1 || matches[0].ID != "flask/2.3.x" || !slices.Equal(matches[0].Versions, []string{"2.3.x"}) {
		t.Errorf("versioned match = %+v, err = %v", matches, err)
	}
	if matches, err = r.ResolveLibrary(ctx, "no-such-project", 5); err != nil || len(matches) != 0 {
		t.Errorf("404 must be no match: %+v, %v", matches, err)
	}
	if matches, err = r.ResolveLibrary(ctx, "project:flask retries", 5); err != nil || len(matches) != 0 {
		t.Errorf("a query with filters cannot be a slug: %+v, %v", matches, err)
	}
	if len(requests) != 3 {
		t.Errorf("requests = %v, want three lookups and none for the filtered query", requests)
	}
}

func TestReadTheDocsSendsTokenAndMapsStatuses(t *testing.T) {
	status := http.StatusOK
	r := newTestReadTheDocs(t, "secret-token", func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Token secret-token" {
			t.Errorf("Authorization = %q", req.Header.Get("Authorization"))
		}
		w.WriteHeader(status)
		fmt.Fprint(w, `{"results":[]}`)
	})
	for _, tc := range []struct {
		code int
		want string
	}{
		{http.StatusUnauthorized, "token rejected"},
		{http.StatusForbidden, "readthedocs_api_token"},
		{http.StatusTooManyRequests, "rate limited"},
		{http.StatusBadGateway, "status 502"},
	} {
		status = tc.code
		_, err := r.Search(context.Background(), "project:pip x", 1)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("status %d: err = %v, want %q", tc.code, err, tc.want)
		}
	}
	status = http.StatusOK
	if results, err := r.Search(context.Background(), "project:pip x", 1); err != nil || len(results) != 0 {
		t.Errorf("empty page: %v %v", results, err)
	}
}

func TestProbeReadTheDocs(t *testing.T) {
	ctx := context.Background()
	if status, detail := ProbeReadTheDocs(ctx, http.DefaultClient, "", ""); status != health.StatusMisconfigured || !strings.Contains(detail, "readthedocs_url not set") {
		t.Errorf("unset URL: %s %q", status, detail)
	}
	cases := []struct {
		name   string
		code   int
		body   string
		token  string
		status health.Status
		detail string
	}{
		{"ok", 200, `{"count":0,"results":[]}`, "", health.StatusOK, ""},
		{"html", 200, `<html>login</html>`, "", health.StatusMisconfigured, "non-JSON"},
		{"bad token", 401, `{"detail":"Invalid token."}`, "junk", health.StatusMisconfigured, "token rejected"},
		{"blocked", 403, ``, "", health.StatusMisconfigured, "HTTP 403"},
		{"throttled", 429, ``, "", health.StatusMisconfigured, "rate limited"},
		{"down", 503, ``, "", health.StatusUnreachable, "status 503"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != "/api/v3/search/" || !strings.HasPrefix(req.URL.Query().Get("q"), "project:") {
					t.Errorf("probe must search a named project: %s?%s", req.URL.Path, req.URL.RawQuery)
				}
				if (req.Header.Get("Authorization") != "") != (tc.token != "") {
					t.Errorf("Authorization = %q with token %q", req.Header.Get("Authorization"), tc.token)
				}
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			}))
			defer ts.Close()
			status, detail := ProbeReadTheDocs(ctx, ts.Client(), ts.URL, tc.token)
			if status != tc.status || !strings.Contains(detail, tc.detail) {
				t.Errorf("got %s %q, want %s containing %q", status, detail, tc.status, tc.detail)
			}
		})
	}
}

// Zero config: usable with the public instance by default, listed for
// --library, and a required doctor check only when selected or when a token
// was configured explicitly.
func TestReadTheDocsRegistry(t *testing.T) {
	p, ok := Lookup("readthedocs")
	if !ok {
		t.Fatal("readthedocs is not registered")
	}
	if !slices.Contains(AvailableBackends(), "readthedocs") || !slices.Contains(LibraryBackends(), "readthedocs") {
		t.Fatalf("backends = %v, library backends = %v", AvailableBackends(), LibraryBackends())
	}
	cfg := config.Defaults().WithSettings(p.Settings)
	if cfg.String("readthedocs_url") != readTheDocsDefaultURL || !p.Usable(&cfg) || p.Required(&cfg) {
		t.Fatalf("defaults: url=%q usable=%v required=%v", cfg.String("readthedocs_url"), p.Usable(&cfg), p.Required(&cfg))
	}
	s, err := p.Build(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(LibraryResolver); !ok {
		t.Fatal("readthedocs must offer library operations")
	}
	if _, ok := s.(Resolving); ok {
		t.Fatal("readthedocs must not guess a project from a bare query")
	}
	selected := cfg
	selected.DocsBackend = "readthedocs"
	if !p.Required(&selected) {
		t.Error("selected backend must be a required doctor check")
	}
	withToken := cfg
	withToken.SetProvider("readthedocs_api_token", "t")
	if !p.Required(&withToken) {
		t.Error("a configured token must be verified by doctor")
	}
	cfg.SetProvider("readthedocs_url", " ")
	if p.Usable(&cfg) {
		t.Error("blank URL must not be usable")
	}
	if _, err := p.Build(&cfg); err == nil || !errors.Is(err, err) || !strings.Contains(err.Error(), "readthedocs_url") {
		t.Errorf("build with blank URL: %v", err)
	}
}
