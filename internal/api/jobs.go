package api

import (
	"fmt"
	"slices"
)

// A job moves created -> pending -> running -> success or failed. A job that can no longer
// run because an earlier one failed goes created or pending -> skipped. Manual jobs stay
// manual: nothing starts them.
var transitions = map[string][]string{
	"created": {"pending", "skipped"},
	"pending": {"running", "skipped"},
	"running": {"success", "failed"},
}

func canMove(from, to string) bool { return slices.Contains(transitions[from], to) }

// move changes a job's status if the transition is allowed. Callers hold s.runMu.
func (s *Server) move(j *PipelineJob, to string) error {
	if !canMove(j.Status, to) {
		return fmt.Errorf("job %d cannot go from %s to %s", j.ID, j.Status, to)
	}
	now := s.now()
	switch to {
	case "running":
		j.StartedAt = now
	case "success", "failed":
		j.FinishedAt = now
	}
	j.Status = to
	return nil
}

// settled reports whether a job no longer holds up what comes after it.
func settled(j *PipelineJob) bool {
	return j.Status == "success" || j.Status == "manual" || j.Status == "failed" && j.AllowFailure
}

// blocked reports whether a job's outcome means later jobs that depend on it cannot run.
func blocked(j *PipelineJob) bool {
	return j.Status == "skipped" || j.Status == "failed" && !j.AllowFailure
}

// advance starts the jobs that became ready, skips the ones that never will run, and
// recomputes the pipeline status. Callers hold s.runMu.
func (s *Server) advance(pl *Pipeline) {
	for changed := true; changed; {
		changed = false
		for _, j := range pl.Jobs {
			if j.Status != "created" {
				continue
			}
			var deps []*PipelineJob
			if len(j.Needs) > 0 {
				for _, n := range j.Needs {
					for _, o := range pl.Jobs {
						if o.Name == n {
							deps = append(deps, o)
						}
					}
				}
			} else {
				idx := slices.Index(pl.Stages, j.Stage)
				for _, o := range pl.Jobs {
					if slices.Index(pl.Stages, o.Stage) < idx {
						deps = append(deps, o)
					}
				}
			}
			switch {
			case slices.ContainsFunc(deps, blocked):
				_ = s.move(j, "skipped")
				changed = true
			case !slices.ContainsFunc(deps, func(o *PipelineJob) bool { return !settled(o) }):
				_ = s.move(j, "pending")
				changed = true
			}
		}
	}
	pl.Status = deriveStatus(pl.Jobs)
}

// deriveStatus is the pipeline status implied by its jobs.
func deriveStatus(jobs []*PipelineJob) string {
	var running, pending, created, started, failed, manual bool
	for _, j := range jobs {
		switch j.Status {
		case "running":
			running = true
		case "pending":
			pending = true
		case "created":
			created = true
		case "manual":
			manual = true
		}
		if j.Status == "failed" && !j.AllowFailure {
			failed = true
		}
		if j.Status != "pending" && j.Status != "created" && j.Status != "manual" {
			started = true
		}
	}
	switch {
	case running, (pending || created) && started:
		return "running"
	case pending || created:
		return "pending"
	case failed:
		return "failed"
	case manual:
		return "manual"
	}
	return "success"
}

// claim hands the oldest pending job to a runner. Callers hold s.runMu.
func (s *Server) claim(runner int) *jobRef {
	var best *jobRef
	for _, r := range s.jobIndex {
		if r.job.Status == "pending" && (best == nil || r.job.ID < best.job.ID) {
			best = r
		}
	}
	if best == nil {
		return nil
	}
	_ = s.move(best.job, "running")
	best.job.Runner = runner
	s.advance(best.pipeline)
	return best
}
