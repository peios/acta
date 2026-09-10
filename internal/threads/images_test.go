package threads

import (
	"encoding/base64"
	"testing"
)

func TestInputImageValidation(t *testing.T) {
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="
	image := InputImage{Name: "pixel.png", MediaType: "image/png", Base64: png}
	if e := ValidateMessage("", []InputImage{image}); e != nil {
		t.Fatal(e)
	}
	for name, images := range map[string][]InputImage{
		"empty": {}, "too-many": {image, image, image, image, image},
		"svg":        {{MediaType: "image/svg+xml", Base64: base64.StdEncoding.EncodeToString([]byte("<svg/>"))}},
		"spoofed":    {{MediaType: "image/png", Base64: base64.StdEncoding.EncodeToString([]byte("<html>not an image"))}},
		"bad-base64": {{MediaType: "image/png", Base64: "!!!!"}},
	} {
		t.Run(name, func(t *testing.T) {
			if ValidateMessage("", images) == nil {
				t.Fatal("accepted invalid image")
			}
		})
	}
	bytes := make([]byte, MaxImageBytes/2+1)
	copy(bytes, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	big := InputImage{MediaType: "image/png", Base64: base64.StdEncoding.EncodeToString(bytes)}
	if ValidateMessage("", []InputImage{big, big}) == nil {
		t.Fatal("accepted oversized batch")
	}
}
