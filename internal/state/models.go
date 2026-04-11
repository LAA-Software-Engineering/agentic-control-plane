package state

import "time"

// AppliedResource is one row in applied_resources (design doc §14.1).
type AppliedResource struct {
	Kind               string
	Name               string
	Env                string
	SpecHash           string
	NormalizedSpecJSON string
	AppliedAt          time.Time
}

// AppliedProject is one row in applied_projects (design doc §14.1).
type AppliedProject struct {
	ProjectName string
	Env         string
	Version     string
	AppliedAt   time.Time
}
