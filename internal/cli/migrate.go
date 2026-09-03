package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Terfyn/terfyn/internal/lang"
	"github.com/Terfyn/terfyn/internal/lang/lower"
	"github.com/Terfyn/terfyn/internal/lang/raise"
	"github.com/Terfyn/terfyn/internal/models"
	"github.com/Terfyn/terfyn/internal/project"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/spf13/cobra"
)

// newMigrateCmd registers `terfyn migrate --to-agent` (issue #440, Phase 2b): convert a project's
// YAML-authored resources into .agent source, the sole authoring surface (#430). It raises the YAML
// declarative resources (providers/tools/policies/environments/agents) to a .agent file; any
// construct with no .agent form — notably YAML-authored workflows — is reported as needing manual
// migration rather than emitted lossily.
func newMigrateCmd() *cobra.Command {
	var toAgent bool
	var output string
	var force bool
	cmd := &cobra.Command{
		Use:          "migrate",
		Short:        "Migrate YAML-authored resources to .agent source",
		SilenceUsage: true,
		Long: `Convert a project's YAML-authored resources into .agent source (ADR 003, issue #430).

--to-agent raises the YAML declarative resources — custom provider aliases, tools, policies,
environments, and agents — into a single .agent file, printed to stdout by default. Built-in model
providers (anthropic, openai, mock, …) are implicit and dropped; imports are auto-discovered and
dropped. Migration is lossless or it refuses: any construct with no .agent authoring form (a
YAML-authored workflow, or a field like tool.retry / policy.security) is listed as needing manual
migration and the file is not written unless --force.

Pass --output FILE to write the .agent source (default: print to stdout). This command is
non-destructive: it never deletes the existing YAML — review the output, then remove the migrated
YAML yourself.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !toAgent {
				return NewExitErrorf(ExitValidationError, "migrate: specify a direction (only --to-agent is supported)")
			}
			return runMigrateToAgent(cmd, output, force)
		},
	}
	cmd.Flags().BoolVar(&toAgent, "to-agent", false, "migrate YAML resources to .agent source")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write the .agent source to this file (default: stdout)")
	cmd.Flags().BoolVar(&force, "force", false, "write the .agent output even when some resources could not be migrated")
	return cmd
}

func runMigrateToAgent(cmd *cobra.Command, output string, force bool) error {
	g := Globals()

	// Legacy-compatibility (ADR 007 step 1): fields removed from the canonical model (tool.permissions,
	// policy.security, agent.memory, agent.runtime) would make the strict loader reject a legacy YAML
	// project. Strip them from a temp copy first — accept, warn once per field per resource, and omit —
	// so migration never hard-fails on a valid old project and nothing meaningful is silently dropped.
	migRoot, legacyWarnings, cleanup, err := prepareMigrationRoot(g.ProjectRoot)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}
	defer cleanup()

	graph, yamlPaths, err := project.LoadYAMLResources(migRoot)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	// A providers.models entry that merely restates a built-in namespace (e.g. mock: {type: mock})
	// needs no .agent declaration — built-ins resolve implicitly (#430). Drop those so the migration
	// does not emit a redundant `provider` block; a customized entry (differing config) is kept.
	dropRedundantBuiltinProviders(graph)

	file, unsupported := raise.Graph(graph)
	source := lang.Print(file)

	// Verify the generated source parses and re-lowers without diagnostics — a self-check that the
	// raised declarations are well-formed before we hand them to the author (the byte-identical
	// spec round-trip is proven in internal/lang/raise's tests).
	if _, diags := lang.Parse("migrated.agent", source); diags.HasErrors() {
		return NewExitErrorf(ExitGenericFailure, "migrate: generated .agent did not parse (internal error):\n%s", diags.Error())
	}
	if pf, _ := lang.Parse("migrated.agent", source); pf != nil {
		if _, ld := lower.LowerFile(pf, lower.Options{}); ld.HasErrors() {
			return NewExitErrorf(ExitGenericFailure, "migrate: generated .agent did not re-lower (internal error):\n%s", ld.Error())
		}
	}

	reportMigrationSummary(cmd, yamlPaths, unsupported, legacyWarnings)

	if len(unsupported) > 0 && !force {
		return NewExitErrorf(ExitValidationError, "migrate: %d resource(s) could not be migrated to .agent (see above); resolve them or pass --force to write the rest", len(unsupported))
	}

	if output == "" {
		_, err := fmt.Fprint(cmd.OutOrStdout(), source)
		return err
	}
	if err := writeMigratedFile(output, source, force); err != nil {
		return NewExitError(ExitGenericFailure, err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s — review it, then remove the migrated YAML\n", output)
	return nil
}

// reportMigrationSummary prints, to stderr, which YAML files were read and any resources that could
// not be migrated, grouped for a clean actionable report.
func reportMigrationSummary(cmd *cobra.Command, yamlPaths []string, unsupported []raise.Unsupported, legacyWarnings []string) {
	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "migrate --to-agent: read %d YAML file(s)\n", len(yamlPaths))
	if len(legacyWarnings) > 0 {
		fmt.Fprintf(w, "\n%d deprecated field(s) dropped (removed from the canonical model, ADR 007):\n", len(legacyWarnings))
		for _, warn := range legacyWarnings {
			fmt.Fprintf(w, "  - %s\n", warn)
		}
	}
	if len(unsupported) == 0 {
		return
	}
	sorted := append([]raise.Unsupported(nil), unsupported...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Kind != sorted[j].Kind {
			return sorted[i].Kind < sorted[j].Kind
		}
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].Field < sorted[j].Field
	})
	fmt.Fprintf(w, "\n%d resource construct(s) need manual migration (no .agent authoring form):\n", len(sorted))
	for _, u := range sorted {
		fmt.Fprintf(w, "  - %s %q: %s — %s\n", u.Kind, u.Name, u.Field, u.Detail)
	}
	fmt.Fprintln(w, "\nKeep these in YAML, or resolve them, then re-run.")
}

// dropRedundantBuiltinProviders removes provider aliases whose config exactly equals the built-in
// namespace of the same name, since those resolve implicitly and need no .agent declaration (#440).
func dropRedundantBuiltinProviders(graph *spec.ProjectGraph) {
	if graph.Spec.Providers == nil || graph.Spec.Providers.Models == nil {
		return
	}
	for name, cfg := range graph.Spec.Providers.Models {
		if builtin, ok := models.BuiltinProviderConfig(name); ok && builtin == cfg {
			delete(graph.Spec.Providers.Models, name)
		}
	}
	if len(graph.Spec.Providers.Models) == 0 {
		graph.Spec.Providers = nil
	}
}

// writeMigratedFile writes the .agent source to output via a temp file + rename, refusing to
// overwrite an existing file unless force is set.
func writeMigratedFile(output, source string, force bool) error {
	if filepath.Ext(output) != ".agent" {
		return fmt.Errorf("migrate: --output %q must end in .agent", output)
	}
	if _, err := os.Stat(output); err == nil && !force {
		return fmt.Errorf("migrate: %s already exists (pass --force to overwrite)", output)
	}
	return writeAgentFileAtomic(output, source)
}
