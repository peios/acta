-- Existing captures keep their identity at output zero. New captures are atomic
-- bundles indexed by capture sequence and the adapter's deterministic output.
ALTER TABLE provider_thread_frames ADD COLUMN output_index integer NOT NULL DEFAULT 0 CHECK(output_index >= 0);
ALTER TABLE provider_thread_frames DROP CONSTRAINT provider_thread_frames_pkey;
ALTER TABLE provider_thread_frames ADD PRIMARY KEY(thread_id,sequence,output_index);
