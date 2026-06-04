package inspect

import "fmt"

// ValidateInspectPort returns an error when port is not 0 (ephemeral) or in 1..65535.
func ValidateInspectPort(port int) error {
	if port == 0 {
		return nil
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("inspect: port %d out of range (use 1-65535, or 0 for an ephemeral test port)", port)
	}
	return nil
}
