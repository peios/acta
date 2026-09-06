package model

import "fmt"

// Prune categories: the parts of a transcript a reader may choose to drop
// to save space. Messages and turn markers are never offered; they are
// small, and they are the record. A backend's pruner knows which fields of
// its own records each category names.
const (
	PruneToolOutput = "tool_output" // what tools returned: command output, file contents read, search hits
	PruneToolInput  = "tool_input"  // what tools were given: file contents written, long command text
	PruneThinking   = "thinking"    // the model's reasoning
	PruneImages     = "images"      // images the user attached
)

// PruneCategories is every category, in the order a picker lists them.
var PruneCategories = []string{PruneToolOutput, PruneToolInput, PruneThinking, PruneImages}

// Pruned is the text a pruned value is replaced with: it says what went
// so the page still reads, and how much, so the saving is visible.
func Pruned(what string, n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("[%s pruned, %.1f MB]", what, float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("[%s pruned, %d KB]", what, n>>10)
	}
	return fmt.Sprintf("[%s pruned, %d bytes]", what, n)
}

// PruneKeep is the length under which a value is not worth pruning: the
// marker would be as long as what it replaces, and short values (a file
// path, a command) are what makes a pruned call still readable.
const PruneKeep = 240
