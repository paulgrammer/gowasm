package tsmap

import (
	"reflect"
	"testing"
)

func TestCamelCase(t *testing.T) {
	cases := map[string]string{
		"GetPosts":    "getPosts",
		"ID":          "id",
		"HTMLParser":  "htmlParser",
		"ExtractURLs": "extractURLs",
		"URL":         "url",
		"A":           "a",
		"already":     "already",
		"":            "",
		"HTTPSProxy":  "httpsProxy",
		"Ping":        "ping",
	}
	for in, want := range cases {
		if got := CamelCase(in); got != want {
			t.Errorf("CamelCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJSONFieldName(t *testing.T) {
	cases := []struct {
		name     string
		tag      string
		wantName string
		optional bool
		skip     bool
	}{
		// With no json tag, encoding/json emits the Go name verbatim. Returning
		// a camelCased name here would make the generated type disagree with
		// the actual payload.
		{"Title", "", "Title", false, false},
		{"Title", `json:"title"`, "title", false, false},
		{"Title", `json:"title,omitempty"`, "title", true, false},
		{"Title", `json:"title,omitzero"`, "title", true, false},
		{"Title", `json:"-"`, "", false, true},
		// A field named "-" is spelled `json:"-,"`, which is not a skip.
		{"Dash", `json:"-,"`, "-", false, false},
		// Only the json tag's own options count. A validator rule that happens
		// to contain omitempty says nothing about JSON encoding.
		{"Title", `validate:"omitempty" json:"title"`, "title", false, false},
		// An empty name in the tag keeps the Go field name.
		{"Title", `json:",omitempty"`, "Title", true, false},
	}

	for _, c := range cases {
		lookup := reflect.StructTag(c.tag).Lookup
		name, optional, skip := JSONFieldName(c.name, lookup)
		if skip != c.skip {
			t.Errorf("tag %q: skip = %v, want %v", c.tag, skip, c.skip)
			continue
		}
		if skip {
			continue
		}
		if name != c.wantName {
			t.Errorf("tag %q: name = %q, want %q", c.tag, name, c.wantName)
		}
		if optional != c.optional {
			t.Errorf("tag %q: optional = %v, want %v", c.tag, optional, c.optional)
		}
	}
}
