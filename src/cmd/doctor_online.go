package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/SteerSpec/strspc-manager/src/remote"
)

// strspcRelease is the repository whose releases online.latest compares with.
var strspcRelease = remote.Ref{Owner: "SteerSpec", Repo: "strspc-CLI"}

// doctorOnlineChecks are the checks that call the GitHub API. They run only
// with --online, and read nothing into a cache.
func doctorOnlineChecks(env *doctorEnv, project *doctorProject) []doctorResult {
	if !env.online {
		return nil
	}
	return append([]doctorResult{doctorLatestCheck(env)}, doctorReachabilityChecks(env, project)...)
}

// doctorLatestCheck compares the running version with the latest strspc
// release. Not knowing the latest release is a warning: it says nothing about
// the project.
func doctorLatestCheck(env *doctorEnv) doctorResult {
	const g = "online"
	v := env.version
	if v == "" || v == "dev" {
		return doctorOutcome(g, "latest", "", doctorSkip, "a development build has no release to compare with", "")
	}
	current, err := semver.NewVersion(v)
	if err != nil {
		return doctorOutcome(g, "latest", "", doctorSkip, fmt.Sprintf("%q is not a release version", v), "")
	}

	resp, err := getGitHubAPI(env.ctx, env.onlineClient, releasesLatestPath(strspcRelease),
		"application/vnd.github+json", remote.TokenForOwner(strspcRelease.Owner))
	if err != nil {
		return doctorOutcome(g, "latest", "", doctorWarn, "can't reach GitHub: "+err.Error(), "")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, rateLimited := githubProblem(resp, strspcRelease.Owner)
		remedy := ""
		if rateLimited {
			remedy = rateLimitRemedy(strspcRelease.Owner)
		}
		return doctorOutcome(g, "latest", "", doctorWarn, "can't look up the latest release: "+msg, remedy)
	}
	tag, err := decodeReleaseTag(resp, strspcRelease)
	if err != nil {
		return doctorOutcome(g, "latest", "", doctorWarn, err.Error(), "")
	}
	latest, err := semver.NewVersion(tag)
	if err != nil {
		return doctorOutcome(g, "latest", "", doctorWarn, fmt.Sprintf("the latest release tag %q is not a version", tag), "")
	}

	switch {
	case current.LessThan(latest):
		return doctorOutcome(g, "latest", "", doctorWarn,
			fmt.Sprintf("%s is older than the latest release, %s", v, tag), doctorInstallRemedy)
	case current.Equal(latest):
		return doctorOutcome(g, "latest", "", doctorPass, v+" is the latest release", "")
	default:
		return doctorOutcome(g, "latest", "", doctorPass, fmt.Sprintf("%s is newer than the latest release, %s", v, tag), "")
	}
}

// doctorReachabilityChecks asks GitHub, for each github:// source, whether its
// ref and path exist, sending the token only to the owners it is allowed for.
func doctorReachabilityChecks(env *doctorEnv, project *doctorProject) []doctorResult {
	const g = "online"
	if project.cfg == nil {
		return skipped(g, "no usable configuration", "sources")
	}
	var out []doctorResult
	for _, r := range project.cfg.Rules {
		if !strings.HasPrefix(r.Source, remote.Scheme) {
			continue
		}
		ref, err := remote.ParseGitHub(r.Source)
		if err != nil {
			continue // sources.state reports it
		}
		out = append(out, doctorReachSource(env, r.Source, ref))
	}
	if len(out) == 0 {
		return []doctorResult{doctorOutcome(g, "sources", "", doctorSkip, "no github:// source", "")}
	}
	return out
}

