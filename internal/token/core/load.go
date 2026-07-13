package core

import "sync"

// Load returns the aggregate for a single harness key.
func Load(harness string) *Aggregate {
	switch harness {
	case Claude:
		return loadClaude()
	case Codex:
		return loadCodex()
	case Pi:
		return loadPi()
	case Cursor:
		return loadCursor()
	default:
		return LoadAll()
	}
}

// LoadAll loads and merges every harness concurrently.
func LoadAll() *Aggregate {
	aggs := make([]*Aggregate, len(Harnesses))
	var wg sync.WaitGroup
	for i, h := range Harnesses {
		wg.Add(1)
		go func(i int, h string) {
			defer wg.Done()
			aggs[i] = Load(h)
		}(i, h)
	}
	wg.Wait()
	a := newAggregate(Combined)
	for _, agg := range aggs {
		a.Merge(agg)
	}
	return a
}

// LoadEach loads every concrete harness once and returns them keyed by
// harness name, with the merged Combined aggregate under Combined.
func LoadEach() map[string]*Aggregate {
	aggs := make([]*Aggregate, len(Harnesses))
	var wg sync.WaitGroup
	for i, h := range Harnesses {
		wg.Add(1)
		go func(i int, h string) {
			defer wg.Done()
			aggs[i] = Load(h)
		}(i, h)
	}
	wg.Wait()
	m := make(map[string]*Aggregate, len(Harnesses)+1)
	all := newAggregate(Combined)
	for i, h := range Harnesses {
		m[h] = aggs[i]
		all.Merge(aggs[i])
	}
	m[Combined] = all
	return m
}

// mergeParts folds non-nil partial aggregates into a.
func mergeParts(a *Aggregate, parts []*Aggregate) {
	for _, p := range parts {
		if p != nil {
			a.Merge(p)
		}
	}
}

// loadParts runs fn on each path concurrently and merges the results.
func loadParts(paths []string, fn func(string) *Aggregate) *Aggregate {
	if len(paths) == 0 {
		return nil
	}
	if len(paths) == 1 {
		return fn(paths[0])
	}
	parts := make([]*Aggregate, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			parts[i] = fn(p)
		}(i, p)
	}
	wg.Wait()
	out := newAggregate("")
	out.Harness = "" // caller sets harness
	mergeParts(out, parts)
	return out
}
