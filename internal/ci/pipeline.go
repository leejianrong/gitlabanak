package ci

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Job is one job of a pipeline, with extends and default applied.
type Job struct {
	Name         string
	Stage        string
	Needs        []string
	When         string // on_success unless the file says otherwise
	AllowFailure bool
	Config       map[string]any  // every keyword after extends and default are applied
	optional     map[string]bool // needs entries marked optional: true
}

// Plan is the pipeline a file would create for one ref. Errors is non-empty when the file is
// invalid or no job can run as written; Jobs are in stage order, then file order.
type Plan struct {
	Errors    []string
	Stages    []string
	Jobs      []Job
	Variables map[string]string // variables: from the file, before predefined ones are added
}

// BuildPlan expands content into jobs. vars are the predefined variables used by rules:if, only
// and except; variables: from the file are added underneath them.
func BuildPlan(content string, local LocalFile, vars map[string]string) Plan {
	l := parse(content, local)
	if len(l.res.Errors) > 0 {
		return Plan{Errors: l.res.Errors}
	}
	stages := l.stage
	if stages == nil {
		stages = defaultStages
	}
	all := append(append([]string{".pre"}, stages...), ".post")

	env := map[string]string{}
	if l.vars != nil {
		for i := 0; i+1 < len(l.vars.Content); i += 2 {
			v := l.vars.Content[i+1]
			if v.Kind == yaml.MappingNode {
				if inner := value(v, "value"); inner != nil {
					v = inner
				}
			}
			env[l.vars.Content[i].Value] = v.Value
		}
	}
	fileVars := map[string]string{}
	for k, v := range env {
		fileVars[k] = v
	}
	for k, v := range vars {
		env[k] = v
	}

	byName := map[string]*yaml.Node{}
	for _, j := range l.jobs {
		byName[j.name] = j.node
	}
	defaults := map[string]any{}
	for k, n := range l.globs {
		var v any
		_ = n.Decode(&v)
		defaults[k] = v
	}
	if l.deflt != nil {
		var block map[string]any
		_ = l.deflt.Decode(&block)
		for k, v := range block {
			defaults[k] = v
		}
	}

	var plan Plan
	plan.Stages, plan.Variables = all, fileVars
	for _, j := range l.jobs {
		if strings.HasPrefix(j.name, ".") {
			continue
		}
		cfg := merged(j.name, byName, nil)
		for k, v := range defaults {
			if _, set := cfg[k]; !set && defaultable[k] {
				cfg[k] = v
			}
		}
		include, when, err := decide(cfg, env)
		if err != nil {
			plan.Errors = append(plan.Errors, fmt.Sprintf("jobs:%s:%s", j.name, err))
			continue
		}
		if !include {
			continue
		}
		job := Job{Name: j.name, Stage: "test", When: "on_success", Config: cfg}
		job.Needs, job.optional = needsOf(cfg["needs"])
		if s, ok := cfg["stage"].(string); ok {
			job.Stage = s
		}
		if when != "" {
			job.When = when
		}
		job.AllowFailure = allowFailure(cfg["allow_failure"], job.When)
		plan.Jobs = append(plan.Jobs, job)
	}
	slices.SortStableFunc(plan.Jobs, func(a, b Job) int { return slices.Index(all, a.Stage) - slices.Index(all, b.Stage) })
	planned := map[string]bool{}
	for _, j := range plan.Jobs {
		planned[j.Name] = true
	}
	for _, j := range plan.Jobs {
		for _, n := range j.Needs {
			if !planned[n] && !j.optional[n] {
				plan.Errors = append(plan.Errors, fmt.Sprintf("'%s' job needs '%s' job, but '%s' does not exist in the pipeline", j.Name, n, n))
			}
		}
	}
	return plan
}

// defaultable are the keywords a default: block may supply to jobs that do not set them.
var defaultable = map[string]bool{
	"before_script": true, "after_script": true, "image": true, "services": true, "cache": true, "artifacts": true,
	"retry": true, "timeout": true, "tags": true, "interruptible": true,
}

// merged returns a job's keys with everything it extends applied underneath: maps merge
// key by key, anything else is replaced by the child. The caller has linted the file, so
// extends cannot be unknown or circular.
func merged(name string, byName map[string]*yaml.Node, seen []string) map[string]any {
	var own map[string]any
	_ = byName[name].Decode(&own)
	if own == nil || slices.Contains(seen, name) {
		return map[string]any{}
	}
	var bases []string
	switch e := own["extends"].(type) {
	case string:
		bases = []string{e}
	case []any:
		for _, b := range e {
			bases = append(bases, fmt.Sprint(b))
		}
	}
	out := map[string]any{}
	for _, b := range bases {
		out = deepMerge(out, merged(b, byName, append(seen, name)))
	}
	return deepMerge(out, own)
}

func deepMerge(base, over map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if bm, ok := out[k].(map[string]any); ok {
			if vm, ok := v.(map[string]any); ok {
				out[k] = deepMerge(bm, vm)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// decide applies rules:, then only:/except:, and returns whether the job is in the pipeline
// and the when: the matching rule asks for, if any.
func decide(cfg map[string]any, env map[string]string) (include bool, when string, err error) {
	if rules, ok := cfg["rules"].([]any); ok {
		for _, r := range rules {
			rule, _ := r.(map[string]any)
			if cond, ok := rule["if"].(string); ok {
				e, cerr := Compile(cond)
				if cerr != nil {
					return false, "", fmt.Errorf("rules:rule if invalid expression syntax")
				}
				if !e(env) {
					continue
				}
			}
			w, _ := rule["when"].(string)
			if w == "never" {
				return false, "", nil
			}
			return true, w, nil
		}
		return false, "", nil
	}
	ref := env["CI_COMMIT_REF_NAME"]
	isTag := env["CI_COMMIT_TAG"] != ""
	if only, ok := cfg["only"]; ok && !refMatches(only, ref, isTag) {
		return false, "", nil
	}
	if except, ok := cfg["except"]; ok && refMatches(except, ref, isTag) {
		return false, "", nil
	}
	return true, "", nil
}

// refMatches evaluates a simple only:/except: list: ref names, /regex/, branches, tags.
func refMatches(v any, ref string, isTag bool) bool {
	items, ok := v.([]any)
	if !ok {
		if m, isMap := v.(map[string]any); isMap {
			items, _ = m["refs"].([]any)
		} else {
			items = []any{v}
		}
	}
	for _, it := range items {
		s := fmt.Sprint(it)
		switch {
		case s == "branches" && !isTag, s == "tags" && isTag, s == ref:
			return true
		case len(s) > 2 && strings.HasPrefix(s, "/") && strings.HasSuffix(s, "/"):
			if re, err := regexp.Compile(s[1 : len(s)-1]); err == nil && re.MatchString(ref) {
				return true
			}
		}
	}
	return false
}

func needsOf(v any) (out []string, optional map[string]bool) {
	optional = map[string]bool{}
	items, _ := v.([]any)
	for _, it := range items {
		switch n := it.(type) {
		case string:
			out = append(out, n)
		case map[string]any:
			if _, cross := n["project"]; !cross {
				if job, ok := n["job"].(string); ok {
					out = append(out, job)
					optional[job] = n["optional"] == true
				}
			}
		}
	}
	return out, optional
}

func allowFailure(v any, when string) bool {
	switch a := v.(type) {
	case bool:
		return a
	case map[string]any:
		return a["exit_codes"] != nil
	}
	return when == "manual"
}
