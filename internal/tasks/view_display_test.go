package tasks

import "testing"

func TestViewPresentation(t *testing.T) {
	for _, mode := range []string{"", "table", "board"} {
		got, err := NormalizeViewDisplay(ViewDisplay{Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		want := mode
		if want == "" {
			want = "table"
		}
		if got.Mode != want {
			t.Fatalf("mode: got %q want %q", got.Mode, want)
		}
	}
	if _, err := NormalizeViewDisplay(ViewDisplay{Mode: "unknown"}); err == nil {
		t.Fatal("unsupported view accepted")
	}
}
