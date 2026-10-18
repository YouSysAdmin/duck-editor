package ui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

// ModeKind selects the mode chip style.
type ModeKind int

// Mode kinds.
const (
	ModeNormal ModeKind = iota
	ModeInsert
	ModeSelect
)

// Status holds what the statusline elements can display.
type Status struct {
	Mode     string
	ModeKind ModeKind

	// Focused selects the active statusline style and shows the mode.
	Focused  bool
	Path     string
	BaseName string
	AbsPath  string
	Modified bool
	ReadOnly bool

	// AsRoot marks a file read and written through sudo.
	AsRoot bool

	// Encoding, LineEnding, IndentStyle and FileType describe the file.
	Encoding    string
	LineEnding  string
	IndentStyle string
	FileType    string
	Selections  int

	// PrimaryLen is the char count of the primary selection.
	PrimaryLen int

	// Line and Col are one-based for display.
	Line       int
	Col        int
	TotalLines int

	// Diagnostics counts per severity: hint, info, warning, error.
	Diagnostics          [4]int
	WorkspaceDiagnostics [4]int
	Register             string
	Cwd                  string
	Branch               string
	Spinner              string

	// Host names the remote machine of the file such as ssh://box, empty
	// for a local one.
	// HostDown is set while it cannot be reached.
	Host     string
	HostDown bool
}

// StatuslineLayout is the element configuration.
type StatuslineLayout struct {
	Left, Center, Right  []string
	Separator            string
	Diagnostics          []string
	WorkspaceDiagnostics []string
	ColorModes           bool
}

// RenderStatusline draws a single line of exactly width cells.
func RenderStatusline(s Status, layout StatuslineLayout, width int, th *Theme) string {
	line := th.Statusline
	if !s.Focused {
		line = th.StatuslineInact
	}

	r := statusRenderer{s: s, layout: layout, th: th, line: line}
	left := r.section(layout.Left)
	center := r.section(layout.Center)
	right := r.section(layout.Right) + r.host()

	lw, cw, rw := lipgloss.Width(left), lipgloss.Width(center), lipgloss.Width(right)
	if lw+cw+rw > width {
		// Too narrow, keep the left part and cut it hard.
		cut := ansi.Truncate(left, width, "")

		return cut + th.Render(line, strings.Repeat(" ", max(width-lipgloss.Width(cut), 0)))
	}

	if cw == 0 {
		return left + th.Render(line, strings.Repeat(" ", width-lw-rw)) + right
	}

	// Center the middle section on the whole width when it fits.
	gapLeft := max((width-cw)/2-lw, 1)
	gapRight := width - lw - gapLeft - cw - rw
	if gapRight < 1 {
		gapRight = 1
		gapLeft = max(width-lw-cw-rw-gapRight, 0)
	}

	return left + th.Render(line, strings.Repeat(" ", gapLeft)) + center + th.Render(line, strings.Repeat(" ", gapRight)) + right
}

type statusRenderer struct {
	s      Status
	layout StatuslineLayout
	th     *Theme
	line   theme.Style
}

// host marks a remote file at the right end, whatever the layout, and
// warns while the connection is down.
func (r statusRenderer) host() string {
	switch {
	case r.s.Host == "":
		return ""
	case r.s.HostDown:
		return r.th.Render(r.line.Patch(r.th.Error).Patch(theme.Style{Add: theme.ModBold}), " "+r.s.Host+": no connection ")
	}

	return r.th.Render(r.line.Patch(r.th.StatusSeparator), r.s.Host+" ")
}

