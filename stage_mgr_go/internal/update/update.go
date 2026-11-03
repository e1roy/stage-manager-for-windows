// Package update asks GitHub which version was released last, so the app
// can tell the user a newer one is out. It only reads: downloading and
// installing stay the user's, from the release page.
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// VersionURL is the file at the root of the repository that holds the
// version released last, as "0.2.16": it is committed with the release, so
// it never runs ahead of the installer by more than the build takes. It is
// read from raw.githubusercontent.com, which unlike api.github.com is not
// limited to 60 requests an hour for each address, a limit that a shared
// address, as an office's, uses up for everyone behind it.
const VersionURL = "https://raw.githubusercontent.com/e1roy/stage-manager-for-windows/main/.version"

// ReleasesURL is the page of the latest release, where its installer is.
const ReleasesURL = "https://github.com/e1roy/stage-manager-for-windows/releases/latest"

// Release is a version published on GitHub.
type Release struct {
	// Version is the released version, as "0.2.14".
	Version string
	// URL is the release's page, where its installer is.
	URL string
}

// Latest returns the release that the version file at src names, VersionURL
// outside tests, asking through proxy (SystemProxy), if it names one. In
// China GitHub is often reached only through a proxy such as v2rayN's; a
// proxy left in the settings by a client that is no longer running is
// passed by, and GitHub asked directly, which the same deadline bounds.
func Latest(ctx context.Context, src string, proxy Proxy) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return Release{}, err
	}
	// GitHub turns requests without a User-Agent away.
	req.Header.Set("User-Agent", "StageManagerForWindows")
	if proxy != nil {
		if u, err := proxy(req); err != nil || u == nil {
			proxy = nil
		}
	}
	if proxy == nil {
		return fetch(req, nil)
	}
	r, err := fetch(req, proxy)
	if err != nil && ctx.Err() == nil {
		return fetch(req, nil)
	}
	return r, err
}

// fetch sends req through proxy, or directly when it is nil, and reads the
// version it answers with.
func fetch(req *http.Request, proxy Proxy) (Release, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = proxy
	defer t.CloseIdleConnections()
	resp, err := (&http.Client{Transport: t}).Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("update: %s", resp.Status)
	}
	// A version is a few bytes; a page that is not one (a proxy's login
	// page, say) is not read to its end.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return Release{}, err
	}
	// The first line, without the byte order mark that Windows editors and
	// PowerShell put at the start of a file.
	line, _, _ := strings.Cut(strings.TrimPrefix(string(body), "\xef\xbb\xbf"), "\n")
	v := strings.TrimPrefix(strings.TrimSpace(line), "v")
	if _, ok := parse(v); !ok {
		return Release{}, errors.New("update: no version in " + strconv.Quote(line))
	}
	return Release{Version: v, URL: ReleasesURL}, nil
}

// Newer reports whether version v is later than current. A version that
// is not dotted numbers, as a build run with "go run" has none, is never
// newer nor older, so such a build shows no update.
func Newer(v, current string) bool {
	a, ok := parse(v)
	b, ok2 := parse(current)
	if !ok || !ok2 {
		return false
	}
	for i := range max(len(a), len(b)) {
		x, y := at(a, i), at(b, i)
		if x != y {
			return x > y
		}
	}
	return false
}

// parse reads "1.2.3", with or without a leading "v"; what follows a "-"
// or "+", as a prerelease or build, is left out.
func parse(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

func at(v []int, i int) int {
	if i < len(v) {
		return v[i]
	}
	return 0
}
