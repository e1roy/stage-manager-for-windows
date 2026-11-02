package update

import (
	"net/http"
	"net/url"
	"path"
	"strings"
)

// Proxy picks the proxy of a request, as http.Transport.Proxy does; nil
// for none.
type Proxy func(*http.Request) (*url.URL, error)

// SystemProxy returns the proxy of Windows' Internet settings, server and
// bypass being their ProxyServer and ProxyOverride, as v2rayN, Clash and
// the like set them when they take the system proxy. A proxy in the
// environment (HTTPS_PROXY), set on purpose, comes first. It names none
// when there is neither, and Latest asks directly.
func SystemProxy(server, bypass string) Proxy {
	sys := parseServer(server)
	return func(req *http.Request) (*url.URL, error) {
		if u, err := http.ProxyFromEnvironment(req); u != nil || err != nil {
			return u, err
		}
		if sys == nil || bypassed(req.URL.Hostname(), bypass) {
			return nil, nil
		}
		return sys, nil
	}
}

// parseServer reads ProxyServer: one address for every protocol, as
// "127.0.0.1:10809", or one per protocol, as
// "http=127.0.0.1:10809;https=127.0.0.1:10809;socks=127.0.0.1:10808". For
// the HTTPS of GitHub's API it takes the https one, then the one for all,
// then the http one, then socks, which Go speaks as SOCKS5.
func parseServer(s string) *url.URL {
	var all string
	by := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if k, v, ok := strings.Cut(part, "="); ok {
			by[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		} else if part != "" {
			all = part
		}
	}
	addr, scheme := by["https"], "http"
	switch {
	case addr != "":
	case all != "":
		addr = all
	case by["http"] != "":
		addr = by["http"]
	case by["socks"] != "":
		addr, scheme = by["socks"], "socks5"
	default:
		return nil
	}
	if !strings.Contains(addr, "://") {
		addr = scheme + "://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}

// bypassed reports whether ProxyOverride sends host around the proxy: its
// entries are host names with "*" wildcards, as "*.local" or "10.*", and
// "<local>" for names without a dot.
func bypassed(host, bypass string) bool {
	host = strings.ToLower(host)
	for _, p := range strings.Split(bypass, ";") {
		p = strings.ToLower(strings.TrimSpace(p))
		switch {
		case p == "":
		case p == "<local>":
			if !strings.Contains(host, ".") {
				return true
			}
		default:
			if ok, _ := path.Match(p, host); ok {
				return true
			}
		}
	}
	return false
}
