ALTER TABLE shared_purchase_drafts
    DROP CONSTRAINT IF EXISTS shared_purchase_drafts_check,
    DROP CONSTRAINT IF EXISTS shared_purchase_drafts_payload_check,
    ADD CONSTRAINT shared_purchase_drafts_payload_size_check
        CHECK (pg_column_size(payload) <= 524288);
