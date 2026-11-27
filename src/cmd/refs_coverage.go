package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulerefs"
)

// refsConfigPath is where RF008 findings point: the coverage set they belong
// to is configured there.
const refsConfigPath = ".strspc/config.yaml"

// refsCoverageSet is a validated entry of refs.coverage.
type refsCoverageSet struct {
	field    string // refs.coverage[i], for errors
	entities []string
	files    []string // globs, relative to the project root
	states   []string // empty means rulerefs' default: D, P and I
}

// buildRefsCoverage validates refs.coverage. The config.v1 schema check only
// warns, so an entry that can't be run is a configuration error here.
func buildRefsCoverage(entries []refsCoverageConfig) ([]refsCoverageSet, error) {
	sets := make([]refsCoverageSet, 0, len(entries))
	for i, e := range entries {
		set := refsCoverageSet{field: fmt.Sprintf("refs.coverage[%d]", i)}
		for _, id := range e.Entities {
			if id = strings.TrimSpace(id); id != "" {
				set.entities = append(set.entities, id)
			}
		}
		if len(set.entities) == 0 {
			return nil, fmt.Errorf("%s in .strspc/config.yaml: entities must name at least one entity", set.field)
		}
		for _, g := range e.Files {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			if _, err := path.Match(g, ""); err != nil {
				return nil, fmt.Errorf("%s in .strspc/config.yaml: invalid files glob %q: %w", set.field, g, err)
			}
			set.files = append(set.files, g)
		}
		if len(set.files) == 0 {
			return nil, fmt.Errorf("%s in .strspc/config.yaml: files must list at least one glob", set.field)
		}
		set.states = normalizeStateCodes(e.States)
		if err := validateStateCodes(set.field+".states", set.states); err != nil {
			return nil, err
		}
		sets = append(sets, set)
	}
	return sets, nil
}

// runRefsCoverage checks each coverage set against checker: every rule of the
// set's entities, in its states, must be cited by one of the files its globs
// match in the whole project, whatever paths and patterns the main scan uses.
// Only RF008 comes from here: the citations in those files are judged by the
// main scan, when it scans them. A glob that matches no file is a
// configuration error, since the set would no longer read what it names.
func runRefsCoverage(cwd string, checker *rulerefs.Checker, sets []refsCoverageSet) ([]result.Diagnostic, error) {
	if len(sets) == 0 {
		return nil, nil
	}
	listed, err := refsListFiles(cwd, []string{"."})
	if err != nil {
		return nil, err
	}
	listed = slices.DeleteFunc(listed, func(f string) bool {
		info, err := os.Lstat(filepath.Join(cwd, filepath.FromSlash(f)))
		return err != nil || !info.Mode().IsRegular() // deleted, a link or a submodule
	})

	var diags []result.Diagnostic
	for _, set := range sets {
		var files []string
		for _, g := range set.files {
			n := len(files)
			for _, f := range listed {
				if refsGlobMatch([]string{g}, f) {
					files = append(files, f)
				}
			}
			if len(files) == n {
				return nil, fmt.Errorf("%s in .strspc/config.yaml: no file matches %q", set.field, printableText(g))
			}
		}
		slices.Sort(files)
		files = slices.Compact(files)

		checker.ResetCoverage()
		for _, f := range files {
			data, ok, err := readRefsFile(cwd, f)
			if err != nil {
				return nil, err
			}
			if ok {
				checker.CheckText(printableText(f), data)
			}
		}
		res, err := checker.Coverage(set.entities, set.states)
		if err != nil {
			return nil, fmt.Errorf("%s in .strspc/config.yaml: %w", set.field, err)
		}
		globs := make([]string, len(set.files))
		for i, g := range set.files {
			globs[i] = printableText(g)
		}
		for _, d := range res.Diagnostics {
			d.Path = refsConfigPath
			d.Message += ": " + strings.Join(globs, ", ")
			diags = append(diags, d)
		}
	}
	return diags, nil
}

// readRefsFile reads f, a slash path relative to cwd. ok is false for a binary
// file, which holds no citation.
func readRefsFile(cwd, f string) (data []byte, ok bool, err error) {
	data, err = os.ReadFile(filepath.Join(cwd, filepath.FromSlash(f)))
	if err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", printableText(f), err)
	}
	if bytes.IndexByte(data[:min(len(data), refsBinaryProbe)], 0) >= 0 {
		return nil, false, nil
	}
	return data, true, nil
}
