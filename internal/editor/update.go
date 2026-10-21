package editor

import (
	"context"

	"github.com/YouSysAdmin/duck-editor/internal/update"
)

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"self-update", "upgrade"},
		doc:   "Download the latest release and replace the dk binary with it, the new version runs after a restart",
		run: func(cx *Context, _ []string) error {
			return cx.Editor.selfUpdate()
		},
	})
}

// CheckUpdate asks in the background whether a newer release exists,
// once a day, and says so in the statusline. A development build asks
// nothing.
func (e *Editor) CheckUpdate() {
	current, cacheDir := e.Version, e.stateDir
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		latest, newer, err := update.Check(ctx, cacheDir, current)
		emit(func(e *Editor) {
			switch {
			case err != nil:
				e.logf("update check: %v", err)
			case newer:
				e.SetStatus("dk %s is available, this is %s, :self-update installs it", latest, current)
			}
		})
	})
}

// selfUpdate installs the latest release over the running binary.
func (e *Editor) selfUpdate() error {
	current := e.Version
	if !update.IsVersion(current) {
		return cmdError("dk " + current + " is not a release build, task install builds and installs one")
	}

	e.SetStatus("looking for the latest dk release")
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		latest, err := update.Latest(ctx)
		if err != nil {
			emit(func(e *Editor) { e.SetError("update: %v", err) })

			return
		}

		if !update.Newer(current, latest) {
			emit(func(e *Editor) { e.SetStatus("dk %s is the latest release", current) })

			return
		}

		path, err := update.Install(ctx, latest, func(step string) {
			emit(func(e *Editor) { e.SetStatus("%s", step) })
		})
		emit(func(e *Editor) {
			if err != nil {
				e.SetError("update to dk %s: %v", latest, err)

				return
			}

			e.SetStatus("dk %s installed into %s, restart dk to run it", latest, path)
		})
	})

	return nil
}
