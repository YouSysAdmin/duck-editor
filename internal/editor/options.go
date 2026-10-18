package editor

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/YouSysAdmin/duck-editor/internal/config"
)

// optionField finds the field of the editor config at a dotted path of
// yaml names such as lsp.display-inlay-hints.
func (e *Editor) optionField(key string) (reflect.Value, error) {
	v := reflect.ValueOf(&e.Config.Editor).Elem()
	for _, part := range strings.Split(strings.ToLower(key), ".") {
		if v.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf("unknown key `%s`", key)
		}

		found := false
		for i := range v.NumField() {
			tag := strings.Split(v.Type().Field(i).Tag.Get("yaml"), ",")[0]
			if tag == part {
				v = v.Field(i)
				found = true

				break
			}
		}

		if !found {
			return reflect.Value{}, fmt.Errorf("unknown key `%s`", key)
		}
	}

	return v, nil
}

// optionKeys lists every dotted option path for completion.
func optionKeys() []string {
	var out []string
	var walk func(t reflect.Type, prefix string)
	walk = func(t reflect.Type, prefix string) {
		for i := range t.NumField() {
			f := t.Field(i)
			tag := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if tag == "" {
				continue
			}

			if f.Type.Kind() == reflect.Struct {
				walk(f.Type, prefix+tag+".")

				continue
			}

			out = append(out, prefix+tag)
		}
	}
	walk(reflect.TypeOf(config.Editor{}), "")
	slices.Sort(out)

	return out
}

// setOption assigns a value written in yaml to an option.
func (e *Editor) setOption(key, raw string) error {
	field, err := e.optionField(key)
	if err != nil {
		return err
	}

	target := reflect.New(field.Type())
	if field.Kind() == reflect.String {
		target.Elem().SetString(raw)
	} else if err := yaml.Unmarshal([]byte(raw), target.Interface()); err != nil {
		return fmt.Errorf("could not parse field `%s`: %w", raw, err)
	}

	old := reflect.New(field.Type()).Elem()
	old.Set(field)
	field.Set(target.Elem())
	if err := e.Config.Editor.Validate(); err != nil {
		field.Set(old)

		return err
	}

	e.applyOptions()

	return nil
}

// toggleOption flips a bool or cycles through the given values.
func (e *Editor) toggleOption(key string, values []string) error {
	field, err := e.optionField(key)
	if err != nil {
		return err
	}

	// An optional boolean such as soft-wrap.enable counts as false while
	// it is unset.
	optional := field.Kind() == reflect.Pointer && field.Type().Elem().Kind() == reflect.Bool
	if field.Kind() == reflect.Bool || optional {
		if len(values) > 0 {
			return fmt.Errorf("bad arguments, for boolean configurations use: `:toggle %s`", key)
		}

		if optional {
			on := !field.IsNil() && field.Elem().Bool()
			next := !on
			field.Set(reflect.ValueOf(&next))
		} else {
			field.SetBool(!field.Bool())
		}

		e.applyOptions()
		if lang := e.Doc().language; key == "soft-wrap.enable" && lang != nil && lang.SoftWrap != nil {
			if _, set := lang.SoftWrap["enable"]; set {
				e.SetStatus("the language %s sets soft-wrap.enable itself, the option does not apply here", lang.Name)
			}
		}

		return nil
	}

	if len(values) == 0 {
		return fmt.Errorf("bad arguments, for %s configurations use: `:toggle %s val1 val2 ...`", field.Kind(), key)
	}

	current := e.optionString(field)
	next := values[0]
	for i, v := range values {
		if v == current && i+1 < len(values) {
			next = values[i+1]

			break
		}
	}

	return e.setOption(key, next)
}

// optionString renders an option value the way :set reads it.
func (e *Editor) optionString(field reflect.Value) string {
	if field.Kind() == reflect.String {
		return field.String()
	}

	out, err := yaml.Marshal(field.Interface())
	if err != nil {
		return fmt.Sprint(field.Interface())
	}

	return strings.TrimSpace(string(out))
}

// applyOptions re-reads the options other components cache.
func (e *Editor) applyOptions() {
	e.clipboard = nil
	e.gutters, e.gutterMinWidth = e.Config.Editor.GutterLayout()
}
