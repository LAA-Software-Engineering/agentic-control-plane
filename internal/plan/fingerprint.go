package plan

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// DeploymentStateFingerprint returns a stable SHA-256 hex digest of deployment rows for env.
// It covers every applied_resources row for env (kind, name, spec hash, normalized JSON) and
// the applied_projects row for (env, projectName), or a sentinel when that row is missing.
// Used for optimistic concurrency between plan and apply (issue #78).
func DeploymentStateFingerprint(ctx context.Context, dep state.DeploymentStore, env, projectName string) (string, error) {
	if dep == nil {
		return "", errors.New("plan: nil deployment store")
	}
	env = strings.TrimSpace(env)
	projectName = strings.TrimSpace(projectName)
	if env == "" || projectName == "" {
		return "", errors.New("plan: empty env or project name")
	}
	list, err := dep.ListAppliedResourcesByEnv(ctx, env)
	if err != nil {
		return "", err
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Kind != list[j].Kind {
			return list[i].Kind < list[j].Kind
		}
		return list[i].Name < list[j].Name
	})
	var b strings.Builder
	for _, r := range list {
		fmt.Fprintf(&b, "%s\x00%s\x00%s\x00%s\n", r.Kind, r.Name, r.SpecHash, r.NormalizedSpecJSON)
	}
	proj, err := dep.GetAppliedProject(ctx, env, projectName)
	switch {
	case err != nil && errors.Is(err, sql.ErrNoRows):
		b.WriteString("applied_projects\x00MISSING\n")
	case err != nil:
		return "", err
	default:
		if proj == nil {
			b.WriteString("applied_projects\x00MISSING\n")
		} else {
			fmt.Fprintf(&b, "applied_projects\x00%s\x00%s\x00%s\n", proj.ProjectName, proj.Env, proj.Version)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), nil
}
