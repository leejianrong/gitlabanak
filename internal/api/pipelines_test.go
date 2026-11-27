package api

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

const threeStageCI = `stages: [build, test, deploy]
deploy-prod:
  stage: deploy
  needs: [unit]
  script: [x]
unit:
  stage: test
  needs: [compile]
  script: [x]
compile:
  stage: build
  script: [x]
`

func pipelineList(t *testing.T, base, query string) []map[string]any {
	t.Helper()
	_, out := list(t, base+"/api/v4/projects/100/pipelines"+query)
	return out
}

func jobNames(t *testing.T, base string, id any) (names []string, jobs []map[string]any) {
	t.Helper()
	_, jobs = list(t, fmt.Sprintf("%s/api/v4/projects/100/pipelines/%v/jobs", base, id))
	for _, j := range jobs {
		names = append(names, j["stage"].(string)+"/"+j["name"].(string))
	}
	return names, jobs
}

func TestRESTCommitOfACIFileCreatesAPipelineWithJobsInStageOrder(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", "README", "no ci yet")
	if got := pipelineList(t, srv.URL, ""); len(got) != 0 {
		t.Fatalf("no CI file, yet pipelines: %v", got)
	}
	sha := commitFile(t, st, "main", ".gitlab-ci.yml", threeStageCI)

	ps := pipelineList(t, srv.URL, "")
	if len(ps) != 1 || ps[0]["sha"] != sha || ps[0]["ref"] != "main" || ps[0]["status"] != "pending" || ps[0]["source"] != "push" {
		t.Fatalf("pipelines: %v", ps)
	}
	names, jobs := jobNames(t, srv.URL, ps[0]["id"])
	if want := []string{"build/compile", "test/unit", "deploy/deploy-prod"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("jobs %v, want %v", names, want)
	}
	if !reflect.DeepEqual(jobs[1]["needs"], []any{"compile"}) || jobs[0]["status"] != "pending" || jobs[1]["status"] != "created" {
		t.Errorf("needs/status: %v", jobs)
	}
	_, one := get(t, fmt.Sprintf("%s/api/v4/projects/100/pipelines/%v", srv.URL, ps[0]["id"]), auth)
	if one["yaml_errors"] != nil || one["tag"] != false || one["before_sha"] == nil {
		t.Errorf("single pipeline: %v", one)
	}
}

func TestBrokenCIFileGivesAFailedPipelineWithYamlErrors(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", "job:\n  stage: nope\n  script: [x]\n")
	ps := pipelineList(t, srv.URL, "")
	if len(ps) != 1 || ps[0]["status"] != "failed" {
		t.Fatalf("%v", ps)
	}
	_, one := get(t, fmt.Sprintf("%s/api/v4/projects/100/pipelines/%v", srv.URL, ps[0]["id"]), auth)
	if one["yaml_errors"] != "jobs:job chosen stage does not exist; available stages are .pre, build, test, deploy, .post" {
		t.Errorf("yaml_errors = %v", one["yaml_errors"])
	}
	if names, _ := jobNames(t, srv.URL, ps[0]["id"]); len(names) != 0 {
		t.Errorf("failed pipeline has jobs: %v", names)
	}
}

