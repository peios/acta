package web

import (
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/peios/acta/internal/mcpcfg"
	"github.com/peios/acta/internal/store"
)

// --- guide ---

type guidePageData struct {
	chrome
	Preview template.HTML // rendered markdown of the hardcoded guide agents read
}

// settingsGuide shows the conventions guide (served as acta://guide) read-only.
// The guide is hardcoded and ships with each release — Acta's equivalent of a
// system prompt — so there is nothing to edit; the page exists so operators can
// always see exactly what agents are told. It's rendered with the same live
// context (the workspace list) agents receive.
func (h *handlers) settingsGuide(w http.ResponseWriter, r *http.Request) {
	body, err := h.guideDoc(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	ch, err := h.chromeFor(r, "settings", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "settings_guide.html", guidePageData{
		chrome:  ch,
		Preview: mdToHTML(body),
	})
}

// --- prompts ---

type promptRow struct {
	ID          string
	Name        string
	Title       string
	Description string
	Slash       string
	Args        string
}

type promptsPageData struct {
	chrome
	Prompts []promptRow
}

func (h *handlers) settingsPrompts(w http.ResponseWriter, r *http.Request) {
	prompts, err := h.mcpcfg.Prompts(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows := make([]promptRow, len(prompts))
	for i, p := range prompts {
		rows[i] = promptRow{
			ID:          p.ID,
			Name:        p.Name,
			Title:       p.Title,
			Description: p.Description,
			Slash:       "/mcp__acta__" + p.Name,
			Args:        argSummary(p.Arguments),
		}
	}
	ch, err := h.chromeFor(r, "settings", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "settings_prompts.html", promptsPageData{chrome: ch, Prompts: rows})
}

func argSummary(args []store.MCPPromptArg) string {
	if len(args) == 0 {
		return "—"
	}
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.Name
		if a.Required {
			parts[i] += "*"
		}
	}
	return strings.Join(parts, ", ")
}

type promptFormData struct {
	chrome
	New         bool
	ID          string
	Name        string
	Title       string
	Description string
	Body        string
	ArgsText    string
	Err         string
}

func (h *handlers) promptNew(w http.ResponseWriter, r *http.Request) {
	h.renderPromptForm(w, r, http.StatusOK, promptFormData{New: true})
}

func (h *handlers) promptEdit(w http.ResponseWriter, r *http.Request) {
	p, err := h.mcpcfg.Prompt(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrMCPPromptNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.renderPromptForm(w, r, http.StatusOK, promptFormData{
		ID:          p.ID,
		Name:        p.Name,
		Title:       p.Title,
		Description: p.Description,
		Body:        p.Body,
		ArgsText:    mcpcfg.FormatArgs(p.Arguments),
	})
}

func (h *handlers) promptCreate(w http.ResponseWriter, r *http.Request) {
	in, ok := h.promptInput(w, r)
	if !ok {
		return
	}
	if _, err := h.mcpcfg.CreatePrompt(r.Context(), in); err != nil {
		h.promptFormError(w, r, promptFormData{New: true}, in, err)
		return
	}
	http.Redirect(w, r, "/settings/prompts", http.StatusSeeOther)
}

func (h *handlers) promptUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	in, ok := h.promptInput(w, r)
	if !ok {
		return
	}
	err := h.mcpcfg.UpdatePrompt(r.Context(), id, in)
	if errors.Is(err, store.ErrMCPPromptNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.promptFormError(w, r, promptFormData{ID: id}, in, err)
		return
	}
	http.Redirect(w, r, "/settings/prompts", http.StatusSeeOther)
}

func (h *handlers) promptDelete(w http.ResponseWriter, r *http.Request) {
	err := h.mcpcfg.DeletePrompt(r.Context(), r.PathValue("id"))
	if err != nil && !errors.Is(err, store.ErrMCPPromptNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings/prompts", http.StatusSeeOther)
}

// promptInput parses the shared form into a PromptInput. It returns ok=false
// (after writing a response) only on a malformed request body.
func (h *handlers) promptInput(w http.ResponseWriter, r *http.Request) (mcpcfg.PromptInput, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return mcpcfg.PromptInput{}, false
	}
	return mcpcfg.PromptInput{
		Name:        r.PostFormValue("name"),
		Title:       r.PostFormValue("title"),
		Description: r.PostFormValue("description"),
		Body:        r.PostFormValue("body"),
		ArgsText:    r.PostFormValue("args"),
	}, true
}

// promptFormError re-renders the form with the submitted values and the error.
// A non-validation error is treated as an internal failure.
func (h *handlers) promptFormError(w http.ResponseWriter, r *http.Request, base promptFormData, in mcpcfg.PromptInput, err error) {
	var ve mcpcfg.ValidationError
	if !errors.As(err, &ve) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	base.Name = in.Name
	base.Title = in.Title
	base.Description = in.Description
	base.Body = in.Body
	base.ArgsText = in.ArgsText
	base.Err = ve.Msg
	h.renderPromptForm(w, r, http.StatusOK, base)
}

func (h *handlers) renderPromptForm(w http.ResponseWriter, r *http.Request, status int, data promptFormData) {
	ch, err := h.chromeFor(r, "settings", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data.chrome = ch
	render(w, status, "settings_prompt_form.html", data)
}
