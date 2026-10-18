// Package keymap parses key names and resolves key sequences
// to command names through mode specific tries.
package keymap

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Modifier is a set of modifier keys.
type Modifier uint8

// Modifier bits.
const (
	ModShift Modifier = 1 << iota
	ModAlt
	ModCtrl
	ModSuper
)

// Special key codes live in the Unicode private use area so a Key can
// hold either a printable char or one of these.
const (
	KeyBackspace rune = 0xE000 + iota
	KeyEnter
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyTab
	KeyDelete
	KeyInsert
	KeyNull
	KeyEsc

	// KeyF1 is the first function key, KeyF1 + n - 1 is Fn.
	KeyF1
)

// KeyF returns the code of function key n.
func KeyF(n int) rune {
	return KeyF1 + rune(n-1)
}

// Key is one key press with modifiers.
type Key struct {
	Code rune
	Mods Modifier
}

// Char creates a plain character key.
func Char(ch rune) Key {
	return Key{Code: ch}
}

// Ctrl creates a control chord.
func Ctrl(ch rune) Key {
	return Key{Code: ch, Mods: ModCtrl}
}

// Alt creates an alt chord.
func Alt(ch rune) Key {
	return Key{Code: ch, Mods: ModAlt}
}

var specialNames = map[string]rune{
	"backspace": KeyBackspace,
	"ret":       KeyEnter,
	"left":      KeyLeft,
	"right":     KeyRight,
	"up":        KeyUp,
	"down":      KeyDown,
	"home":      KeyHome,
	"end":       KeyEnd,
	"pageup":    KeyPageUp,
	"pagedown":  KeyPageDown,
	"tab":       KeyTab,
	"del":       KeyDelete,
	"ins":       KeyInsert,
	"null":      KeyNull,
	"esc":       KeyEsc,
	"space":     ' ',
	"minus":     '-',
	"lt":        '<',
	"gt":        '>',
}

var specialCodes = func() map[rune]string {
	out := make(map[rune]string, len(specialNames))
	for name, code := range specialNames {
		out[code] = name
	}

	return out
}()

// Parse reads a key in notation such as "C-x", "A-ret" or "space".
func Parse(s string) (Key, error) {
	if s == "-" {
		return Char('-'), nil
	}

	tokens := strings.Split(s, "-")
	last := tokens[len(tokens)-1]
	tokens = tokens[:len(tokens)-1]

	var code rune
	switch {
	case last == "":
		if strings.HasSuffix(s, "-") && len(tokens) > 0 {
			return Key{}, fmt.Errorf("key '-' cannot be used with modifiers, use '%s-minus' instead", strings.TrimSuffix(s, "-"))
		}

		return Key{}, fmt.Errorf("missing key code in %q", s)
	case specialNames[last] != 0:
		code = specialNames[last]
	case utf8.RuneCountInString(last) == 1:
		code, _ = utf8.DecodeRuneInString(last)
	case len(last) > 1 && last[0] == 'F':
		n, err := strconv.Atoi(last[1:])
		if err != nil || n < 1 || n > 24 {
			return Key{}, fmt.Errorf("invalid function key %q", last)
		}

		code = KeyF(n)
	default:
		return Key{}, fmt.Errorf("invalid key code %q", last)
	}

	var mods Modifier
	for _, tok := range tokens {
		var flag Modifier
		switch tok {
		case "S":
			flag = ModShift
		case "A":
			flag = ModAlt
		case "C":
			flag = ModCtrl
		case "Meta", "Cmd", "Win":
			flag = ModSuper
		default:
			return Key{}, fmt.Errorf("invalid key modifier %q", tok+"-")
		}

		if mods&flag != 0 {
			return Key{}, fmt.Errorf("repeated key modifier %q", tok+"-")
		}

		mods |= flag
	}

	return Key{Code: code, Mods: mods}.Normalize(), nil
}

// MustParse is Parse for literals and panics on error.
func MustParse(s string) Key {
	k, err := Parse(s)
	if err != nil {
		panic(err)
	}

	return k
}

// Normalize folds shift into lowercase ASCII letters so that S-a and A
// are the same key.
func (k Key) Normalize() Key {
	if k.Mods&ModShift != 0 && k.Code >= 'a' && k.Code <= 'z' {
		k.Code = unicode.ToUpper(k.Code)
		k.Mods &^= ModShift
	}

	return k
}

// IsChar reports a printable char without ctrl or alt.
func (k Key) IsChar() bool {
	return k.Code < 0xE000 && k.Mods&(ModCtrl|ModAlt|ModSuper) == 0
}

// Char returns the printable char of the key, if it is one. Modifiers are
// ignored.
func (k Key) Char() (rune, bool) {
	if k.Code >= 0xE000 && k.Code <= KeyF(24) {
		return 0, false
	}

	return k.Code, true
}

// String renders the key in key notation.
func (k Key) String() string {
	var sb strings.Builder
	if k.Mods&ModSuper != 0 {
		sb.WriteString("Meta-")
	}

	if k.Mods&ModShift != 0 {
		sb.WriteString("S-")
	}

	if k.Mods&ModAlt != 0 {
		sb.WriteString("A-")
	}

	if k.Mods&ModCtrl != 0 {
		sb.WriteString("C-")
	}

	switch {
	case specialCodes[k.Code] != "":
		sb.WriteString(specialCodes[k.Code])
	case k.Code >= KeyF1 && k.Code <= KeyF(24):
		fmt.Fprintf(&sb, "F%d", k.Code-KeyF1+1)
	default:
		sb.WriteRune(k.Code)
	}

	return sb.String()
}
