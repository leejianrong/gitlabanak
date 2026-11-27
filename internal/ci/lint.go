// Package ci checks a .gitlab-ci.yml. Messages are modelled on GitLab's wording from public
// documentation and are unverified against a real server (ADR-0003, ADR-0005).
package ci

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Result is what a lint run found. Valid means no errors; warnings do not count.
type Result struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// LocalFile returns the content of a repository file for include:local.
type LocalFile func(path string) (string, bool)

// Keys of the pipeline file that are not jobs.
var reserved = map[string]bool{
	"image": true, "services": true, "stages": true, "types": true, "before_script": true, "after_script": true,
	"variables": true, "cache": true, "include": true, "default": true, "workflow": true, "spec": true,
}

var jobKeys = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`script before_script after_script stage image services variables rules only except when
		allow_failure needs dependencies artifacts cache tags extends environment retry timeout parallel interruptible
		resource_group coverage release trigger inherit start_in secrets id_tokens manual_confirmation run pages hooks
		publish identity dast_configuration`) {
		jobKeys[k] = true
	}
}

var defaultStages = []string{"build", "test", "deploy"}

const maxIncludeDepth = 100

type linter struct {
	local LocalFile
	res   Result
	jobs  []job // in file order, includes after the including file
	stage []string
	seen  map[string]bool       // top-level keys already taken, so the including file wins
	deflt *yaml.Node            // the default: block
	vars  *yaml.Node            // the top-level variables: block
	globs map[string]*yaml.Node // top-level image, services, before_script, after_script, cache: defaults in all but name
}

type job struct {
	name string
	node *yaml.Node
}

// Lint checks content. local resolves include:local; nil means no files exist.
func Lint(content string, local LocalFile) Result {
	l := parse(content, local)
	return l.result()
}

// parse reads content and its local includes and runs every lint rule. Its findings are in
// l.res; the loaded jobs are only meaningful when there are no errors.
func parse(content string, local LocalFile) *linter {
	l := &linter{local: local, seen: map[string]bool{}}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		l.errorf("(<unknown>): %s", strings.TrimPrefix(err.Error(), "yaml: "))
		return l
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		l.errorf("Invalid configuration format")
		return l
	}
	l.load(root.Content[0], nil, 0)
	l.check()
	return l
}

func (l *linter) result() Result {
	if l.res.Errors == nil {
		l.res.Errors = []string{} // JSON [] rather than null
	}
	if l.res.Warnings == nil {
		l.res.Warnings = []string{}
	}
	l.res.Valid = len(l.res.Errors) == 0
	return l.res
}

func (l *linter) errorf(f string, a ...any) {
	l.res.Errors = append(l.res.Errors, fmt.Sprintf(f, a...))
}
func (l *linter) warnf(f string, a ...any) {
	l.res.Warnings = append(l.res.Warnings, fmt.Sprintf(f, a...))
}

// load reads one file's top level into the job list, then follows its includes.
func (l *linter) load(m *yaml.Node, stack []string, depth int) {
	var includes *yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i].Value, m.Content[i+1]
		switch {
		case k == "include":
			includes = v
		case l.seen[k]:
			// The including file overrides what it includes.
		case k == "stages" && depth == 0:
			l.stage = scalars(v)
			l.seen[k] = true
		case k == "stages":
			if l.stage == nil {
				l.stage = scalars(v)
			}
			l.seen[k] = true
		case reserved[k]:
			l.seen[k] = true
			switch k {
			case "default":
				l.deflt = v
			case "variables":
				l.vars = v
			case "image", "services", "before_script", "after_script", "cache":
				if l.globs == nil {
					l.globs = map[string]*yaml.Node{}
				}
				l.globs[k] = v
			}
		default:
			l.seen[k] = true
			l.jobs = append(l.jobs, job{k, v})
		}
	}
	if includes != nil {
		l.follow(includes, stack, depth)
	}
}

func (l *linter) follow(n *yaml.Node, stack []string, depth int) {
	items := []*yaml.Node{n}
	if n.Kind == yaml.SequenceNode {
		items = n.Content
	}
	for _, it := range items {
		var file string
		switch it.Kind {
		case yaml.ScalarNode:
			file = it.Value
		case yaml.MappingNode:
			for i := 0; i+1 < len(it.Content); i += 2 {
				switch k, v := it.Content[i].Value, it.Content[i+1].Value; k {
				case "local":
					file = v
				case "remote", "project", "template", "component":
					l.warnf("include:%s %q is not resolved offline and was skipped", k, v)
				}
			}
		}
		if file == "" {
			continue
		}
		if strings.HasPrefix(file, "http://") || strings.HasPrefix(file, "https://") {
			l.warnf("include:remote %q is not resolved offline and was skipped", file)
			continue
		}
		file = path.Clean("/" + file)[1:]
		content, ok := "", false
		if l.local != nil {
			content, ok = l.local(file)
		}
		switch {
		case !ok:
			l.errorf("Local file `%s` does not exist!", "/"+file)
		case slices.Contains(stack, file):
			l.errorf("Include `/%s` is included recursively", file)
		case depth >= maxIncludeDepth:
			l.errorf("Maximum of %d nested includes reached", maxIncludeDepth)
		default:
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(content), &root); err != nil || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
				l.errorf("Included file `/%s` does not have valid YAML syntax!", file)
				continue
			}
			l.load(root.Content[0], append(stack, file), depth+1)
		}
	}
}

