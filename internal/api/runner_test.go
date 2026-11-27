package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"sync"
	"testing"
)

// runnerCall sends a runner-style request: no PRIVATE-TOKEN, credentials in the body or JOB-TOKEN.
// reply is a response whose body has already been read and closed.
type reply struct {
	StatusCode int
	Header     http.Header
}

func runnerCall(t *testing.T, method, url string, hdr map[string]string, body []byte) (reply, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, resp.Header}, b
}

// poll asks for a job as the runner with the given token and returns it, or nil on 204.
func poll(t *testing.T, base, runner string) map[string]any {
	t.Helper()
	resp, b := runnerCall(t, "POST", base+"/api/v4/jobs/request", nil, []byte(`{"token":"`+runner+`"}`))
	if resp.StatusCode == 204 {
		return nil
	}
	if resp.StatusCode != 201 {
		t.Fatalf("job request: %d %s", resp.StatusCode, b)
	}
	var job map[string]any
	if err := json.Unmarshal(b, &job); err != nil {
		t.Fatal(err)
	}
	return job
}

func report(t *testing.T, base string, job map[string]any, state string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"token": job["token"].(string), "state": state})
	resp, _ := runnerCall(t, "PUT", fmt.Sprintf("%s/api/v4/jobs/%v", base, job["id"]), nil, body)
	return resp.StatusCode
}

func pipelineStatus(t *testing.T, base string, id any) string {
	t.Helper()
	_, p := get(t, fmt.Sprintf("%s/api/v4/projects/100/pipelines/%v", base, id), auth)
	return p["status"].(string)
}

func jobStatuses(t *testing.T, base string, pid any) map[string]string {
	t.Helper()
	_, jobs := jobNames(t, base, pid)
	out := map[string]string{}
	for _, j := range jobs {
		out[j["name"].(string)] = j["status"].(string)
	}
	return out
}

func TestRunnerRunsAPipelineThroughItsStagesToSuccess(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", "stages: [build, test]\nbuild:\n  stage: build\n  script: [echo b]\ntest:\n  stage: test\n  script: [echo t]\n")
	pid := pipelineList(t, srv.URL, "")[0]["id"]

	if got := jobStatuses(t, srv.URL, pid); !reflect.DeepEqual(got, map[string]string{"build": "pending", "test": "created"}) {
		t.Fatalf("start: %v", got)
	}
	first := poll(t, srv.URL, "glrt-a")
	if first == nil || first["job_info"].(map[string]any)["name"] != "build" {
		t.Fatalf("first job: %v", first)
	}
	if got := pipelineStatus(t, srv.URL, pid); got != "running" {
		t.Fatalf("pipeline while building: %s", got)
	}
	if poll(t, srv.URL, "glrt-b") != nil {
		t.Fatal("the test job was handed out before its stage")
	}
	if code := report(t, srv.URL, first, "success"); code != 200 {
		t.Fatalf("report: %d", code)
	}
	second := poll(t, srv.URL, "glrt-b")
	if second == nil || second["job_info"].(map[string]any)["name"] != "test" {
		t.Fatalf("second job: %v", second)
	}
	report(t, srv.URL, second, "success")
	if got := pipelineStatus(t, srv.URL, pid); got != "success" {
		t.Fatalf("pipeline at the end: %s", got)
	}
	if poll(t, srv.URL, "glrt-a") != nil {
		t.Fatal("a finished pipeline still hands out jobs")
	}
}

func TestFailingJobFailsThePipelineAndSkipsLaterStages(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", "stages: [a, b, c]\none:\n  stage: a\n  script: [x]\ntwo:\n  stage: b\n  script: [x]\nthree:\n  stage: c\n  script: [x]\n")
	pid := pipelineList(t, srv.URL, "")[0]["id"]
	report(t, srv.URL, poll(t, srv.URL, "glrt-a"), "failed")
	if got := jobStatuses(t, srv.URL, pid); !reflect.DeepEqual(got, map[string]string{"one": "failed", "two": "skipped", "three": "skipped"}) {
		t.Fatalf("%v", got)
	}
	if got := pipelineStatus(t, srv.URL, pid); got != "failed" {
		t.Fatalf("pipeline: %s", got)
	}
	if poll(t, srv.URL, "glrt-a") != nil {
		t.Fatal("skipped jobs were handed out")
	}
}

func TestAllowedFailureDoesNotStopThePipeline(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", "stages: [a, b]\nflaky:\n  stage: a\n  allow_failure: true\n  script: [x]\nafter:\n  stage: b\n  script: [x]\n")
	pid := pipelineList(t, srv.URL, "")[0]["id"]
	report(t, srv.URL, poll(t, srv.URL, "glrt-a"), "failed")
	report(t, srv.URL, poll(t, srv.URL, "glrt-a"), "success")
	if got := pipelineStatus(t, srv.URL, pid); got != "success" {
		t.Fatalf("pipeline: %s", got)
	}
}

