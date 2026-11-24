package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var errAmbiguousKey = errors.New("repeats a key")

// checkUniqueKeys reports an error when an object in data has a key twice,
// compared as encoding/json compares it to a field, ignoring case. The schema
// validator keeps the last of two such keys while the parser merges them, so
// what is validated wouldn't be what is used. The schemas themselves refuse a
// lone key of another case, as their objects take no other properties.
//
// Malformed JSON isn't reported: the validation that follows does that.
func checkUniqueKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // else a huge number would end the scan

	type container struct {
		object  bool
		wantKey bool
		keys    map[string]string // by foldKey, as the file spells it
	}
	var open []*container
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil //nolint:nilerr // the end, or malformed JSON
		}
		var cur *container
		if len(open) > 0 {
			cur = open[len(open)-1]
		}
		delim, isDelim := tok.(json.Delim)
		if isDelim && (delim == '}' || delim == ']') {
			open = open[:len(open)-1]
			continue
		}
		if cur != nil && cur.object {
			if cur.wantKey {
				key, ok := tok.(string)
				if !ok {
					return nil
				}
				folded := foldKey(key)
				if prev, dup := cur.keys[folded]; dup {
					return fmt.Errorf("%w (%q and %q)", errAmbiguousKey, prev, key)
				}
				cur.keys[folded] = key
				cur.wantKey = false
				continue
			}
			cur.wantKey = true // this token starts the key's value
		}
		if isDelim {
			open = append(open, &container{object: delim == '{', wantKey: delim == '{', keys: map[string]string{}})
		}
	}
}

// foldKey maps the keys encoding/json takes for one another to one string:
// each rune becomes the smallest of those it folds with, as strings.EqualFold
// compares them. Lowering isn't enough: it keeps \u017f apart from s.
func foldKey(key string) string {
	return strings.Map(func(r rune) rune {
		least := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			least = min(least, f)
		}
		return least
	}, key)
}
