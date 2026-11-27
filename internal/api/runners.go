package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/leejianrong/gitlabanak/internal/errs"
)

// Runner-facing routes authenticate with the runner's token in the body, or the job's token in
// the JOB-TOKEN header, not PRIVATE-TOKEN. docs/spike-runner.md records what gitlab-runner sends.
func isRunnerRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v4/runners") || strings.HasPrefix(path, "/api/v4/jobs/")
}

func randomToken() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// runners gives each distinct runner token a number. Any non-empty token is accepted: there is
// no way to create a runner in the UI of a test double.
type runners struct {
	mu  sync.Mutex
	ids map[string]int
}

func (r *runners) id(token string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ids == nil {
		r.ids = map[string]int{}
	}
	if _, ok := r.ids[token]; !ok {
		r.ids[token] = len(r.ids) + 1
	}
	return r.ids[token]
}

func runnerToken(r *http.Request) string {
	var in struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	return in.Token
}

// registerRunner serves POST /runners (registration) and POST /runners/verify.
func (s *Server) registerRunner(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := runnerToken(r)
		if tok == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "403 Forbidden"})
			return
		}
		writeJSON(w, status, map[string]any{"id": s.registry.id(tok), "token": tok, "token_expires_at": nil})
	}
}

func (s *Server) unregisterRunner(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// requestJob serves POST /jobs/request: the oldest pending job, or 204.
func (s *Server) requestJob(w http.ResponseWriter, r *http.Request) {
	tok := runnerToken(r)
	if tok == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "403 Forbidden"})
		return
	}
	s.runMu.Lock()
	ref := s.claim(s.registry.id(tok))
	var payload map[string]any
	if ref != nil {
		payload = s.jobPayload(r, ref)
	}
	s.runMu.Unlock()
	if ref == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusCreated, payload)
}

// jobPayload is the job as gitlab-runner expects to receive it. Callers hold s.runMu.
func (s *Server) jobPayload(r *http.Request, ref *jobRef) map[string]any {
	j, pl, p := ref.job, ref.pipeline, ref.project
	vars := map[string]string{}
	for k, v := range pl.Vars {
		vars[k] = v
	}
	if m, ok := j.Config["variables"].(map[string]any); ok {
		for k, v := range m {
			if mv, ok := v.(map[string]any); ok {
				v = mv["value"]
			}
			vars[k] = fmt.Sprint(v)
		}
	}
	base := "http://" + r.Host
	repo := "http://gitlab-ci-token:" + j.Token + "@" + r.Host + "/" + p.Path + ".git"
	for k, v := range map[string]string{
		"CI_JOB_ID": strconv.Itoa(j.ID), "CI_JOB_NAME": j.Name, "CI_JOB_STAGE": j.Stage, "CI_JOB_TOKEN": j.Token,
		"CI_PIPELINE_ID": strconv.Itoa(pl.ID), "CI_PIPELINE_IID": strconv.Itoa(pl.IID), "CI_SERVER_URL": base,
		"CI_REPOSITORY_URL": repo, "CI_API_V4_URL": base + "/api/v4", "CI_JOB_STATUS": "running",
	} {
		vars[k] = v
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	list := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		list = append(list, map[string]any{"key": k, "value": vars[k], "public": !strings.Contains(k, "TOKEN"), "masked": strings.Contains(k, "TOKEN")})
	}

	refspec, refType := "+refs/heads/"+pl.Ref+":refs/remotes/origin/"+pl.Ref, "branch"
	if pl.Tag {
		refspec, refType = "+refs/tags/"+pl.Ref+":refs/tags/"+pl.Ref, "tag"
	}
	out := map[string]any{
		"id": j.ID, "token": j.Token, "allow_git_fetch": true,
		"job_info":    map[string]any{"name": j.Name, "stage": j.Stage, "project_id": p.ID, "project_name": p.Path[strings.LastIndex(p.Path, "/")+1:]},
		"git_info":    map[string]any{"repo_url": repo, "ref": pl.Ref, "sha": pl.SHA, "before_sha": strings.Repeat("0", 40), "ref_type": refType, "refspecs": []string{refspec}, "depth": 0},
		"runner_info": map[string]any{"timeout": 3600},
		"variables":   list,
		"steps":       steps(j.Config),
		"services":    []any{},
		"cache":       []any{},
		"credentials": []any{},
		"features":    map[string]any{},
	}
	switch img := j.Config["image"].(type) {
	case string:
		out["image"] = map[string]any{"name": img}
	case map[string]any:
		out["image"] = img
	}
	if a, ok := j.Config["artifacts"].(map[string]any); ok {
		a["artifact_type"], a["artifact_format"] = "archive", "zip"
		if _, set := a["when"]; !set {
			a["when"] = "on_success"
		}
		out["artifacts"] = []any{a}
	}
	out["dependencies"] = s.dependencies(ref)
	return out
}

// steps maps before_script and script to the runner's "script" step, and after_script to its own.
func steps(cfg map[string]any) []map[string]any {
	script := append(lines(cfg["before_script"]), lines(cfg["script"])...)
	out := []map[string]any{{"name": "script", "script": script, "timeout": 3600, "when": "on_success", "allow_failure": false}}
	if after := lines(cfg["after_script"]); len(after) > 0 {
		out = append(out, map[string]any{"name": "after_script", "script": after, "timeout": 300, "when": "always", "allow_failure": true})
	}
	return out
}

func lines(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, lines(e)...)
		}
		return out
	}
	return nil
}

