package update

import (
	"acta/internal/backup"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Only the GitHub API receives the optional repository credential. Redirects to
// release storage are followed without it, including private asset redirects.
func (s *Service) github(ctx context.Context, path string, binary bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+s.c.Repository+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if binary {
		req.Header.Set("Accept", "application/octet-stream")
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if s.c.GitHubTokenFile != "" {
		raw, err := backup.Secret(s.c.GitHubTokenFile)
		if err != nil {
			return nil, errors.New("release credential unavailable")
		}
		req.Header.Set("Authorization", "Bearer "+stringTrim(raw))
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 4 || r.URL.Scheme != "https" {
			return errors.New("invalid release redirect")
		}
		r.Header.Del("Authorization")
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("could not reach GitHub releases")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub releases returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, errors.New("release response exceeds limit")
	}
	return raw, nil
}
func (s *Service) Check(ctx context.Context) error {
	s.mu.Lock()
	if s.busy() || s.checking {
		s.mu.Unlock()
		return ErrBusy
	}
	s.checking = true
	s.mu.Unlock()
	var selected *Signed
	err := func() error {
		raw, err := s.github(ctx, "/releases?per_page=30", false)
		if err != nil {
			return err
		}
		var releases []struct {
			Tag        string `json:"tag_name"`
			Draft      bool   `json:"draft"`
			Prerelease bool   `json:"prerelease"`
			Assets     []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"assets"`
		}
		if err = json.Unmarshal(raw, &releases); err != nil {
			return err
		}
		current, err := Verify(s.View().Current, s.key, s.c.Repository)
		if err != nil {
			return err
		}
		sequence := current.Sequence
		for _, r := range releases {
			if r.Draft || r.Prerelease && !s.c.Prereleases {
				continue
			}
			for _, a := range r.Assets {
				if a.Name != "acta-release.json" {
					continue
				}
				raw, err = s.github(ctx, "/releases/assets/"+strconv.FormatInt(a.ID, 10), true)
				if err != nil {
					return err
				}
				var signed Signed
				if err = json.Unmarshal(raw, &signed); err != nil {
					return err
				}
				manifest, err := Verify(signed, s.key, s.c.Repository)
				if err != nil {
					return err
				}
				if manifest.Version != r.Tag {
					return errors.New("release tag and signed version disagree")
				}
				if manifest.Sequence > sequence {
					copy := signed
					selected = &copy
					sequence = manifest.Sequence
				}
			}
		}
		return nil
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checking = false
	now := time.Now().UTC()
	s.state.Checked = &now
	s.state.CheckError = ""
	if err != nil {
		s.state.CheckError = err.Error()
	} else {
		s.state.Available = selected
	}
	if e := s.save(); e != nil {
		return e
	}
	return err
}
