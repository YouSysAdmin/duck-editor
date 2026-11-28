//go:build unix

package editor

import (
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestStopFollowsKillsGroup ends a task together with the processes its
// shell started.
func TestStopFollowsKillsGroup(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	taskProject(t, e, "serve: sleep 300 & echo $!; wait\n")
	e.runTyped(&Context{Editor: e}, "task serve")
	doc := e.taskDoc("serve")
	results := make(chan JobResult, 64)
	for _, job := range e.TakeJobs() {
		go job.Run(job.Ctx, func(r JobResult) { results <- r })
	}

	deadline := time.After(5 * time.Second)
	for doc.Text().Len() == 0 {
		select {
		case r := <-results:
			e.ApplyResult(r)
		case <-deadline:
			t.Fatal("no pid printed")
		}
	}

	pid, err := strconv.Atoi(strings.TrimSpace(doc.Text().String()))
	if err != nil {
		t.Fatal(err)
	}

	e.StopFollows()
	for !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		select {
		case <-deadline:
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatal("the child of the task shell survived")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
