package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Engine interface {
	Inspect(context.Context, Policy) (Repository, error)
	Backup(context.Context, Policy, string, bool) (Point, error)
	Verify(context.Context, Policy, string) error
	Drill(context.Context, Policy, string) error
	Expire(context.Context, Policy) error
}

type PGBackRest struct{ Config Config }

func (e PGBackRest) run(ctx context.Context, p Policy, op string, args ...string) ([]byte, error) {
	d, ok := e.Config.Destination(p.Destination)
	if !ok {
		return nil, errors.New("unknown destination")
	}
	base := []string{"--config=" + d.ConfigFile, "--stanza=" + e.Config.Stanza, "--repo=" + strconv.Itoa(d.Repo), "--log-level-console=error"}
	if op == "check" {
		base = []string{"--config=" + d.ConfigFile, "--stanza=" + e.Config.Stanza, "--log-level-console=error"}
	}
	return command(ctx, e.Config.PGBackRest, append(append(base, args...), op), nil)
}

func (e PGBackRest) connect(ctx context.Context) (*pgx.Conn, error) {
	raw, err := Secret(e.Config.DatabaseURLFile)
	if err != nil {
		return nil, err
	}
	c, err := pgx.Connect(ctx, strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("cannot connect to configured PostgreSQL database")
	}
	return c, nil
}

type recovery struct {
	Settings        map[string]string `json:"settings"`
	Version         int               `json:"version"`
	Release         Release           `json:"release"`
	Database        string            `json:"database"`
	DatabaseUser    string            `json:"database_user"`
	SystemID        string            `json:"system_id"`
	PostgresVersion int               `json:"postgres_version"`
	SecurityKey     string            `json:"security_key"`
	Migrations      map[string]string `json:"migrations"`
}

func (e PGBackRest) recovery(ctx context.Context) (recovery, error) {
	var r recovery
	r.Version = 1
	if err := ReadJSON(e.Config.ReleaseFile, &r.Release); err != nil {
		return r, err
	}
	if r.Release.ID == "" || r.Release.Installation == "" || len(r.Release.SHA256) != 64 || !strings.HasPrefix(r.Release.PublicURL, "http") {
		return r, errors.New("release record needs an ID, installation, executable SHA-256 and public URL")
	}
	if _, err := hex.DecodeString(r.Release.SHA256); err != nil {
		return r, errors.New("release SHA-256 is invalid")
	}
	if _, err := e.releaseExecutable(r.Release); err != nil {
		return r, err
	}
	raw, err := Secret(e.Config.SecurityKeyFile)
	if err != nil {
		return r, err
	}
	r.SecurityKey = strings.TrimSpace(string(raw))
	key, err := base64.RawStdEncoding.DecodeString(r.SecurityKey)
	if err != nil || len(key) != 32 {
		return r, errors.New("invalid installation security key")
	}
	c, err := e.connect(ctx)
	if err != nil {
		return r, err
	}
	defer c.Close(ctx)
	if err = c.QueryRow(ctx, `SELECT current_database(),current_user,system_identifier::text,current_setting('server_version_num')::int FROM pg_control_system()`).Scan(&r.Database, &r.DatabaseUser, &r.SystemID, &r.PostgresVersion); err != nil {
		return r, err
	}
	if r.Database != e.Config.DatabaseName || r.DatabaseUser != e.Config.DatabaseUser {
		return r, errors.New("database connection does not match operator-configured identity")
	}
	var fingerprint []byte
	if err = c.QueryRow(ctx, `SELECT fingerprint FROM security_key WHERE singleton`).Scan(&fingerprint); err != nil {
		return r, err
	}
	digest := sha256.Sum256(key)
	if !strings.EqualFold(hex.EncodeToString(fingerprint), hex.EncodeToString(digest[:])) {
		return r, errors.New("security key does not match this database")
	}
	r.Settings = map[string]string{}
	settings, err := c.Query(ctx, `SELECT name,setting FROM pg_settings WHERE name IN ('max_connections','max_worker_processes','max_wal_senders','max_prepared_transactions','max_locks_per_transaction')`)
	if err != nil {
		return r, err
	}
	for settings.Next() {
		var name, value string
		if err = settings.Scan(&name, &value); err != nil {
			settings.Close()
			return r, err
		}
		r.Settings[name] = value
	}
	err = settings.Err()
	settings.Close()
	if err != nil {
		return r, err
	}
	r.Migrations = map[string]string{}
	rows, err := c.Query(ctx, `SELECT name,checksum FROM schema_migrations ORDER BY name`)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, sum string
		if err = rows.Scan(&name, &sum); err != nil {
			return r, err
		}
		r.Migrations[name] = sum
	}
	return r, rows.Err()
}

