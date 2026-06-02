package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/mattn/go-isatty"
)

// EnvHitlActor overrides the actor recorded on approval trace events.
const EnvHitlActor = "AGENTCTL_HITL_ACTOR"

func hitlActorFromEnv() string {
	if v := strings.TrimSpace(os.Getenv(EnvHitlActor)); v != "" {
		return v
	}
	if u := strings.TrimSpace(os.Getenv("USER")); u != "" {
		return u
	}
	return "operator"
}

func applyHitlRunOptions(opts *runtime.WorkflowRunOptions, autoApprove bool, decision string, editJSON string, switchTarget string) error {
	opts.AutoApprove = autoApprove || envAutoApproveEnabled()
	opts.HitlActor = hitlActorFromEnv()
	decision = strings.TrimSpace(decision)
	if decision == "" {
		return nil
	}
	kind, err := spec.ParseHitlDecisionKind(decision)
	if err != nil {
		return err
	}
	hd := &runtime.HitlDecisionOptions{Kind: string(kind)}
	switch kind {
	case spec.HitlDecisionEdit:
		if strings.TrimSpace(editJSON) == "" {
			return fmt.Errorf("run: --decision edit requires --decision-edit-json")
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(editJSON), &m); err != nil {
			return fmt.Errorf("run: --decision-edit-json: %w", err)
		}
		hd.EditedWith = m
	case spec.HitlDecisionSwitch:
		hd.SwitchTarget = strings.TrimSpace(switchTarget)
		if hd.SwitchTarget == "" {
			return fmt.Errorf("run: --decision switch requires --decision-switch-target")
		}
	}
	opts.HitlDecision = hd
	return nil
}

func maybePromptHitlDecision(in io.Reader, out io.Writer, gate policy.HitlGate) (*policy.HitlDecisionInput, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return nil, nil
	}
	actor := hitlActorFromEnv()
	display := policy.RedactHitlArgs(gate.With, gate.Review.RedactKeys)
	fmt.Fprintf(out, "\n%s\n", gate.Review.Description)
	fmt.Fprintf(out, "Tool: %s\nArguments: %v\n", gate.Uses, display)
	fmt.Fprintf(out, "Allowed decisions: %v\n", gate.Review.AllowedDecisions)
	if len(gate.Review.SwitchTargets) > 0 {
		fmt.Fprintf(out, "Switch targets: %v\n", gate.Review.SwitchTargets)
	}
	for {
		fmt.Fprintf(out, "Decision [approve/reject/edit/switch]: ")
		line, err := readLine(in)
		if err != nil {
			return nil, err
		}
		kind, err := spec.ParseHitlDecisionKind(line)
		if err != nil {
			fmt.Fprintf(out, "Unknown decision %q\n", line)
			continue
		}
		if !decisionAllowed(kind, gate.Review.AllowedDecisions) {
			fmt.Fprintf(out, "Decision %q is not allowed for this call\n", kind)
			continue
		}
		dec := &policy.HitlDecisionInput{Kind: kind, Actor: actor}
		switch kind {
		case spec.HitlDecisionEdit:
			fmt.Fprintf(out, "Edited args JSON: ")
			editLine, err := readLine(in)
			if err != nil {
				return nil, err
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(editLine), &m); err != nil {
				fmt.Fprintf(out, "Invalid JSON: %v\n", err)
				continue
			}
			if err := policy.ValidateHitlEdit(gate.With, m, gate.Review); err != nil {
				fmt.Fprintf(out, "%v\n", err)
				continue
			}
			dec.EditedWith = m
		case spec.HitlDecisionSwitch:
			fmt.Fprintf(out, "Switch target operation: ")
			target, err := readLine(in)
			if err != nil {
				return nil, err
			}
			dec.SwitchTarget = target
		}
		return dec, nil
	}
}

func decisionAllowed(kind spec.HitlDecisionKind, allowed []spec.HitlDecisionKind) bool {
	for _, a := range allowed {
		if a == kind {
			return true
		}
	}
	return false
}

func readLine(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("run: unexpected EOF reading hitl decision")
	}
	return strings.TrimSpace(sc.Text()), nil
}