// section renders elements separated by one space, a leading space in
// front and a trailing one after.
func (r statusRenderer) section(elements []string) string {
	var parts []string
	for _, el := range elements {
		if text := r.element(el); text != "" {
			parts = append(parts, text)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	sp := r.th.Render(r.line, " ")

	return sp + strings.Join(parts, sp) + sp
}

func (r statusRenderer) plain(text string) string {
	return r.th.Render(r.line, text)
}

func (r statusRenderer) element(name string) string {
	s := r.s
	switch name {
	case "mode":
		if !s.Focused {
			return r.plain(strings.Repeat(" ", lipgloss.Width(s.Mode)))
		}

		modeStyle := theme.Style{Add: theme.ModBold}
		if r.layout.ColorModes {
			switch s.ModeKind {
			case ModeInsert:
				modeStyle = r.th.ModeInsert
			case ModeSelect:
				modeStyle = r.th.ModeSelect
			default:
				modeStyle = r.th.ModeNormal
			}

			if modeStyle.IsZero() {
				modeStyle = theme.Style{Add: theme.ModBold}
			}
		}

		return r.th.Render(r.line.Patch(modeStyle), s.Mode)
	case "spinner":
		if s.Spinner == "" {
			return r.plain(" ")
		}

		return r.plain(s.Spinner)
	case "file-name":
		return r.plain(orScratch(s.Path))
	case "file-base-name":
		return r.plain(orScratch(s.BaseName))
	case "file-absolute-path":
		return r.plain(orScratch(s.AbsPath))
	case "file-modification-indicator":
		if s.Modified {
			return r.plain("[+]")
		}
	case "read-only-indicator":
		switch {
		case s.AsRoot:
			return r.plain("[root]")
		case s.ReadOnly:
			return r.plain("[readonly]")
		}
	case "file-encoding":
		// Only an unusual encoding is worth the space.
		if s.Encoding != "" && !strings.EqualFold(s.Encoding, "utf-8") {
			return r.plain(s.Encoding)
		}
	case "file-line-ending":
		return r.plain(s.LineEnding)
	case "file-indent-style":
		return r.plain(s.IndentStyle)
	case "file-type":
		return r.plain(s.FileType)
	case "diagnostics":
		return r.diagnostics(s.Diagnostics, r.layout.Diagnostics)
	case "workspace-diagnostics":
		if out := r.diagnostics(s.WorkspaceDiagnostics, r.layout.WorkspaceDiagnostics); out != "" {
			return r.plain("W ") + out
		}
	case "selections":
		// One selection is the usual case, the count shows from two.
		if s.Selections <= 1 {
			return ""
		}

		return r.plain(strconv.Itoa(s.Selections) + " sels")
	case "primary-selection-length":
		if s.PrimaryLen == 1 {
			return r.plain("1 char")
		}

		return r.plain(strconv.Itoa(s.PrimaryLen) + " chars")
	case "position":
		return r.plain(strconv.Itoa(s.Line) + ":" + strconv.Itoa(s.Col))
	case "position-percentage":
		if s.TotalLines > 0 {
			return r.plain(strconv.Itoa(s.Line*100/s.TotalLines) + "%")
		}
	case "total-line-numbers":
		return r.plain(strconv.Itoa(s.TotalLines))
	case "separator":
		return r.th.Render(r.line.Patch(r.th.StatusSeparator), r.layout.Separator)
	case "spacer":
		return r.plain(" ")
	case "version-control":
		return r.plain(s.Branch)
	case "register":
		if s.Register != "" {
			return r.plain("reg=" + s.Register)
		}
	case "current-working-directory":
		return r.plain(s.Cwd)
	}

	return ""
}

// diagnostics renders a dot and count per enabled severity.
func (r statusRenderer) diagnostics(counts [4]int, enabled []string) string {
	var parts []string
	for i := 3; i >= 0; i-- {
		name := [4]string{"hint", "info", "warning", "error"}[i]
		if counts[i] == 0 || !contains(enabled, name) {
			continue
		}

		parts = append(parts, r.th.Render(r.line.Patch(r.th.SeverityText[i]), "●")+r.plain(" "+strconv.Itoa(counts[i])))
	}

	return strings.Join(parts, r.plain(" "))
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}

	return false
}

func orScratch(s string) string {
	if s == "" {
		return "[scratch]"
	}

	return s
}

// BufferTab is one entry of the bufferline.
type BufferTab struct {
	Name     string
	Active   bool
	Modified bool
}

// RenderBufferline draws the open buffers in one row.
func RenderBufferline(tabs []BufferTab, width int, th *Theme) string {
	var sb strings.Builder
	used := 0
	for _, tab := range tabs {
		name := tab.Name
		if tab.Modified {
			name += "[+]"
		}

		text := " " + name + " "
		style := th.Bufferline
		if tab.Active {
			style = th.BufferlineActive
		}

		if used+lipgloss.Width(text) > width {
			text = ansi.Truncate(text, max(width-used, 0), "…")
		}

		sb.WriteString(th.Render(style, text))
		used += lipgloss.Width(text)
		if used >= width {
			break
		}
	}

	if used < width {
		sb.WriteString(th.Render(th.BufferlineBg, strings.Repeat(" ", width-used)))
	}

	return sb.String()
}

// CommandLine is the bottom row content.
type CommandLine struct {
	Prompt      string
	Input       string
	Message     string
	IsError     bool
	PendingKeys string

	// Shift is how many cells of a long prompt line scrolled out on the
	// left so the cursor stays on screen.
	Shift int
}

// RenderCommandLine draws the bottom row padded to width.
func RenderCommandLine(c CommandLine, width int, th *Theme) string {
	base := th.Base()
	var out string
	switch {
	case c.Prompt != "" && c.Shift > 0:
		out = th.Render(base, ansi.TruncateLeft(c.Prompt+c.Input, c.Shift+1, "…"))
	case c.Prompt != "":
		out = th.Render(base, c.Prompt+c.Input)
	case c.IsError:
		out = th.Render(th.Background.Patch(th.Error), c.Message)
	default:
		out = th.Render(base, c.Message)
	}

	if c.PendingKeys != "" {
		keys := th.Render(base, c.PendingKeys)
		pad := width - lipgloss.Width(out) - lipgloss.Width(keys) - 1
		if pad > 0 {
			return out + th.Render(base, strings.Repeat(" ", pad)) + keys + th.Render(base, " ")
		}
	}

	out = ansi.Truncate(out, width, "")

	return out + th.Render(base, strings.Repeat(" ", max(width-lipgloss.Width(out), 0)))
}

// MessageRows wraps a status message that does not fit one row into at
// most maxRows rows, a longer one keeps its end. A prompt or a message
// that fits gives one row.
func MessageRows(c CommandLine, width, maxRows int) []string {
	if c.Prompt != "" || c.Message == "" || width < 1 {
		return nil
	}

	var rows []string
	for line := range strings.SplitSeq(strings.TrimRight(c.Message, "\n"), "\n") {
		line = strings.ReplaceAll(line, "\t", "    ")
		rows = append(rows, strings.Split(ansi.Hardwrap(line, width, true), "\n")...)
	}

	if len(rows) > maxRows {
		rows = append([]string{"…"}, rows[len(rows)-maxRows+1:]...)
	}

	return rows
}

// RenderMessage draws the rows of a long status message in its style.
func RenderMessage(c CommandLine, rows []string, width int, th *Theme) string {
	style := th.Base()
	if c.IsError {
		style = th.Background.Patch(th.Error)
	}

	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = th.Render(style, r+strings.Repeat(" ", max(width-lipgloss.Width(r), 0)))
	}

	return strings.Join(out, "\n")
}

// Bordered wraps content in a single line box of the given style. The
// content must have rows of equal width.
func Bordered(content string, th *Theme, style theme.Style) string {
	rows := strings.Split(content, "\n")
	width := 0
	for _, r := range rows {
		width = max(width, lipgloss.Width(r))
	}

	var sb strings.Builder
	sb.WriteString(th.Render(style, "┌"+strings.Repeat("─", width)+"┐"))
	for _, r := range rows {
		sb.WriteByte('\n')
		sb.WriteString(th.Render(style, "│") + r + th.Render(style, strings.Repeat(" ", max(width-lipgloss.Width(r), 0))+"│"))
	}

	sb.WriteByte('\n')
	sb.WriteString(th.Render(style, "└"+strings.Repeat("─", width)+"┘"))

	return sb.String()
}