func doctorReachSource(env *doctorEnv, source string, ref remote.Ref) doctorResult {
	const g = "online"
	token := remote.TokenForOwner(ref.Owner)
	get := func(path string) (*http.Response, error) {
		return getGitHubAPI(env.ctx, env.onlineClient, path, "application/vnd.github+json", token)
	}
	target := ref.Owner + "/" + ref.Repo + "@" + ref.Ref

	tag := ref.Ref
	if ref.Ref == remote.LatestRef {
		resp, err := get(releasesLatestPath(ref))
		if err != nil {
			return doctorOutcome(g, "sources", source, doctorFail, target+": unreachable: "+err.Error(), "")
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return unreachableSource(source, target+": resolving latest release", resp, ref.Owner)
		}
		if tag, err = decodeReleaseTag(resp, ref); err != nil {
			return doctorOutcome(g, "sources", source, doctorFail, target+": "+err.Error(), "")
		}
		target += " (" + tag + ")"
	}
	if ref.Path != "" {
		target += "/" + ref.Path
	}

	resp, err := get(contentsPath(ref, tag))
	if err != nil {
		return doctorOutcome(g, "sources", source, doctorFail, target+": unreachable: "+err.Error(), "")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return unreachableSource(source, target, resp, ref.Owner)
	}
	return doctorOutcome(g, "sources", source, doctorPass, target+": reachable", "")
}

// contentsPath is the API path of ref's path (the root when empty) at tag.
func contentsPath(ref remote.Ref, tag string) string {
	p := fmt.Sprintf("/repos/%s/%s/contents", url.PathEscape(ref.Owner), url.PathEscape(ref.Repo))
	if ref.Path != "" {
		segs := strings.Split(ref.Path, "/")
		for i, s := range segs {
			segs[i] = url.PathEscape(s)
		}
		p += "/" + strings.Join(segs, "/")
	}
	return p + "?ref=" + url.QueryEscape(tag)
}

// unreachableSource reports a source GitHub didn't serve: rate limiting is a
// warning, since the source may well exist; anything else fails.
func unreachableSource(source, target string, resp *http.Response, owner string) doctorResult {
	const g = "online"
	msg, rateLimited := githubProblem(resp, owner)
	if rateLimited {
		return doctorOutcome(g, "sources", source, doctorWarn, target+": "+msg, rateLimitRemedy(owner))
	}
	remedy := "fix the source in .strspc/config.yaml"
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		remedy = "fix or unset GITHUB_TOKEN"
	case resp.StatusCode == http.StatusNotFound && remote.TokenFromEnv() == "":
		remedy += ", or export GITHUB_TOKEN if it is private"
	case resp.StatusCode == http.StatusNotFound && remote.TokenForOwner(owner) == "":
		remedy += ", or add " + owner + " to " + remote.TokenOwnersEnv + " if it is private"
	}
	return doctorOutcome(g, "sources", source, doctorFail, target+": "+msg, remedy)
}

// githubProblem describes a response other than 200, and reports whether it
// is rate limiting. The body is drained so the connection can be reused.
func githubProblem(resp *http.Response, owner string) (msg string, rateLimited bool) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return "rate limited by GitHub", true
	case resp.StatusCode == http.StatusNotFound && remote.TokenFromEnv() == "":
		return "not found (missing, or private and no token is set)", false
	case resp.StatusCode == http.StatusNotFound && remote.TokenForOwner(owner) == "":
		return "not found (missing, or private and the token is not sent to " + owner + ")", false
	case resp.StatusCode == http.StatusNotFound:
		return "not found", false
	case resp.StatusCode == http.StatusUnauthorized:
		return "HTTP 401: the token is invalid", false
	default:
		return fmt.Sprintf("HTTP %d", resp.StatusCode), false
	}
}

func rateLimitRemedy(owner string) string {
	switch {
	case remote.TokenFromEnv() == "":
		return "export GITHUB_TOKEN to raise the limit, or retry later"
	case remote.TokenForOwner(owner) == "":
		return "add " + owner + " to " + remote.TokenOwnersEnv + " to raise the limit, or retry later"
	default:
		return "retry later"
	}
}
