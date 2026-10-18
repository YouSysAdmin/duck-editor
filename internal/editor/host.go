package editor

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// SetHost makes h the workspace host with dir as the working directory,
// used for a session on a remote machine.
func (e *Editor) SetHost(h host.Host, dir string) {
	e.Host = h
	e.workDir = dir
	if h.Name() == "" {
		e.workDir = ""
	}

	// Scratch buffers are saved on the workspace host.
	for _, doc := range e.Docs {
		if doc.path == "" {
			doc.host = h
		}
	}
}

// Remote reports whether the workspace lives on another machine.
func (e *Editor) Remote() bool {
	return e.Host.Name() != ""
}

// WorkingDir returns the working directory on the workspace host, "."
// when it is unknown.
func (e *Editor) WorkingDir() string {
	if e.workDir != "" {
		return e.workDir
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	return cwd
}

// absPath resolves path against the working directory of the workspace.
func (e *Editor) absPath(path string) string {
	// ~ is the home directory of the workspace host.
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := e.Host.Home(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}

	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	return filepath.Join(e.WorkingDir(), path)
}

// workspaceConfigDir returns the .duck directory above the working
// directory on the workspace host, "" when there is none.
func (e *Editor) workspaceConfigDir() string {
	if !e.Remote() {
		return config.WorkspaceDir()
	}

	return FindWorkspaceConfig(e.Host, e.WorkingDir())
}

// FindWorkspaceConfig returns the .duck directory above dir on h, "" when
// there is none.
func FindWorkspaceConfig(h host.Host, dir string) string {
	var candidates []string
	for ; ; dir = filepath.Dir(dir) {
		candidates = append(candidates, filepath.Join(dir, ".duck"))
		if filepath.Dir(dir) == dir {
			break
		}
	}

	found, _ := h.Exists(candidates)
	for i, ok := range found {
		if ok {
			return candidates[i]
		}
	}

	return ""
}

// ConfigFiles lists the user file name and the one of the workspace .duck
// directory wsDir on h, in the order they apply.
func ConfigFiles(h host.Host, wsDir, name string) []host.File {
	files := []host.File{{Path: filepath.Join(config.Dir(), name)}}
	if wsDir != "" {
		files = append(files, host.File{Host: h, Path: filepath.Join(wsDir, name)})
	}

	return files
}

// hostOf returns the host holding the file of doc.
func (e *Editor) hostOf(doc *Document) host.Host {
	if doc != nil && doc.host != nil {
		return doc.host
	}

	return e.Host
}

// exists reports whether path exists on h.
func exists(h host.Host, path string) bool {
	_, err := h.Stat(path)

	return err == nil
}

// isDir reports whether path is a directory on h.
func isDir(h host.Host, path string) bool {
	info, err := h.Stat(path)

	return err == nil && info.IsDir()
}

// GotoPosition puts the cursor of the focused document on line and
// column, both one based and clamped to the text, for dk file:line:col.
func (e *Editor) GotoPosition(line, col int) {
	e.GotoPositionIn(e.Doc(), line, col)
	e.EnsureCursorInView()
}

// GotoPositionIn puts the cursor of doc in the focused view at a 1-based
// line and column, for a document opened behind the shown one.
func (e *Editor) GotoPositionIn(doc *Document, line, col int) {
	text := doc.Text()
	l := min(max(line-1, 0), text.LenLines()-1)
	start := text.LineToChar(l)
	length := utf8.RuneCountInString(text.LineWithoutEnding(l))
	doc.SetSelectionFor(e.Focus, edit.PointSelection(start+min(max(col-1, 0), length)))
}

// AddDocument opens a document behind the shown one, as :open does for
// a file that stays in the background. A file already open comes back
// as the open document.
func (e *Editor) AddDocument(doc *Document) *Document {
	if doc.Path() != "" {
		if open, ok := e.documentOn(e.hostOf(doc), e.absPath(doc.Path())); ok {
			return open
		}
	}

	e.addDocument(doc)

	return doc
}
