package spec

import "strings"

// ExpandPresetsInGraph materializes built-in policy presets referenced by Project defaults,
// agents, workflows, and Policy.spec.preset (issue #104). User-defined Policy resources with
// the same metadata.name override built-ins. Mutates g in place.
func ExpandPresetsInGraph(g *ProjectGraph) {
	if g == nil {
		return
	}
	if g.Policies == nil {
		g.Policies = make(map[string]*PolicyResource)
	}
	for _, name := range collectReferencedPolicyNames(g) {
		if !IsBuiltinPreset(name) {
			continue
		}
		if _, exists := g.Policies[name]; exists {
			continue
		}
		preset, err := BuildPreset(name)
		if err != nil {
			continue
		}
		g.Policies[name] = &PolicyResource{
			APIVersion: APIVersionV0,
			Kind:       KindPolicy,
			Metadata:   Metadata{Name: name},
			Spec:       preset,
		}
	}
	for _, pr := range g.Policies {
		if pr == nil {
			continue
		}
		resolved, err := resolvePolicyResourcePreset(&pr.Spec)
		if err != nil || resolved == nil {
			continue
		}
		pr.Spec = *resolved
	}
}

func resolvePolicyResourcePreset(pol *PolicySpec) (*PolicySpec, error) {
	if pol == nil {
		return nil, nil
	}
	presetName := strings.TrimSpace(pol.Preset)
	if presetName == "" {
		if pol.ResolvedPreset != "" {
			return nil, nil
		}
		return nil, nil
	}
	if !IsBuiltinPreset(presetName) {
		return nil, nil
	}
	return ResolvePolicySpec(pol)
}

func collectReferencedPolicyNames(g *ProjectGraph) []string {
	seen := make(map[string]struct{})
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		seen[name] = struct{}{}
	}
	if g.Spec.Defaults != nil {
		add(g.Spec.Defaults.Policy)
	}
	for _, ar := range g.Agents {
		if ar != nil {
			add(ar.Spec.Policy)
		}
	}
	for _, wr := range g.Workflows {
		if wr != nil {
			add(wr.Spec.Policy)
		}
	}
	for name, pr := range g.Policies {
		add(name)
		_ = pr
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	return out
}
