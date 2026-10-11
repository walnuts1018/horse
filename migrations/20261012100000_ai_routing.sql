ALTER TABLE extraction_jobs
    ADD COLUMN failure_count integer NOT NULL DEFAULT 0 CHECK (failure_count >= 0);
