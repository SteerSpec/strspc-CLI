package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestValidateHTTPS(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{"https://api.anthropic.com", true},
		{"HTTPS://api.anthropic.com/v1", true},
		{"http://localhost:8080", true},
		{"http://LOCALHOST", true},
		{"http://127.0.0.1", true},
		{"http://127.1.2.3:9000/x", true},
		{"http://[::1]:8080", true},
		{"http://api.anthropic.com", false},
		{"http://localhost.example.com", false},
		{"http://127.0.0.1.example.com", false},
		{"http://10.0.0.1", false},
		{"ftp://localhost", false},
		{"https://", false},
		{"api.anthropic.com", false},
	} {
		err := validateHTTPS(tc.raw)
		if (err == nil) != tc.ok {
			t.Errorf("validateHTTPS(%q) = %v, want ok=%v", tc.raw, err, tc.ok)
		}
		if err != nil && !errors.Is(err, errInsecureURL) {
			t.Errorf("validateHTTPS(%q) = %v, want errInsecureURL", tc.raw, err)
		}
	}
	if err := validateHTTPS("http://[::1"); err == nil || errors.Is(err, errInsecureURL) {
		t.Errorf("unparsable URL: got %v, want a parse error", err)
	}
}

func TestValidateHTTPSRedactsPassword(t *testing.T) {
	err := validateHTTPS("http://user:secret@proxy.example")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("error = %v, want one without the password", err)
	}
}

func TestHTTPSOnlyRedirect(t *testing.T) {
	req := func(raw string) *http.Request {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Request{URL: u}
	}
	if err := httpsOnlyRedirect(req("https://example.com/a"), nil); err != nil {
		t.Errorf("https redirect refused: %v", err)
	}
	if err := httpsOnlyRedirect(req("http://example.com/a"), nil); !errors.Is(err, errInsecureURL) {
		t.Errorf("http redirect: got %v, want errInsecureURL", err)
	}
	via := make([]*http.Request, maxRedirects)
	if err := httpsOnlyRedirect(req("https://example.com/a"), via); err == nil {
		t.Error("redirect past the limit was followed")
	}
	// An insecure target is reported as such even past the limit.
	if err := httpsOnlyRedirect(req("http://example.com/a"), via); !errors.Is(err, errInsecureURL) {
		t.Errorf("insecure redirect past the limit: got %v, want errInsecureURL", err)
	}
}

func TestHTTPSOnlyClientRefusesInsecureRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://insecure.example/next", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	resp, err := newHTTPSOnlyClient(0).Get(server.URL) // loopback http is allowed
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("redirect to a plain-http host was followed")
	}
	if !errors.Is(err, errInsecureURL) {
		t.Errorf("error = %v, want errInsecureURL", err)
	}
}

func TestGetGitHubAPIRefusesInsecureBase(t *testing.T) {
	orig := githubAPIBase
	t.Cleanup(func() { githubAPIBase = orig })
	githubAPIBase = "http://api.github.example"

	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected request")
	})}
	resp, err := getGitHubAPI(context.Background(), client, "/repos/o/r/releases/latest", "application/json", "token")
	if err == nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, errInsecureURL) {
		t.Errorf("error = %v, want errInsecureURL", err)
	}
	if called {
		t.Error("a request carrying the token was sent over plain http")
	}
}

func TestFetchSchemaRefusesInsecureBase(t *testing.T) {
	orig := schemaBaseURL
	t.Cleanup(func() { schemaBaseURL = orig })
	schemaBaseURL = "http://raw.example"

	err := fetchSchema(t.TempDir(), "v1.0.0", schemaFiles[0].local)
	if !errors.Is(err, errInsecureURL) {
		t.Errorf("error = %v, want errInsecureURL", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
