package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/rulerefs"
)

// ruleShowArgRe matches what rule show looks up: an EUID or a rule ID, either
// optionally qualified by a realm, in the syntax refs accepts. Group 1 is the
// realm, group 2 the EUID and group 3 the rule number.
var ruleShowArgRe = regexp.MustCompile(`^(?:([a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)*):)?([A-Za-z0-9]{3,18})(?:-(\d{3}))?$`)

type ruleShowOptions struct {
	jsonOutput bool
	states     []string
}

// shownRule is a rule as rule show prints it, with the realm and entity that
// hold it.
type shownRule struct {
	Realm      string      `json:"realm"`
	Entity     string      `json:"entity"`
	ID         string      `json:"id"`
	State      string      `json:"state"`
	Revision   int         `json:"revision"`
	Body       string      `json:"body"`
	AddedBy    string      `json:"added_by"`
	AddedAt    string      `json:"added_at"`
	Supersedes *string     `json:"supersedes"`
	Overrides  *string     `json:"overrides"`
	Notes      []shownNote `json:"notes"`
}

type shownNote struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Content string `json:"content"`
	AddedBy string `json:"added_by"`
	AddedAt string `json:"added_at"`
}

func newRuleShowCmd() *cobra.Command {
	var o ruleShowOptions

	cmd := &cobra.Command{
		Use:   "show <ID|EUID>...",
		Short: "Print rules by ID or entity",
		Long: "Print the ID, state, body and notes of rules, looked up by rule ID or by entity, so that\n" +
			"text can cite a rule instead of copying it.\n\n" +
			"Unlike the other rule subcommands, show reads .strspc/config.yaml in the current\n" +
			"directory, like refs, so the realm dependencies it pins are included. It has no --dir.\n\n" +
			"An argument is a rule ID such as MOD-010 or an EUID such as GOTEST, optionally qualified\n" +
			"by a realm, such as dev.steerspec.core:ENT-001. An unqualified one resolves to the first\n" +
			"source that holds it. An EUID selects the entity's own rules; a sub-entity is selected\n" +
			"by its own EUID. --state narrows the rules an EUID selects; a rule named by its ID is\n" +
			"shown whatever its state.\n\n" +
			"Exit codes: 0 found, 1 an argument matched no rule or entity (nothing is printed),\n" +
			"2 configuration or resolution error.\n\n" +
			"Examples:\n" +
			"  strspc rule show MOD-010\n" +
			"  strspc rule show GOTEST --state D\n" +
			"  strspc rule show dev.steerspec.core:ENT-001 RUL --json",
		Args: func(c *cobra.Command, args []string) error {
			return withExitCode(exitConfig, cobra.MinimumNArgs(1)(c, args))
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return withExitCode(exitConfig, fmt.Errorf("getting working directory: %w", err))
			}
			return runRuleShow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cwd, args, &o)
		},
	}

	cmd.Flags().BoolVar(&o.jsonOutput, "json", false, "output rules as JSON")
	cmd.Flags().StringSliceVar(&o.states, "state", nil, "only the rules of an entity in this state: D, A, P, I, R or T")

	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return withExitCode(exitConfig, err) })

	return cmd
}

// ruleShowArg is a parsed argument of rule show.
type ruleShowArg struct {
	raw   string
	realm string
	euid  string
	id    string // the rule ID; empty when the argument is an EUID
}

func parseRuleShowArg(s string) (ruleShowArg, error) {
	if err := rejectControlChars("argument", s); err != nil {
		return ruleShowArg{}, err
	}
	m := ruleShowArgRe.FindStringSubmatch(s)
	if m == nil {
		return ruleShowArg{}, fmt.Errorf("invalid argument %q: want a rule ID such as MOD-010 or an EUID such as MOD, optionally realm-qualified", s)
	}
	a := ruleShowArg{raw: s, realm: m[1], euid: m[2]}
	if m[3] != "" {
		a.id = m[2] + "-" + m[3]
	}
	return a, nil
}

