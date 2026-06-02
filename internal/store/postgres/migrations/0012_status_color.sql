-- A per-lane colour. Empty means "auto": the UI derives a palette colour from
-- the lane's board position, so existing boards stay colourful without a
-- backfill. A non-empty value is an explicit choice from the board palette.
ALTER TABLE statuses ADD COLUMN color text NOT NULL DEFAULT '';
