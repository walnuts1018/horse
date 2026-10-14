ALTER TABLE document_confirmations
    ALTER COLUMN purchase_id DROP NOT NULL,
    ADD COLUMN correction_id uuid,
    ADD CONSTRAINT document_confirmations_correction_household_fk FOREIGN KEY (household_id, correction_id) REFERENCES corrections(household_id, id),
    ADD CONSTRAINT document_confirmations_target_check CHECK ((purchase_id IS NOT NULL) <> (correction_id IS NOT NULL));

CREATE TABLE correction_evidence_links (
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    source_document_id uuid NOT NULL,
    source_reference text NOT NULL,
    correction_id uuid NOT NULL UNIQUE,
    evidence_region jsonb,
    observation jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_document_id, source_reference),
    FOREIGN KEY (household_id, source_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, correction_id) REFERENCES corrections(household_id, id) ON DELETE CASCADE,
    CHECK (length(source_reference) <= 256)
);
