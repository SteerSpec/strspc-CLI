package cmd

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The CLI's own HTTP clients connect over HTTPS only, unless the host is
// loopback (SEC-001). This mirrors strspc-manager's internal httpsonly
// package, which covers the manager's schema fetcher, remote sources and
// provider but can't be imported.

// maxRedirects matches net/http's default limit, which a custom CheckRedirect
// replaces.
const maxRedirects = 10

// errInsecureURL is wrapped into the error for a URL that is neither https
// nor http to a loopback host.
var errInsecureURL = errors.New("HTTPS is required unless the host is loopback")

// isLoopbackHost reports whether host is "localhost" or a loopback IP
// literal. It never resolves a name, so a name that merely resolves to
// 127.0.0.1 doesn't count.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// checkHTTPS reports an error wrapping errInsecureURL unless u is https, or
// http to a loopback host, and names a host.
func checkHTTPS(u *url.URL) error {
	if u.Hostname() != "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			return nil
		case "http":
			if isLoopbackHost(u.Hostname()) {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: %s", errInsecureURL, u.Redacted())
}

// validateHTTPS parses raw and applies checkHTTPS.
func validateHTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	return checkHTTPS(u)
}

// httpsOnlyRedirect refuses a redirect to an insecure URL and, like
// net/http's default, stops after 10 redirects.
func httpsOnlyRedirect(req *http.Request, via []*http.Request) error {
	if err := checkHTTPS(req.URL); err != nil {
		return fmt.Errorf("refusing redirect: %w", err)
	}
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	return nil
}

// newHTTPSOnlyClient returns a client with the given timeout that refuses
// insecure redirects. Callers check the first URL with checkHTTPS before
// sending the request.
func newHTTPSOnlyClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: httpsOnlyRedirect}
}
