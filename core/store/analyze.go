package store

import "context"

// Analyzer is an optional DB capability: Analyze refreshes the query planner's statistics of Hydrogen's tables.
// Imports call it after loading a lot of rows at once, when the statistics are still those of an empty
// database. Adapters whose planner needs no help (or that run the equivalent after migrations themselves)
// may leave it out.
type Analyzer interface {
	Analyze(ctx context.Context) error
}

// Analyze runs db's Analyze when it has one.
func Analyze(ctx context.Context, db DB) error {
	if a, ok := db.(Analyzer); ok {
		return a.Analyze(ctx)
	}
	return nil
}
