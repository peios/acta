package documents

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateBoundariesAndDetectedContent(t *testing.T) {
	for _, in := range []Save{
		{Filename: "../file"}, {Filename: "file\nname"}, {Filename: "file", Revision: 1},
		{Filename: "file", Revision: -1}, {Filename: "file", ID: "bad"},
		{Filename: "file", Content: bytes.Repeat([]byte{'x'}, MaxBytes+1)},
	} {
		if Validate(&in) == nil {
			t.Fatalf("invalid upload accepted: %s", in.Filename)
		}
	}
	for _, length := range []int{0, MaxBytes} {
		in := Save{Filename: "file.txt", Content: bytes.Repeat([]byte{'x'}, length)}
		if e := Validate(&in); e != nil {
			t.Fatal(e)
		}
		if len(in.SHA256) != 64 || in.ID == "" {
			t.Fatal("missing file identity")
		}
	}
	in := Save{Filename: "photo.png", MediaType: "image/png", Content: []byte("<html>not an image</html>")}
	if e := Validate(&in); e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(in.MediaType, "text/html") {
		t.Fatal("trusted user media type", in.MediaType)
	}
}
