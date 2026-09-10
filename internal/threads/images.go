package threads

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
)

// Images travel with the immutable send command, never as a provider-local path.
const MaxImageBytes = 4 * 1024 * 1024
const MaxMessageWire = 8 * 1024 * 1024
const MaxImages = 4

type InputImage struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Base64    string `json:"base64"`
}

func (i InputImage) URL() string { return "data:" + i.MediaType + ";base64," + i.Base64 }
func ValidateMessage(text string, images []InputImage) error {
	if len(text) > 64*1024 || (strings.TrimSpace(text) == "" && len(images) == 0) {
		return errors.New("Enter text or attach an image; text can be up to 64 KiB.")
	}
	return ValidateImages(images)
}
func ValidateImages(images []InputImage) error {
	if len(images) > MaxImages {
		return errors.New("Attach up to four images.")
	}
	total := 0
	for _, i := range images {
		if len(i.Name) > 1024 || len(i.Base64) > base64.StdEncoding.EncodedLen(MaxImageBytes) {
			return errors.New("Images can total up to 4 MiB.")
		}
		switch i.MediaType {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			return errors.New("Choose PNG, JPEG, GIF or WebP images.")
		}
		b, e := base64.StdEncoding.Strict().DecodeString(i.Base64)
		if e != nil || len(b) == 0 || http.DetectContentType(b) != i.MediaType {
			return errors.New("The image data does not match its format.")
		}
		total += len(b)
		if total > MaxImageBytes {
			return errors.New("Images can total up to 4 MiB.")
		}
	}
	return nil
}
