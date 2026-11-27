package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leejianrong/gitlabanak/internal/ci"
	"github.com/leejianrong/gitlabanak/internal/errs"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

const ciFile = ".gitlab-ci.yml"

// Pipeline is a pipeline held as data. Nothing runs it, so a job stays pending, created or
// manual until a runner reports otherwise (0.5.0).
type Pipeline struct {
	ID, IID, ProjectID int
	SHA, Ref, Source   string
	Tag                bool
	Status             string
	YamlErrors         string
	CreatedAt          time.Time
	Jobs               []*PipelineJob
}

type PipelineJob struct {
	ID           int
	Name, Stage  string
	Status       string
	Needs        []string
	AllowFailure bool
}

// pipelines is the per-project list. ids come from the server so they are unique across projects.
type pipelines struct {
	mu   sync.Mutex
	list []*Pipeline
}

// attach makes pushes, REST commits and branch creations on p create pipelines.
func (s *Server) attach(p *Project) {
	p.Store.OnRefUpdate(func(c gitstore.RefChange) { s.onRefUpdate(p, c) })
}

func (s *Server) onRefUpdate(p *Project, c gitstore.RefChange) {
	ref, isBranch := strings.CutPrefix(c.Ref, "refs/heads/")
	ref, isTag := strings.CutPrefix(ref, "refs/tags/")
	if !isBranch && !isTag {
		return
	}
	// A push without a CI file, or whose rules select no job, creates no pipeline.
	plan, commit, ok := s.plan(p, c.SHA, ref, isTag)
	if !ok || len(plan.Errors) == 0 && len(plan.Jobs) == 0 {
		return
	}
	p.pipes.add(s.newPipeline(p, c.SHA, ref, isTag, "push", commit.CommittedAt, plan))
}

// plan reads the CI file at sha and expands it for ref. ok is false when there is no file.
func (s *Server) plan(p *Project, sha, ref string, tag bool) (ci.Plan, gitstore.CommitDetail, bool) {
	f, err := p.Store.Read(sha, ciFile)
	if err != nil {
		return ci.Plan{}, gitstore.CommitDetail{}, false
	}
	commit, err := p.Store.CommitByID(sha)
	if err != nil {
		return ci.Plan{}, gitstore.CommitDetail{}, false
	}
	return ci.BuildPlan(string(f.Content), readLocal(p.Store, sha), s.predefined(p, commit, ref, tag, "push")), commit, true
}

func (s *Server) predefined(p *Project, c gitstore.CommitDetail, ref string, tag bool, source string) map[string]string {
	v := map[string]string{
		"GITLAB_CI": "true", "CI_SERVER": "yes",
		"CI_COMMIT_SHA": c.ID, "CI_COMMIT_SHORT_SHA": c.ID[:8], "CI_COMMIT_REF_NAME": ref,
		"CI_COMMIT_TITLE": c.Title, "CI_COMMIT_MESSAGE": c.Message,
		"CI_PROJECT_ID": strconv.Itoa(p.ID), "CI_PROJECT_PATH": p.Path, "CI_PROJECT_NAME": p.Path[strings.LastIndex(p.Path, "/")+1:],
		"CI_DEFAULT_BRANCH": p.DefaultBranch, "CI_PIPELINE_SOURCE": source,
	}
	if tag {
		v["CI_COMMIT_TAG"] = ref
	} else {
		v["CI_COMMIT_BRANCH"] = ref
	}
	return v
}

// newPipeline turns a plan into a pipeline: failed with yaml_errors when the plan has errors.
// created_at is the commit time, not the wall clock, so repeated runs give identical output.
func (s *Server) newPipeline(p *Project, sha, ref string, tag bool, source string, at time.Time, plan ci.Plan) *Pipeline {
	pl := &Pipeline{ID: int(s.pipelineIDs.Add(1)), ProjectID: p.ID, SHA: sha, Ref: ref, Tag: tag, Source: source, Status: "pending", CreatedAt: at}
	if len(plan.Errors) > 0 {
		pl.Status, pl.YamlErrors = "failed", strings.Join(plan.Errors, "\n")
		return pl
	}
	first := ""
	if len(plan.Jobs) > 0 {
		first = plan.Jobs[0].Stage
	}
	for _, j := range plan.Jobs {
		status := "created"
		switch {
		case j.When == "manual":
			status = "manual"
		case j.Stage == first && len(j.Needs) == 0:
			status = "pending"
		}
		pl.Jobs = append(pl.Jobs, &PipelineJob{ID: int(s.jobIDs.Add(1)), Name: j.Name, Stage: j.Stage, Status: status, Needs: j.Needs, AllowFailure: j.AllowFailure})
	}
	return pl
}

func (ps *pipelines) add(pl *Pipeline) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	pl.IID = len(ps.list) + 1
	ps.list = append(ps.list, pl)
}

func (ps *pipelines) snapshot() []*Pipeline {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return slices.Clone(ps.list)
}

func (ps *pipelines) get(id int) *Pipeline {
	for _, pl := range ps.snapshot() {
		if pl.ID == id {
			return pl
		}
	}
	return nil
}

