package editor

import (
	"fmt"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/lsp"
)

func init() {
	register("incoming_calls", "Pick a function that calls the one under the cursor", func(cx *Context) { callHierarchy(cx, true) })
	register("outgoing_calls", "Pick a function the one under the cursor calls", func(cx *Context) { callHierarchy(cx, false) })
}

// callHierarchy lists the callers or callees of the function under the
// cursor. A caller opens at its call, a callee at its name.
func callHierarchy(cx *Context, incoming bool) {
	e := cx.Editor
	doc, client, ok := requireClient(cx, featCallHierarchy)
	if !ok {
		return
	}

	client.PrepareCallHierarchy(e.lspPosition(doc, client), func(items []lsp.CallHierarchyItem, err error) {
		switch {
		case err != nil:
			e.SetError("%v", err)
		case len(items) == 0:
			e.SetStatus("No function under the cursor")
		default:
			client.Calls(items[0], incoming, func(calls []lsp.CallHierarchyCall, err error) {
				showCalls(e, client, items[0], incoming, calls, err)
			})
		}
	})
}

func showCalls(e *Editor, client *lsp.Client, of lsp.CallHierarchyItem, incoming bool, calls []lsp.CallHierarchyCall, err error) {
	if err != nil {
		e.SetError("%v", err)

		return
	}

	title := "Calls from " + of.Name
	if incoming {
		title = "Callers of " + of.Name
	}

	if len(calls) == 0 {
		e.SetStatus("%s: none", title)

		return
	}

	locs := make([]lsp.Location, 0, len(calls))
	items := make([]PickerItem, 0, len(calls))
	for _, call := range calls {
		item := call.To
		r := item.SelectionRange
		if incoming {
			item = call.From
			r = item.SelectionRange
			if len(call.FromRanges) > 0 {
				r = call.FromRanges[0]
			}
		}

		loc := lsp.Location{URI: item.URI, Range: r}
		label := item.Name
		if item.Detail != "" {
			label += " " + strings.TrimSpace(item.Detail)
		}

		label = fmt.Sprintf("%s  %s", label, locationLabel(e, loc))
		items = append(items, PickerItem{Label: label, Path: lsp.URIToPath(loc.URI), Line: r.Start.Line, Index: len(locs)})
		locs = append(locs, loc)
	}

	e.openPicker(NewPicker(title, items, func(cx *Context, item PickerItem, action PickerAction) {
		cx.Editor.jumpToLocation(locs[item.Index], client, actionFor(action))
	}))
}
