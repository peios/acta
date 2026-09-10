package tasks

import "acta2/internal/accounts"

type Archive struct {
	Archived bool  `json:"archived"`
	Version  int64 `json:"version"`
}

func ArchivedError() error {
	return &accounts.FieldError{Field: "archived", Message: "Restore this archived task before changing it."}
}
