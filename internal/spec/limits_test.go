package spec

import "testing"

func TestDefaultExecutionLimits(t *testing.T) {
	t.Parallel()
	def := DefaultExecutionLimits()
	if def.MaxToolInputBytes != DefaultMaxToolInputBytes {
		t.Fatalf("MaxToolInputBytes = %d", def.MaxToolInputBytes)
	}
	if def.MaxToolOutputBytes != DefaultMaxToolOutputBytes {
		t.Fatalf("MaxToolOutputBytes = %d", def.MaxToolOutputBytes)
	}
	if def.MaxCheckpointBytes != DefaultMaxCheckpointBytes {
		t.Fatalf("MaxCheckpointBytes = %d", def.MaxCheckpointBytes)
	}
	if def.ToolInputExceedPolicy != LimitExceedTruncate {
		t.Fatalf("ToolInputExceedPolicy = %q", def.ToolInputExceedPolicy)
	}
	if def.ToolOutputExceedPolicy != LimitExceedTruncate {
		t.Fatalf("ToolOutputExceedPolicy = %q", def.ToolOutputExceedPolicy)
	}
	if def.CheckpointExceedPolicy != LimitExceedFail {
		t.Fatalf("CheckpointExceedPolicy = %q", def.CheckpointExceedPolicy)
	}
}

func TestDefaultExecutionLimits_binaryUnits(t *testing.T) {
	t.Parallel()
	if DefaultMaxToolInputBytes != 262144 {
		t.Fatalf("DefaultMaxToolInputBytes = %d", DefaultMaxToolInputBytes)
	}
	if DefaultMaxCheckpointBytes != 1048576 {
		t.Fatalf("DefaultMaxCheckpointBytes = %d", DefaultMaxCheckpointBytes)
	}
}

func TestMergeExecutionLimits_partialOverride(t *testing.T) {
	t.Parallel()
	base := ExecutionLimits{
		MaxToolInputBytes:      100,
		MaxToolOutputBytes:     200,
		MaxCheckpointBytes:     300,
		ToolInputExceedPolicy:  LimitExceedTruncate,
		ToolOutputExceedPolicy: LimitExceedTruncate,
		CheckpointExceedPolicy: LimitExceedFail,
	}
	override := &ExecutionLimits{
		MaxToolOutputBytes:     500,
		ToolOutputExceedPolicy: LimitExceedFail,
	}
	got := MergeExecutionLimits(base, override)
	if got.MaxToolInputBytes != 100 {
		t.Fatalf("MaxToolInputBytes = %d", got.MaxToolInputBytes)
	}
	if got.MaxToolOutputBytes != 500 {
		t.Fatalf("MaxToolOutputBytes = %d", got.MaxToolOutputBytes)
	}
	if got.MaxCheckpointBytes != 300 {
		t.Fatalf("MaxCheckpointBytes = %d", got.MaxCheckpointBytes)
	}
	if got.ToolOutputExceedPolicy != LimitExceedFail {
		t.Fatalf("ToolOutputExceedPolicy = %q", got.ToolOutputExceedPolicy)
	}
}

func TestNormalizeExecutionLimits_maxStateBytesAlias(t *testing.T) {
	t.Parallel()
	l := ExecutionLimits{MaxStateBytes: 42}
	NormalizeExecutionLimits(&l)
	if l.MaxCheckpointBytes != 42 {
		t.Fatalf("MaxCheckpointBytes = %d", l.MaxCheckpointBytes)
	}
}

func TestResolveExecutionLimits_precedence(t *testing.T) {
	t.Parallel()
	project := &ProjectSpec{
		Limits: &ExecutionLimits{
			MaxToolInputBytes:  1000,
			MaxToolOutputBytes: 2000,
			MaxCheckpointBytes: 3000,
		},
	}
	workflow := &WorkflowSpec{
		Limits: &ExecutionLimits{MaxToolOutputBytes: 4000},
	}
	tool := &ToolSpec{
		Limits: &ExecutionLimits{MaxToolInputBytes: 500},
	}
	got := ResolveExecutionLimits(project, workflow, tool)
	if got.MaxToolInputBytes != 500 {
		t.Fatalf("tool should win input: %d", got.MaxToolInputBytes)
	}
	if got.MaxToolOutputBytes != 4000 {
		t.Fatalf("workflow should win output: %d", got.MaxToolOutputBytes)
	}
	if got.MaxCheckpointBytes != 3000 {
		t.Fatalf("project checkpoint: %d", got.MaxCheckpointBytes)
	}
}

func TestResolveExecutionLimits_defaultsWhenUnset(t *testing.T) {
	t.Parallel()
	got := ResolveExecutionLimits(nil, nil, nil)
	def := DefaultExecutionLimits()
	if got != def {
		t.Fatalf("got %+v want %+v", got, def)
	}
}

func TestValidateExecutionLimits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		limits  ExecutionLimits
		wantErr bool
	}{
		{
			name: "valid truncate policies",
			limits: ExecutionLimits{
				MaxToolInputBytes:      100,
				ToolInputExceedPolicy:  LimitExceedTruncate,
				CheckpointExceedPolicy: LimitExceedFail,
			},
		},
		{
			name: "negative bytes",
			limits: ExecutionLimits{
				MaxToolInputBytes: -1,
			},
			wantErr: true,
		},
		{
			name: "unknown policy",
			limits: ExecutionLimits{
				ToolInputExceedPolicy: LimitExceedPolicy("drop"),
			},
			wantErr: true,
		},
		{
			name: "checkpoint truncate forbidden",
			limits: ExecutionLimits{
				CheckpointExceedPolicy: LimitExceedTruncate,
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateExecutionLimits(&tc.limits)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
		})
	}
}

func TestValidateExecutionLimitsGraph(t *testing.T) {
	t.Parallel()
	g := &ProjectGraph{
		Spec: ProjectSpec{
			Limits: &ExecutionLimits{CheckpointExceedPolicy: LimitExceedTruncate},
		},
	}
	errs := validateExecutionLimitsGraph(g)
	if len(errs) == 0 {
		t.Fatal("expected validation error")
	}
}
