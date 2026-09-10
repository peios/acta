package threads

import "time"

type Descriptor struct {
	NameSyncPending bool      `json:"name_sync_pending,omitempty"`
	NameSyncError   string    `json:"name_sync_error,omitempty"`
	Name            string    `json:"name,omitempty"`
	Committed       bool      `json:"committed"`
	Runtime         *Runtime  `json:"runtime,omitempty"`
	ID              string    `json:"id"`
	Provider        string    `json:"provider"`
	CWD             string    `json:"cwd"`
	ProviderID      string    `json:"provider_id,omitempty"`
	RunID           string    `json:"run_id"`
	State           string    `json:"state"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	Revision        int64     `json:"revision"`
}
type Control struct {
	Name           string              `json:"name,omitempty"`
	LaneID         string              `json:"lane_id,omitempty"`
	QuestionID     string              `json:"question_id,omitempty"`
	Answers        map[string][]string `json:"answers,omitempty"`
	PermissionMode string              `json:"permission_mode,omitempty"`
	ApprovalID     string              `json:"approval_id,omitempty"`
	Decision       string              `json:"decision,omitempty"`
	ID             string              `json:"id"`
	ThreadID       string              `json:"thread_id"`
	Action         string              `json:"action"`
	Provider       string              `json:"provider,omitempty"`
	CWD            string              `json:"cwd,omitempty"`
	RunID          string              `json:"run_id,omitempty"`
	Images         []InputImage        `json:"images,omitempty"`
	Text           string              `json:"text,omitempty"`
	Settings       ModelSettings       `json:"settings,omitzero"`
}
type Result struct {
	Answers        map[string][]string `json:"answers,omitempty"`
	PermissionMode string              `json:"permission_mode,omitempty"`
	Decision       string              `json:"decision,omitempty"`
	Settings       *ModelSettings      `json:"settings,omitempty"`
	ID             string              `json:"id"`
	ThreadID       string              `json:"thread_id"`
	Error          string              `json:"error,omitempty"`
	Outcome        string              `json:"outcome,omitempty"` // send: accepted, rejected, or uncertain
	Models         []ModelOption       `json:"models,omitempty"`
}

// Model capabilities belong to the connected provider, not a server-side list.
type ModelSettings struct {
	Model    string `json:"model"`
	Effort   string `json:"effort"`
	FastMode bool   `json:"fast_mode"`
}
type EffortOption struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}
type ModelOption struct {
	InputModalities []string       `json:"input_modalities,omitempty"`
	ID              string         `json:"id"`
	ResolvedModel   string         `json:"resolved_model,omitempty"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Efforts         []EffortOption `json:"efforts"`
	DefaultEffort   string         `json:"default_effort"`
	FastMode        bool           `json:"fast_mode"`
	FastDescription string         `json:"fast_description,omitempty"`
}

type CommandStatus struct {
	Control
	Result *Result `json:"result"`
	// MessageConfirmed means the provider echo has reached the conversation,
	// even when it is outside the browser's currently loaded history pages.
	MessageConfirmed bool `json:"message_confirmed,omitempty"`
}

// Runtime is provider process metadata, carried by discovery, not conversation.
type Runtime struct {
	UserAgent      string `json:"user_agent,omitempty"`
	PlatformFamily string `json:"platform_family,omitempty"`
	PlatformOS     string `json:"platform_os,omitempty"`
	Version        string `json:"version,omitempty"`
}

// SupportedProvider is shared by creation and discovery validation.
func SupportedProvider(id string) bool { return id == "codex" || id == "claude" }

func ValidPermissionMode(mode string) bool {
	return mode == "ask" || mode == "automatic" || mode == "bypass"
}
