package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
		keys    []string
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
				for _, k := range cur.keys {
					if strings.EqualFold(k, key) {
						return fmt.Errorf("%w (%q and %q)", errAmbiguousKey, k, key)
					}
				}
				cur.keys = append(cur.keys, key)
				cur.wantKey = false
				continue
			}
			cur.wantKey = true // this token starts the key's value
		}
		if isDelim {
			open = append(open, &container{object: delim == '{', wantKey: delim == '{'})
		}
	}
}
