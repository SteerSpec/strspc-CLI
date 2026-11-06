package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/rulehistory"
)

type historyOptions struct {
	dir        string
	rule       string
	since      string
	between    string
	jsonOutput bool
}

// historyLongCommon is the part of the help that history and rule history share.
const historyLongCommon = "The history is derived from git: every commit that changed the entity file, oldest first,\n" +
	"with what it did to each rule (added, body_changed, promoted, retired, terminated, abandoned,\n" +
	"state_changed, metadata_changed, superseded or removed), the rule set version it produced\n" +
	"and the pull request named in its subject. Dates are commit dates in UTC. Renames of the\n" +
	"file are followed. Uncommitted changes are not shown.\n\n" +
	"It needs the full git history of a checkout: a shallow clone, a realm downloaded into the\n" +
	"cache or a file that was never committed is an error.\n\n" +
	"--since V keeps the changes that produced a rule set version above V. --between V1..V2 keeps\n" +
	"those above V1 and up to V2; either side may be empty.\n\n" +
	"Exit codes: 0 success (also when nothing matches), 1 unknown entity or malformed rule ID,\n" +
	"2 git, version or usage error."

func newHistoryCmd() *cobra.Command {
	var o historyOptions
	cmd := &cobra.Command{
		Use:   "history <EUID>",
		Short: "Show how an entity's rules changed, from git",
		Long: "List the changes to the rules of an entity, or of one of its rules with --rule.\n\n" +
			historyLongCommon + "\n\n" +
			"A sub-entity EUID shows the changes to that sub-entity only; a top-level EUID includes\n" +
			"its sub-entities.\n\n" +
			"Examples:\n" +
			"  strspc history CMD\n" +
			"  strspc history CMD --rule CMD-001 --json\n" +
			"  strspc history CFG --between 0.1.0..0.3.0",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          historyArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHistory(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], &o)
		},
	}
	addHistoryFlags(cmd, &o)
	cmd.Flags().StringVar(&o.rule, "rule", "", "only the changes to this rule, such as CMD-001")
	return cmd
}

func newRuleHistoryCmd() *cobra.Command {
	var o historyOptions
	cmd := &cobra.Command{
		Use:   "history <RULE-ID>",
		Short: "Show how a rule changed, from git",
		Long: "List the changes to one rule. This is strspc history <EUID> --rule <RULE-ID>; the\n" +
			"changes include the addition of a rule that supersedes it.\n\n" +
			historyLongCommon + "\n\n" +
			"Examples:\n" +
			"  strspc rule history CMD-001\n" +
			"  strspc rule history CMD-001 --since 0.2.0 --json",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          historyArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			euid, err := entityIDFromRuleID(args[0])
			if err != nil {
				return err
			}
			o.rule = args[0]
			return runHistory(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), euid, &o)
		},
	}
	addHistoryFlags(cmd, &o)
	return cmd
}

func historyArgs(c *cobra.Command, args []string) error {
	return withExitCode(exitConfig, cobra.ExactArgs(1)(c, args))
}

func addHistoryFlags(cmd *cobra.Command, o *historyOptions) {
	cmd.Flags().StringVar(&o.dir, "dir", "./rules", "realm directory")
	cmd.Flags().StringVar(&o.since, "since", "", "only changes that produced a rule set version above this one")
	cmd.Flags().StringVar(&o.between, "between", "", "only changes that produced a version in V1..V2 (above V1, up to V2)")
	cmd.Flags().BoolVar(&o.jsonOutput, "json", false, "output the history as JSON")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return withExitCode(exitConfig, err) })
}

// historyOutput is the JSON document history writes.
type historyOutput struct {
	Entity   string                `json:"entity"`
	Path     string                `json:"path"`
	Events   []rulehistory.Event   `json:"events"`
	Warnings []rulehistory.Warning `json:"warnings"`
}