type infoBackup struct {
	HasRecovery bool `json:"-"`

	Label     string `json:"label"`
	Type      string `json:"type"`
	Error     bool   `json:"error"`
	Timestamp struct {
		Start int64 `json:"start"`
		Stop  int64 `json:"stop"`
	} `json:"timestamp"`
	Annotation map[string]string `json:"annotation"`
	Database   struct {
		ID      int `json:"id"`
		RepoKey int `json:"repo-key"`
	} `json:"database"`
}

type infoStanza struct {
	Name   string `json:"name"`
	Status struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
	Cipher string       `json:"cipher"`
	Backup []infoBackup `json:"backup"`
	DB     []struct {
		ID       int    `json:"id"`
		Version  string `json:"version"`
		RepoKey  int    `json:"repo-key"`
		SystemID uint64 `json:"system-id"`
	} `json:"db"`
}

func parseInfo(raw []byte, stanza string) (Repository, error) {
	return parseInfoReader(bytes.NewReader(raw), stanza, "")
}

// Decode one backup at a time; encrypted recovery records can dominate large
// catalogs. Only retain the selected restore record, never every sealed key.
func parseInfoReader(reader io.Reader, stanza, selected string) (Repository, error) {
	out := Repository{ObservedAt: time.Now().UTC(), Points: []Point{}}
	d := json.NewDecoder(reader)
	token, err := d.Token()
	if err != nil || token != json.Delim('[') {
		return out, errors.New("invalid repository catalog")
	}
	var all []infoStanza
	for d.More() {
		token, err = d.Token()
		if err != nil || token != json.Delim('{') {
			return out, errors.New("invalid stanza")
		}
		var s infoStanza
		for d.More() {
			name, err := d.Token()
			if err != nil {
				return out, err
			}
			switch name {
			case "name":
				err = d.Decode(&s.Name)
			case "status":
				err = d.Decode(&s.Status)
			case "cipher":
				err = d.Decode(&s.Cipher)
			case "db":
				err = d.Decode(&s.DB)
			case "backup":
				token, err = d.Token()
				if err != nil {
					return out, err
				}
				if token != json.Delim('[') {
					return out, errors.New("invalid backup array")
				}
				for d.More() {
					var b infoBackup
					if err = d.Decode(&b); err != nil {
						return out, err
					}
					b.HasRecovery = b.Annotation["acta-recovery"] != ""
					if b.Label != selected {
						delete(b.Annotation, "acta-recovery")
					}
					s.Backup = append(s.Backup, b)
				}
				_, err = d.Token()
			default:
				var ignored json.RawMessage
				err = d.Decode(&ignored)
			}
			if err != nil {
				return out, err
			}
		}
		if _, err = d.Token(); err != nil {
			return out, err
		}
		all = append(all, s)
	}
	if _, err = d.Token(); err != nil {
		return out, err
	}
	if _, err = d.Token(); err != io.EOF {
		return out, errors.New("trailing repository output")
	}
	for _, s := range all {
		if s.Name != stanza {
			continue
		}
		// 2 means a valid stanza with no backups yet. All other nonzero codes
		// are repository faults; never report stale/partial data as healthy.
		if s.Status.Code != 0 && s.Status.Code != 2 {
			return out, errors.New("pgBackRest repository is not healthy")
		}
		if s.Cipher == "" || s.Cipher == "none" {
			return out, errors.New("repository encryption must be enabled by the deployment operator")
		}
		latestDB := 0
		for _, db := range s.DB {
			if db.ID > latestDB {
				latestDB = db.ID
				out.SystemID = strconv.FormatUint(db.SystemID, 10)
			}
		}
		for _, b := range s.Backup {
			p := Point{Label: b.Label, Type: b.Type, StartedAt: time.Unix(b.Timestamp.Start, 0).UTC(), FinishedAt: time.Unix(b.Timestamp.Stop, 0).UTC(), Release: b.Annotation["acta-release"], JobID: b.Annotation["acta-job"], SealedRecovery: b.Annotation["acta-recovery"]}
			p.RecoveryFingerprint = b.Annotation["acta-recovery-fingerprint"]
			p.Complete = b.HasRecovery && p.Release != "" && b.Annotation["acta-complete"] == "1"
			if t, err := time.Parse(time.RFC3339Nano, b.Annotation["acta-integrity"]); err == nil {
				p.IntegrityAt = &t
			}
			if t, err := time.Parse(time.RFC3339Nano, b.Annotation["acta-restored"]); err == nil {
				p.RestoredAt = &t
			}
			if b.Error {
				p.Complete = false
				p.IntegrityAt = nil
				p.RestoredAt = nil
			}
			for _, db := range s.DB {
				if db.ID == b.Database.ID && db.RepoKey == b.Database.RepoKey {
					p.DatabaseVersion = db.Version
				}
			}
			out.Points = append(out.Points, p)
		}
		out.PointCount = len(out.Points)
		sort.Slice(out.Points, func(i, j int) bool { return out.Points[i].FinishedAt.After(out.Points[j].FinishedAt) })
		return out, nil
	}
	return out, errors.New("configured stanza is missing")
}

