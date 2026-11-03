package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/remote"
)

// fakeGitHubReply is how the fake GitHub API answers one path and query.
type fakeGitHubReply struct {
	status int
	body   string
	header map[string]string
}

// fakeGitHubRequest is a request the fake GitHub API received.
type fakeGitHubRequest struct {
	target string // path?query
	auth   string // the Authorization header
}

// fakeGitHub points githubAPIBase at a server answering replies, keyed by
// path?query, and 404 for anything else. It returns the requests it received.
func fakeGitHub(t *testing.T, replies map[string]fakeGitHubReply) func() []fakeGitHubRequest {
	t.Helper()
	var (
		mu  sync.Mutex
		got []fakeGitHubRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Path + "?" + r.URL.RawQuery
		mu.Lock()
		got = append(got, fakeGitHubRequest{target: target, auth: r.Header.Get("Authorization")})
		mu.Unlock()
		reply, ok := replies[target]
		if !ok {
			http.NotFound(w, r)
			return
		}
		for k, v := range reply.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
	}))
	t.Cleanup(srv.Close)
	orig := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = orig })
	return func() []fakeGitHubRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]fakeGitHubRequest(nil), got...)
	}
}

const latestCLIRelease = "/repos/SteerSpec/strspc-CLI/releases/latest?"

func release(tag string) fakeGitHubReply {
	return fakeGitHubReply{status: http.StatusOK, body: `{"tag_name":"` + tag + `"}`}
}

var rateLimited = fakeGitHubReply{status: http.StatusForbidden, header: map[string]string{"X-RateLimit-Remaining": "0"}}

func TestDoctorOfflineSendsNoGitHubRequest(t *testing.T) {
	fakeDoctorHost(t)
	requests := fakeGitHub(t, map[string]fakeGitHubReply{latestCLIRelease: release("v9.0.0")})
	dir := writeDoctorProject(t, "rules:\n  - source: github://acme/rules@v1.0.0/rules\n    scope: global\n")

	report, _ := runDoctorJSON(t, dir)
	if got := requests(); len(got) != 0 {
		t.Errorf("doctor without --online sent %v", got)
	}
	for _, c := range report.Checks {
		if c.Group == "online" {
			t.Errorf("online check without --online: %+v", c)
		}
	}
}

func TestDoctorOnlineLatest(t *testing.T) {
	tests := []struct {
		name    string
		version string
		reply   fakeGitHubReply
		want    string
		message string
		remedy  string
	}{
		{name: "older", version: "v1.2.3", reply: release("v1.3.0"), want: doctorWarn, message: "older than the latest release, v1.3.0", remedy: "install a release"},
		{name: "older without v", version: "1.2.3", reply: release("v1.3.0"), want: doctorWarn, message: "older"},
		{name: "latest", version: "v1.3.0", reply: release("v1.3.0"), want: doctorPass, message: "is the latest release"},
		{name: "newer", version: "v1.4.0-rc.1", reply: release("v1.3.0"), want: doctorPass, message: "newer"},
		{name: "dev build", version: "dev", reply: release("v1.3.0"), want: doctorSkip, message: "development build"},
		{name: "not a version", version: "abc1234", reply: release("v1.3.0"), want: doctorSkip, message: "not a release version"},
		{name: "rate limited", version: "v1.2.3", reply: rateLimited, want: doctorWarn, message: "rate limited", remedy: "GITHUB_TOKEN"},
		{name: "server error", version: "v1.2.3", reply: fakeGitHubReply{status: http.StatusBadGateway}, want: doctorWarn, message: "HTTP 502"},
		{name: "no tag", version: "v1.2.3", reply: fakeGitHubReply{status: http.StatusOK, body: `{}`}, want: doctorWarn, message: "has no tag"},
		{name: "bad tag", version: "v1.2.3", reply: release("nightly"), want: doctorWarn, message: "not a version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := fakeDoctorHost(t)
			h.version = tt.version
			fakeGitHub(t, map[string]fakeGitHubReply{latestCLIRelease: tt.reply})

			report, err := runDoctorJSON(t, t.TempDir(), "--online")
			assertExit(t, err, exitOK) // a missing config warns; nothing fails
			c := assertDoctorStatus(t, report, "online.latest", "", tt.want)
			testutil.AssertContains(t, c.Message, tt.message)
			if tt.remedy != "" {
				testutil.AssertContains(t, c.Remedy, tt.remedy)
			}
		})
	}
}

