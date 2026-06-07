package tsclient

import (
	"testing"

	"github.com/paulgrammer/gowasm/internal/scan"
	"github.com/paulgrammer/gowasm/internal/tsmap"
)

// The generated interface and the package's own types are exported from the
// same module, so a package named store that declares a type Store would
// declare that name twice and fail to compile.
func TestAPINameYieldsToTheUsersTypes(t *testing.T) {
	cases := []struct {
		why  string
		mod  *scan.Module
		want string
	}{
		{
			why:  "nothing in the way",
			mod:  &scan.Module{PkgName: "urls"},
			want: "Urls",
		},
		{
			why:  "a struct already holds the name",
			mod:  &scan.Module{PkgName: "store", Structs: []scan.Struct{{Name: "Store"}}},
			want: "StoreAPI",
		},
		{
			why:  "an enum already holds it",
			mod:  &scan.Module{PkgName: "casing", Enums: []scan.Enum{{Name: "Casing"}}},
			want: "CasingAPI",
		},
		{
			why: "both the name and the first fallback are taken",
			mod: &scan.Module{PkgName: "store", Structs: []scan.Struct{
				{Name: "Store"}, {Name: "StoreAPI"},
			}},
			want: "StoreAPI2",
		},
		{
			why:  "the time alias is declared",
			mod:  &scan.Module{PkgName: tsmap.ISODateTime, UsesISODateTime: true},
			want: tsmap.ISODateTime + "API",
		},
		{
			why:  "a package name with nothing usable in it",
			mod:  &scan.Module{PkgName: "_"},
			want: "API",
		},
	}
	for _, c := range cases {
		if got := apiName(c.mod); got != c.want {
			t.Errorf("%s: apiName = %q, want %q", c.why, got, c.want)
		}
	}
}

// Every namespace exports its own reserved names, so the note has to say which
// package it is talking about or it reads as the same warning three times.
func TestReservedNamesAreQualified(t *testing.T) {
	b := &scan.Bundle{Modules: []*scan.Module{
		{Namespace: "text", Funcs: []scan.Func{{GoName: "New", JSName: "new"}}},
		{Namespace: "store", Funcs: []scan.Func{
			{GoName: "New", JSName: "new"},
			{GoName: "Get", JSName: "get"},
		}},
	}}
	got := ReservedNames(b)
	want := []string{"New -> store.new", "New -> text.new"}
	if len(got) != len(want) {
		t.Fatalf("ReservedNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ReservedNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
