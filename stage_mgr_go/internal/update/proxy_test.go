package update

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestParseServer(t *testing.T) {
	for _, c := range []struct{ server, want string }{
		// v2rayN and Clash set one address for every protocol.
		{"127.0.0.1:10809", "http://127.0.0.1:10809"},
		{"http=127.0.0.1:1080;https=127.0.0.1:1081;socks=127.0.0.1:1082", "http://127.0.0.1:1081"},
		{"http=127.0.0.1:1080;socks=127.0.0.1:1082", "http://127.0.0.1:1080"},
		{"socks=127.0.0.1:10808", "socks5://127.0.0.1:10808"},
		{"http://127.0.0.1:7890", "http://127.0.0.1:7890"},
		{"", ""},
		{"ftp=127.0.0.1:21", ""},
	} {
		got := ""
		if u := parseServer(c.server); u != nil {
			got = u.String()
		}
		if got != c.want {
			t.Errorf("parseServer(%q) = %q, want %q", c.server, got, c.want)
		}
	}
}

func TestBypassed(t *testing.T) {
	const bypass = "localhost;127.*;10.*;*.corp.example;<local>"
	for host, want := range map[string]bool{
		"api.github.com":     false,
		"localhost":          true,
		"127.0.0.1":          true,
		"build.corp.example": true,
		"intranet":           true,
		"API.GITHUB.COM":     false,
	} {
		if got := bypassed(host, bypass); got != want {
			t.Errorf("bypassed(%q) = %v, want %v", host, got, want)
		}
	}
	if !bypassed("api.github.com", "*.github.com") {
		t.Error("a wildcard for GitHub did not bypass it")
	}
}

// The release comes through the proxy when one is named.
func TestLatestThroughProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A proxy is asked for the whole URL.
		if r.URL.Host != "api.github.invalid" {
			http.Error(w, "not proxied", http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{"tag_name": "0.3.0", "html_url": "https://example.com/0.3.0"}`))
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	r, err := Latest(context.Background(), "http://api.github.invalid/releases/latest", func(*http.Request) (*url.URL, error) { return u, nil })
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.3.0" {
		t.Fatalf("got %+v", r)
	}
}

// A proxy left in the settings by a client that has quit is passed by.
func TestLatestPastDeadProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name": "0.3.1", "html_url": "https://example.com/0.3.1"}`))
	}))
	defer srv.Close()
	// A port nothing listens on any more.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := &url.URL{Scheme: "http", Host: l.Addr().String()}
	l.Close()
	r, err := Latest(context.Background(), srv.URL, func(*http.Request) (*url.URL, error) { return dead, nil })
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.3.1" {
		t.Fatalf("got %+v", r)
	}
}
