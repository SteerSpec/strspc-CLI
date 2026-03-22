package render

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"text/template"

	"github.com/SteerSpec/strspc-CLI/src/internal/entity"
)

//go:embed templates/default.md.tmpl
var defaultTemplateFS embed.FS

// Option configures a MarkdownRenderer.
type Option func(*MarkdownRenderer)

// WithTemplate overrides the built-in template with a custom template file.
func WithTemplate(path string) Option {
	return func(r *MarkdownRenderer) {
		r.templatePath = path
	}
}

// MarkdownRenderer renders entity files to Markdown using Go templates.
type MarkdownRenderer struct {
	formatter    RuleIDFormatter
	templatePath string

	tmplOnce sync.Once
	tmpl     *template.Template
	tmplErr  error
}

// NewMarkdownRenderer creates a new Markdown renderer with optional configuration.
func NewMarkdownRenderer(opts ...Option) *MarkdownRenderer {
	r := &MarkdownRenderer{
		formatter: DefaultRuleIDFormatter(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *MarkdownRenderer) getTemplate() (*template.Template, error) {
	r.tmplOnce.Do(func() {
		r.tmpl, r.tmplErr = r.parseTemplate()
	})
	return r.tmpl, r.tmplErr
}

// renderContext holds the data passed to the template.
type renderContext struct {
	entity.File
	Depth       int
	NotesByRule map[string][]entity.Note
	SubEntities []renderContext
}

func newRenderContext(ef *entity.File, depth int) renderContext {
	notesByRule := make(map[string][]entity.Note, len(ef.Notes))
	for _, n := range ef.Notes {
		notesByRule[n.RuleRef] = append(notesByRule[n.RuleRef], n)
	}

	subCtxs := make([]renderContext, len(ef.SubEntities))
	for i := range ef.SubEntities {
		subCtxs[i] = newRenderContext(&ef.SubEntities[i], depth+1)
	}

	return renderContext{
		File:        *ef,
		Depth:       depth,
		NotesByRule: notesByRule,
		SubEntities: subCtxs,
	}
}

// Render writes the entity file as Markdown to w.
func (r *MarkdownRenderer) Render(w io.Writer, ef *entity.File) error {
	tmpl, err := r.getTemplate()
	if err != nil {
		return err
	}

	ctx := newRenderContext(ef, 1)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	var output string
	if r.templatePath == "" {
		// Clean up excessive blank lines (3+ consecutive newlines → 2) for the default template.
		output = collapseBlankLines(buf.String())
	} else {
		// Preserve exact formatting for custom templates.
		output = buf.String()
	}

	_, err = io.WriteString(w, output)
	return err
}

func (r *MarkdownRenderer) parseTemplate() (*template.Template, error) {
	// cachedTmpl holds the parsed template so renderEntity doesn't re-parse.
	var cachedTmpl *template.Template

	funcMap := template.FuncMap{
		"heading": func(depth int) string {
			if depth > 6 {
				depth = 6
			}
			return strings.Repeat("#", depth)
		},
		"add": func(a, b int) int {
			return a + b
		},
		"formatRuleID": func(rule entity.Rule) string {
			return r.formatter.Format(&rule)
		},
		"hashOrNone": func(h *string) string {
			if h == nil {
				return "none"
			}
			return *h
		},
		"notesFor": func(notesByRule map[string][]entity.Note, ruleID string) []entity.Note {
			return notesByRule[ruleID]
		},
		"renderEntity": func(ctx renderContext) (string, error) {
			var buf bytes.Buffer
			if err := cachedTmpl.Execute(&buf, ctx); err != nil {
				return "", err
			}
			return buf.String(), nil
		},
	}

	var (
		data []byte
		err  error
		name string
	)

	if r.templatePath != "" {
		data, err = os.ReadFile(r.templatePath)
		name = "custom"
	} else {
		data, err = defaultTemplateFS.ReadFile("templates/default.md.tmpl")
		name = "default"
	}
	if err != nil {
		return nil, fmt.Errorf("reading template: %w", err)
	}

	cachedTmpl, err = template.New(name).Funcs(funcMap).Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}

	return cachedTmpl, nil
}

func collapseBlankLines(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	newlines := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			newlines++
			if newlines <= 2 {
				b.WriteByte('\n')
			}
		} else {
			newlines = 0
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