func runHistory(ctx context.Context, out, errOut io.Writer, euid string, o *historyOptions) error {
	if err := rejectControlChars("EUID", euid); err != nil {
		return err
	}
	if o.since != "" && o.between != "" {
		return withExitCode(exitConfig, errors.New("--since and --between can't be used together"))
	}
	filter := rulehistory.Filter{Since: o.since}
	if o.between != "" {
		since, until, err := rulehistory.ParseRange(o.between)
		if err != nil {
			return withExitCode(exitConfig, err)
		}
		filter.Since, filter.Until = since, until
	}
	if o.rule != "" {
		if err := rejectControlChars("--rule", o.rule); err != nil {
			return err
		}
		if _, err := entityIDFromRuleID(o.rule); err != nil {
			return err
		}
		filter.RuleID = o.rule
	}

	target, err := resolveEntityTargetForRead(o.dir, euid)
	if err != nil {
		return err
	}
	filter.Entity = target.subID

	repo, rel, err := historyFile(ctx, target.path)
	if err != nil {
		return withExitCode(exitConfig, err)
	}
	commits, err := historyCommits(ctx, repo, rel)
	if err != nil {
		return withExitCode(exitConfig, err)
	}
	h, err := rulehistory.Build(commits).Filter(filter)
	if err != nil {
		return withExitCode(exitConfig, err)
	}
	if status, err := git(ctx, repo, "status", "--porcelain", "--", rel); err == nil && strings.TrimSpace(status) != "" {
		writeln(errOut, descStyle.Render(printableText(rel)+" has uncommitted changes, which are not shown"))
	}

	if o.jsonOutput {
		warnings := h.Warnings
		if warnings == nil {
			warnings = []rulehistory.Warning{}
		}
		doc := historyOutput{Entity: euid, Path: rel, Events: h.Events, Warnings: warnings}
		if err := writeJSONOutput(out, doc); err != nil {
			return withExitCode(exitOutput, err)
		}
		return nil
	}
	for _, w := range h.Warnings {
		writeln(errOut, descStyle.Render("warning: "+printableText(w.String())))
	}
	writeHistoryTable(out, euid, rel, h.Events)
	return nil
}

// historyFile checks that path can have a git history and returns the
// repository root and the path relative to it, with forward slashes. A
// linked entity file is followed: the link's own history says nothing about
// the rules.
func historyFile(ctx context.Context, path string) (repo, rel string, err error) {
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("resolving %s: %w", printableText(path), err)
	}
	repo, err = gitRoot(realPath)
	if err != nil {
		return "", "", fmt.Errorf("history needs a git checkout of the realm: %s is not in a git work tree", printableText(realPath))
	}
	rel, err = repoRelative(repo, realPath)
	if err != nil {
		return "", "", err
	}
	shallow, err := git(ctx, repo, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(shallow) == "true" {
		return "", "", errors.New("history needs the full git history, but this is a shallow clone: run `git fetch --unshallow` (in GitHub Actions, check out with fetch-depth: 0)")
	}
	if _, err := git(ctx, repo, "ls-files", "--error-unmatch", "--", rel); err != nil {
		return "", "", fmt.Errorf("no committed history for %s: the file is not tracked by git", printableText(rel))
	}
	return repo, rel, nil
}

// historyCommit is one entry of git log with the paths that hold the file
// at the commit and at its first parent.
type historyCommit struct {
	sha, parent, subject string
	date                 time.Time
	path, parentPath     string
}

// historyCommits lists the commits that changed rel, oldest first, with the
// file's content before and after each one.
//
// git log runs with its default history simplification: a merge that is
// TREESAME to one of its parents, such as a develop→main promotion, is
// pruned, so each change is listed once, at the commit that made it. Each
// commit is compared with its first parent.
func historyCommits(ctx context.Context, repo, rel string) ([]rulehistory.Commit, error) {
	logOut, err := git(ctx, repo, "log", "--follow", "--format=%x1e%H%x1f%P%x1f%cI%x1f%s", "--name-status", "--", rel)
	if err != nil {
		return nil, err
	}
	entries, err := parseHistoryLog(logOut, rel)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		// ls-files passes for a file that is staged but was never committed.
		return nil, fmt.Errorf("no committed history for %s: the file has not been committed yet", printableText(rel))
	}

	var specs []string
	for _, e := range entries {
		specs = append(specs, e.sha+":"+e.path)
		if e.parent != "" {
			specs = append(specs, e.parent+":"+e.parentPath)
		}
	}
	blobs, err := catFileBatch(ctx, repo, specs)
	if err != nil {
		return nil, err
	}

	commits := make([]rulehistory.Commit, 0, len(entries))
	for _, e := range entries {
		c := rulehistory.Commit{SHA: e.sha, Date: e.date, Subject: e.subject, After: blobs[e.sha+":"+e.path]}
		if e.parent != "" {
			c.Before = blobs[e.parent+":"+e.parentPath]
		}
		commits = append(commits, c)
	}
	slices.Reverse(commits)
	return commits, nil
}

