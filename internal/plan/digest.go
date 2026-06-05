package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// ResolvedGraphDigest returns a stable SHA-256 hex digest over canonical desired resource JSON.
func ResolvedGraphDigest(g *spec.ProjectGraph) (string, error) {
	if g == nil {
		return "", fmt.Errorf("plan: nil project graph")
	}
	rows, err := desiredRows(g)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "%s\x00%s\x00%s\n", row.id.Kind, row.id.Name, row.json)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), nil
}
