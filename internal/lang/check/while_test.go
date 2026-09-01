package check

import "testing"

// TestCheckWhile_LoopCarriedState covers the #288 scope rule: a binding that
// existed BEFORE the loop may be rebound inside and survives after it; a binding
// first introduced INSIDE the loop is loop-local and does not escape.
func TestCheckWhile_LoopCarriedState(t *testing.T) {
	t.Parallel()

	t.Run("preexisting binding rebound in the loop survives", func(t *testing.T) {
		t.Parallel()
		src := `
workflow W(input: Repos) {
    s = input
    while input.go limit 3 {
        s = github.get_pr()
    }
    return s
}
`
		f := parseOrFatal(t, src)
		_, diags := Check(f, Options{Project: projectWith(githubTool())})
		if diags.HasErrors() {
			t.Fatalf("loop-carried rebinding should type-check, got: %v", diagMessages(diags))
		}
	})

	t.Run("loop-local binding does not escape the loop", func(t *testing.T) {
		t.Parallel()
		src := `
workflow W(input: Repos) {
    while input.go limit 3 {
        temp = github.get_pr()
    }
    return temp
}
`
		f := parseOrFatal(t, src)
		_, diags := Check(f, Options{Project: projectWith(githubTool())})
		if !diags.HasErrors() {
			t.Fatalf("a loop-local binding must not be visible after the loop (it may run zero times), got no error")
		}
	})
}
