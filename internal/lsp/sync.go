package lsp

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// Text document sync kinds of textDocumentSync.change.
const (
	SyncNone        = 0
	SyncFull        = 1
	SyncIncremental = 2
)

// SyncOptions is what a server asked for in textDocumentSync: which
// document notifications it takes and how changes are sent.
type SyncOptions struct {
	// OpenClose sends didOpen and didClose.
	OpenClose bool

	// Change is the sync kind of didChange, SyncNone sends none.
	Change int

	// Save sends didSave, with the whole text when IncludeText is set.
	Save        bool
	IncludeText bool
}

// parseSync reads textDocumentSync, a sync kind number or an options
// object. The number is the older form of the capability: a kind other
// than none stands for open, close and save notifications without the
// text, as VS Code takes it. In the object form every notification is
// off unless named, so a server that leaves out openClose gets no
// documents, and nothing at all means no sync.
func parseSync(raw jsontext.Value) SyncOptions {
	s := string(raw)
	if s == "" || s == "null" {
		return SyncOptions{}
	}

	var kind int
	if json.Unmarshal(raw, &kind) == nil {
		if kind == SyncNone {
			return SyncOptions{}
		}

		return SyncOptions{OpenClose: true, Change: kind, Save: true}
	}

	var obj struct {
		OpenClose bool           `json:"openClose"`
		Change    int            `json:"change"`
		Save      jsontext.Value `json:"save"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return SyncOptions{}
	}

	out := SyncOptions{OpenClose: obj.OpenClose, Change: obj.Change}
	out.Save, out.IncludeText = parseSave(obj.Save)

	return out
}

// parseSave reads the save option, a boolean or SaveOptions.
func parseSave(raw jsontext.Value) (save, includeText bool) {
	var on bool
	if json.Unmarshal(raw, &on) == nil {
		return on, false
	}

	var opts struct {
		IncludeText bool `json:"includeText"`
	}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &opts) != nil {
		return false, false
	}

	return true, opts.IncludeText
}

// Sync returns the static document sync options the server negotiated,
// on the editor thread.
func (c *Client) Sync() SyncOptions {
	return c.sync
}

// syncFor returns the sync options of one document: the static ones
// with every matching dynamic registration applied. On the editor
// thread.
func (c *Client) syncFor(uri string) SyncOptions {
	out := c.sync
	languageID := c.docs[uri].languageID
	for _, r := range c.registrations {
		if !syncMethods[r.method] || !matchesDocument(r.selector, uri, languageID) {
			continue
		}

		switch r.method {
		case "textDocument/didOpen", "textDocument/didClose":
			out.OpenClose = true
		case "textDocument/didChange":
			var o struct {
				SyncKind int `json:"syncKind"`
			}
			if json.Unmarshal(r.options, &o) == nil {
				out.Change = o.SyncKind
			}
		case "textDocument/didSave":
			out.Save, out.IncludeText = parseSave(r.options)
		}
	}

	return out
}

// SyncFor returns the sync options of a document, for :info.
func (c *Client) SyncFor(uri string) SyncOptions {
	return c.syncFor(uri)
}

// docState is a document the editor opened on the server: its language
// and whether didOpen went out.
type docState struct {
	languageID string
	opened     bool
}
