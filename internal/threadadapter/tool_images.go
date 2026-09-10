package threadadapter

import "encoding/base64"

// ToolImage carries provider-supplied raster bytes, never a host path or a URL
// for the browser to fetch. Native metadata can contain a second copy; it is
// intentionally not copied into the tool snapshot.
type ToolImage struct {
	MediaType string `json:"media_type"`
	Base64    string `json:"base64"`
}

func toolImage(mediaType, data string) (ToolImage, bool) {
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return ToolImage{}, false
	}
	if data == "" {
		return ToolImage{}, false
	}
	if _, err := base64.StdEncoding.Strict().DecodeString(data); err != nil {
		return ToolImage{}, false
	}
	return ToolImage{MediaType: mediaType, Base64: data}, true
}
