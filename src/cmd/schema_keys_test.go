package cmd

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
)

func TestCheckUniqueKeysScalesLinearly(t *testing.T) {
	var b strings.Builder
	b.WriteString("{")
	for i := range 100_000 {
		b.WriteString(`"key` + strconv.Itoa(i) + `":0,`)
	}
	b.WriteString(`"last":0}`)
	data := []byte(b.String())

	done := make(chan error, 1)
	go func() { done <- checkUniqueKeys(data) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("distinct keys refused: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("100,000 keys took over 10s")
	}
}

// The folded set must match what encoding/json matches, beyond ASCII too.
func TestCheckUniqueKeysFoldsAsEncodingJSON(t *testing.T) {
	for _, pair := range [][2]string{
		{"state", "STATE"}, {"state", "\u017ftate"}, {"kind", "\u212aind"}, {"\u03c3", "\u03c2"}, {"\u03a3", "\u03c2"},
	} {
		data := `{"` + pair[0] + `":1,"` + pair[1] + `":2}`
		if err := checkUniqueKeys([]byte(data)); !errors.Is(err, errAmbiguousKey) {
			t.Errorf("%s: error = %v, want errAmbiguousKey", data, err)
		}
	}
	if err := checkUniqueKeys([]byte(`{"a":{"x":1},"b":{"x":2},"c":[{"x":3},{"x":4}]}`)); err != nil {
		t.Errorf("the same key in other objects: %v", err)
	}
}

func TestCheckRuleTellsCopiesApart(t *testing.T) {
	v := newSchemaValidator(context.Background())
	used := inherit.Extended{ID: "dev.lib:ACCT-002", Body: "Old."}
	copyWith := func(body string) *entity.File {
		return &entity.File{Rules: []entity.Rule{{ID: "ACCT-002", Body: body}}}
	}
	tree := func(body string) realmCopy { return realmCopy{"ACCT": {copyWith(body)}} }
	v.trees[realmName{"./rules/", "dev.lib"}] = []realmCopy{tree("Old."), tree("New.")}
	if err := v.checkRule("./rules/", "dev.app", used); !errors.Is(err, errDifferingCopies) {
		t.Errorf("two releases in one source: error = %v, want errDifferingCopies", err)
	}
	v.trees[realmName{"./one/", "dev.lib"}] = []realmCopy{tree("New.")}
	v.trees[realmName{"./two/", "dev.lib"}] = []realmCopy{tree("Old.")}
	if err := v.checkRule("./one/", "dev.app", used); !errors.Is(err, errNotValidated) {
		t.Errorf("another source's release vouched: error = %v", err)
	}
}