// dependencies lists the jobs whose artifacts this job should download: dependencies: when
// set, else its needs, else every job of an earlier stage that uploaded something.
func (s *Server) dependencies(ref *jobRef) []map[string]any {
	j, pl := ref.job, ref.pipeline
	var from []*PipelineJob
	idx := slices.Index(pl.Stages, j.Stage)
	for _, o := range pl.Jobs {
		var want bool
		switch deps := j.Config["dependencies"].(type) {
		case []any:
			want = slices.Contains(lines(deps), o.Name)
		default:
			if len(j.Needs) > 0 {
				want = slices.Contains(j.Needs, o.Name)
			} else {
				want = slices.Index(pl.Stages, o.Stage) < idx
			}
		}
		if want && o.Artifact != nil {
			from = append(from, o)
		}
	}
	out := []map[string]any{}
	for _, o := range from {
		out = append(out, map[string]any{"id": o.ID, "name": o.Name, "token": o.Token,
			"artifacts_file": map[string]any{"filename": o.Artifact.Filename, "size": len(o.Artifact.Data)}})
	}
	return out
}

// jobByToken finds the job a JOB-TOKEN header or body token belongs to.
func (s *Server) jobFor(r *http.Request, token string) *jobRef {
	id, err := strconv.Atoi(r.PathValue("jid"))
	ref := s.jobIndex[id]
	if err != nil || ref == nil || ref.job.Token != token {
		return nil
	}
	return ref
}

func jobToken(r *http.Request) string {
	if t := r.Header.Get("JOB-TOKEN"); t != "" {
		return t
	}
	return r.URL.Query().Get("token")
}

func forbidden(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]string{"message": "403 Forbidden"})
}

// patchTrace serves PATCH /jobs/:id/trace. Content-Range gives the offset of the chunk; the
// reply's Range header says how many bytes the server now holds.
func (s *Server) patchTrace(w http.ResponseWriter, r *http.Request) {
	chunk, _ := io.ReadAll(r.Body)
	s.runMu.Lock()
	defer s.runMu.Unlock()
	ref := s.jobFor(r, jobToken(r))
	if ref == nil {
		forbidden(w)
		return
	}
	j := ref.job
	start := 0
	if from, _, ok := strings.Cut(r.Header.Get("Content-Range"), "-"); ok {
		start, _ = strconv.Atoi(from)
	}
	w.Header().Set("Job-Status", j.Status)
	w.Header().Set("X-GitLab-Trace-Update-Interval", "3")
	if start > len(j.Trace) {
		w.Header().Set("Range", "0-"+strconv.Itoa(len(j.Trace)))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if skip := len(j.Trace) - start; skip < len(chunk) {
		j.Trace = append(j.Trace, chunk[skip:]...)
	}
	w.Header().Set("Range", "0-"+strconv.Itoa(len(j.Trace)))
	w.WriteHeader(http.StatusAccepted)
}

// updateJob serves PUT /jobs/:id: the runner reports success or failed.
func (s *Server) updateJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
		State string `json:"state"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	s.runMu.Lock()
	defer s.runMu.Unlock()
	ref := s.jobFor(r, in.Token)
	if ref == nil {
		forbidden(w)
		return
	}
	j := ref.job
	w.Header().Set("X-GitLab-Trace-Update-Interval", "3")
	if in.State != "running" { // the runner repeats running as a keep-alive
		if err := s.move(j, in.State); err != nil {
			w.Header().Set("Job-Status", j.Status)
			forbidden(w)
			return
		}
		s.advance(ref.pipeline)
	}
	w.Header().Set("Job-Status", j.Status)
	w.WriteHeader(http.StatusOK)
}

// uploadArtifacts serves POST /jobs/:id/artifacts: the zip is kept as bytes.
func (s *Server) uploadArtifacts(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "400 Bad request"})
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "400 Bad request"})
		return
	}
	defer func() { _ = file.Close() }()
	data, _ := io.ReadAll(file)
	s.runMu.Lock()
	defer s.runMu.Unlock()
	ref := s.jobFor(r, jobToken(r))
	if ref == nil {
		forbidden(w)
		return
	}
	ref.job.Artifact = &Artifact{Filename: hdr.Filename, Data: data}
	writeJSON(w, http.StatusCreated, map[string]any{"id": ref.job.ID, "size": len(data)})
}

// downloadArtifacts serves GET /jobs/:id/artifacts to a runner holding the job's token.
func (s *Server) downloadArtifacts(w http.ResponseWriter, r *http.Request) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	ref := s.jobFor(r, jobToken(r))
	if ref == nil {
		forbidden(w)
		return
	}
	if ref.job.Artifact == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "404 Not Found"})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	_, _ = w.Write(ref.job.Artifact.Data)
}

// runningJobToken reports whether token belongs to a job that is running now.
func (s *Server) runningJobToken(token string) bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	for _, r := range s.jobIndex {
		if r.job.Status == "running" && subtle.ConstantTimeCompare([]byte(r.job.Token), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

func (s *Server) projectJob(w http.ResponseWriter, r *http.Request) (*Project, *jobRef) {
	p := s.project(w, r)
	if p == nil {
		return nil, nil
	}
	id, _ := strconv.Atoi(r.PathValue("jid"))
	ref := s.jobIndex[id]
	if ref == nil || ref.project != p {
		errs.PipelineNotFound.Write(w)
		return nil, nil
	}
	return p, ref
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if _, ref := s.projectJob(w, r); ref != nil {
		writeJSON(w, http.StatusOK, jobJSON(r, ref.project, ref.pipeline, ref.job))
	}
}

func (s *Server) getTrace(w http.ResponseWriter, r *http.Request) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if _, ref := s.projectJob(w, r); ref != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(ref.job.Trace)
	}
}

func (s *Server) getJobArtifacts(w http.ResponseWriter, r *http.Request) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if _, ref := s.projectJob(w, r); ref != nil {
		if ref.job.Artifact == nil {
			errs.NotFoundMsg("404 Not Found").Write(w)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(ref.job.Artifact.Data)
	}
}