func scalars(n *yaml.Node) []string {
	var out []string
	for _, c := range n.Content {
		out = append(out, c.Value)
	}
	return out
}

func (l *linter) check() {
	stages := l.stage
	if stages == nil {
		stages = defaultStages
	}
	allStages := append(append([]string{".pre"}, stages...), ".post")

	byName := map[string]*yaml.Node{}
	visible := 0
	for _, j := range l.jobs {
		byName[j.name] = j.node
		if !strings.HasPrefix(j.name, ".") {
			visible++
		}
	}
	if visible == 0 && len(l.res.Errors) == 0 {
		l.errorf("jobs config should contain at least one visible job")
	}
	for _, j := range l.jobs {
		l.checkJob(j, byName, allStages)
	}
}

func (l *linter) checkJob(j job, byName map[string]*yaml.Node, stages []string) {
	if j.node.Kind != yaml.MappingNode {
		l.errorf("jobs:%s config should be a hash", j.name)
		return
	}
	keys := mapKeys(j.node)
	var unknown []string
	for _, k := range keys {
		if !jobKeys[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		l.errorf("jobs:%s config contains unknown keys: %s", j.name, strings.Join(unknown, ", "))
	}

	merged, ok := l.mergeExtends(j, byName)
	if !ok {
		return
	}
	if !strings.HasPrefix(j.name, ".") {
		if !merged["script"] && !merged["run"] && !merged["trigger"] {
			l.errorf("jobs:%s config should implement the script:, run:, or trigger: keyword", j.name)
		}
		stage := "test"
		if v := value(j.node, "stage"); v != nil {
			stage = v.Value
		}
		if !slices.Contains(stages, stage) {
			l.errorf("jobs:%s chosen stage does not exist; available stages are %s", j.name, strings.Join(stages, ", "))
		}
	}
	if rules := value(j.node, "rules"); rules != nil {
		for _, r := range rules.Content {
			if cond := value(r, "if"); cond != nil {
				if _, err := Compile(cond.Value); err != nil {
					l.errorf("jobs:%s:rules:rule if invalid expression syntax", j.name)
				}
			}
		}
	}
	if needs := value(j.node, "needs"); needs != nil {
		for _, n := range needsNames(needs) {
			if _, exists := byName[n]; !exists {
				l.errorf("'%s' job needs '%s' job, but '%s' does not exist in the pipeline", j.name, n, n)
			}
		}
	}
}

// mergeExtends returns the keys a job has once everything it extends is applied. It reports
// false after raising an error for an unknown or circular base.
func (l *linter) mergeExtends(j job, byName map[string]*yaml.Node) (map[string]bool, bool) {
	have := map[string]bool{}
	visited := map[string]bool{}
	var walk func(name string, n *yaml.Node) bool
	walk = func(name string, n *yaml.Node) bool {
		if visited[name] {
			l.errorf("jobs:%s extends: circular dependency detected", j.name)
			return false
		}
		visited[name] = true
		for _, k := range mapKeys(n) {
			have[k] = true
		}
		if ext := value(n, "extends"); ext != nil {
			bases := scalars(ext)
			if ext.Kind == yaml.ScalarNode {
				bases = []string{ext.Value}
			}
			for _, b := range bases {
				bn, ok := byName[b]
				if !ok {
					l.errorf("jobs:%s extends unknown: %s", j.name, b)
					return false
				}
				if bn.Kind == yaml.MappingNode && !walk(b, bn) {
					return false
				}
			}
		}
		return true
	}
	if j.node.Kind != yaml.MappingNode {
		return have, false
	}
	return have, walk(j.name, j.node)
}

func mapKeys(n *yaml.Node) []string {
	var out []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, n.Content[i].Value)
	}
	return out
}

func value(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// needsNames lists the jobs a needs: entry points at: plain names or {job: name} maps.
func needsNames(n *yaml.Node) []string {
	var out []string
	for _, c := range n.Content {
		switch c.Kind {
		case yaml.ScalarNode:
			out = append(out, c.Value)
		case yaml.MappingNode:
			if v := value(c, "job"); v != nil {
				if cross := value(c, "project"); cross == nil {
					out = append(out, v.Value)
				}
			}
		}
	}
	return out
}
