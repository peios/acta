// Package backup owns installation recovery policy and orchestration. Its state
// and credentials are independent of the database that it protects.
package backup

import (
	"errors"
	"time"
)

var ErrBusy = errors.New("a backup operation is already queued or running")
var ErrConflict = errors.New("backup policy changed; reload before saving")

// Policy uses anchored UTC intervals, not local calendar times. Missed slots
// coalesce into one run; retries never shift the configured schedule.
type Policy struct {
	Revision          int64     `json:"revision"`
	Enabled           bool      `json:"enabled"`
	Destination       string    `json:"destination"`
	Mode              string    `json:"mode"` // snapshot or continuous
	Anchor            time.Time `json:"anchor"`
	BackupMinutes     int       `json:"backup_minutes"`
	FullMinutes       int       `json:"full_minutes"`
	DrillMinutes      int       `json:"drill_minutes"`
	MaxAgeMinutes     int       `json:"max_age_minutes"`
	ArchiveAgeMinutes int       `json:"archive_age_minutes"`
	RetainFull        int       `json:"retain_full"`
}

type Job struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	State       string     `json:"state"`
	Actor       string     `json:"actor"`
	Policy      Policy     `json:"policy"`
	RequestedAt time.Time  `json:"requested_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Backup      string     `json:"backup,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type Point struct {
	RecoveryFingerprint string     `json:"-"`
	Label               string     `json:"label"`
	Type                string     `json:"type"`
	StartedAt           time.Time  `json:"started_at"`
	FinishedAt          time.Time  `json:"finished_at"`
	DatabaseVersion     string     `json:"database_version"`
	Release             string     `json:"release"`
	Complete            bool       `json:"complete"`
	IntegrityAt         *time.Time `json:"integrity_at,omitempty"`
	RestoredAt          *time.Time `json:"restored_at,omitempty"`
	JobID               string     `json:"-"`
	SealedRecovery      string     `json:"-"`
}

type Repository struct {
	PointCount            int        `json:"point_count"`
	SystemID              string     `json:"-"`
	ObservedAt            time.Time  `json:"observed_at"`
	Points                []Point    `json:"points"`
	ArchiveLast           *time.Time `json:"archive_last,omitempty"`
	ArchiveFailure        *time.Time `json:"archive_failure,omitempty"`
	ArchiveMode           bool       `json:"archive_mode"`
	ArchiveTimeoutSeconds int        `json:"archive_timeout_seconds"`
	Error                 string     `json:"error,omitempty"`
}

type State struct {
	Version     int        `json:"version"`
	Policy      Policy     `json:"policy"`
	Jobs        []Job      `json:"jobs"`
	Repository  Repository `json:"repository"`
	AlertKey    string     `json:"alert_key,omitempty"`
	AlertSentAt *time.Time `json:"alert_sent_at,omitempty"`
	AlertError  string     `json:"alert_error,omitempty"`
}

type Destination struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ConfigFile string `json:"config_file"`
	Repo       int    `json:"repo"`
}

type View struct {
	MinRetainFull int        `json:"min_retain_full"`
	MaxRetainFull int        `json:"max_retain_full"`
	Configured    bool       `json:"configured"`
	Driver        string     `json:"driver"`
	Policy        Policy     `json:"policy"`
	Destinations  []Choice   `json:"destinations"`
	CanDrill      bool       `json:"can_drill"`
	Jobs          []Job      `json:"jobs"`
	Repository    Repository `json:"repository"`
	Warnings      []string   `json:"warnings"`
	AlertError    string     `json:"alert_error,omitempty"`
}

type Choice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (p Policy) Validate(c Config) error {
	if _, ok := c.Destination(p.Destination); !ok {
		return errors.New("choose an operator-configured destination")
	}
	if p.Mode != "snapshot" && p.Mode != "continuous" {
		return errors.New("choose snapshot or continuous recovery")
	}
	if p.Anchor.IsZero() || p.BackupMinutes < 5 || p.BackupMinutes > 525600 {
		return errors.New("backup interval must be 5 minutes to one year with a UTC anchor")
	}
	if p.FullMinutes < p.BackupMinutes || p.FullMinutes > 525600 {
		return errors.New("full backup interval must be at least the backup interval and at most one year")
	}
	if p.DrillMinutes < 0 || p.DrillMinutes > 525600 || (p.DrillMinutes > 0 && p.DrillMinutes < 60) {
		return errors.New("restore drill interval must be zero (manual) or 60 minutes to one year")
	}
	if p.DrillMinutes > 0 && c.RecoveryIdentityFile == "" {
		return errors.New("the operator must configure a recovery identity before enabling restore drills")
	}
	if p.MaxAgeMinutes < p.BackupMinutes || p.MaxAgeMinutes > 1051200 {
		return errors.New("backup age alert must be at least the backup interval and at most two years")
	}
	if p.ArchiveAgeMinutes < 1 || p.ArchiveAgeMinutes > 1440 {
		return errors.New("archive age alert must be 1–1440 minutes")
	}
	if p.RetainFull < c.MinRetainFull || p.RetainFull > c.MaxRetainFull {
		return errors.New("retention is outside the operator's permitted range")
	}
	return nil
}

// Due returns the latest scheduled slot. It never backlogs every missed run.
func Due(now, anchor time.Time, minutes int, last time.Time) bool {
	if minutes <= 0 || now.Before(anchor) {
		return false
	}
	d := time.Duration(minutes) * time.Minute
	slot := anchor.Add(now.Sub(anchor) / d * d)
	return last.Before(slot)
}
