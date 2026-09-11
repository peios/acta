package hyperharness

import (
	"acta/internal/threads"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

type modelCatalogue struct {
	Version int
	RunID   string
	SavedAt time.Time
	Models  []threads.ModelOption
}

// Read the catalogue from this provider process: account, provider configuration
// and available capabilities may differ between connected development machines.
func (c *Controller) models(ctx context.Context, r *localThread, q threads.Control) ([]threads.ModelOption, error) {
	c.mu.Lock()
	cached := c.records[r.Thread.ID].Catalogue
	c.mu.Unlock()
	if cached != nil && cached.Version == 1 && cached.RunID == r.Thread.RunID && time.Since(cached.SavedAt) < 15*time.Minute {
		return cached.Models, nil
	}
	var models []threads.ModelOption
	var err error
	if r.Thread.Provider == "claude" {
		models, err = c.claudeModels(ctx, r, q)
	} else {
		models, err = c.codexModels(ctx, r, q)
	}
	if err == nil {
		c.mu.Lock()
		c.records[r.Thread.ID].Catalogue = &modelCatalogue{Version: 1, RunID: r.Thread.RunID, SavedAt: time.Now(), Models: models}
		c.mu.Unlock()
	}
	return models, err
}
func (c *Controller) codexModels(ctx context.Context, r *localThread, q threads.Control) ([]threads.ModelOption, error) {
	models := []threads.ModelOption{}
	cursor := ""
	seen := map[string]bool{}
	cursors := map[string]bool{}
	for page := 0; page < 8; page++ {
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.rpcID(ctx, r, "model/list", fmt.Sprintf("model/list/%s/%d", q.ID, page), params)
		if err != nil {
			return nil, err
		}
		var response struct {
			Data []struct {
				InputModalities []string `json:"inputModalities"`
				Model           string   `json:"model"`
				Name            string   `json:"displayName"`
				Description     string   `json:"description"`
				Hidden          bool     `json:"hidden"`
				Efforts         []struct {
					ID          string `json:"reasoningEffort"`
					Description string `json:"description"`
				} `json:"supportedReasoningEfforts"`
				DefaultEffort string `json:"defaultReasoningEffort"`
				Tiers         []struct {
					ID          string `json:"id"`
					Description string `json:"description"`
				} `json:"serviceTiers"`
			} `json:"data"`
			Next *string `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &response) != nil || response.Data == nil {
			return nil, errors.New("provider returned an invalid model catalogue")
		}
		for _, native := range response.Data {
			if native.Hidden {
				continue
			}
			if native.Model == "" || len(native.Model) > 256 || len(native.Name) > 256 || len(native.Description) > 4096 {
				return nil, errors.New("provider returned invalid model metadata")
			}
			if seen[native.Model] {
				continue
			}
			seen[native.Model] = true
			option := threads.ModelOption{ID: native.Model, InputModalities: native.InputModalities, Name: native.Name, Description: native.Description, DefaultEffort: native.DefaultEffort, Efforts: []threads.EffortOption{}}
			if option.Name == "" {
				option.Name = option.ID
			}
			for _, effort := range native.Efforts {
				if effort.ID == "" || len(effort.ID) > 32 || len(effort.Description) > 2048 {
					return nil, errors.New("provider returned invalid effort metadata")
				}
				option.Efforts = append(option.Efforts, threads.EffortOption{ID: effort.ID, Description: effort.Description})
			}
			for _, tier := range native.Tiers {
				if tier.ID == "priority" {
					option.FastMode = true
					option.FastDescription = tier.Description
				}
			}
			models = append(models, option)
			if len(models) > 256 {
				return nil, errors.New("provider model catalogue exceeds supported size")
			}
		}
		if response.Next == nil || *response.Next == "" {
			return models, nil
		}
		cursor = *response.Next
		if cursors[cursor] {
			return nil, errors.New("provider repeated a model catalogue cursor")
		}
		cursors[cursor] = true
	}
	return nil, errors.New("provider model catalogue did not finish pagination")
}
func validateModelSettings(models []threads.ModelOption, settings threads.ModelSettings) error {
	index := slices.IndexFunc(models, func(m threads.ModelOption) bool {
		return m.ID == settings.Model || (m.ResolvedModel != "" && m.ResolvedModel == settings.Model)
	})
	if index < 0 {
		return errors.New("this model is not available from the connected provider")
	}
	model := models[index]
	if !(model.DefaultEffort == "" && settings.Effort == "") && !slices.ContainsFunc(model.Efforts, func(e threads.EffortOption) bool { return e.ID == settings.Effort }) {
		return errors.New("this effort level is not supported by the selected model")
	}
	if settings.FastMode && !model.FastMode {
		return errors.New("fast mode is not supported by the selected model")
	}
	return nil
}
func (c *Controller) executeModels(ctx context.Context, r *localThread) {
	q := r.Requests[r.CommandID]
	result := threads.Result{ID: q.ID, ThreadID: q.ThreadID, Outcome: "rejected"}
	models, err := c.models(ctx, r, q)
	if err != nil {
		result.Error = err.Error()
	} else {
		result.Outcome = "accepted"
		result.Models = models
	}
	c.finishOperation(r, result)
}
func (c *Controller) executeConfigure(ctx context.Context, r *localThread) {
	q := r.Requests[r.CommandID]
	if r.Thread.Provider == "claude" {
		c.executeClaudeConfigure(ctx, r, q)
		return
	}
	result := threads.Result{ID: q.ID, ThreadID: q.ThreadID, Outcome: "rejected"}
	if !r.SendAttempted {
		models, err := c.models(ctx, r, q)
		if err == nil {
			err = validateModelSettings(models, q.Settings)
		}
		if err == nil {
			err = c.requireIdle(ctx, r, "configure-"+q.ID)
		}
		if err != nil {
			result.Error = err.Error()
			c.finishOperation(r, result)
			return
		}
		if !c.prepareInput(r) {
			return
		}
	}
	raw, err := c.rpcID(ctx, r, "thread/settings/update", "thread/settings/update/"+q.ID, settingsParams(r.Thread.ProviderID, q.Settings))
	result.Outcome = "accepted"
	if err != nil {
		result.Outcome = "uncertain"
		var rejected *providerRPCError
		if errors.As(err, &rejected) {
			result.Outcome = "rejected"
		}
		result.Error = err.Error()
	} else {
		var response map[string]any
		if json.Unmarshal(raw, &response) != nil || response == nil {
			result.Outcome = "uncertain"
			result.Error = "Provider settings acknowledgement could not be read."
		}
	}
	c.finishOperation(r, result)
}

func (c *Controller) requireIdle(ctx context.Context, r *localThread, suffix string) error {
	if r.Thread.Provider == "claude" {
		return c.requireClaudeIdle(ctx, r, suffix)
	}
	raw, err := c.rpcID(ctx, r, "thread/read", "thread/read/"+suffix, map[string]any{"threadId": r.Thread.ProviderID, "includeTurns": false})
	if err != nil {
		return fmt.Errorf("could not confirm that the provider is idle: %w", err)
	}
	var read struct {
		Thread struct {
			ID     string `json:"id"`
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &read) != nil || read.Thread.ID != r.Thread.ProviderID || read.Thread.Status.Type != "idle" {
		return errors.New("wait for the active turn to finish before changing settings or sending a message")
	}
	return nil
}
func (c *Controller) prepareInput(r *localThread) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.records[r.Thread.ID]
	current.SendAttempted = true
	return c.save(current) == nil
}

func settingsParams(nativeID string, settings threads.ModelSettings) map[string]any {
	tier := "default"
	if settings.FastMode {
		tier = "priority"
	}
	return map[string]any{"threadId": nativeID, "model": settings.Model, "effort": settings.Effort, "serviceTier": tier}
}
