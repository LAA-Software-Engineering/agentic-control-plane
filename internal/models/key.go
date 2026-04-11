package models

import (
	"fmt"
	"os"
	"strings"
)

// ResolveAPIKeyFrom parses apiKeyFrom values from project YAML (§7.1), e.g. "env:OPENAI_API_KEY".
func ResolveAPIKeyFrom(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", fmt.Errorf("models: empty apiKeyFrom")
	}
	if strings.HasPrefix(spec, "env:") {
		name := strings.TrimSpace(strings.TrimPrefix(spec, "env:"))
		if name == "" {
			return "", fmt.Errorf("models: apiKeyFrom env: requires a variable name")
		}
		v := os.Getenv(name)
		if v == "" {
			return "", fmt.Errorf("models: environment variable %q is not set", name)
		}
		return v, nil
	}
	return "", fmt.Errorf("models: unsupported apiKeyFrom %q (MVP: env:VAR only)", spec)
}
