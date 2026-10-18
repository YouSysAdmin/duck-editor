package editor

import "context"

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

// ApplyResult runs a job result on the editor.
func (e *Editor) ApplyResult(r JobResult) {
	r(e)
	e.afterCommand()
}
