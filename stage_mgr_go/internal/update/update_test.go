package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		v, current string
		want       bool
	}{
		{"0.2.14", "0.2.13", true},
		{"0.2.13", "0.2.13", false},
		{"0.2.9", "0.2.13", false},
		{"0.3.0", "0.2.13", true},
		{"v0.2.14", "0.2.13", true},
		{"0.2.13.1", "0.2.13", true},
		{"0.2", "0.2.0", false},
		{"0.2.14-beta.1", "0.2.13", true},
		// A build without a version, as "go run" makes, shows no update.
		{"0.2.14", "", false},
		{"", "0.2.13", false},
		{"latest", "0.2.13", false},
	} {
		if got := Newer(c.v, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.v, c.current, got, c.want)
		}
	}
}

func TestLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"tag_name": "v0.2.14", "html_url": "https://example.com/releases/tag/v0.2.14", "draft": false}`))
	}))
	defer srv.Close()
	r, err := Latest(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.2.14" || r.URL != "https://example.com/releases/tag/v0.2.14" {
		t.Fatalf("got %+v", r)
	}
}

func TestLatestFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/limited":
			http.Error(w, "rate limited", http.StatusForbidden)
		default:
			w.Write([]byte(`{"tag_name": "nightly"}`))
		}
	}))
	defer srv.Close()
	for _, path := range []string{"/limited", "/nightly"} {
		if r, err := Latest(context.Background(), srv.URL+path, nil); err == nil {
			t.Errorf("%s: got %+v, want an error", path, r)
		}
	}
}
