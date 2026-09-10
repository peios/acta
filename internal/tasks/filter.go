package tasks

func NormalizeFilter(f Filter) (Filter, error) {
	var e error
	if f.Board != "*" {
		f.Board, e = BoardSlug(f.Board)
		if e != nil {
			return f, e
		}
	}
	f, e = NormalizeTaskSort(f)
	if e != nil {
		return f, e
	}
	if e = ValidateSelections(f.Statuses, f.Assignees); e != nil {
		return f, e
	}
	if e = ValidatePropertyFilters(f.Priorities, f.Types, f.Sizes); e != nil {
		return f, e
	}
	if e = ValidateGroupFilter(f); e != nil {
		return f, e
	}
	if f.State != "" && f.State != "all" && f.State != "unfinished" && f.State != "completed" {
		return f, field("state", "Choose all, unfinished or completed.")
	}
	return f, nil
}
