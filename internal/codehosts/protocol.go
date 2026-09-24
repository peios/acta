package codehosts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const MaxWireBytes = 1024 * 1024
const MaxRequestBytes = 32 * 1024

type Request struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (p *Problem) Error() string { return p.Message }

type Response struct {
	ProviderStatus *ProviderReport `json:"provider_status,omitempty"`
	ID             string          `json:"id,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	Error          *Problem        `json:"error,omitempty"`
}

func Decode(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}