func (e PGBackRest) repository(ctx context.Context, p Policy) (Repository, error) {
	return e.catalog(ctx, p, "")
}
func (e PGBackRest) catalog(ctx context.Context, p Policy, selected string) (Repository, error) {
	dest, ok := e.Config.Destination(p.Destination)
	if !ok {
		return Repository{}, errors.New("unknown destination")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	done := make(chan error, 1)
	go func() {
		err := commandOutput(ctx, e.Config.PGBackRest, []string{"--config=" + dest.ConfigFile, "--stanza=" + e.Config.Stanza, "--repo=" + strconv.Itoa(dest.Repo), "--log-level-console=error", "--output=json", "info"}, nil, nil, writer)
		writer.CloseWithError(err)
		done <- err
	}()
	r, err := parseInfoReader(reader, e.Config.Stanza, selected)
	if err != nil {
		cancel()
		reader.CloseWithError(err)
	}
	if commandErr := <-done; commandErr != nil {
		return r, commandErr
	}
	return r, err
}
func (e PGBackRest) Inspect(ctx context.Context, p Policy) (Repository, error) {
	r, err := e.repository(ctx, p)
	if err != nil {
		return r, err
	}
	c, err := e.connect(ctx)
	if err != nil {
		return r, err
	}
	defer c.Close(ctx)
	var sourceID string
	if err = c.QueryRow(ctx, `SELECT system_identifier::text FROM pg_control_system()`).Scan(&sourceID); err != nil {
		return r, err
	}
	if r.SystemID == "" || r.SystemID != sourceID {
		return r, errors.New("repository and source PostgreSQL cluster identities differ")
	}
	err = c.QueryRow(ctx, `SELECT last_archived_time,last_failed_time,current_setting('archive_mode') IN ('on','always'),extract(epoch FROM current_setting('archive_timeout')::interval)::int FROM pg_stat_archiver`).Scan(&r.ArchiveLast, &r.ArchiveFailure, &r.ArchiveMode, &r.ArchiveTimeoutSeconds)
	return r, err
}

func (e PGBackRest) Backup(ctx context.Context, p Policy, id string, full bool) (Point, error) {
	r, err := e.Inspect(ctx, p)
	if err != nil {
		return Point{}, err
	}
	if p.Mode == "continuous" && (!r.ArchiveMode || r.ArchiveTimeoutSeconds <= 0 || r.ArchiveTimeoutSeconds > p.ArchiveAgeMinutes*60) {
		return Point{}, errors.New("continuous recovery needs archive_mode and archive_timeout within the configured archive-age limit")
	}
	meta, err := e.recovery(ctx)
	if err != nil {
		return Point{}, err
	}
	raw, _ := json.Marshal(meta)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(raw))
	// A set uploaded before a crash is finalized only if the complete source
	// identity still matches. Never bless a backup crossing a deployment.
	for _, point := range r.Points {
		if point.JobID == id {
			if point.Complete {
				return point, nil
			}
			if point.RecoveryFingerprint != fingerprint {
				return Point{}, errors.New("unfinished backup crossed an installation change; take a new full backup")
			}
			return e.finalize(ctx, p, point)
		}
	}
	sealed, err := command(ctx, e.Config.Age, []string{"--encrypt", "--recipient", e.Config.RecoveryRecipient}, raw)
	if err != nil {
		return Point{}, err
	}
	typ := "incr"
	if full || len(r.Points) == 0 {
		typ = "full"
	}
	_, err = e.run(ctx, p, "backup", "--type="+typ, "--start-fast", "--no-expire-auto", "--annotation=acta-job="+id, "--annotation=acta-recovery-fingerprint="+fingerprint, "--annotation=acta-release="+meta.Release.ID, "--annotation=acta-recovery="+base64.StdEncoding.EncodeToString(sealed))
	if err != nil {
		return Point{}, err
	}
	after, err := e.recovery(ctx)
	if err != nil {
		return Point{}, err
	}
	a, _ := json.Marshal(after)
	if string(a) != string(raw) {
		return Point{}, errors.New("installation identity, release or schema changed during backup; recovery set was not verified")
	}
	r, err = e.repository(ctx, p)
	if err != nil {
		return Point{}, err
	}
	for _, point := range r.Points {
		if point.JobID == id {
			return e.finalize(ctx, p, point)
		}
	}
	return Point{}, errors.New("completed backup was not found in repository")
}

