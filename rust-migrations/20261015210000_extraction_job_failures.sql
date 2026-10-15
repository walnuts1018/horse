ALTER TABLE extraction_jobs
    DROP CONSTRAINT extraction_jobs_check3;

ALTER TABLE extraction_jobs
    ADD COLUMN failure_count integer NOT NULL DEFAULT 0 CHECK (failure_count >= 0);
