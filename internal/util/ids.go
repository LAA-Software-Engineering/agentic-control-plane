package util

import "github.com/google/uuid"

// NewRunID returns a new unique run identifier (issue #23, design doc section 14.2).
func NewRunID() string {
	return uuid.NewString()
}

// NewRequestID returns a per-invocation correlation identifier distinct from [NewRunID] (issue #111).
func NewRequestID() string {
	return uuid.NewString()
}
