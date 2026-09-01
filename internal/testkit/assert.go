package testkit

import (
	"fmt"
	"strings"

	"github.com/Terfyn/terfyn/internal/config"
	"github.com/Terfyn/terfyn/internal/effects"
	"gopkg.in/yaml.v3"
)

// AssertSuite is a capability-assertion file under tests/ (issue #332): declarative, model-free
// invariants over the effect bound, checked statically (no run) so a project's core capability
// guarantees live next to the agents and fail terfyn test in CI when they drift.
type AssertSuite struct {
	APIVersion string      `yaml:"apiVersion"`
	Name       string      `yaml:"name"`
	Assert     assertBlock `yaml:"assert"`
}

type assertBlock struct {
	ForbidEffect     []rootEffectYAML `yaml:"forbidEffect"`
	ExpectAutonomous []rootEffectYAML `yaml:"expectAutonomous"`
	ExpectGated      []string         `yaml:"expectGated"`
}

// rootEffectYAML names a bound root and an effect. The root may be given as root:, agent:, or
// workflow: (aliases) so a suite reads naturally either way.
type rootEffectYAML struct {
	Root     string `yaml:"root"`
	Agent    string `yaml:"agent"`
	Workflow string `yaml:"workflow"`
	Effect   string `yaml:"effect"`
}

func (r rootEffectYAML) rootName() string {
	for _, s := range []string{r.Root, r.Agent, r.Workflow} {
		if v := strings.TrimSpace(s); v != "" {
			return v
		}
	}
	return ""
}

// isAssertSuiteBytes reports whether a tests/ file is a capability-assertion suite (has a top-level
// `assert:` mapping) rather than a workflow run suite (`workflow:`).
func isAssertSuiteBytes(data []byte) bool {
	var probe struct {
		Assert *assertBlock `yaml:"assert"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Assert != nil
}

// ParseAssertSuiteBytes decodes and validates a capability-assertion suite.
func ParseAssertSuiteBytes(data []byte) (*AssertSuite, error) {
	var s AssertSuite
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("testkit: parse assert suite: %w", err)
	}
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		s.Name = "capability-assertions"
	}
	if s.Assert.empty() {
		return nil, fmt.Errorf("testkit: assert suite %q has no assertions", s.Name)
	}
	for i, re := range s.Assert.ForbidEffect {
		if re.rootName() == "" || strings.TrimSpace(re.Effect) == "" {
			return nil, fmt.Errorf("testkit: forbidEffect[%d] needs a root (root/agent/workflow) and effect", i)
		}
	}
	for i, re := range s.Assert.ExpectAutonomous {
		if re.rootName() == "" || strings.TrimSpace(re.Effect) == "" {
			return nil, fmt.Errorf("testkit: expectAutonomous[%d] needs a root (root/agent/workflow) and effect", i)
		}
	}
	for i, u := range s.Assert.ExpectGated {
		if strings.TrimSpace(u) == "" {
			return nil, fmt.Errorf("testkit: expectGated[%d] is empty", i)
		}
	}
	return &s, nil
}

func (a assertBlock) empty() bool {
	return len(a.ForbidEffect)+len(a.ExpectAutonomous)+len(a.ExpectGated) == 0
}

// RunAssertSuite resolves the project graph (with the environment overlay) and evaluates every
// assertion statically, one CaseOutcome per assertion so each invariant is an individual pass/fail
// row. No workflow is run and no model is invoked.
func RunAssertSuite(projectRoot string, opts RunOptions, suitePath string, s *AssertSuite) []CaseOutcome {
	rc, err := config.Resolve(config.ResolveOptions{
		ProjectRoot: projectRoot,
		Env:         strings.TrimSpace(opts.EnvironmentName),
	})
	if err != nil {
		return []CaseOutcome{{File: suitePath, Case: s.Name, Passed: false, Detail: fmt.Sprintf("resolve config: %v", err)}}
	}
	graph := rc.Graph()

	var outcomes []CaseOutcome
	item := func(label string, single effects.CapabilityAssertions) {
		out := CaseOutcome{File: suitePath, Case: label, Passed: true}
		if vs := single.Evaluate(graph); len(vs) > 0 {
			out.Passed = false
			out.Detail = vs[0].Detail
		}
		outcomes = append(outcomes, out)
	}

	for _, re := range s.Assert.ForbidEffect {
		item(fmt.Sprintf("forbid %s → %s", re.rootName(), strings.TrimSpace(re.Effect)),
			effects.CapabilityAssertions{ForbidEffect: []effects.RootEffect{{Root: re.rootName(), Effect: strings.TrimSpace(re.Effect)}}})
	}
	for _, re := range s.Assert.ExpectAutonomous {
		item(fmt.Sprintf("expectAutonomous %s → %s", re.rootName(), strings.TrimSpace(re.Effect)),
			effects.CapabilityAssertions{ExpectAutonomous: []effects.RootEffect{{Root: re.rootName(), Effect: strings.TrimSpace(re.Effect)}}})
	}
	for _, u := range s.Assert.ExpectGated {
		item(fmt.Sprintf("expectGated %s", strings.TrimSpace(u)),
			effects.CapabilityAssertions{ExpectGated: []string{strings.TrimSpace(u)}})
	}
	return outcomes
}
