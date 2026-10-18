package editor

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// kubeState is a buffer holding a Kubernetes object: saves show the diff
// against the cluster and replace the object after a yes.
type kubeState struct {
	resource string
	opts     []string
}

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"kube-edit", "kedit"},
		doc:   "Edit a Kubernetes object such as deploy/api, kubectl options such as -n ns pass through. A save shows the diff and replaces the object after a yes. Secrets open with their data as plain stringData",
		run: func(cx *Context, args []string) error {
			if len(args) == 0 {
				return errArgument("kube-edit", "a resource such as deploy/api")
			}

			cx.Editor.openKube(args[0], args[1:])

			return nil
		},
	})
}

// kubectl runs kubectl on the workspace host with stdin.
func kubectl(ctx context.Context, h host.Host, stdin string, args ...string) (string, string, error) {
	stdout, stderr, err := h.Run(ctx, host.Command{Name: "kubectl", Args: args, Stdin: []byte(stdin)})

	return string(stdout), strings.TrimSpace(string(stderr)), err
}

// openKube loads an object into a new buffer.
func (e *Editor) openKube(resource string, opts []string) {
	h := e.Host
	e.SetStatus("loading %s", resource)
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		text, err := fetchKube(ctx, h, resource, opts)
		emit(func(e *Editor) {
			if err != nil {
				e.SetError("%v", err)

				return
			}

			doc := NewDocument("", text)
			doc.host = h
			doc.kube = &kubeState{resource: resource, opts: opts}
			e.addDocument(doc)
			if e.Syntax != nil && e.Syntax.Languages() != nil {
				if lang, ok := e.Syntax.Languages().Get("yaml"); ok {
					e.attachLanguage(doc, lang)
				}
			}

			e.Switch(doc.ID, ActionReplace)
			e.SetStatus("%s loaded, :w shows the diff and applies it after a yes", resource)
		})
	})
}

// fetchKube reads an object as YAML, a secret with its data decoded.
func fetchKube(ctx context.Context, h host.Host, resource string, opts []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	out, stderr, err := kubectl(ctx, h, "", append([]string{"get", resource, "-o", "yaml"}, opts...)...)
	if err != nil {
		return "", fmt.Errorf("kubectl get %s: %s", resource, cmp.Or(stderr, err.Error()))
	}

	return secretToStringData(out), nil
}

// secretToStringData moves the text values of a Secret from base64 data
// to stringData, which the API server turns back into data. Values that
// are not text stay in data. Other objects come back unchanged.
func secretToStringData(text string) string {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil || len(doc.Content) == 0 {
		return text
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode || mappingValue(root, "kind") == nil || mappingValue(root, "kind").Value != "Secret" {
		return text
	}

	data := mappingValue(root, "data")
	if data == nil || data.Kind != yaml.MappingNode {
		return text
	}

	strData := &yaml.Node{Kind: yaml.MappingNode}
	var keep []*yaml.Node
	for i := 0; i+1 < len(data.Content); i += 2 {
		k, v := data.Content[i], data.Content[i+1]
		raw, err := base64.StdEncoding.DecodeString(v.Value)
		if err != nil || !utf8.Valid(raw) {
			keep = append(keep, k, v)

			continue
		}

		value := &yaml.Node{Kind: yaml.ScalarNode, Value: string(raw)}
		if strings.Contains(value.Value, "\n") {
			value.Style = yaml.LiteralStyle
		}

		strData.Content = append(strData.Content, k, value)
	}

	if len(strData.Content) == 0 {
		return text
	}

	data.Content = keep
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "data" {
			if len(keep) == 0 {
				root.Content = append(root.Content[:i], root.Content[i+2:]...)
			}

			break
		}
	}

	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "stringData"}, strData)
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return text
	}

	return out.String()
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}

	return nil
}

// applyKube shows the diff of the buffer against the cluster and, after
// a yes, replaces the object. The buffer is loaded again afterwards, so
// the next save carries the new resourceVersion. then runs once the
// object is applied, or right away when nothing changed.
func (e *Editor) applyKube(doc *Document, then func()) {
	h, docID, text, k := e.Host, doc.ID, doc.Text().String(), doc.kube
	if doc.host != nil {
		h = doc.host
	}

	edits := doc.edits
	e.SetStatus("comparing %s with the cluster", k.resource)
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		ctx, cancel := context.WithTimeout(ctx, shellTimeout)
		defer cancel()

		diff, stderr, err := kubectl(ctx, h, text, append([]string{"diff", "-f", "-"}, k.opts...)...)
		var exitErr *host.ExitError
		changed := errors.As(err, &exitErr) && exitErr.Code == 1
		emit(func(e *Editor) {
			doc, ok := e.Docs[docID]
			if !ok {
				return
			}

			switch {
			case err != nil && !changed:
				e.SetError("kubectl diff %s: %s", k.resource, cmp.Or(stderr, err.Error()))

				return
			case !changed:
				doc.markSaved()
				e.SetStatus("%s has no changes against the cluster", k.resource)
				if then != nil {
					then()
				}

				return
			case doc.edits != edits:
				e.SetError("the text changed while comparing, :w again")

				return
			}

			e.Popup = &Popup{Lines: strings.Split(strings.TrimRight(diff, "\n"), "\n")}
			e.SetStatus("replace %s in the cluster with these changes? y/n", k.resource)
			e.onNextKey = func(cx *Context, key keymap.Key) {
				if ch, ok := key.Char(); !ok || ch != 'y' && ch != 'Y' {
					cx.Editor.SetStatus("not applied")

					return
				}

				cx.Editor.replaceKube(docID, text, then)
			}
		})
	})
}

// replaceKube replaces the object with text and loads it again.
func (e *Editor) replaceKube(docID DocID, text string, then func()) {
	doc, ok := e.Docs[docID]
	if !ok {
		return
	}

	h, k := doc.host, doc.kube
	e.SetStatus("replacing %s", k.resource)
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		ctx, cancel := context.WithTimeout(ctx, shellTimeout)
		defer cancel()

		_, stderr, err := kubectl(ctx, h, text, append([]string{"replace", "-f", "-"}, k.opts...)...)
		var fresh string
		var ferr error
		if err == nil {
			fresh, ferr = fetchKube(ctx, h, k.resource, k.opts)
		}

		emit(func(e *Editor) {
			doc, ok := e.Docs[docID]
			if !ok {
				return
			}

			if err != nil {
				e.SetError("kubectl replace %s: %s", k.resource, cmp.Or(stderr, err.Error()))

				return
			}

			if ferr == nil {
				old := doc.Text()
				tx := edit.ChangeText(old, []edit.Change{{From: 0, To: old.Len(), Text: fresh}})
				doc.applyQuiet(&tx)
				doc.SetSelection(doc.Selection().EnsureInvariants(doc.Text()))
			}

			doc.markSaved()
			e.SetStatus("%s replaced in the cluster", k.resource)
			if then != nil {
				then()
			}
		})
	})
}
