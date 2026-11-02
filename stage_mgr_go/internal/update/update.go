// Package update asks GitHub for the latest release, so the app can tell
// the user a newer version is out. It only reads: downloading and
// installing stay the user's, from the release page.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// LatestAPI is the GitHub endpoint of the project's latest release, which
// leaves out drafts and prereleases.
const LatestAPI = "https://api.github.com/repos/e1roy/stage-manager-for-windows/releases/latest"

// Release is a version published on GitHub.
type Release struct {
	// Version is the release's tag without a leading "v", as "0.2.14".
	Version string
	// URL is the release's page, where its installer is.
	URL string
}

// Latest returns the release api describes, LatestAPI outside tests,
// asking through proxy (SystemProxy), if it names one. In China GitHub is
// often reached only through a proxy such as v2rayN's; a proxy left in
// the settings by a client that is no longer running is passed by, and
// GitHub asked directly, which the same deadline bounds.
func Latest(ctx context.Context, api string, proxy Proxy) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return Release{}, err
	}
	// GitHub turns requests without a User-Agent away.
	req.Header.Set("User-Agent", "StageManagerForWindows")
	req.Header.Set("Accept", "application/vnd.github+json")
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
// release it answers with.
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
	var body struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, err
	}
	v := strings.TrimPrefix(strings.TrimSpace(body.Tag), "v")
	if _, ok := parse(v); !ok {
		return Release{}, errors.New("update: no version in tag " + strconv.Quote(body.Tag))
	}
	return Release{Version: v, URL: body.URL}, nil
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