func TestRulesDecideWhichJobsRunPerBranch(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", `build:
  script: [x]
deploy:
  script: [x]
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
`)
	if _, err := st.CreateBranch("feat", "main"); err != nil {
		t.Fatal(err)
	}
	main := pipelineList(t, srv.URL, "?ref=main")
	feat := pipelineList(t, srv.URL, "?ref=feat")
	if len(main) != 1 || len(feat) != 1 {
		t.Fatalf("main %d, feat %d", len(main), len(feat))
	}
	if names, _ := jobNames(t, srv.URL, main[0]["id"]); !reflect.DeepEqual(names, []string{"test/build", "test/deploy"}) {
		t.Errorf("main jobs %v", names)
	}
	if names, _ := jobNames(t, srv.URL, feat[0]["id"]); !reflect.DeepEqual(names, []string{"test/build"}) {
		t.Errorf("feat jobs %v", names)
	}
}

func TestEachPushCreatesOnePipelinePerPushedRef(t *testing.T) {
	needGit(t)
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", "build:\n  script: [x]\n")
	before := len(pipelineList(t, srv.URL, ""))
	url := strings.Replace(srv.URL, "http://", "http://x:"+token+"@", 1) + "/group/project.git"
	dir := t.TempDir()
	mustGit(t, dir, "clone", "-q", url, "c")
	c := dir + "/c"

	mustGit(t, c, "commit", "-q", "--allow-empty", "-m", "one")
	mustGit(t, c, "push", "-q", "origin", "HEAD:refs/heads/a")
	if n := len(pipelineList(t, srv.URL, "?ref=a")); n != 1 {
		t.Fatalf("after the first push: %d pipelines on a", n)
	}
	mustGit(t, c, "commit", "-q", "--allow-empty", "-m", "two")
	mustGit(t, c, "push", "-q", "origin", "HEAD:refs/heads/a")
	if n := len(pipelineList(t, srv.URL, "?ref=a")); n != 2 {
		t.Fatalf("after the second push: %d pipelines on a", n)
	}
	mustGit(t, c, "push", "-q", "origin", "HEAD:refs/heads/b", "HEAD:refs/heads/c")
	if n := len(pipelineList(t, srv.URL, "")); n != before+4 {
		t.Fatalf("pushing two refs at once: %d pipelines, want %d", n, before+4)
	}
	mustGit(t, c, "push", "-q", "origin", "--delete", "b")
	if n := len(pipelineList(t, srv.URL, "")); n != before+4 {
		t.Fatalf("a delete created a pipeline: %d", n)
	}
	// a branch pushed without a CI file creates none
	mustGit(t, c, "checkout", "-q", "--orphan", "bare")
	mustGit(t, c, "rm", "-rfq", ".")
	mustGit(t, c, "commit", "-q", "--allow-empty", "-m", "empty")
	mustGit(t, c, "push", "-q", "origin", "bare")
	if n := len(pipelineList(t, srv.URL, "?ref=bare")); n != 0 {
		t.Fatalf("bare branch got %d pipelines", n)
	}
	if err := os.WriteFile(c+"/.gitlab-ci.yml", []byte("x:\n  script: [y]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, c, "add", ".")
	mustGit(t, c, "commit", "-q", "-m", "add ci")
	mustGit(t, c, "push", "-q", "origin", "bare")
	if n := len(pipelineList(t, srv.URL, "?ref=bare")); n != 1 {
		t.Fatalf("after adding the file: %d pipelines", n)
	}
}

func TestCreatePipelineOnARef(t *testing.T) {
	srv, st := fileServer(t)
	url := srv.URL + "/api/v4/projects/100/pipeline"
	if code, body := post(t, url, map[string]string{}); code != 400 || body["error"] != "ref is missing" {
		t.Errorf("no ref: %d %v", code, body)
	}
	if code, body := post(t, url, map[string]string{"ref": "nope"}); code != 400 || fmt.Sprint(body["message"]) != "map[base:[Reference not found]]" {
		t.Errorf("unknown ref: %d %v", code, body)
	}
	if code, body := post(t, url, map[string]string{"ref": "main"}); code != 400 || fmt.Sprint(body["message"]) != "map[base:[Missing CI config file]]" {
		t.Errorf("no CI file: %d %v", code, body)
	}
	commitFile(t, st, "main", ".gitlab-ci.yml", "a:\n  script: [x]\n")
	before := len(pipelineList(t, srv.URL, ""))
	code, body := post(t, url, map[string]string{"ref": "main"})
	if code != 201 || body["source"] != "api" || body["status"] != "pending" {
		t.Fatalf("create: %d %v", code, body)
	}
	if n := len(pipelineList(t, srv.URL, "?source=api")); n != 1 || len(pipelineList(t, srv.URL, "")) != before+1 {
		t.Errorf("source filter %d", n)
	}
	commitFile(t, st, "main", "later.txt", "x") // rewrite the CI file into a broken one
	broken := "a:\n  stage: nope\n  script: [x]\n"
	if _, err := st.Commit("main", changeOf(".gitlab-ci.yml", broken)); err != nil {
		t.Fatal(err)
	}
	if code, body := post(t, url, map[string]string{"ref": "main"}); code != 400 || !strings.Contains(fmt.Sprint(body["message"]), "chosen stage does not exist") {
		t.Errorf("broken file: %d %v", code, body)
	}
}

func TestPipelineListingOrderPaginationAndNotFound(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", "a:\n  script: [x]\n")
	for i := range 4 {
		commitFile(t, st, "main", fmt.Sprintf("f%d", i), "x")
	}
	all := pipelineList(t, srv.URL, "")
	if len(all) != 5 || all[0]["iid"] != float64(5) || all[4]["iid"] != float64(1) {
		t.Fatalf("newest first expected: %v", all)
	}
	if asc := pipelineList(t, srv.URL, "?sort=asc"); asc[0]["iid"] != float64(1) {
		t.Errorf("asc: %v", asc[0])
	}
	if page := pipelineList(t, srv.URL, "?per_page=2&page=3"); len(page) != 1 {
		t.Errorf("page 3 of 2: %d", len(page))
	}
	if code, body := get(t, srv.URL+"/api/v4/projects/100/pipelines/9999", auth); code != 404 || body["message"] != "404 Not found" {
		t.Errorf("unknown pipeline: %d %v", code, body)
	}
	if code, _ := get(t, srv.URL+"/api/v4/projects/100/pipelines/9999/jobs", auth); code != 404 {
		t.Errorf("jobs of unknown pipeline: %d", code)
	}
}

func TestStateShowsPipelinesAndResetClearsThem(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", ".gitlab-ci.yml", threeStageCI)
	_, state := get(t, srv.URL+"/_admin/state", auth)
	pipes := state["projects"].(map[string]any)["100"].(map[string]any)["pipelines"].([]any)
	if len(pipes) != 1 || len(pipes[0].(map[string]any)["jobs"].([]any)) != 3 {
		t.Fatalf("state pipelines: %v", pipes)
	}
	if code, _ := do(t, "POST", srv.URL+"/_admin/reset", auth, ""); code != 204 {
		t.Fatal("reset")
	}
	if got := pipelineList(t, srv.URL, ""); len(got) != 0 {
		t.Fatalf("reset left pipelines: %v", got)
	}
}
