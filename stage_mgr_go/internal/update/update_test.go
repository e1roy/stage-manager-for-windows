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
		switch r.URL.Path {
		case "/bom":
			// What PowerShell's Set-Content writes, with Windows' line end.
			w.Write([]byte("\xef\xbb\xbf0.2.14\r\n"))
		case "/prefixed":
			w.Write([]byte("v0.2.14\n"))
		default:
			w.Write([]byte("0.2.14\n"))
		}
	}))
	defer srv.Close()
	for _, path := range []string{"/plain", "/bom", "/prefixed"} {
		r, err := Latest(context.Background(), srv.URL+path, nil)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if r.Version != "0.2.14" || r.URL != ReleasesURL {
			t.Errorf("%s: got %+v", path, r)
		}
	}
}

func TestLatestFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/limited":
			http.Error(w, "rate limited", http.StatusForbidden)
		case "/missing":
			http.NotFound(w, r)
		default:
			w.Write([]byte("<html>sign in to the network</html>"))
		}
	}))
	defer srv.Close()
	for _, path := range []string{"/limited", "/missing", "/page"} {
		if r, err := Latest(context.Background(), srv.URL+path, nil); err == nil {
			t.Errorf("%s: got %+v, want an error", path, r)
		}
	}
}
