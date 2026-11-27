package ci_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/leejianrong/gitlabanak/internal/ci"
)

func TestExpressions(t *testing.T) {
	vars := map[string]string{"CI_COMMIT_BRANCH": "main", "CI_PIPELINE_SOURCE": "push", "EMPTY": ""}
	for expr, want := range map[string]bool{
		`$CI_COMMIT_BRANCH == "main"`:  true,
		`$CI_COMMIT_BRANCH != "main"`:  false,
		`$CI_COMMIT_BRANCH`:            true,
		`$EMPTY`:                       false,
		`$MISSING`:                     false,
		`$MISSING == null`:             true,
		`$CI_COMMIT_BRANCH == null`:    false,
		`$CI_COMMIT_BRANCH =~ /^ma/`:   true,
		`$CI_COMMIT_BRANCH =~ /^MA/i`:  true,
		`$CI_COMMIT_BRANCH !~ /^feat/`: true,
		`$CI_COMMIT_BRANCH == 'main' && $CI_PIPELINE_SOURCE == "push"`: true,
		`$CI_COMMIT_BRANCH == "dev" || $CI_PIPELINE_SOURCE == "push"`:  true,
		`($CI_COMMIT_BRANCH == "dev" || $MISSING) && $EMPTY == ""`:     false,
		`$CI_COMMIT_BRANCH == "dev" || $EMPTY == "" && $MISSING`:       false,
	} {
		e, err := ci.Compile(expr)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
			continue
		}
		if got := e(vars); got != want {
			t.Errorf("%s = %v, want %v", expr, got, want)
		}
	}
}

func TestBadExpressionsAreRejected(t *testing.T) {
	for _, expr := range []string{`$`, `$A ==`, `$A == "x`, `/re/ == $A`, `$A =~ "x"`, `($A`, `$A $B`, `$A =~ /(/`} {
		if _, err := ci.Compile(expr); err == nil {
			t.Errorf("%q compiled", expr)
		}
	}
}

func vars(branch string) map[string]string {
	return map[string]string{"CI_COMMIT_BRANCH": branch, "CI_COMMIT_REF_NAME": branch, "CI_PROJECT_ID": "100"}
}

func names(p ci.Plan) []string {
	var out []string
	for _, j := range p.Jobs {
		out = append(out, j.Stage+"/"+j.Name)
	}
	return out
}

const threeStages = `stages: [build, test, deploy]
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
lint:
  stage: test
  script: [x]
`

func TestJobsComeOutInStageOrderThenFileOrderWithNeeds(t *testing.T) {
	p := ci.BuildPlan(threeStages, nil, vars("main"))
	if len(p.Errors) != 0 {
		t.Fatal(p.Errors)
	}
	want := []string{"build/compile", "test/unit", "test/lint", "deploy/deploy-prod"}
	if !reflect.DeepEqual(names(p), want) {
		t.Fatalf("got %v, want %v", names(p), want)
	}
	if !reflect.DeepEqual(p.Jobs[1].Needs, []string{"compile"}) {
		t.Errorf("needs = %v", p.Jobs[1].Needs)
	}
}

func TestExtendsAndDefaultAreExpanded(t *testing.T) {
	p := ci.BuildPlan(`default:
  image: alpine
.base:
  stage: build
  script: [from-base]
  variables: {A: "1"}
job:
  extends: .base
  variables: {B: "2"}
`, nil, vars("main"))
	if len(p.Errors) != 0 || len(p.Jobs) != 1 {
		t.Fatalf("%+v", p)
	}
	j := p.Jobs[0]
	if j.Stage != "build" || j.Config["image"] != "alpine" || !reflect.DeepEqual(j.Config["script"], []any{"from-base"}) {
		t.Errorf("job = %+v", j)
	}
	if !reflect.DeepEqual(j.Config["variables"], map[string]any{"A": "1", "B": "2"}) {
		t.Errorf("variables not merged: %v", j.Config["variables"])
	}
}

func TestRulesIfIncludesAndExcludes(t *testing.T) {
	file := `a:
  script: [x]
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
b:
  script: [x]
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
      when: never
    - when: always
c:
  script: [x]
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
      when: manual
d:
  script: [x]
`
	if got := names(ci.BuildPlan(file, nil, vars("main"))); !reflect.DeepEqual(got, []string{"test/a", "test/c", "test/d"}) {
		t.Errorf("main: %v", got)
	}
	if got := names(ci.BuildPlan(file, nil, vars("feat"))); !reflect.DeepEqual(got, []string{"test/b", "test/d"}) {
		t.Errorf("feat: %v", got)
	}
	c := ci.BuildPlan(file, nil, vars("main")).Jobs[1]
	if c.When != "manual" || !c.AllowFailure {
		t.Errorf("manual job = %+v", c)
	}
}

func TestOnlyAndExcept(t *testing.T) {
	file := "a:\n  script: [x]\n  only: [main, /^release-/]\nb:\n  script: [x]\n  except: [main]\n"
	for branch, want := range map[string][]string{"main": {"test/a"}, "release-1": {"test/a", "test/b"}, "feat": {"test/b"}} {
		if got := names(ci.BuildPlan(file, nil, vars(branch))); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", branch, got, want)
		}
	}
}

func TestFileVariablesFeedRulesButPredefinedWin(t *testing.T) {
	file := "variables:\n  DEPLOY: \"yes\"\n  CI_PROJECT_ID: \"999\"\na:\n  script: [x]\n  rules:\n    - if: '$DEPLOY == \"yes\" && $CI_PROJECT_ID == \"100\"'\n"
	if got := names(ci.BuildPlan(file, nil, vars("main"))); len(got) != 1 {
		t.Errorf("got %v", got)
	}
}

func TestPlanReportsLintErrorsAndNeedsOnExcludedJobs(t *testing.T) {
	if p := ci.BuildPlan("job:\n  stage: nope\n  script: [x]\n", nil, vars("main")); len(p.Errors) != 1 || len(p.Jobs) != 0 {
		t.Errorf("lint errors: %+v", p)
	}
	file := "a:\n  script: [x]\n  rules: [{if: '$CI_COMMIT_BRANCH == \"never\"'}]\nb:\n  needs: [a]\n  script: [x]\nc:\n  needs: [{job: a, optional: true}]\n  script: [x]\n"
	p := ci.BuildPlan(file, nil, vars("main"))
	if len(p.Errors) != 1 || !strings.Contains(p.Errors[0], "'b' job needs 'a' job") {
		t.Errorf("%+v", p.Errors)
	}
}

func TestInvalidRuleExpressionIsALintError(t *testing.T) {
	res := ci.Lint("a:\n  script: [x]\n  rules:\n    - if: '$X =='\n", nil)
	if res.Valid || res.Errors[0] != "jobs:a:rules:rule if invalid expression syntax" {
		t.Errorf("%+v", res)
	}
}