func TestDoctorOnlineLatestUnreachable(t *testing.T) {
	fakeDoctorHost(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // nothing listens there any more
	orig := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = orig })
	dir := writeDoctorProject(t, "rules:\n  - source: github://acme/rules@v1.0.0\n    scope: global\n")

	report, err := runDoctorJSON(t, dir, "--online")
	assertExit(t, err, exitViolations)
	c := assertDoctorStatus(t, report, "online.latest", "", doctorWarn)
	testutil.AssertContains(t, c.Message, "can't reach GitHub")
	c = assertDoctorStatus(t, report, "online.sources", "github://acme/rules@v1.0.0", doctorFail)
	testutil.AssertContains(t, c.Message, "unreachable")
}

func TestDoctorOnlineSources(t *testing.T) {
	fakeDoctorHost(t)
	const (
		pinned  = "github://acme/rules@v1.0.0/rules/core"
		missing = "github://acme/rules@v9.9.9/rules"
		latest  = "github://acme/rules@latest/rules"
		root    = "github://acme/rules@v1.0.0"
		busy    = "github://busy/rules@v1.0.0/rules"
		broken  = "github://acme/broken@latest"
	)
	requests := fakeGitHub(t, map[string]fakeGitHubReply{
		latestCLIRelease: release("v1.2.3"),
		"/repos/acme/rules/contents/rules/core?ref=v1.0.0": {status: http.StatusOK, body: `[]`},
		"/repos/acme/rules/releases/latest?":               release("v2.0.0"),
		"/repos/acme/rules/contents/rules?ref=v2.0.0":      {status: http.StatusOK, body: `[]`},
		"/repos/acme/rules/contents?ref=v1.0.0":            {status: http.StatusOK, body: `[]`},
		"/repos/busy/rules/contents/rules?ref=v1.0.0":      rateLimited,
		"/repos/acme/broken/releases/latest?":              {status: http.StatusInternalServerError},
	})
	var cfg strings.Builder
	cfg.WriteString("rules:\n  - source: ./local\n    scope: local\n")
	for _, s := range []string{pinned, missing, latest, root, busy, broken} {
		cfg.WriteString("  - source: " + s + "\n    scope: global\n")
	}
	dir := writeDoctorProject(t, cfg.String())

	report, err := runDoctorJSON(t, dir, "--online")
	assertExit(t, err, exitViolations)
	assertDoctorStatus(t, report, "online.latest", "", doctorPass)

	c := assertDoctorStatus(t, report, "online.sources", pinned, doctorPass)
	testutil.AssertContains(t, c.Message, "acme/rules@v1.0.0/rules/core: reachable")
	c = assertDoctorStatus(t, report, "online.sources", missing, doctorFail)
	testutil.AssertContains(t, c.Message, "not found (missing, or private and no token is set)")
	testutil.AssertContains(t, c.Remedy, "export GITHUB_TOKEN if it is private")
	c = assertDoctorStatus(t, report, "online.sources", latest, doctorPass)
	testutil.AssertContains(t, c.Message, "acme/rules@latest (v2.0.0)/rules: reachable")
	assertDoctorStatus(t, report, "online.sources", root, doctorPass)
	c = assertDoctorStatus(t, report, "online.sources", busy, doctorWarn)
	testutil.AssertContains(t, c.Message, "rate limited")
	c = assertDoctorStatus(t, report, "online.sources", broken, doctorFail)
	testutil.AssertContains(t, c.Message, "resolving latest release: HTTP 500")

	for _, c := range report.Checks {
		if c.ID == "online.sources" && c.Subject == "./local" {
			t.Errorf("a local source was checked online: %+v", c)
		}
	}
	for _, r := range requests() {
		if r.auth != "" {
			t.Errorf("a token was sent to %s with none set", r.target)
		}
	}
}

