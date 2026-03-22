package render

import (
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/entity"
)

func TestDefaultFormat(t *testing.T) {
	f := DefaultRuleIDFormatter()
	r := &entity.Rule{ID: "ENT-001", Revision: 0, State: "D"}
	got := f.Format(r)
	want := "[ENT-001.0/D]"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestFormatVariousStates(t *testing.T) {
	f := DefaultRuleIDFormatter()
	tests := []struct {
		state string
		want  string
	}{
		{"D", "[RUL-003.0/D]"},
		{"A", "[RUL-003.0/A]"},
		{"P", "[RUL-003.0/P]"},
		{"I", "[RUL-003.0/I]"},
		{"R", "[RUL-003.0/R]"},
		{"T", "[RUL-003.0/T]"},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			r := &entity.Rule{ID: "RUL-003", Revision: 0, State: tt.state}
			got := f.Format(r)
			if got != tt.want {
				t.Errorf("Format() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatWithRevision(t *testing.T) {
	f := DefaultRuleIDFormatter()
	r := &entity.Rule{ID: "TST-002", Revision: 3, State: "P"}
	got := f.Format(r)
	want := "[TST-002.3/P]"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestCustomBrackets(t *testing.T) {
	f := RuleIDFormatter{
		OpenBracket:      "(",
		CloseBracket:     ")",
		RevisionSplitter: ".",
		StateSplitter:    "/",
	}
	r := &entity.Rule{ID: "ENT-001", Revision: 0, State: "D"}
	got := f.Format(r)
	want := "(ENT-001.0/D)"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestCustomSplitters(t *testing.T) {
	f := RuleIDFormatter{
		OpenBracket:      "{",
		CloseBracket:     "}",
		RevisionSplitter: "#",
		StateSplitter:    "|",
	}
	r := &entity.Rule{ID: "ENT-001", Revision: 2, State: "I"}
	got := f.Format(r)
	want := "{ENT-001#2|I}"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}