func TestJobWithNeedsStartsBeforeItsStageWouldNormally(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", `stages: [build, test, deploy]
slow-build:
  stage: build
  script: [x]
fast-build:
  stage: build
  script: [x]
quick-test:
  stage: test
  needs: [fast-build]
  script: [x]
`)
	pid := pipelineList(t, srv.URL, "")[0]["id"]
	slow, fast := poll(t, srv.URL, "glrt-a"), poll(t, srv.URL, "glrt-b")
	if slow["job_info"].(map[string]any)["name"] != "slow-build" {
		t.Fatalf("job order: %v", slow["job_info"])
	}
	report(t, srv.URL, fast, "success") // slow-build is still running
	if got := jobStatuses(t, srv.URL, pid)["quick-test"]; got != "pending" {
		t.Fatalf("quick-test should start once fast-build is done, not wait for slow-build: %s", got)
	}
	if j := poll(t, srv.URL, "glrt-b"); j == nil || j["job_info"].(map[string]any)["name"] != "quick-test" {
		t.Fatalf("got %v", j)
	}
}

func TestTwoRunnersPollingConcurrentlyNeverGetTheSameJob(t *testing.T) {
	srv, st := fileServer(t)
	var ci string
	for i := range 40 {
		ci += fmt.Sprintf("job%02d:\n  script: [x]\n", i)
	}
	commitFile(t, st, "main", ".gitlab-ci.yml", ci)
	var mu sync.Mutex
	seen := map[float64]int{}
	var wg sync.WaitGroup
	for r := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				job := poll(t, srv.URL, fmt.Sprintf("glrt-%d", r))
				if job == nil {
					return
				}
				mu.Lock()
				seen[job["id"].(float64)]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != 40 {
		t.Fatalf("handed out %d distinct jobs, want 40", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %v was handed out %d times", id, n)
		}
	}
}

func TestRunnerReportsAreCheckedAgainstTheStateMachineAndTheToken(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", "a:\n  script: [x]\n")
	job := poll(t, srv.URL, "glrt-a")
	url := fmt.Sprintf("%s/api/v4/jobs/%v", srv.URL, job["id"])

	if resp, _ := runnerCall(t, "PUT", url, nil, []byte(`{"token":"wrong","state":"success"}`)); resp.StatusCode != 403 {
		t.Errorf("wrong token: %d", resp.StatusCode)
	}
	if code := report(t, srv.URL, job, "running"); code != 200 {
		t.Errorf("keep-alive: %d", code)
	}
	if code := report(t, srv.URL, job, "success"); code != 200 {
		t.Fatalf("finish: %d", code)
	}
	if code := report(t, srv.URL, job, "failed"); code != 403 {
		t.Errorf("success -> failed must be refused: %d", code)
	}
	if code := report(t, srv.URL, job, "pending"); code != 403 {
		t.Errorf("success -> pending must be refused: %d", code)
	}
}

func TestTraceChunksAreAssembledByOffsetAndReadableOverREST(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", "a:\n  script: [x]\n")
	job := poll(t, srv.URL, "glrt-a")
	url := fmt.Sprintf("%s/api/v4/jobs/%v/trace", srv.URL, job["id"])
	hdr := func(rng string) map[string]string {
		return map[string]string{"JOB-TOKEN": job["token"].(string), "Content-Range": rng}
	}

	if resp, _ := runnerCall(t, "PATCH", url, hdr("0-5"), []byte("hello")); resp.StatusCode != 202 || resp.Header.Get("Range") != "0-5" {
		t.Fatalf("first chunk: %d %v", resp.StatusCode, resp.Header)
	}
	if resp, _ := runnerCall(t, "PATCH", url, hdr("5-11"), []byte(" world")); resp.Header.Get("Range") != "0-11" {
		t.Fatalf("second chunk: %v", resp.Header)
	}
	if resp, _ := runnerCall(t, "PATCH", url, hdr("3-8"), []byte("lo wo")); resp.Header.Get("Range") != "0-11" {
		t.Fatalf("overlapping resend must not duplicate: %v", resp.Header)
	}
	if resp, _ := runnerCall(t, "PATCH", url, hdr("40-42"), []byte("zz")); resp.StatusCode != 416 || resp.Header.Get("Range") != "0-11" {
		t.Fatalf("gap: %d %v", resp.StatusCode, resp.Header)
	}
	if resp, _ := runnerCall(t, "PATCH", url, map[string]string{"JOB-TOKEN": "nope", "Content-Range": "0-1"}, []byte("x")); resp.StatusCode != 403 {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	code, _, body := fetch(t, fmt.Sprintf("%s/api/v4/projects/100/jobs/%v/trace", srv.URL, job["id"]))
	if code != 200 || string(body) != "hello world" {
		t.Fatalf("trace over REST: %d %q", code, body)
	}
}

func TestArtifactsUploadedByOneJobAreDownloadableByTheNext(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", "stages: [a, b]\nbuild:\n  stage: a\n  script: [x]\n  artifacts: {paths: [out/]}\nuse:\n  stage: b\n  script: [x]\n")
	build := poll(t, srv.URL, "glrt-a")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "artifacts.zip")
	_, _ = fw.Write([]byte("PK-not-really-a-zip"))
	_ = mw.Close()
	url := fmt.Sprintf("%s/api/v4/jobs/%v/artifacts", srv.URL, build["id"])
	if resp, b := runnerCall(t, "POST", url, map[string]string{"JOB-TOKEN": build["token"].(string), "Content-Type": mw.FormDataContentType()}, buf.Bytes()); resp.StatusCode != 201 {
		t.Fatalf("upload: %d %s", resp.StatusCode, b)
	}
	report(t, srv.URL, build, "success")

	use := poll(t, srv.URL, "glrt-a")
	deps := use["dependencies"].([]any)
	if len(deps) != 1 {
		t.Fatalf("dependencies: %v", deps)
	}
	dep := deps[0].(map[string]any)
	if dep["name"] != "build" || dep["artifacts_file"].(map[string]any)["filename"] != "artifacts.zip" {
		t.Fatalf("dependency: %v", dep)
	}
	resp, b := runnerCall(t, "GET", fmt.Sprintf("%s/api/v4/jobs/%v/artifacts", srv.URL, dep["id"]), map[string]string{"JOB-TOKEN": dep["token"].(string)}, nil)
	if resp.StatusCode != 200 || string(b) != "PK-not-really-a-zip" {
		t.Fatalf("download: %d %q", resp.StatusCode, b)
	}
	if resp, _ := runnerCall(t, "GET", fmt.Sprintf("%s/api/v4/jobs/%v/artifacts", srv.URL, dep["id"]), map[string]string{"JOB-TOKEN": "wrong"}, nil); resp.StatusCode != 403 {
		t.Fatalf("wrong token download: %d", resp.StatusCode)
	}
}

func TestJobPayloadHasWhatTheRunnerNeeds(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", "variables: {FILE_VAR: from-file}\nimage: alpine\nbuild:\n  variables: {JOB_VAR: from-job}\n  before_script: [prep]\n  script: [one, two]\n  after_script: [bye]\n")
	job := poll(t, srv.URL, "glrt-a")
	vars := map[string]string{}
	for _, v := range job["variables"].([]any) {
		m := v.(map[string]any)
		vars[m["key"].(string)] = m["value"].(string)
	}
	for k, want := range map[string]string{"FILE_VAR": "from-file", "JOB_VAR": "from-job", "CI_COMMIT_BRANCH": "main", "CI_JOB_NAME": "build", "CI_PROJECT_ID": "100", "CI_PIPELINE_ID": "1"} {
		if vars[k] != want {
			t.Errorf("%s = %q, want %q", k, vars[k], want)
		}
	}
	steps := job["steps"].([]any)
	if s0 := steps[0].(map[string]any); !reflect.DeepEqual(s0["script"], []any{"prep", "one", "two"}) || s0["name"] != "script" {
		t.Errorf("script step: %v", s0)
	}
	if s1 := steps[1].(map[string]any); s1["name"] != "after_script" || !reflect.DeepEqual(s1["script"], []any{"bye"}) {
		t.Errorf("after_script step: %v", s1)
	}
	if job["image"].(map[string]any)["name"] != "alpine" {
		t.Errorf("image: %v", job["image"])
	}
	git := job["git_info"].(map[string]any)
	if git["ref"] != "main" || len(git["sha"].(string)) != 40 || git["depth"] != float64(0) {
		t.Errorf("git_info: %v", git)
	}
}

func TestStateMachineRefusesIllegalMoves(t *testing.T) {
	ok := [][2]string{{"created", "pending"}, {"pending", "running"}, {"running", "success"}, {"running", "failed"}, {"created", "skipped"}}
	for _, m := range ok {
		if !canMove(m[0], m[1]) {
			t.Errorf("%s -> %s should be allowed", m[0], m[1])
		}
	}
	bad := [][2]string{{"success", "running"}, {"failed", "success"}, {"pending", "success"}, {"created", "running"}, {"running", "pending"}, {"manual", "running"}, {"skipped", "pending"}}
	for _, m := range bad {
		if canMove(m[0], m[1]) {
			t.Errorf("%s -> %s should be refused", m[0], m[1])
		}
	}
}

func TestDeriveStatus(t *testing.T) {
	mk := func(sts ...string) []*PipelineJob {
		var out []*PipelineJob
		for _, s := range sts {
			out = append(out, &PipelineJob{Status: s})
		}
		return out
	}
	for want, jobs := range map[string][]*PipelineJob{
		"pending": mk("pending", "created"),
		"running": mk("running", "created"),
		"success": mk("success", "success"),
		"failed":  mk("success", "failed", "skipped"),
		"manual":  mk("success", "manual"),
	} {
		if got := deriveStatus(jobs); got != want {
			t.Errorf("%v: got %s, want %s", want, got, want)
		}
	}
	if got := deriveStatus(mk("success", "pending", "created")); got != "running" {
		t.Errorf("between stages: %s", got)
	}
	allowed := mk("failed", "success")
	allowed[0].AllowFailure = true
	if got := deriveStatus(allowed); got != "success" {
		t.Errorf("allowed failure: %s", got)
	}
}
