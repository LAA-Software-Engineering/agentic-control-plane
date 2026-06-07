package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime/catalog"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/schema"
)

// ValidateProjectGraph runs MVP validation rules from design doc §9.1–§9.5 on a merged graph.
// projectRoot is used to resolve Agent/Workflow input and output schema paths (§9.2).
//
// Multiple violations are combined with [errors.Join]. Callers (e.g. agentctl validate)
// should treat a non-nil return as exit code 2 per §11.2.
func ValidateProjectGraph(g *ProjectGraph, projectRoot string) error {
	if g == nil {
		return nil
	}
	root := filepath.Clean(projectRoot)

	var errs []error
	errs = append(errs, validateMetadataKeys(g)...)
	errs = append(errs, validateRuntimes(g)...)
	errs = append(errs, validateToolSpecs(g)...)
	errs = append(errs, validatePolicySpecs(g)...)
	errs = append(errs, validatePolicyPresets(g)...)
	errs = append(errs, validateAgentSpecs(g)...)
	errs = append(errs, validateEnvironmentOverrides(g)...)
	errs = append(errs, validateProjectTelemetry(g)...)
	errs = append(errs, validateExecutionLimitsGraph(g)...)
	errs = append(errs, validateSchemaFiles(g, root)...)
	if err := ResolveReferences(g); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func validateMetadataKeys(g *ProjectGraph) []error {
	var errs []error
	check := func(kind string, m map[string]any) {
		for k, v := range m {
			name := metaName(v)
			if name == "" {
				continue
			}
			if k != name {
				errs = append(errs, fmt.Errorf("%s: map key %q does not match metadata.name %q", kind, k, name))
			}
		}
	}
	ag := make(map[string]any)
	for k, v := range g.Agents {
		ag[k] = v
	}
	tg := make(map[string]any)
	for k, v := range g.Tools {
		tg[k] = v
	}
	wg := make(map[string]any)
	for k, v := range g.Workflows {
		wg[k] = v
	}
	pg := make(map[string]any)
	for k, v := range g.Policies {
		pg[k] = v
	}
	eg := make(map[string]any)
	for k, v := range g.Environments {
		eg[k] = v
	}
	check("Agent", ag)
	check("Tool", tg)
	check("Workflow", wg)
	check("Policy", pg)
	check("Environment", eg)
	return errs
}

func metaName(v any) string {
	switch t := v.(type) {
	case *AgentResource:
		if t == nil {
			return ""
		}
		return strings.TrimSpace(t.Metadata.Name)
	case *ToolResource:
		if t == nil {
			return ""
		}
		return strings.TrimSpace(t.Metadata.Name)
	case *WorkflowResource:
		if t == nil {
			return ""
		}
		return strings.TrimSpace(t.Metadata.Name)
	case *PolicyResource:
		if t == nil {
			return ""
		}
		return strings.TrimSpace(t.Metadata.Name)
	case *EnvironmentResource:
		if t == nil {
			return ""
		}
		return strings.TrimSpace(t.Metadata.Name)
	default:
		return ""
	}
}

// validateRuntimes rejects unknown spec.runtime names via the runtime registry (issue #114).
// Empty means implicit local; only registered names are allowed when set.
func validateRuntimes(g *ProjectGraph) []error {
	var errs []error
	check := func(prefix, name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if !catalog.IsKnown(name) {
			errs = append(errs, fmt.Errorf("%s %q is unknown (valid: %s)", prefix, name, strings.Join(catalog.KnownNames(), ", ")))
		}
	}
	if g.Spec.Defaults != nil {
		check("Project: defaults.runtime", g.Spec.Defaults.Runtime)
	}
	for name, ar := range g.Agents {
		if ar == nil {
			continue
		}
		check(fmt.Sprintf("Agent/%s: spec.runtime", name), ar.Spec.Runtime)
	}
	for name, wr := range g.Workflows {
		if wr == nil {
			continue
		}
		check(fmt.Sprintf("Workflow/%s: spec.runtime", name), wr.Spec.Runtime)
	}
	return errs
}

func validateToolSpecs(g *ProjectGraph) []error {
	var errs []error
	for name, tr := range g.Tools {
		if tr == nil {
			continue
		}
		t := strings.TrimSpace(tr.Spec.Type)
		switch t {
		case "mcp":
			if tr.Spec.MCP == nil {
				errs = append(errs, fmt.Errorf("Tool/%s: type mcp requires spec.mcp", name))
			} else {
				errs = append(errs, validateToolMCP(name, tr.Spec.MCP)...)
			}
		case "http":
			if tr.Spec.HTTP == nil {
				errs = append(errs, fmt.Errorf("Tool/%s: type http requires spec.http", name))
			}
		case "native", "mock":
			// MVP: no required transport block
		case "":
			errs = append(errs, fmt.Errorf("Tool/%s: spec.type is required", name))
		default:
			errs = append(errs, fmt.Errorf("Tool/%s: unsupported spec.type %q", name, t))
		}
		if tr.Spec.Permissions != nil {
			for i, a := range tr.Spec.Permissions.Allow {
				if strings.TrimSpace(a) == "" {
					errs = append(errs, fmt.Errorf("Tool/%s: permissions.allow[%d] must be non-empty", name, i))
				}
			}
			for i, d := range tr.Spec.Permissions.Deny {
				if strings.TrimSpace(d) == "" {
					errs = append(errs, fmt.Errorf("Tool/%s: permissions.deny[%d] must be non-empty", name, i))
				}
			}
		}
		if tr.Spec.Retry != nil && tr.Spec.Retry.MaxAttempts < 0 {
			errs = append(errs, fmt.Errorf("Tool/%s: retry.maxAttempts must be non-negative", name))
		}
		errs = append(errs, validateToolSafety(name, tr.Spec.Safety)...)
	}
	return errs
}

func validateToolSafety(toolName string, s *ToolSafety) []error {
	if s == nil {
		return nil
	}
	var errs []error
	prefix := fmt.Sprintf("Tool/%s: spec.safety", toolName)
	if s.Trusted == nil && s.SideEffects == nil && s.RequiresApproval == nil {
		errs = append(errs, fmt.Errorf("%s: at least one of trusted, sideEffects, requiresApproval must be set (or omit safety to use fail-closed defaults via normalize)", prefix))
	}
	return errs
}

func validateToolMCP(name string, m *ToolMCP) []error {
	var errs []error
	trans := strings.ToLower(strings.TrimSpace(m.Transport))
	if trans == "" {
		errs = append(errs, fmt.Errorf("Tool/%s: spec.mcp.transport is required (stdio or http)", name))
		return errs
	}
	switch trans {
	case "stdio":
		if strings.TrimSpace(m.URL) != "" {
			errs = append(errs, fmt.Errorf("Tool/%s: mcp stdio transport must not set url", name))
		}
		if strings.TrimSpace(m.Command) == "" {
			errs = append(errs, fmt.Errorf("Tool/%s: mcp stdio requires command", name))
		}
	case "http":
		if strings.TrimSpace(m.Command) != "" || len(m.Args) > 0 {
			errs = append(errs, fmt.Errorf("Tool/%s: mcp http transport must not set command or args", name))
		}
		if strings.TrimSpace(m.URL) == "" {
			errs = append(errs, fmt.Errorf("Tool/%s: mcp http transport requires url", name))
		}
	default:
		errs = append(errs, fmt.Errorf("Tool/%s: unsupported mcp.transport %q (stdio or http)", name, m.Transport))
	}
	return errs
}

func validatePolicySpecs(g *ProjectGraph) []error {
	var errs []error
	for name, pr := range g.Policies {
		if pr == nil {
			continue
		}
		if ex := pr.Spec.Execution; ex != nil {
			if ex.MaxWallClockSeconds < 0 {
				errs = append(errs, fmt.Errorf("Policy/%s: execution.maxWallClockSeconds must be non-negative", name))
			}
			if ex.MaxTotalCostUsd < 0 {
				errs = append(errs, fmt.Errorf("Policy/%s: execution.maxTotalCostUsd must be non-negative", name))
			}
		}
		if ap := pr.Spec.Approvals; ap != nil {
			if ApprovalRequireAllTools(ap) && ApprovalPermissive(ap) {
				errs = append(errs, fmt.Errorf("Policy/%s: approvals.requireAllTools and approvals.permissive are mutually exclusive", name))
			}
			seen := make(map[string]struct{})
			for i, act := range ap.RequiredFor {
				a := strings.TrimSpace(act)
				if a == "" {
					errs = append(errs, fmt.Errorf("Policy/%s: approvals.requiredFor[%d] must be non-empty", name, i))
					continue
				}
				if _, dup := seen[a]; dup {
					errs = append(errs, fmt.Errorf("Policy/%s: duplicate approvals.requiredFor entry %q", name, a))
				}
				seen[a] = struct{}{}
			}
		}
		errs = append(errs, validateHitlPolicy(name, pr.Spec.Hitl, g)...)
	}
	return errs
}

func validateHitlPolicy(policyName string, hitl *HitlPolicy, g *ProjectGraph) []error {
	if hitl == nil {
		return nil
	}
	var errs []error
	prefix := fmt.Sprintf("Policy/%s: hitl", policyName)
	for toolName, iv := range hitl.InterruptOn {
		tn := strings.TrimSpace(toolName)
		if tn == "" {
			errs = append(errs, fmt.Errorf("%s.interruptOn contains empty tool name", prefix))
			continue
		}
		if g != nil && g.Tools != nil {
			if _, ok := g.Tools[tn]; !ok {
				errs = append(errs, fmt.Errorf("%s.interruptOn[%q]: no Tool/%s in project (interruptOn keys must match Tool metadata.name)", prefix, toolName, tn))
			}
		}
		if !iv.Enabled {
			errs = append(errs, fmt.Errorf("%s.interruptOn[%q] must be true or a config object", prefix, toolName))
			continue
		}
		if iv.Config != nil {
			errs = append(errs, validateHitlInterruptConfig(prefix+".interruptOn["+toolName+"]", iv.Config, g)...)
		}
	}
	if hitl.ToolSwitchMap != nil {
		for from, targets := range hitl.ToolSwitchMap {
			if strings.TrimSpace(from) == "" {
				errs = append(errs, fmt.Errorf("%s.toolSwitchMap contains empty source key", prefix))
			}
			for i, tgt := range targets {
				if strings.TrimSpace(tgt) == "" {
					errs = append(errs, fmt.Errorf("%s.toolSwitchMap[%q][%d] must be non-empty", prefix, from, i))
				}
			}
		}
	}
	return errs
}

func validateHitlInterruptConfig(prefix string, cfg *HitlInterruptConfig, g *ProjectGraph) []error {
	if cfg == nil {
		return nil
	}
	var errs []error
	seenDecisions := make(map[HitlDecisionKind]struct{})
	for i, d := range cfg.AllowedDecisions {
		if !IsValidHitlDecisionKind(d) {
			errs = append(errs, fmt.Errorf("%s.allowedDecisions[%d]: unknown decision %q", prefix, i, d))
			continue
		}
		if _, dup := seenDecisions[d]; dup {
			errs = append(errs, fmt.Errorf("%s.allowedDecisions: duplicate %q", prefix, d))
		}
		seenDecisions[d] = struct{}{}
	}
	for i, tn := range cfg.AllowedEditTools {
		tn = strings.TrimSpace(tn)
		if tn == "" {
			errs = append(errs, fmt.Errorf("%s.allowedEditTools[%d] must be non-empty", prefix, i))
			continue
		}
		if g != nil && g.Tools != nil {
			if _, ok := g.Tools[tn]; !ok {
				errs = append(errs, fmt.Errorf("%s.allowedEditTools[%q]: no Tool/%s in project", prefix, tn, tn))
			}
		}
	}
	if overlap := intersectStringSets(cfg.AllowedEditArgs, cfg.DeniedEditArgs); len(overlap) > 0 {
		errs = append(errs, fmt.Errorf("%s: allowedEditArgs and deniedEditArgs overlap: %v", prefix, overlap))
	}
	if overlap := intersectStringSets(cfg.AllowedEditPaths, cfg.DeniedEditPaths); len(overlap) > 0 {
		errs = append(errs, fmt.Errorf("%s: allowedEditPaths and deniedEditPaths overlap: %v", prefix, overlap))
	}
	if cfg.SwitchMap != nil {
		for from, targets := range cfg.SwitchMap {
			if strings.TrimSpace(from) == "" {
				errs = append(errs, fmt.Errorf("%s.switchMap contains empty source key", prefix))
			}
			for i, tgt := range targets {
				if strings.TrimSpace(tgt) == "" {
					errs = append(errs, fmt.Errorf("%s.switchMap[%q][%d] must be non-empty", prefix, from, i))
				}
			}
		}
	}
	return errs
}

func intersectStringSets(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	setB := make(map[string]struct{}, len(b))
	for _, s := range b {
		setB[strings.TrimSpace(s)] = struct{}{}
	}
	var out []string
	for _, s := range a {
		s = strings.TrimSpace(s)
		if _, ok := setB[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

func validatePolicyPresets(g *ProjectGraph) []error {
	var errs []error
	if g.Spec.Defaults != nil {
		if p := strings.TrimSpace(g.Spec.Defaults.Policy); p != "" {
			if _, ok := g.Policies[p]; !ok && !IsBuiltinPreset(p) {
				errs = append(errs, fmt.Errorf("Project: defaults.policy %q is not a Policy resource or built-in preset (%s)",
					p, strings.Join(BuiltinPresetNames(), ", ")))
			}
		}
	}
	for name, pr := range g.Policies {
		if pr == nil {
			continue
		}
		if preset := strings.TrimSpace(pr.Spec.Preset); preset != "" && !IsBuiltinPreset(preset) {
			errs = append(errs, fmt.Errorf("Policy/%s: unknown preset %q (valid: %s)",
				name, preset, strings.Join(BuiltinPresetNames(), ", ")))
		}
	}
	return errs
}

func validateAgentSpecs(g *ProjectGraph) []error {
	var errs []error
	for name, ar := range g.Agents {
		if ar == nil {
			continue
		}
		if c := ar.Spec.Constraints; c != nil {
			if c.MaxIterations < 0 {
				errs = append(errs, fmt.Errorf("Agent/%s: constraints.maxIterations must be non-negative", name))
			}
			if c.TimeoutSeconds < 0 {
				errs = append(errs, fmt.Errorf("Agent/%s: constraints.timeoutSeconds must be non-negative", name))
			}
		}
	}
	return errs
}

func validateEnvironmentOverrides(g *ProjectGraph) []error {
	var errs []error
	for envName, er := range g.Environments {
		if er == nil || er.Spec.Overrides == nil {
			continue
		}
		ov := er.Spec.Overrides
		for an := range ov.Agents {
			if _, ok := g.Agents[an]; !ok {
				errs = append(errs, fmt.Errorf("Environment/%s: overrides.agents references unknown Agent/%s", envName, an))
			}
		}
		for pn := range ov.Policies {
			if _, ok := g.Policies[pn]; !ok {
				errs = append(errs, fmt.Errorf("Environment/%s: overrides.policies references unknown Policy/%s", envName, pn))
			}
		}
	}
	return errs
}

func validateSchemaFiles(g *ProjectGraph, projectRoot string) []error {
	var errs []error
	for name, ar := range g.Agents {
		if ar == nil {
			continue
		}
		if ar.Spec.Input != nil {
			if p := strings.TrimSpace(ar.Spec.Input.Schema); p != "" {
				if err := schemaFileReadable(projectRoot, p); err != nil {
					errs = append(errs, fmt.Errorf("Agent/%s input.schema: %w", name, err))
				}
			}
		}
		if ar.Spec.Output != nil {
			if p := strings.TrimSpace(ar.Spec.Output.Schema); p != "" {
				if err := schemaFileReadable(projectRoot, p); err != nil {
					errs = append(errs, fmt.Errorf("Agent/%s output.schema: %w", name, err))
				}
			}
		}
	}
	for name, wr := range g.Workflows {
		if wr == nil {
			continue
		}
		if wr.Spec.Input != nil {
			if p := strings.TrimSpace(wr.Spec.Input.Schema); p != "" {
				if err := schemaFileReadable(projectRoot, p); err != nil {
					errs = append(errs, fmt.Errorf("Workflow/%s input.schema: %w", name, err))
				}
			}
		}
	}
	return errs
}

func schemaFileReadable(projectRoot, ref string) error {
	abs, err := schema.ResolveSchemaPath(projectRoot, ref)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("schema file %q: %w", abs, err)
	}
	return nil
}