func runRuleShow(ctx context.Context, out, errOut io.Writer, cwd string, args []string, o *ruleShowOptions) error {
	states := normalizeStateCodes(o.states)
	if err := validateStateFlag("--state", states); err != nil {
		return withExitCode(exitConfig, err)
	}
	parsed := make([]ruleShowArg, 0, len(args))
	for _, s := range args {
		a, err := parseRuleShowArg(s)
		if err != nil {
			return withExitCode(exitConfig, err)
		}
		parsed = append(parsed, a)
	}

	sources, err := refsSources(ctx, errOut, cwd)
	if err != nil {
		return withExitCode(exitConfig, err)
	}
	idx := newRuleShowIndex(sources)

	want := make(map[string]bool, len(states))
	for _, s := range states {
		want[s] = true
	}
	var (
		shown   []*shownRule
		unknown []string
	)
	seen := make(map[*shownRule]bool)
	add := func(r *shownRule) {
		if !seen[r] {
			seen[r] = true
			shown = append(shown, r)
		}
	}
	for _, a := range parsed {
		if a.id != "" {
			r := idx.rule(a.realm, a.id)
			if r == nil {
				unknown = append(unknown, a.raw)
				continue
			}
			add(r)
			continue
		}
		rules, ok := idx.entity(a.realm, a.euid)
		if !ok {
			unknown = append(unknown, a.raw)
			continue
		}
		for _, r := range rules {
			if len(want) == 0 || want[r.State] {
				add(r)
			}
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("no rule or entity named %s in the configured sources", strings.Join(unknown, ", "))
	}

	if o.jsonOutput {
		if shown == nil {
			shown = []*shownRule{}
		}
		if err := writeJSONOutput(out, shown); err != nil {
			return withExitCode(exitOutput, err)
		}
		return nil
	}
	writeShownRules(out, shown)
	return nil
}

// validateStateFlag reports a state code given to a flag that is not one of
// the rule lifecycle states.
func validateStateFlag(flag string, codes []string) error {
	for _, c := range codes {
		if !entity.IsValidState(c) {
			return fmt.Errorf("invalid %s state %q: allowed codes are %s (or %s)",
				flag, c, strings.Join(allStateCodes, ", "), strings.Join(allStateNames, ", "))
		}
	}
	return nil
}

// ruleShowIndex holds the rules and entities of the sources, by ID. An
// unqualified lookup finds the first source that holds the ID, the way
// rulerefs judges citations; a qualified one looks only in that realm.
type ruleShowIndex struct {
	rules         map[string]*shownRule
	entities      map[string][]*shownRule
	realmRules    map[string]map[string]*shownRule
	realmEntities map[string]map[string][]*shownRule
}

func newRuleShowIndex(sources []rulerefs.Source) *ruleShowIndex {
	idx := &ruleShowIndex{
		rules:         make(map[string]*shownRule),
		entities:      make(map[string][]*shownRule),
		realmRules:    make(map[string]map[string]*shownRule),
		realmEntities: make(map[string]map[string][]*shownRule),
	}
	for _, s := range sources {
		rules := make(map[string]*shownRule)
		entities := make(map[string][]*shownRule)
		for _, ef := range s.Entities {
			collectShownRules(ef, s.RealmID, rules, entities)
		}
		for id, r := range rules {
			if _, dup := idx.rules[id]; !dup {
				idx.rules[id] = r
			}
		}
		for euid, rs := range entities {
			if _, dup := idx.entities[euid]; !dup {
				idx.entities[euid] = rs
			}
		}
		if _, dup := idx.realmRules[s.RealmID]; s.RealmID != "" && !dup {
			idx.realmRules[s.RealmID] = rules
			idx.realmEntities[s.RealmID] = entities
		}
	}
	return idx
}

// collectShownRules adds the rules of ef and of its sub-entities. Within a
// source, the first entity or rule with an ID wins.
func collectShownRules(ef *entity.File, realm string, rules map[string]*shownRule, entities map[string][]*shownRule) {
	if ef == nil {
		return
	}
	_, dupEntity := entities[ef.Entity.ID]
	own := make([]*shownRule, 0, len(ef.Rules))
	for i := range ef.Rules {
		r := &ef.Rules[i]
		sr := &shownRule{
			Realm: realm, Entity: ef.Entity.ID, ID: r.ID, State: r.State, Revision: r.Revision,
			Body: r.Body, AddedBy: r.AddedBy, AddedAt: r.AddedAt, Supersedes: r.Supersedes, Overrides: r.Overrides,
			Notes: []shownNote{},
		}
		for _, n := range ef.Notes {
			if n.RuleRef == r.ID {
				sr.Notes = append(sr.Notes, shownNote{ID: n.ID, Type: n.Type, Content: n.Content, AddedBy: n.AddedBy, AddedAt: n.AddedAt})
			}
		}
		if _, dup := rules[r.ID]; !dup {
			rules[r.ID] = sr
		}
		own = append(own, sr)
	}
	if !dupEntity {
		entities[ef.Entity.ID] = own
	}
	for i := range ef.SubEntities {
		collectShownRules(&ef.SubEntities[i], realm, rules, entities)
	}
}

func (idx *ruleShowIndex) rule(realm, id string) *shownRule {
	if realm == "" {
		return idx.rules[id]
	}
	return idx.realmRules[realm][id]
}

func (idx *ruleShowIndex) entity(realm, euid string) ([]*shownRule, bool) {
	if realm == "" {
		rs, ok := idx.entities[euid]
		return rs, ok
	}
	rs, ok := idx.realmEntities[realm][euid]
	return rs, ok
}

// writeShownRules prints rules for a terminal. Their text can come from a
// remote source, so every line goes through printableText.
func writeShownRules(w io.Writer, rules []*shownRule) {
	for i, r := range rules {
		if i > 0 {
			writeln(w, "")
		}
		where := r.Entity
		if r.Realm != "" {
			where = r.Realm + " · " + r.Entity
		}
		writeln(w, fmt.Sprintf("%s [%s] %s", brandStyle.Render(printableText(r.ID)), printableText(r.State), descStyle.Render(printableText(where))))
		writeln(w, indentLines(r.Body, "  "))
		for _, n := range r.Notes {
			head := fmt.Sprintf("  %s %s: ", printableText(n.ID), printableText(n.Type))
			writeln(w, descStyle.Render(head)+strings.TrimPrefix(indentLines(n.Content, "    "), "    "))
		}
	}
}

// indentLines prefixes every line of s, made printable.
func indentLines(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + printableText(l)
	}
	return strings.Join(lines, "\n")
}