// parseHistoryLog reads the output of historyCommits' git log, newest first.
// A rename line (R<score>, old, new) gives the path at the commit and at its
// parent; a commit without a status line, such as a merge, keeps the path of
// the newer commit.
func parseHistoryLog(out, rel string) ([]historyCommit, error) {
	var entries []historyCommit
	cur := rel
	for _, rec := range strings.Split(out, "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		lines := strings.Split(rec, "\n")
		fields := strings.Split(lines[0], "\x1f")
		if len(fields) != 4 {
			return nil, fmt.Errorf("reading git log: unexpected record %q", lines[0])
		}
		date, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			return nil, fmt.Errorf("reading git log: commit %s: %w", fields[0], err)
		}
		e := historyCommit{sha: fields[0], subject: fields[3], date: date, path: cur, parentPath: cur}
		if parents := strings.Fields(fields[1]); len(parents) > 0 {
			e.parent = parents[0]
		}
		for _, l := range lines[1:] {
			parts := strings.Split(l, "\t")
			if len(parts) < 2 {
				continue
			}
			e.path = parts[len(parts)-1]
			e.parentPath = e.path
			if strings.HasPrefix(parts[0], "R") && len(parts) == 3 {
				e.parentPath = parts[1]
			}
			break
		}
		cur = e.parentPath
		entries = append(entries, e)
	}
	return entries, nil
}

// catFileBatch reads the objects named by specs (<rev>:<path>) through one
// git cat-file --batch process. A missing object, such as the file before
// the commit that added it, is left out of the map.
func catFileBatch(ctx context.Context, repo string, specs []string) (map[string][]byte, error) {
	blobs := make(map[string][]byte, len(specs))
	if len(specs) == 0 {
		return blobs, nil
	}
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader(strings.Join(specs, "\n") + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	r := bufio.NewReader(stdout)
	readErr := func() error {
		for _, spec := range specs {
			header, err := r.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading %s: %w", spec, err)
			}
			f := strings.Fields(header)
			if len(f) >= 2 && f[len(f)-1] == "missing" {
				continue
			}
			if len(f) != 3 {
				return fmt.Errorf("reading %s: unexpected header %q", spec, strings.TrimSpace(header))
			}
			size, err := strconv.Atoi(f[2])
			if err != nil {
				return fmt.Errorf("reading %s: %w", spec, err)
			}
			buf := make([]byte, size+1) // the content and its trailing newline
			if _, err := io.ReadFull(r, buf); err != nil {
				return fmt.Errorf("reading %s: %w", spec, err)
			}
			if f[1] == "blob" {
				blobs[spec] = buf[:size]
			}
		}
		return nil
	}()
	_, _ = io.Copy(io.Discard, r)
	if err := cmd.Wait(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git cat-file: %s", msg)
		}
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	if readErr != nil {
		return nil, fmt.Errorf("git cat-file: %w", readErr)
	}
	return blobs, nil
}

// writeHistoryTable prints the events as a table, oldest first. Cells hold
// text from the repository, so each goes through printableText, and none is
// styled: escape codes would throw off the column widths.
func writeHistoryTable(w io.Writer, euid, rel string, events []rulehistory.Event) {
	writeln(w, brandStyle.Render(printableText(euid))+descStyle.Render(" ("+printableText(rel)+"), oldest first"))
	if len(events) == 0 {
		writeln(w, "No changes.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "DATE\tVERSION\tRULE\tREV\tCHANGE\tSTATE\tPR\tCOMMIT")
	for i := range events {
		ev := &events[i]
		date := ev.Date
		if len(date) >= 10 {
			date = date[:10]
		}
		pr := ""
		if ev.PR > 0 {
			pr = "#" + strconv.Itoa(ev.PR)
		}
		sha := ev.Commit
		if len(sha) > 7 {
			sha = sha[:7]
		}
		cells := []string{
			date, ev.RuleSetVersion, ev.RuleID, strconv.Itoa(ev.Revision), historyChange(ev),
			historyState(ev), pr, sha,
		}
		for j, c := range cells {
			cells[j] = printableText(c)
		}
		_, _ = fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	_ = tw.Flush()
}

// historyChange is the CHANGE cell: the kind, with the rule on the other
// side of a supersession.
func historyChange(ev *rulehistory.Event) string {
	switch {
	case ev.SupersededBy != "":
		return string(ev.Kind) + " by " + ev.SupersededBy
	case ev.Supersedes != "":
		return string(ev.Kind) + " (supersedes " + ev.Supersedes + ")"
	default:
		return string(ev.Kind)
	}
}

// historyState is the STATE cell: from→to on a change of state, the state
// before a removal, and the state after the commit otherwise.
func historyState(ev *rulehistory.Event) string {
	switch {
	case ev.FromState != "" && ev.ToState != "":
		return ev.FromState + "→" + ev.ToState
	case ev.FromState != "":
		return ev.FromState
	default:
		return ev.ToState
	}
}