func TestDoctorOnlineSourcesNoGitHubSource(t *testing.T) {
	fakeDoctorHost(t)
	fakeGitHub(t, map[string]fakeGitHubReply{latestCLIRelease: release("v1.2.3")})

	report, _ := runDoctorJSON(t, writeDoctorProject(t, "rules: []\n"), "--online")
	assertDoctorStatus(t, report, "online.sources", "", doctorSkip)

	report, _ = runDoctorJSON(t, t.TempDir(), "--online")
	c := assertDoctorStatus(t, report, "online.sources", "", doctorSkip)
	testutil.AssertContains(t, c.Message, "no usable configuration")
}

func TestDoctorOnlineSendsTokenOnlyToAllowedOwners(t *testing.T) {
	fakeDoctorHost(t)
	t.Setenv("GITHUB_TOKEN", "ghp-secret")
	t.Setenv(remote.TokenOwnersEnv, "acme")
	const (
		acme  = "github://acme/rules@v1.0.0"
		other = "github://other/rules@v1.0.0"
	)
	requests := fakeGitHub(t, map[string]fakeGitHubReply{
		latestCLIRelease:                         release("v1.2.3"),
		"/repos/acme/rules/contents?ref=v1.0.0":  {status: http.StatusOK, body: `[]`},
		"/repos/other/rules/contents?ref=v1.0.0": rateLimited,
	})
	dir := writeDoctorProject(t, "rules:\n  - source: "+acme+"\n    scope: global\n  - source: "+other+"\n    scope: global\n")

	out, _ := testutil.ExecuteCommand(NewRootCmd(), "doctor", dir, "--online")
	testutil.AssertNotContains(t, out, "ghp-secret")
	report, _ := runDoctorJSON(t, dir, "--online")
	assertDoctorStatus(t, report, "online.sources", acme, doctorPass)
	c := assertDoctorStatus(t, report, "online.sources", other, doctorWarn)
	testutil.AssertContains(t, c.Remedy, "add other to "+remote.TokenOwnersEnv)

	for _, r := range requests() {
		owner := strings.Split(strings.TrimPrefix(r.target, "/repos/"), "/")[0]
		// SteerSpec always receives the token (remote.TokenOwnersFromEnv).
		switch want := owner == "acme" || owner == "SteerSpec"; {
		case want && r.auth != "Bearer ghp-secret":
			t.Errorf("%s: Authorization %q, want the token", r.target, r.auth)
		case !want && r.auth != "":
			t.Errorf("%s: the token was sent to %s", r.target, owner)
		}
	}
}

func TestDoctorOnlineNotFoundHints(t *testing.T) {
	fakeDoctorHost(t)
	t.Setenv("GITHUB_TOKEN", "ghp-secret")
	t.Setenv(remote.TokenOwnersEnv, "acme")
	const (
		withheld = "github://other/rules@v1.0.0"
		sent     = "github://acme/rules@v1.0.0"
		badToken = "github://acme/private@v1.0.0"
	)
	fakeGitHub(t, map[string]fakeGitHubReply{
		latestCLIRelease: release("v1.2.3"),
		"/repos/acme/private/contents?ref=v1.0.0": {status: http.StatusUnauthorized},
	})
	dir := writeDoctorProject(t, "rules:\n"+
		"  - source: "+withheld+"\n    scope: global\n"+
		"  - source: "+sent+"\n    scope: global\n"+
		"  - source: "+badToken+"\n    scope: global\n")

	report, _ := runDoctorJSON(t, dir, "--online")
	c := assertDoctorStatus(t, report, "online.sources", withheld, doctorFail)
	testutil.AssertContains(t, c.Message, "the token is not sent to other")
	testutil.AssertContains(t, c.Remedy, "add other to "+remote.TokenOwnersEnv)
	c = assertDoctorStatus(t, report, "online.sources", sent, doctorFail)
	testutil.AssertContains(t, c.Message, ": not found")
	if c.Remedy != "fix the source in .strspc/config.yaml" {
		t.Errorf("remedy %q", c.Remedy)
	}
	c = assertDoctorStatus(t, report, "online.sources", badToken, doctorFail)
	testutil.AssertContains(t, c.Message, "token is invalid")
	testutil.AssertContains(t, c.Remedy, "GITHUB_TOKEN")
}

