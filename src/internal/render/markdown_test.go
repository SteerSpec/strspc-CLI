package render

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/entity"
)

var update = flag.Bool("update", false, "update golden files")

func testRenderGolden(t *testing.T, name string) {
	t.Helper()

	jsonPath := filepath.Join("testdata", name+".json")
	goldenPath := filepath.Join("testdata", name+".md")

	ef, err := entity.Load(jsonPath)
	if err != nil {
		t.Fatalf("loading %s: %v", jsonPath, err)
	}

	r := NewMarkdownRenderer()
	var buf bytes.Buffer
	if err := r.Render(&buf, ef); err != nil {
		t.Fatalf("rendering %s: %v", name, err)
	}
	got := buf.String()

	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("updating golden file: %v", err)
		}
		t.Logf("updated %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file %s: %v (run with -update to generate)", goldenPath, err)
	}

	if got != string(want) {
		t.Errorf("output mismatch for %s.\nGot:\n%s\nWant:\n%s", name, got, string(want))
	}
}

func TestRenderBasic(t *testing.T) {
	testRenderGolden(t, "basic")
}

func TestRenderNested(t *testing.T) {
	testRenderGolden(t, "nested")
}

func TestRenderEmpty(t *testing.T) {
	testRenderGolden(t, "empty")
}

func TestRenderDeterministic(t *testing.T) {
	ef, err := entity.Load("testdata/basic.json")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	r := NewMarkdownRenderer()
	var buf1, buf2 bytes.Buffer
	if err := r.Render(&buf1, ef); err != nil {
		t.Fatalf("render 1: %v", err)
	}
	if err := r.Render(&buf2, ef); err != nil {
		t.Fatalf("render 2: %v", err)
	}

	if buf1.String() != buf2.String() {
		t.Error("rendering is not deterministic: two renders produced different output")
	}
}

func TestRenderUnsupportedFormat(t *testing.T) {
	_, err := New("html")
	if err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
}

func TestRenderCustomTemplate(t *testing.T) {
	customTmpl := filepath.Join(t.TempDir(), "custom.md.tmpl")
	err := os.WriteFile(customTmpl, []byte(`# {{.Entity.ID}}: {{.Entity.Title}}
`), 0o644)
	if err != nil {
		t.Fatalf("writing custom template: %v", err)
	}

	ef, err := entity.Load("testdata/basic.json")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	r := NewMarkdownRenderer(WithTemplate(customTmpl))
	var buf bytes.Buffer
	if err := r.Render(&buf, ef); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	got := buf.String()
	want := "# TST: Test Entity\n"
	if got != want {
		t.Errorf("custom template output = %q, want %q", got, want)
	}
}

func TestNewMarkdownFormat(t *testing.T) {
	r, err := New(FormatMarkdown)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("renderer is nil")
	}
}
