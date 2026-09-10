package update

import (
	"context"
	"fmt"
	"path/filepath"

	"acta2/internal/localstate"
)

// Prune only copies named in our durable terminal jobs and carrying both exact
// installation and job ownership labels. Unknown volumes are never candidates.
func (d Docker) Prune(ctx context.Context) error {
	var state State
	if err := localstate.Read(filepath.Join(d.Config.StateDir, "state.json"), &state); err != nil {
		return err
	}
	keep := d.Config.RetainRecoveryCopies
	if keep == 0 {
		keep = 2
	}
	for _, j := range state.Jobs {
		if !terminal(j.Phase) {
			continue
		}
		if err := d.stopCopy(ctx, j); err != nil {
			return err
		}
		if j.RestoreData && keep > 0 {
			keep--
			continue
		}
		raw, err := command(ctx, "volume", "ls", "-q", "--filter", "label=acta.update.installation="+d.Config.Installation, "--filter", "label=acta.update.job="+j.ID)
		if err != nil {
			return err
		}
		if stringTrim(raw) == "" {
			continue
		}
		if stringTrim(raw) != d.snapshotName(j) {
			return fmt.Errorf("unexpected recovery copy for job %s", j.ID)
		}
		if err = d.ownedSnapshot(ctx, j); err != nil {
			return err
		}
		if _, err = command(ctx, "volume", "rm", d.snapshotName(j)); err != nil {
			return err
		}
	}
	return nil
}
