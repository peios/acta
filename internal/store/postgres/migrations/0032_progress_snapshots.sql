-- 0032_progress_snapshots: a daily history of how much of a release (or a
-- project) is done, plus a date to measure a release against.
--
-- target_date is the day a release is aiming at — nullable, since plenty of
-- releases ship when they're ready. It's a date, not a timestamp: "we're aiming
-- at the 14th" has no hour.
ALTER TABLE releases ADD COLUMN target_date date;

-- One row per subject per day: the answer to "how much of this was done at the
-- end of that day". Deliberately a snapshot table rather than a replay of the
-- event log — status events record lane *names* and "done" is the positional
-- last lane, so a rename or a reorder silently rewrites history. A snapshot is
-- what was true that day and stays true.
--
-- subject_id is polymorphic (a release id or a project id) so it carries no FK,
-- the same trade memories.scope_id makes; DeleteProgressSnapshots is the manual
-- cascade. Both item counts and size-weighted points are stored: points drive
-- progress, velocity and the forecast, while the item counts stay meaningful to
-- read ("6 of 11 items").
CREATE TABLE progress_snapshots (
    subject_type text    NOT NULL,  -- 'release' | 'project'
    subject_id   text    NOT NULL,
    day          date    NOT NULL,
    done_items   integer NOT NULL DEFAULT 0,
    total_items  integer NOT NULL DEFAULT 0,
    done_points  integer NOT NULL DEFAULT 0,
    total_points integer NOT NULL DEFAULT 0,
    -- true for rows reconstructed from the event log when the feature landed:
    -- best-effort, and shown as such. A measured row always beats a synthetic
    -- one on conflict, never the reverse.
    synthetic    boolean NOT NULL DEFAULT false,
    PRIMARY KEY (subject_type, subject_id, day)
);