func (e PGBackRest) finalize(ctx context.Context, p Policy, point Point) (Point, error) {
	_, err := e.run(ctx, p, "annotate", "--set="+point.Label, "--annotation=acta-complete=1")
	point.Complete = err == nil
	return point, err
}

// Catalog reads recovery points without contacting the source database.
func (e PGBackRest) Catalog(ctx context.Context, p Policy) (Repository, error) {
	return e.repository(ctx, p)
}

func (e PGBackRest) Verify(ctx context.Context, p Policy, label string) error {
	if label == "" {
		return errors.New("backup label is required")
	}
	r, err := e.repository(ctx, p)
	if err != nil {
		return err
	}
	complete := false
	for _, point := range r.Points {
		if point.Label == label {
			complete = point.Complete
		}
	}
	if !complete {
		return errors.New("backup has not passed installation consistency checks")
	}
	raw, verifyErr := e.run(ctx, p, "verify", "--set="+label, "--output=text", "--verbose")
	if verifyErr == nil {
		verifyErr = verifyReport(raw, e.Config.Stanza, label)
	}
	if verifyErr != nil {
		_, _ = e.run(ctx, p, "annotate", "--set="+label, "--annotation=acta-integrity=", "--annotation=acta-restored=")
		return verifyErr
	}
	_, err = e.run(ctx, p, "annotate", "--set="+label, "--annotation=acta-integrity="+time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (e PGBackRest) Expire(ctx context.Context, p Policy) error {
	r, err := e.repository(ctx, p)
	if err != nil {
		return err
	}
	// Keep the latest successfully restored full chain even when newer backups
	// exist. Never expire while any newer full set is incomplete/unverified.
	fullCount := 0
	keep := p.RetainFull
	found := false
	for _, point := range r.Points {
		if point.Type == "full" {
			fullCount++
			if !point.Complete || point.IntegrityAt == nil {
				return nil
			}
		}
		if point.RestoredAt != nil && point.Type == "full" {
			keep = max(keep, fullCount)
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	d, _ := e.Config.Destination(p.Destination)
	_, err = e.run(ctx, p, "expire", fmt.Sprintf("--repo%d-retention-full=%d", d.Repo, keep), fmt.Sprintf("--repo%d-retention-full-type=count", d.Repo), fmt.Sprintf("--repo%d-retention-archive-type=full", d.Repo), fmt.Sprintf("--repo%d-retention-archive=%d", d.Repo, keep))
	return err
}

func (e PGBackRest) openRecovery(ctx context.Context, p Policy, label, identity string) (recovery, error) {
	var meta recovery
	r, err := e.catalog(ctx, p, label)
	if err != nil {
		return meta, err
	}
	for _, point := range r.Points {
		if point.Label != label {
			continue
		}
		if !point.Complete {
			return meta, errors.New("backup has no complete Acta recovery record")
		}
		sealed, err := base64.StdEncoding.DecodeString(point.SealedRecovery)
		if err != nil {
			return meta, err
		}
		if _, err = Secret(identity); err != nil {
			return meta, err
		}
		raw, err := command(ctx, e.Config.Age, []string{"--decrypt", "--identity", identity}, sealed)
		if err != nil {
			return meta, errors.New("cannot decrypt recovery record with the supplied identity")
		}
		if err = json.Unmarshal(raw, &meta); err != nil {
			return meta, err
		}
		if meta.Version != 1 || meta.Database == "" || len(meta.Migrations) == 0 {
			return meta, errors.New("unsupported or incomplete recovery record")
		}
		return meta, nil
	}
	return meta, os.ErrNotExist
}

func (e PGBackRest) releaseExecutable(release Release) (string, error) {
	if len(release.SHA256) != 64 {
		return "", errors.New("invalid release digest")
	}
	if _, err := hex.DecodeString(release.SHA256); err != nil {
		return "", errors.New("invalid release digest")
	}
	path := filepath.Join(e.Config.ReleaseDir, release.SHA256, "acta2-server")
	f, err := os.Open(path)
	if err != nil {
		return "", errors.New("matching Acta executable is missing from the operator release archive")
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	if hex.EncodeToString(h.Sum(nil)) != release.SHA256 {
		return "", errors.New("archived Acta executable does not match recovery digest")
	}
	return path, nil
}

// pgBackRest 2.59 reports damaged files with exit status zero. Require its
// explicit verbose success report; unknown formats fail closed on upgrade.
func verifyReport(raw []byte, stanza, label string) error {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 5 || lines[0] != "stanza: "+stanza || lines[1] != "status: ok" {
		return errors.New("repository verification did not report a healthy stanza")
	}
	entry := regexp.MustCompile(`^(archiveId: [^,]+, total WAL checked: |backup: ([^,]+), status: valid, total files checked: )([0-9]+), total valid (WAL|files): ([0-9]+)$`)
	found, archive, counters := false, false, 0
	for _, line := range lines[2:] {
		line = strings.TrimSpace(line)
		if line == "missing: 0, checksum invalid: 0, size invalid: 0, other: 0" {
			counters++
			continue
		}
		m := entry.FindStringSubmatch(line)
		if m == nil {
			return errors.New("repository verification reported damage or an unsupported result")
		}
		n, _ := strconv.ParseInt(m[3], 10, 64)
		valid, _ := strconv.ParseInt(m[5], 10, 64)
		if n <= 0 || n != valid {
			return errors.New("repository verification did not validate every file")
		}
		if m[2] == label {
			found = true
		}
		if m[4] == "WAL" {
			archive = true
		}
	}
	if !found || !archive || counters*2 != len(lines)-2 {
		return errors.New("repository verification report is incomplete")
	}
	return nil
}
