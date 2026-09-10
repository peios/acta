package threadadapter

import (
	"encoding/base64"
	"path"
	"strings"
)

type ToolAttachment struct {
	MediaType string `json:"media_type"`
	Base64    string `json:"base64"`
	Name      string `json:"name"`
}

func toolPDF(mediaType, data, filePath string) (ToolAttachment, bool) {
	if mediaType != "application/pdf" || data == "" {
		return ToolAttachment{}, false
	}
	if _, err := base64.StdEncoding.Strict().DecodeString(data); err != nil {
		return ToolAttachment{}, false
	}
	name := path.Base(strings.ReplaceAll(filePath, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		name = "document.pdf"
	}
	return ToolAttachment{MediaType: mediaType, Base64: data, Name: name}, true
}
