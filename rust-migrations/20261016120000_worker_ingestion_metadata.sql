ALTER TABLE source_documents
    ADD COLUMN analysis_object_key text,
    ADD COLUMN analysis_mime_type text,
    ADD COLUMN analysis_transform jsonb,
    ADD CONSTRAINT source_documents_analysis_object_pair CHECK (
        (analysis_object_key IS NULL AND analysis_mime_type IS NULL AND analysis_transform IS NULL)
        OR (analysis_object_key IS NOT NULL AND analysis_mime_type IS NOT NULL AND analysis_transform IS NOT NULL)
    );

CREATE UNIQUE INDEX source_documents_analysis_object_key_key
    ON source_documents (analysis_object_key)
    WHERE analysis_object_key IS NOT NULL;

ALTER TABLE extraction_jobs
    ADD COLUMN interaction_reply_preview text NOT NULL DEFAULT '';

ALTER TABLE extraction_jobs
    DROP CONSTRAINT extraction_jobs_attempt_count_check;
