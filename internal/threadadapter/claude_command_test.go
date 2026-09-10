package threadadapter

import "testing"

func TestClaudeCommandEcho(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"<command-name>/compact</command-name>\n<command-message>compact</command-message>\n<command-args>Keep <tags> & ü\nnext line</command-args>", "/compact Keep <tags> & ü\nnext line"},
		{"<command-name>/compact</command-name><command-message>compact</command-message><command-args></command-args>", "/compact"},
	} {
		if got := claudeCommandText(test.in); got != test.want {
			t.Fatalf("%q", got)
		}
	}
	for _, text := range []string{"ordinary text", "<command-name>/compact</command-name>", "<command-name>/compact</command-name><command-message>different</command-message><command-args>x</command-args>", "<command-name>/compact</command-name><command-message>compact</command-message><command-args>x</command-args>trailing"} {
		if got := claudeCommandText(text); got != text {
			t.Fatalf("changed ordinary text %q", got)
		}
	}
}
