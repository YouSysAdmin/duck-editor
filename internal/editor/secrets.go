package editor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// secretTimeout bounds decrypting and encrypting, a key service such as
// KMS or Vault may be slow to answer.
const secretTimeout = 60 * time.Second

// secretState marks a document whose file is encrypted with sops or
// ansible-vault. The buffer holds the decrypted text, saves encrypt it.
type secretState struct {
	// tool is sops or ansible-vault.
	tool string

	// ready is set once the text is decrypted.
	ready bool
}

// secretTool names the program that encrypted a file, empty for a plain
// file.
func secretTool(text string) string {
	switch {
	case strings.HasPrefix(text, "$ANSIBLE_VAULT;"):
		return "ansible-vault"
	case strings.Contains(text, "ENC[AES256_GCM,") &&
		(strings.Contains(text, "\nsops:") || strings.Contains(text, `"sops":`) || strings.Contains(text, "sops_mac=")):
		return "sops"
	}

	return ""
}

// detectSecret decrypts a freshly opened encrypted file in the
// background. The buffer is read only until then, and language servers
// never see the text.
func (e *Editor) detectSecret(doc *Document) {
	if doc.Path() == "" || doc.tail != nil {
		return
	}

	tool := secretTool(doc.Text().String())
	if tool == "" {
		return
	}

	e.closeLSP(doc)
	doc.secret = &secretState{tool: tool}
	h := e.hostOf(doc)
	if _, err := h.LookPath(tool); err != nil {
		doc.readOnly = true
		e.SetError("%s is encrypted with %s, which is not installed, the buffer shows it as it is", doc.displayName(), tool)

		return
	}

	e.decryptSecret(doc)
}

// decryptSecret replaces the buffer with the decrypted file.
func (e *Editor) decryptSecret(doc *Document) {
	doc.readOnly = true
	doc.secret.ready = false
	h, path, tool, asRoot, docID := e.hostOf(doc), doc.Path(), doc.secret.tool, doc.asRoot, doc.ID
	cmd := host.Command{Name: "sops", Args: []string{"--decrypt", "--", path}}
	if tool == "ansible-vault" {
		cmd = host.Command{Name: "ansible-vault", Args: []string{"view", "--", path}}
	}

	if asRoot {
		cmd = host.Command{Name: sudoCommand, Args: append([]string{"-n", cmd.Name}, cmd.Args...)}
	}

	e.SetStatus("decrypting %s with %s", doc.displayName(), tool)
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		ctx, cancel := context.WithTimeout(ctx, secretTimeout)
		defer cancel()

		stdout, stderr, err := h.Run(ctx, cmd)
		emit(func(e *Editor) {
			doc, ok := e.Docs[docID]
			if !ok || doc.secret == nil {
				return
			}

			if err != nil {
				msg := strings.TrimSpace(string(stderr))
				if msg == "" {
					msg = err.Error()
				}

				e.SetError("%s could not decrypt %s, the buffer stays read only: %s", tool, doc.displayName(), firstLines(msg, 2))

				return
			}

			old := doc.Text()
			tx := edit.ChangeText(old, []edit.Change{{From: 0, To: old.Len(), Text: string(stdout)}})
			doc.applyQuiet(&tx)
			doc.markSaved()
			doc.rememberDiskTime()
			doc.readOnly = false
			doc.secret.ready = true
			doc.SetSelection(edit.PointSelection(0))
			e.SetStatus("%s decrypted with %s, saves encrypt it again", doc.displayName(), tool)
		})
	})
}

// secretEditScript runs the edit mode of sops or ansible-vault with an
// editor that writes the text from stdin, so the tool encrypts it with
// the keys and data key of the file. The text only touches a private
// temporary directory. sops exits with 200 for an unchanged file.
const secretEditScript = `set -e
umask 077
tool=$1 file=$2
d=$(mktemp -d)
trap 'rm -rf "$d"' EXIT
cat > "$d/plain"
printf '#!/bin/sh\ncat "%s" > "$1"\n' "$d/plain" > "$d/editor"
chmod 700 "$d/editor"
export EDITOR="$d/editor" SOPS_EDITOR="$d/editor"
set +e
if [ "$tool" = sops ]; then sops "$file"; else ansible-vault edit "$file"; fi
s=$?
if [ "$tool" = sops ] && [ $s -eq 200 ]; then s=0; fi
exit $s`

// saveSecret encrypts the buffer into its file.
func (e *Editor) saveSecret(doc *Document) error {
	if !doc.secret.ready {
		return cmdError(doc.displayName() + " is not decrypted, it is not written")
	}

	e.prepareForSave(doc)
	cmd := host.Command{Name: "sh", Args: []string{"-c", secretEditScript, "sh", doc.secret.tool, doc.Path()}, Stdin: []byte(doc.Text().String())}
	if doc.asRoot {
		cmd = host.Command{Name: sudoCommand, Args: append([]string{"-n", cmd.Name}, cmd.Args...), Stdin: cmd.Stdin}
	}

	ctx, cancel := context.WithTimeout(context.Background(), secretTimeout)
	defer cancel()

	_, stderr, err := e.hostOf(doc).Run(ctx, cmd)
	if err != nil {
		msg := strings.TrimSpace(string(stderr))
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = fmt.Sprintf("no answer within %s", secretTimeout)
		}

		return fmt.Errorf("%s could not encrypt %s: %s", doc.secret.tool, doc.displayName(), firstLines(cmp.Or(msg, err.Error()), 2))
	}

	doc.AppendChangesToHistory()
	doc.markSaved()
	doc.rememberDiskTime()
	e.SetStatus("'%s' written, encrypted with %s", doc.displayName(), doc.secret.tool)

	return nil
}
