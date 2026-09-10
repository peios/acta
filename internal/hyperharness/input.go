package hyperharness

import (
	"acta2/internal/threads"
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

func codexInput(q threads.Control) []map[string]any {
	parts := []map[string]any{}
	if q.Text != "" {
		parts = append(parts, map[string]any{"type": "text", "text": q.Text, "text_elements": []any{}})
	}
	for _, i := range q.Images {
		parts = append(parts, map[string]any{"type": "image", "url": i.URL()})
	}
	return parts
}
func claudeInput(text string, images []threads.InputImage) any {
	if len(images) == 0 {
		return text
	}
	parts := []map[string]any{}
	if text != "" {
		parts = append(parts, map[string]any{"type": "text", "text": text})
	}
	for _, i := range images {
		parts = append(parts, map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": i.MediaType, "data": i.Base64}})
	}
	return parts
}

func (c *Controller) requireImageModel(ctx context.Context, r *localThread, q threads.Control) error {
	raw, err := c.rpcID(ctx, r, "thread/read", "thread/read/image-model-"+q.ID, map[string]any{"threadId": r.Thread.ProviderID, "includeTurns": false})
	if err != nil {
		return err
	}
	var response struct {
		Thread struct {
			Model string `json:"model"`
		} `json:"thread"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return err
	}
	models, err := c.models(ctx, r, q)
	if err != nil {
		return err
	}
	return validateImageModel(models, response.Thread.Model)
}
func validateImageModel(models []threads.ModelOption, model string) error {
	for _, m := range models {
		if m.ID == model && m.InputModalities != nil && !slices.Contains(m.InputModalities, "image") {
			return fmt.Errorf("%s does not support images. Choose an image-capable model or remove the attachments.", m.Name)
		}
	}
	return nil
}

func (c *Controller) requireImageTransport(ctx context.Context, q threads.Control) error {
	pipe, ok := c.pipe.(interface {
		InputLimit(context.Context) (int, error)
	})
	if !ok {
		return nil
	} // Embedded engine is built with this controller.
	limit, err := pipe.InputLimit(ctx)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(q)
	if err != nil {
		return err
	}
	if len(encoded)+4096 > limit {
		return fmt.Errorf("The detached harness pipe needs a restart to accept images this large. Your message has not been sent.")
	}
	return nil
}
