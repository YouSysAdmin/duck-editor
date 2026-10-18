package config

import (
	"reflect"
	"regexp"
	"testing"
)

// TestDefaultFileMatchesDefaults keeps the documented config in step with
// the defaults in code: it parses to exactly Default().
func TestDefaultFileMatchesDefaults(t *testing.T) {
	t.Parallel()

	got, err := Parse(DefaultFile)
	if err != nil {
		t.Fatal(err)
	}

	want := Default()
	gv, wv := reflect.ValueOf(got.Editor), reflect.ValueOf(want.Editor)
	for i := range gv.NumField() {
		if !reflect.DeepEqual(gv.Field(i).Interface(), wv.Field(i).Interface()) {
			t.Errorf("%s: file %#v, code %#v", gv.Type().Field(i).Name, gv.Field(i).Interface(), wv.Field(i).Interface())
		}
	}

	if got.Theme != want.Theme {
		t.Errorf("theme %q, code %q", got.Theme, want.Theme)
	}
}

// TestDefaultFileNamesEveryOption lists every option of the config, set
// or commented out with an example.
func TestDefaultFileNamesEveryOption(t *testing.T) {
	t.Parallel()

	var walk func(t reflect.Type, path string)
	var missing []string
	walk = func(typ reflect.Type, path string) {
		for i := range typ.NumField() {
			f := typ.Field(i)
			name := f.Tag.Get("yaml")
			if name == "" || name == "-" {
				continue
			}

			if !regexp.MustCompile(`(?m)^\s*#?\s*` + regexp.QuoteMeta(name) + `:`).MatchString(DefaultFile) {
				missing = append(missing, path+name)
			}

			if f.Type.Kind() == reflect.Struct {
				walk(f.Type, path+name+".")
			}
		}
	}
	walk(reflect.TypeOf(Editor{}), "editor.")

	for _, m := range missing {
		t.Errorf("default.yaml does not mention %s", m)
	}
}