func (s *Server) pipelineJSON(r *http.Request, p *Project, pl *Pipeline, full bool) map[string]any {
	t := pl.CreatedAt.UTC().Format(gitlabTime)
	out := map[string]any{
		"id": pl.ID, "iid": pl.IID, "project_id": pl.ProjectID, "sha": pl.SHA, "ref": pl.Ref,
		"status": pl.Status, "source": pl.Source, "created_at": t, "updated_at": t,
		"web_url": "http://" + r.Host + "/" + p.Path + "/-/pipelines/" + strconv.Itoa(pl.ID),
	}
	if full {
		var yamlErrors any
		if pl.YamlErrors != "" {
			yamlErrors = pl.YamlErrors
		}
		out["tag"], out["yaml_errors"] = pl.Tag, yamlErrors
		out["before_sha"] = strings.Repeat("0", 40)
		out["started_at"], out["finished_at"], out["committed_at"], out["duration"], out["queued_duration"], out["coverage"], out["user"] = nil, nil, nil, nil, nil, nil, nil
	}
	return out
}

func jobJSON(r *http.Request, p *Project, pl *Pipeline, j *PipelineJob) map[string]any {
	t := pl.CreatedAt.UTC().Format(gitlabTime)
	needs := j.Needs
	if needs == nil {
		needs = []string{}
	}
	return map[string]any{
		"id": j.ID, "status": j.Status, "stage": j.Stage, "name": j.Name, "ref": pl.Ref, "tag": pl.Tag,
		"coverage": nil, "allow_failure": j.AllowFailure, "created_at": t, "started_at": nil, "finished_at": nil,
		"duration": nil, "queued_duration": nil, "user": nil,
		"pipeline": map[string]any{"id": pl.ID, "project_id": pl.ProjectID, "ref": pl.Ref, "sha": pl.SHA, "status": pl.Status},
		"web_url":  "http://" + r.Host + "/" + p.Path + "/-/jobs/" + strconv.Itoa(j.ID),
		"needs":    needs, // not in GitLab's jobs API; lets tests see the dependency graph
	}
}

func (s *Server) listPipelines(w http.ResponseWriter, r *http.Request) {
	p := s.project(w, r)
	if p == nil {
		return
	}
	q := r.URL.Query()
	all := p.pipes.snapshot()
	var match []*Pipeline
	for _, pl := range all {
		if (q.Get("ref") == "" || q.Get("ref") == pl.Ref) && (q.Get("sha") == "" || q.Get("sha") == pl.SHA) &&
			(q.Get("status") == "" || q.Get("status") == pl.Status) && (q.Get("source") == "" || q.Get("source") == pl.Source) {
			match = append(match, pl)
		}
	}
	if q.Get("sort") != "asc" {
		slices.Reverse(match)
	}
	from, to := paginate(w, r, len(match))
	out := make([]map[string]any, 0, to-from)
	for _, pl := range match[from:to] {
		out = append(out, s.pipelineJSON(r, p, pl, false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) pipelineOr404(w http.ResponseWriter, r *http.Request) (*Project, *Pipeline) {
	p := s.project(w, r)
	if p == nil {
		return nil, nil
	}
	id, _ := strconv.Atoi(r.PathValue("pid"))
	pl := p.pipes.get(id)
	if pl == nil {
		errs.PipelineNotFound.Write(w)
	}
	return p, pl
}

func (s *Server) getPipeline(w http.ResponseWriter, r *http.Request) {
	if p, pl := s.pipelineOr404(w, r); pl != nil {
		writeJSON(w, http.StatusOK, s.pipelineJSON(r, p, pl, true))
	}
}

func (s *Server) pipelineJobs(w http.ResponseWriter, r *http.Request) {
	p, pl := s.pipelineOr404(w, r)
	if pl == nil {
		return
	}
	from, to := paginate(w, r, len(pl.Jobs))
	out := make([]map[string]any, 0, to-from)
	for _, j := range pl.Jobs[from:to] {
		out = append(out, jobJSON(r, p, pl, j))
	}
	writeJSON(w, http.StatusOK, out)
}

// createPipeline serves POST .../pipeline {ref}: a pipeline for the ref's head, source api.
// Unlike a push, a broken file or an empty pipeline is refused with 400 and nothing is created.
func (s *Server) createPipeline(w http.ResponseWriter, r *http.Request) {
	p := s.project(w, r)
	if p == nil {
		return
	}
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		var in struct {
			Ref string `json:"ref"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		ref = in.Ref
	}
	if ref == "" {
		errs.Missing("ref").Write(w)
		return
	}
	commit, err := p.Store.CommitByID(ref)
	if errors.Is(err, gitstore.ErrRefNotFound) {
		errs.WritePipelineBase(w, "Reference not found")
		return
	}
	f, err := p.Store.Read(commit.ID, ciFile)
	if err != nil {
		errs.WritePipelineBase(w, "Missing CI config file")
		return
	}
	vars := s.predefined(p, commit, ref, false, "api")
	plan := ci.BuildPlan(string(f.Content), readLocal(p.Store, commit.ID), vars)
	if len(plan.Errors) == 0 && len(plan.Jobs) == 0 {
		plan.Errors = []string{"No stages / jobs for this pipeline."}
	}
	if len(plan.Errors) > 0 {
		errs.WritePipelineBase(w, plan.Errors...)
		return
	}
	pl := s.newPipeline(p, commit.ID, ref, false, "api", commit.CommittedAt, plan)
	p.pipes.add(pl)
	writeJSON(w, http.StatusCreated, s.pipelineJSON(r, p, pl, true))
}
