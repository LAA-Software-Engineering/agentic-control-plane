package engine

import "sync"

// liveCost is the run-wide accumulated cost, shared across the execir run and its
// nested subworkflows so sibling leaves and inner tools admit/commit against one
// total (issue #194). It is mutex-guarded for the goroutines a Graph/Fork/parallel
// Loop spawns.
type liveCost struct {
	mu    sync.Mutex
	total float64
}

func (c *liveCost) get() float64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

func (c *liveCost) add(delta float64) float64 {
	if c == nil {
		return delta
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total += delta
	return c.total
}

// cloneStepResults shallow-copies the accumulated step results so a snapshot can
// be read without racing concurrent writers (the execir invoker snapshots ictx
// for output building and checkpoints).
func cloneStepResults(in map[string]StepResult) map[string]StepResult {
	out := make(map[string]StepResult, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
