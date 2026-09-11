package threadadapter

import (
	"acta/internal/threads"
	"strings"
)

func inputParts(text string, images []threads.InputImage) []any {
	parts := []any{}
	if text != "" {
		parts = append(parts, object{"type": "text", "text": text})
	}
	for _, i := range images {
		parts = append(parts, object{"type": "image", "media_type": i.MediaType, "base64": i.Base64})
	}
	return parts
}

// Normalize only embedded raster bytes; arbitrary URLs and local paths remain
// unknown unless the correlated Acta submission supplies the original image.
func inputImage(part object, provider string) (object, bool) {
	var media, data string
	if provider == "codex" {
		u := str(part["url"])
		prefix, body, ok := strings.Cut(u, ";base64,")
		if !ok || !strings.HasPrefix(prefix, "data:") {
			return nil, false
		}
		media, data = strings.TrimPrefix(prefix, "data:"), body
	} else {
		source := obj(part["source"])
		if source["type"] != "base64" {
			return nil, false
		}
		media, data = str(source["media_type"]), str(source["data"])
	}
	image, ok := toolImage(media, data)
	if !ok {
		return nil, false
	}
	return object{"type": "image", "media_type": image.MediaType, "base64": image.Base64}, true
}
