package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestTaskEditsRequireExplicitValues(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		message string
	}{
		{[]string{"--field", "description", "--version", "1"}, "omitting a value never clears"},
		{[]string{"--field", "assignees", "--version", "1"}, "Supply --assignee"},
		{[]string{"--field", "title", "--version", "1", "--clear"}, "Only description"},
		{[]string{"--field", "title", "--version", "0", "--value", "Hello"}, "Supply --version"},
		{[]string{"--field", "description", "--version", "1", "--value", "", "--clear"}, "none of the others can be"},
		{[]string{"--field", "assignees", "--version", "1", "--assignee", "someone", "--clear"}, "none of the others can be"},
		// Valid explicit clears reach profile resolution; missing values never do.
		{[]string{"--field", "description", "--version", "1", "--clear"}, "No server configured"},
		{[]string{"--field", "description", "--version", "1", "--value", ""}, "No server configured"},
		{[]string{"--field", "assignees", "--version", "1", "--clear"}, "No server configured"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out bytes.Buffer
			a := &App{repo: Repository{t.TempDir()}, input: strings.NewReader(""), out: &out, errOut: &out}
			cmd := a.command()
			cmd.SetArgs(append([]string{"task", "edit", "ACT-1"}, tc.args...))
			err := cmd.ExecuteContext(t.Context())
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("want %q, got %v", tc.message, err)
			}
		})
	}
}

func TestTaskDescriptionInputIsBounded(t *testing.T) {
	a := App{input: strings.NewReader("# Heading\n\nMarkdown\n")}
	got, err := a.readTaskText("-")
	if err != nil || got != "# Heading\n\nMarkdown\n" {
		t.Fatal(got, err)
	}
	a.input = strings.NewReader(strings.Repeat("x", 100001))
	if _, err = a.readTaskText("-"); err == nil {
		t.Fatal("accepted oversized description")
	}
}

func TestTaskTableContainsNoTerminalControls(t *testing.T) {
	got := table([]string{"TITLE"}, [][]string{{"Hello\x1b[2J\nInjected\tcolumn"}})
	if strings.ContainsAny(got, "\x1b\t") || strings.Count(got, "\n") != 1 {
		t.Fatalf("unsafe row: %q", got)
	}
}
