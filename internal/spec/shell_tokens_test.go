package spec

import (
	"errors"
	"testing"
)

func TestClassifyShellToken(t *testing.T) {
	tests := []struct {
		token string
		want  ShellTokenClass
	}{
		{"ls", ShellTokenReadOnly},
		{"/bin/ls", ShellTokenReadOnly},
		{"LS", ShellTokenReadOnly},
		{"cat", ShellTokenReadOnly},
		{"grep", ShellTokenReadOnly},
		{"rm", ShellTokenGate},
		{"rm -rf", ShellTokenGate},
		{"/usr/bin/rm", ShellTokenGate},
		{"curl", ShellTokenGate},
		{"wget", ShellTokenGate},
		{"unknown-cmd", ShellTokenUnknown},
		{"", ShellTokenUnknown},
		{"  ", ShellTokenUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			if got := ClassifyShellToken(FirstShellToken(tt.token)); got != tt.want {
				t.Fatalf("ClassifyShellToken(%q) = %v want %v", tt.token, got, tt.want)
			}
		})
	}
}

func TestFirstShellToken_adversarial(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"ls -la /tmp", "ls"},
		{"  rm   -rf /", "rm"},
		{`"cat" file`, "cat"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := FirstShellToken(tt.cmd); got != tt.want {
			t.Fatalf("FirstShellToken(%q) = %q want %q", tt.cmd, got, tt.want)
		}
	}
}

func TestShellCommandRequiresApproval_metacharactersFailClosed(t *testing.T) {
	adversarial := []string{
		"ls; rm -rf /",
		"ls | rm -rf /",
		"ls && rm -rf /",
		"$(rm -rf /)",
		"`rm -rf /`",
		"ls\nrm -rf /",
		"echo $(whoami)",
	}
	for _, cmd := range adversarial {
		if !ShellCommandRequiresApproval(cmd) {
			t.Fatalf("expected gate for %q", cmd)
		}
	}
	if ShellCommandRequiresApproval("ls -la") {
		t.Fatal("plain ls should not require approval")
	}
}

func TestBuildPreset_unknown(t *testing.T) {
	_, err := BuildPreset("nope")
	if err == nil {
		t.Fatal("expected error")
	}
	var pe *ErrUnknownPreset
	if !errors.As(err, &pe) || pe.Name != "nope" {
		t.Fatalf("got %v", err)
	}
}

func TestMergePolicySpec_localRequiredForOverridesPresetTool(t *testing.T) {
	base, err := BuildPreset(PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	overlay := PolicySpec{
		Approvals: &PolicyApprovals{
			RequiredFor: []string{"tool.helper.echo"},
		},
	}
	merged := MergePolicySpec(base, overlay)
	if merged.Approvals == nil {
		t.Fatal("nil approvals")
	}
	foundEcho := false
	for _, r := range merged.Approvals.RequiredFor {
		if r == "tool.helper.echo" {
			foundEcho = true
		}
	}
	if !foundEcho {
		t.Fatalf("missing local override: %v", merged.Approvals.RequiredFor)
	}
}

func TestMergePolicySpec_overlayRelaxesStrict(t *testing.T) {
	base, err := BuildPreset(PresetStrict)
	if err != nil {
		t.Fatal(err)
	}
	overlay := PolicySpec{
		Approvals: &PolicyApprovals{
			RequireAllTools: BoolPtr(false),
		},
	}
	merged := MergePolicySpec(base, overlay)
	if ApprovalRequireAllTools(merged.Approvals) {
		t.Fatal("overlay requireAllTools: false should relax strict preset")
	}
}

func TestBuildPreset_strict_expanded(t *testing.T) {
	p, err := BuildPreset(PresetStrict)
	if err != nil {
		t.Fatal(err)
	}
	if p.Approvals == nil || !ApprovalRequireAllTools(p.Approvals) {
		t.Fatalf("strict preset: %+v", p.Approvals)
	}
}

func TestBuildPreset_permissive_expanded(t *testing.T) {
	p, err := BuildPreset(PresetPermissive)
	if err != nil {
		t.Fatal(err)
	}
	if p.Approvals == nil || !ApprovalPermissive(p.Approvals) {
		t.Fatalf("permissive preset: %+v", p.Approvals)
	}
}

func TestBuildPreset_shellSafe_noSyntheticRequiredFor(t *testing.T) {
	p, err := BuildPreset(PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	if p.ResolvedPreset != PresetShellSafe {
		t.Fatalf("ResolvedPreset = %q", p.ResolvedPreset)
	}
	if p.Approvals != nil && len(p.Approvals.RequiredFor) > 0 {
		t.Fatalf("shell_safe should not expand synthetic requiredFor: %v", p.Approvals.RequiredFor)
	}
}

func TestValidatePolicySpecs_conflictingApprovalFlags(t *testing.T) {
	g := &ProjectGraph{
		Policies: map[string]*PolicyResource{
			"bad": {
				Metadata: Metadata{Name: "bad"},
				Spec: PolicySpec{
					Approvals: &PolicyApprovals{
						RequireAllTools: BoolPtr(true),
						Permissive:      BoolPtr(true),
					},
				},
			},
		},
	}
	errs := validatePolicySpecs(g)
	if len(errs) == 0 {
		t.Fatal("expected validation error")
	}
}