func TestIsGitHubRateLimit(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header map[string]string
		body   string
		want   bool
	}{
		{name: "429", status: http.StatusTooManyRequests, want: true},
		{name: "primary", status: http.StatusForbidden, header: map[string]string{"X-RateLimit-Remaining": "0"}, want: true},
		{name: "secondary, Retry-After", status: http.StatusForbidden, header: map[string]string{"X-RateLimit-Remaining": "4999", "Retry-After": "60"}, want: true},
		{
			name: "secondary, message", status: http.StatusForbidden, header: map[string]string{"X-RateLimit-Remaining": "4999"},
			body: `{"message":"You have exceeded a secondary rate limit."}`, want: true,
		},
		{name: "forbidden", status: http.StatusForbidden, header: map[string]string{"X-RateLimit-Remaining": "4999"}, body: `{"message":"Resource not accessible"}`},
		{name: "not found", status: http.StatusNotFound, header: map[string]string{"Retry-After": "60"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.status, Header: http.Header{}}
			for k, v := range tt.header {
				resp.Header.Set(k, v)
			}
			if got := isGitHubRateLimit(resp, []byte(tt.body)); got != tt.want {
				t.Errorf("isGitHubRateLimit = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDoctorOnlineSecondaryRateLimitWarns(t *testing.T) {
	fakeDoctorHost(t)
	const (
		secondary = "github://acme/rules@v1.0.0"
		forbidden = "github://acme/locked@v1.0.0"
	)
	fakeGitHub(t, map[string]fakeGitHubReply{
		latestCLIRelease: release("v1.2.3"),
		"/repos/acme/rules/contents?ref=v1.0.0": {
			status: http.StatusForbidden,
			header: map[string]string{"X-RateLimit-Remaining": "4999", "Retry-After": "60"},
			body:   `{"message":"You have exceeded a secondary rate limit."}`,
		},
		"/repos/acme/locked/contents?ref=v1.0.0": {status: http.StatusForbidden, body: `{"message":"Resource not accessible"}`},
	})
	dir := writeDoctorProject(t, "rules:\n  - source: "+secondary+"\n    scope: global\n  - source: "+forbidden+"\n    scope: global\n")

	report, _ := runDoctorJSON(t, dir, "--online")
	c := assertDoctorStatus(t, report, "online.sources", secondary, doctorWarn)
	testutil.AssertContains(t, c.Message, "rate limited")
	c = assertDoctorStatus(t, report, "online.sources", forbidden, doctorFail)
	testutil.AssertContains(t, c.Message, "HTTP 403")
}

func TestContentsPath(t *testing.T) {
	tests := []struct {
		ref  remote.Ref
		tag  string
		want string
	}{
		{remote.Ref{Owner: "o", Repo: "r"}, "v1.0.0", "/repos/o/r/contents?ref=v1.0.0"},
		{remote.Ref{Owner: "o", Repo: "r", Path: "a/b c"}, "v1", "/repos/o/r/contents/a/b%20c?ref=v1"},
		{remote.Ref{Owner: "o", Repo: "r", Path: "rules"}, "feat/x&y", "/repos/o/r/contents/rules?ref=feat%2Fx%26y"},
	}
	for _, tt := range tests {
		if got := contentsPath(tt.ref, tt.tag); got != tt.want {
			t.Errorf("contentsPath(%+v, %q) = %q, want %q", tt.ref, tt.tag, got, tt.want)
		}
	}
}
