package spec

import "testing"

func TestTraceRetentionDays(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		g    *ProjectGraph
		want int
	}{
		{"nil graph", nil, 0},
		{"nil traces", &ProjectGraph{Spec: ProjectSpec{}}, 0},
		{"zero", &ProjectGraph{Spec: ProjectSpec{Traces: &ProjectTracesConfig{RetentionDays: 0}}}, 0},
		{"negative", &ProjectGraph{Spec: ProjectSpec{Traces: &ProjectTracesConfig{RetentionDays: -1}}}, 0},
		{"positive", &ProjectGraph{Spec: ProjectSpec{Traces: &ProjectTracesConfig{RetentionDays: 7}}}, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := TraceRetentionDays(tc.g); got != tc.want {
				t.Fatalf("TraceRetentionDays = %d want %d", got, tc.want)
			}
		})
	}
}
