package board

import (
	"reflect"
	"testing"
)

func TestParseMentions(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"none", "no mentions here", nil},
		{"single", "hey @ben take a look", []string{"ben"}},
		{"two distinct", "@ben and @ada please", []string{"ben", "ada"}},
		{"lowercased", "ping @Ben", []string{"ben"}},
		{"deduped", "@ada @ada @ada", []string{"ada"}},
		{"trailing punctuation", "talk to @ada.", []string{"ada"}},
		{"agent owner/name form", "ping @jack/deploy-bot now", []string{"jack/deploy-bot"}},
		{"bare at sign ignored", "email me @ ok", nil},
		{"email-like is lexical only", "reach foo@bar.com", []string{"bar.com"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseMentions(c.body); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("parseMentions(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}
