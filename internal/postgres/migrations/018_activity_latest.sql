CREATE INDEX activity_entries_latest ON activity_entries(subject_type,subject_id,last_event DESC);
