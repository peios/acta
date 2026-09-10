package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"acta2/internal/backup"
	"acta2/internal/localstate"
	"acta2/internal/recovery"
)

type Docker struct{ Config Config }

// Command diagnostics stay in the operator log: docker/compose may print paths
// and environment values. The browser sees the bounded operation name only.
func command(ctx context.Context, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, "docker", args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 5 * time.Second
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("Docker %s failed; see updater operator log", args[0])
	}
	if out.Len() > 8<<20 {
		return nil, errors.New("Docker response exceeds limit")
	}
	return out.Bytes(), nil
}
func (d Docker) base() []string {
	a := []string{"compose", "--project-name", d.Config.Project, "--env-file", d.Config.EnvFile}
	for _, p := range d.Config.ComposeFiles {
		a = append(a, "-f", p)
	}
	return append(a, "-f", filepath.Join(d.Config.StateDir, "active.json"), "--profile", "backups", "--profile", "updates")
}
func (d Docker) compose(ctx context.Context, args ...string) ([]byte, error) {
	return command(ctx, append(d.base(), args...)...)
}
func (d Docker) releases(j Job) (Release, Release, error) {
	k, e := d.Config.Key()
	if e != nil {
		return Release{}, Release{}, e
	}
	a, e := Verify(j.Previous, k, d.Config.Repository)
	if e != nil {
		return a, Release{}, e
	}
	b, e := Verify(j.Target, k, d.Config.Repository)
	return a, b, e
}
func (d Docker) override(r Release, candidate bool) error {
	services := map[string]any{}
	for k, v := range r.Images {
		services[k] = map[string]any{"image": v}
	}
	services["app"] = map[string]any{"image": r.Images["app"], "environment": map[string]string{"ACTA_RECOVERY_MODE": fmt.Sprint(candidate), "ACTA_UPDATE_CANDIDATE": fmt.Sprint(candidate)}}
	services["backup"] = map[string]any{"image": r.Images["backup"], "environment": map[string]string{"ACTA_RELEASE": r.Version}}
	return localstate.Write(filepath.Join(d.Config.StateDir, "active.json"), map[string]any{"services": services})
}
func (d Docker) Prepare(ctx context.Context, j Job) error {
	a, b, err := d.releases(j)
	if err != nil {
		return err
	}
	if err = Compatible(a, b); err != nil {
		return err
	}
	// The installation must actually be running the journalled source images.
	for _, service := range []string{"app", "db", "backup", "caddy"} {
		id, err := d.compose(ctx, "ps", "-q", service)
		if err != nil || len(bytes.TrimSpace(id)) == 0 {
			return fmt.Errorf("%s must be running before updating", service)
		}
		raw, err := command(ctx, "inspect", "--format", "{{.Config.Image}}", stringTrim(id))
		if err != nil {
			return err
		}
		if stringTrim(raw) != a.Images[service] {
			return fmt.Errorf("%s image has drifted from the recorded release", service)
		}
	}
	for _, service := range []string{"app", "db", "backup", "caddy", "updater"} {
		if _, err = command(ctx, "pull", b.Images[service]); err != nil {
			return err
		}
	}
	// A fresh independently configured full backup and restore drill establish
	// recoverability before maintenance. The later cold copy closes the write gap.
	if err = d.Prune(ctx); err != nil {
		return err
	}
	for _, kind := range []string{"full", "drill"} {
		if err = d.backup(ctx, j, kind); err != nil {
			return err
		}
	}
	return nil
}
func (d Docker) backup(ctx context.Context, j Job, kind string) error {
	c := backup.Client{Socket: d.Config.BackupSocket, TokenFile: d.Config.BackupTokenFile}
	path := filepath.Join(d.Config.StateDir, j.ID+"-"+kind+".json")
	var saved struct {
		ID string `json:"id"`
	}
	err := localstate.Read(path, &saved)
	if errors.Is(err, os.ErrNotExist) {
		status, raw, e := c.Do(ctx, "POST", "/v1/jobs", map[string]string{"kind": kind, "actor": "updater:" + j.Actor})
		if e != nil {
			return e
		}
		if status != 202 {
			return fmt.Errorf("backup service refused %s; check its policy and active jobs", kind)
		}
		if e = json.Unmarshal(raw, &saved); e != nil {
			return e
		}
		if saved.ID == "" {
			return errors.New("backup service returned no job")
		}
		if e = localstate.Write(path, saved); e != nil {
			return e
		}
	} else if err != nil {
		return err
	}
	for {
		status, raw, e := c.Do(ctx, "GET", "/v1/status", nil)
		if e != nil {
			return e
		}
		if status != 200 {
			return errors.New("backup status unavailable")
		}
		var v backup.View
		if e = json.Unmarshal(raw, &v); e != nil {
			return e
		}
		found := false
		for _, job := range v.Jobs {
			if job.ID != saved.ID {
				continue
			}
			found = true
			if job.State == "succeeded" {
				return nil
			}
			if job.State != "queued" && job.State != "running" {
				return fmt.Errorf("pre-update %s did not succeed; inspect Backups", kind)
			}
		}
		if !found {
			return errors.New("pre-update backup job is missing")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
func (d Docker) Quiesce(ctx context.Context, j Job) error {
	// Public marker contains no credentials; Caddy can read it. File data is
	// immaterial: presence is the gate, so JSON encoding is fine.
	if err := localstate.Write(filepath.Join(d.Config.StateDir, "maintenance.json"), map[string]string{"job": j.ID}); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(d.Config.StateDir, "maintenance.json"), 0644); err != nil {
		return err
	}
	if _, err := d.compose(ctx, "stop", "app", "backup"); err != nil {
		return err
	}
	_, err := d.compose(ctx, "stop", "db")
	return err
}
func (d Docker) volume(ctx context.Context) (string, error) {
	id, err := d.compose(ctx, "ps", "-a", "-q", "db")
	if err != nil {
		return "", err
	}
	raw, err := command(ctx, "inspect", stringTrim(id))
	if err != nil {
		return "", err
	}
	var rows []struct {
		State  struct{ Running bool }
		Config struct{ Labels map[string]string }
		Mounts []struct{ Type, Name, Destination string }
	}
	if err = json.Unmarshal(raw, &rows); err != nil || len(rows) != 1 {
		return "", errors.New("database container not found")
	}
	r := rows[0]
	if r.State.Running || r.Config.Labels["com.docker.compose.project"] != d.Config.Project || r.Config.Labels["com.docker.compose.service"] != "db" {
		return "", errors.New("database must be stopped and belong to this installation")
	}
	for _, m := range r.Mounts {
		if m.Destination == "/var/lib/postgresql/data" && m.Type == "volume" {
			raw, err := command(ctx, "volume", "inspect", m.Name)
			if err != nil {
				return "", err
			}
			var vols []struct{ Labels map[string]string }
			if json.Unmarshal(raw, &vols) != nil || len(vols) != 1 || vols[0].Labels["com.docker.compose.project"] != d.Config.Project || vols[0].Labels["com.docker.compose.volume"] != "database" {
				return "", errors.New("database volume is not owned by this installation")
			}
			return m.Name, nil
		}
	}
	return "", errors.New("database must use the installation's named volume")
}
func (d Docker) snapshotName(j Job) string { return d.Config.Project + "-update-" + j.ID }
func (d Docker) Snapshot(ctx context.Context, j Job) error {
	volume, err := d.volume(ctx)
	if err != nil {
		return err
	}
	a, _, err := d.releases(j)
	if err != nil {
		return err
	}
	name := d.snapshotName(j)
	if _, err = command(ctx, "volume", "create", "--label", "acta.update.installation="+d.Config.Installation, "--label", "acta.update.job="+j.ID, name); err != nil {
		return err
	}
	if err = d.ownedSnapshot(ctx, j); err != nil {
		return err
	}
	return d.copy(ctx, j, a.Images["db"], volume, name)
}
func (d Docker) ownedSnapshot(ctx context.Context, j Job) error {
	raw, err := command(ctx, "volume", "inspect", d.snapshotName(j))
	if err != nil {
		return err
	}
	var volumes []struct{ Labels map[string]string }
	if json.Unmarshal(raw, &volumes) != nil || len(volumes) != 1 || volumes[0].Labels["acta.update.installation"] != d.Config.Installation || volumes[0].Labels["acta.update.job"] != j.ID {
		return errors.New("recovery volume ownership mismatch")
	}
	return nil
}
func (d Docker) copy(ctx context.Context, j Job, image, source, target string) error {
	name := d.Config.Project + "-copy-" + j.ID
	// Reconcile a helper left alive when only the updater was killed. Its fixed
	// name is installation-local, and its ownership is checked before removal.
	raw, e := command(ctx, "container", "ls", "-aq", "--filter", "name=^/"+name+"$")
	if e != nil {
		return e
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		label, e := command(ctx, "inspect", "--format", "{{index .Config.Labels \"acta.update.job\"}}", name)
		if e != nil || stringTrim(label) != j.ID {
			return errors.New("copy helper ownership mismatch")
		}
		if _, e = command(ctx, "rm", "-f", name); e != nil {
			return e
		}
	}
	_, e = command(ctx, "run", "--name", name, "--label", "acta.update.job="+j.ID, "--label", "acta.update.installation="+d.Config.Installation, "--network", "none", "--read-only", "--mount", "type=volume,src="+source+",dst=/source,readonly", "--mount", "type=volume,src="+target+",dst=/target", "--entrypoint", "/bin/sh", image, "-ec", `test "$(cat /source/PG_VERSION)" = 17; test -z "$(find /source/pg_tblspc -mindepth 1 -print -quit)"; need=$(du -sb /source | cut -f1); available=$(df -PB1 /target | awk 'NR==2 {print $4}'); existing=$(du -sb /target | cut -f1); test "$((available + existing))" -ge "$((need + need / 10 + 536870912))"; find /target -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +; cp -a /source/. /target/; sync`)
	if e != nil {
		return e
	}
	_, e = command(ctx, "rm", name)
	return e
}
func (d Docker) Apply(ctx context.Context, j Job) error {
	_, b, err := d.releases(j)
	if err != nil {
		return err
	}
	if err = d.override(b, true); err != nil {
		return err
	}
	_, err = d.compose(ctx, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "--wait", "--wait-timeout", "180", "db")
	if err != nil {
		return err
	}
	_, err = d.compose(ctx, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "--wait", "--wait-timeout", "300", "app")
	return err
}
func (d Docker) Validate(ctx context.Context, j Job) error {
	_, err := d.compose(ctx, "exec", "-T", "app", "/usr/local/bin/acta-entrypoint", "/usr/local/bin/acta2-server", "-verify-recovery")
	return err
}
func (d Docker) Restore(ctx context.Context, j Job) error {
	if err := d.Quiesce(ctx, j); err != nil {
		return err
	}
	a, _, err := d.releases(j)
	if err != nil {
		return err
	}
	if j.RestoreData {
		if err = d.ownedSnapshot(ctx, j); err != nil {
			return err
		}
		volume, err := d.volume(ctx)
		if err != nil {
			return err
		}
		if err = d.copy(ctx, j, a.Images["db"], d.snapshotName(j), volume); err != nil {
			return err
		}
	}
	if err = d.override(a, true); err != nil {
		return err
	}
	if _, err = d.compose(ctx, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "--wait", "--wait-timeout", "180", "db", "app"); err != nil {
		return err
	}
	if err = d.Validate(ctx, j); err != nil {
		return err
	}
	if j.RestoreData {
		_, err = d.compose(ctx, "exec", "-T", "-u", "postgres", "db", "psql", "-U", "postgres", "-d", "acta2", "-v", "ON_ERROR_STOP=1", "-c", recovery.RevokeSQL)
	}
	return err
}
func (d Docker) Publish(ctx context.Context, j Job, rollback bool) error {
	a, b, err := d.releases(j)
	if err != nil {
		return err
	}
	r := b
	if rollback {
		r = a
	}
	if err = d.override(r, false); err != nil {
		return err
	}
	// This phase is a forward-only boundary: it must never restore data again.
	// A restarted final app waits for the maintenance marker to be removed.
	if _, err = d.compose(ctx, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "db", "app", "backup"); err != nil {
		return err
	}
	marker := filepath.Join(d.Config.StateDir, "maintenance.json")
	if err = os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir, err := os.Open(d.Config.StateDir)
	if err != nil {
		return err
	}
	err = dir.Sync()
	dir.Close()
	if err != nil {
		return err
	}
	_, err = d.compose(ctx, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "--wait", "--wait-timeout", "300", "db", "app", "backup", "caddy")
	return err
}

// ReconcileUpdater is deliberately outside the data cutover. A tiny detached
// Docker CLI helper replaces the service only after its successful journal entry
// is durable. The new updater reads that same journal and cannot replay cutover.
func (d Docker) ReconcileUpdater(ctx context.Context) error {
	raw, err := d.compose(ctx, "ps", "-q", "updater")
	if err != nil || stringTrim(raw) == "" {
		return err
	}
	id := stringTrim(raw)
	var state State
	if err = localstate.Read(filepath.Join(d.Config.StateDir, "state.json"), &state); err != nil {
		return err
	}
	key, err := d.Config.Key()
	if err != nil {
		return err
	}
	r, err := Verify(state.Current, key, d.Config.Repository)
	if err != nil {
		return err
	}
	current, err := command(ctx, "inspect", "--format", "{{.Config.Image}}", id)
	if err != nil {
		return err
	}
	if stringTrim(current) == r.Images["updater"] {
		return nil
	}
	name := d.Config.Project + "-updater-replace"
	existing, err := command(ctx, "container", "ls", "-aq", "--filter", "name=^/"+name+"$")
	if err != nil {
		return err
	}
	if stringTrim(existing) != "" {
		label, e := command(ctx, "inspect", "--format", "{{index .Config.Labels \"acta.update.installation\"}}", name)
		if e != nil || stringTrim(label) != d.Config.Installation {
			return errors.New("replacement helper ownership mismatch")
		}
		running, e := command(ctx, "inspect", "--format", "{{.State.Running}}", name)
		if e != nil {
			return e
		}
		if stringTrim(running) == "true" {
			return nil
		}
		if _, e = command(ctx, "rm", name); e != nil {
			return e
		}
	}
	args := []string{"run", "-d", "--name", name, "--label", "acta.update.installation=" + d.Config.Installation, "--restart", "on-failure:3", "--network", "none", "--volumes-from", id, "--entrypoint", "docker", r.Images["updater"]}
	args = append(args, d.base()...)
	args = append(args, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "updater")
	_, err = command(ctx, args...)
	return err
}
