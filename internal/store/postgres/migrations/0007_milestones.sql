-- 0007_milestones: any item can be flagged as a milestone.
--
-- A milestone is just a task with is_milestone = true. It changes nothing in
-- Status board mode; in Milestone mode, root milestones become columns holding
-- their children, and root non-milestones fall into a "Backlog" column.

ALTER TABLE items ADD COLUMN is_milestone boolean NOT NULL DEFAULT false;
