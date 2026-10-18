// Package theme loads theme files and resolves scopes to styles.
package theme

import (
	"errors"
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// Modifier is a set of text attributes.
type Modifier uint16

// Modifiers, named after the theme keys.
const (
	ModBold Modifier = 1 << iota
	ModDim
	ModItalic
	ModUnderlined
	ModSlowBlink
	ModRapidBlink
	ModReversed
	ModHidden
	ModCrossedOut
)

var modifierNames = map[string]Modifier{
	"bold":        ModBold,
	"dim":         ModDim,
	"italic":      ModItalic,
	"underlined":  ModUnderlined,
	"slow_blink":  ModSlowBlink,
	"rapid_blink": ModRapidBlink,
	"reversed":    ModReversed,
	"hidden":      ModHidden,
	"crossed_out": ModCrossedOut,
}

// Indexed is one of the 256 terminal palette colors.
type Indexed uint8

// RGBA implements color.Color. It is opaque black for every index, a
// placeholder only, rendering uses the ANSI palette index.
func (c Indexed) RGBA() (r, g, b, a uint32) {
	return color.RGBA{A: 0xff}.RGBA()
}

// reset marks the terminal default color.
type reset struct{}

func (reset) RGBA() (r, g, b, a uint32) {
	return 0, 0, 0, 0
}

// Reset means "use the terminal default".
var Reset color.Color = reset{}

// IsReset reports the terminal default color marker.
func IsReset(c color.Color) bool {
	_, ok := c.(reset)

	return ok
}

// Style is a partial text style. Nil colors mean "not set" so styles can
// be layered with Patch.
type Style struct {
	Fg             color.Color
	Bg             color.Color
	UnderlineColor color.Color
	UnderlineStyle string
	Add            Modifier
}

// Patch overlays other onto s. Set fields of other win.
func (s Style) Patch(other Style) Style {
	if other.Fg != nil {
		s.Fg = other.Fg
	}

	if other.Bg != nil {
		s.Bg = other.Bg
	}

	if other.UnderlineColor != nil {
		s.UnderlineColor = other.UnderlineColor
	}

	if other.UnderlineStyle != "" {
		s.UnderlineStyle = other.UnderlineStyle
	}

	s.Add |= other.Add

	return s
}

// Has reports whether a modifier is active.
func (s Style) Has(m Modifier) bool {
	return s.Add&m != 0
}

// IsZero reports an empty style.
func (s Style) IsZero() bool {
	return s == Style{}
}

var ansiNames = map[string]color.Color{
	"default":       Reset,
	"black":         Indexed(0),
	"red":           Indexed(1),
	"green":         Indexed(2),
	"yellow":        Indexed(3),
	"blue":          Indexed(4),
	"magenta":       Indexed(5),
	"cyan":          Indexed(6),
	"gray":          Indexed(8),
	"light-red":     Indexed(9),
	"light-green":   Indexed(10),
	"light-yellow":  Indexed(11),
	"light-blue":    Indexed(12),
	"light-magenta": Indexed(13),
	"light-cyan":    Indexed(14),
	"light-gray":    Indexed(7),
	"white":         Indexed(15),
}

// palette resolves color names from a theme's [palette] table.
type palette map[string]color.Color

func newPalette(values map[string]any) (palette, []string) {
	p := make(palette, len(ansiNames)+len(values))
	for k, v := range ansiNames {
		p[k] = v
	}

	var warnings []string
	for name, v := range values {
		s, ok := v.(string)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("palette color %q is not a string", name))

			continue
		}

		c, err := parseColorLiteral(s)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("palette color %q: %v", name, err))

			continue
		}

		p[name] = c
	}

	return p, warnings
}

func (p palette) color(v any) (color.Color, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("unrecognized color value %v", v)
	}

	if c, ok := p[s]; ok {
		return c, nil
	}

	return parseColorLiteral(s)
}

// parseColorLiteral reads "#rrggbb" or a 0-255 palette index.
func parseColorLiteral(s string) (color.Color, error) {
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		if len(hex) != 6 {
			return nil, fmt.Errorf("malformed hex color %q", s)
		}

		v, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return nil, fmt.Errorf("malformed hex color %q", s)
		}

		return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, nil
	}

	if idx, err := strconv.ParseUint(s, 10, 8); err == nil {
		return Indexed(idx), nil
	}

	return nil, fmt.Errorf("malformed ANSI color %q", s)
}

// parseStyle reads a theme value: a bare color string or a table with fg,
// bg, underline and modifiers.
func (p palette) parseStyle(v any) (Style, error) {
	var st Style
	table, ok := v.(map[string]any)
	if !ok {
		c, err := p.color(v)
		if err != nil {
			return st, err
		}

		st.Fg = c

		return st, nil
	}

	for name, value := range table {
		switch name {
		case "fg":
			c, err := p.color(value)
			if err != nil {
				return st, err
			}

			st.Fg = c
		case "bg":
			c, err := p.color(value)
			if err != nil {
				return st, err
			}

			st.Bg = c
		case "underline":
			ul, ok := value.(map[string]any)
			if !ok {
				return st, errors.New("underline must be a table")
			}

			for k, uv := range ul {
				switch k {
				case "color":
					c, err := p.color(uv)
					if err != nil {
						return st, err
					}

					st.UnderlineColor = c
				case "style":
					s, _ := uv.(string)
					st.UnderlineStyle = s
					st.Add |= ModUnderlined
				default:
					return st, fmt.Errorf("invalid underline attribute %q", k)
				}
			}
		case "modifiers":
			mods, ok := value.([]any)
			if !ok {
				return st, errors.New("modifiers should be an array")
			}

			for _, m := range mods {
				s, _ := m.(string)
				mod, ok := modifierNames[s]
				if !ok {
					return st, fmt.Errorf("invalid modifier %q", s)
				}

				st.Add |= mod
			}
		default:
			return st, fmt.Errorf("invalid style attribute %q", name)
		}
	}

	return st, nil
}
