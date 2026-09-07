package docs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// context7Fixture routes the resolve endpoint to n candidates and the docs
// endpoint per library: lib0 is gone (404), lib1 exists but is empty, every
// other library returns three snippets.
func context7Fixture(n int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/search") {
			resolveHandler(n)(w, r)
			return
		}
		lib := r.URL.Query().Get("libraryId")
		w.Header().Set("Content-Type", "application/json")
		switch lib {
		case "/org/lib0":
			w.WriteHeader(http.StatusNotFound)
		case "/org/lib1":
			w.Write([]byte(`{"codeSnippets":[],"infoSnippets":[]}`)) //nolint:errcheck
		default:
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"codeSnippets": []map[string]any{{"codeTitle": "Install", "codeId": lib + "#install", "codeList": []map[string]string{{"language": "sh", "code": "go get " + lib}}}},
				"infoSnippets": []map[string]any{{"pageId": lib + "#usage", "breadcrumb": "Usage", "content": "Call it."}, {"pageId": lib + "#faq", "breadcrumb": "FAQ", "content": "Yes."}},
			})
		}
	}
}

// A bare query skips a stale (404) candidate and an empty one, reports the
// library it settled on and the runners-up, and honours the result limit.
func TestContext7BareQueryFallsThroughCandidatesAndReportsThem(t *testing.T) {
	c := newTestContext7(t, context7Fixture(5))
	results, res, err := c.SearchResolved(context.Background(), "how do I install it", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Library != "/org/lib2" {
		t.Fatalf("results = %+v, want 2 from /org/lib2", results)
	}
	if res == nil || res.Library != "/org/lib2" {
		t.Fatalf("resolution = %+v, want library /org/lib2", res)
	}
	var ids []string
	for _, m := range res.Candidates {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, ",") != "/org/lib0,/org/lib1,/org/lib3,/org/lib4" {
		t.Fatalf("candidates = %v, want the other four in rank order", ids)
	}

	// Search keeps the interface contract: same results, resolution dropped.
	plain, err := c.Search(context.Background(), "how do I install it", 0)
	if err != nil || len(plain) != 3 {
		t.Fatalf("Search = %d results, %v; want all 3 with no limit", len(plain), err)
	}
}

// When every attempted candidate has vanished the caller gets not-found, not
// an empty success, and the attempt count stays bounded.
func TestContext7BareQueryAllCandidatesGoneIsNotFound(t *testing.T) {
	calls := 0
	c := newTestContext7(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/search") {
			resolveHandler(5)(w, r)
			return
		}
		calls++
		w.WriteHeader(http.StatusNotFound)
	})
	_, res, err := c.SearchResolved(context.Background(), "gone", 5)
	if !errors.Is(err, ErrNotFound) || res != nil {
		t.Fatalf("err = %v, res = %v; want ErrNotFound and no resolution", err, res)
	}
	if calls != context7Attempts {
		t.Fatalf("fetched %d candidates, want %d", calls, context7Attempts)
	}
}

// docs.Query hands providers without the capability straight to Search.
func TestQueryWithoutResolvingCapability(t *testing.T) {
	results, res, err := Query(context.Background(), libraryFixture{}, "anything", 3)
	if err != nil || res != nil || results != nil {
		t.Fatalf("Query = %v %v %v; want nil results, nil resolution, nil error", results, res, err)
	}
}
