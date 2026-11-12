package editor

import (
	"context"
	"time"
)

// JobResult is applied on the editor when a background job reports.
type JobResult func(e *Editor)

// Job runs in the background and emits results to apply later.
type Job func(ctx context.Context, emit func(JobResult))

// QueuedJob is a job together with the context that cancels it.
type QueuedJob struct {
	Run Job
	Ctx context.Context
}

// TakeJobs returns the jobs queued since the last call. The UI layer runs
// them and feeds their results back through ApplyResult.
func (e *Editor) TakeJobs() []QueuedJob {
	jobs := e.jobs
	e.jobs = nil

	return jobs
}

func (e *Editor) queueJob(ctx context.Context, job Job) {
	e.jobs = append(e.jobs, QueuedJob{Run: job, Ctx: ctx})
}

// debounce cancels the job behind cancel and queues its successor, which
// starts after delay unless it is cancelled first too. cancel is set to
// the cancel func of the new job.
func (e *Editor) debounce(cancel *context.CancelFunc, delay time.Duration, job Job) {
	if *cancel != nil {
		(*cancel)()
	}

	ctx, stop := context.WithCancel(context.Background())
	*cancel = stop
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}

		job(ctx, emit)
	})
}

// ApplyResult runs a job result on the editor.
func (e *Editor) ApplyResult(r JobResult) {
	r(e)
	e.afterCommand()
}
