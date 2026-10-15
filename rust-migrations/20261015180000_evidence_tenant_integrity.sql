ALTER TABLE accounts
    ADD COLUMN card_last4 text,
    ADD CONSTRAINT accounts_card_last4_check CHECK (
        card_last4 IS NULL OR (kind = 'CARD' AND card_last4 ~ '^[0-9]{4}$')
    );

ALTER TABLE evidence_links
    ADD COLUMN purchase_id uuid GENERATED ALWAYS AS (CASE WHEN entity_kind = 'purchase' THEN entity_id END) STORED,
    ADD COLUMN correction_id uuid GENERATED ALWAYS AS (CASE WHEN entity_kind = 'purchase_correction' THEN entity_id END) STORED,
    ADD COLUMN transfer_id uuid GENERATED ALWAYS AS (CASE WHEN entity_kind = 'transfer' THEN entity_id END) STORED,
    ADD CONSTRAINT evidence_links_entity_kind_check CHECK (entity_kind IN ('purchase', 'purchase_correction', 'transfer')),
    ADD CONSTRAINT evidence_links_purchase_fk FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id),
    ADD CONSTRAINT evidence_links_correction_fk FOREIGN KEY (household_id, correction_id) REFERENCES purchase_corrections(household_id, id),
    ADD CONSTRAINT evidence_links_transfer_fk FOREIGN KEY (household_id, transfer_id) REFERENCES transfers(household_id, id),
    ADD CONSTRAINT evidence_links_purchase_item_fk FOREIGN KEY (household_id, purchase_id, item_id) REFERENCES purchase_items(household_id, purchase_id, id),
    ADD CONSTRAINT evidence_links_item_kind_check CHECK (item_id IS NULL OR entity_kind = 'purchase');

ALTER TABLE source_confirmations
    ADD COLUMN purchase_id uuid GENERATED ALWAYS AS (CASE WHEN entity_kind = 'purchase' THEN entity_id END) STORED,
    ADD COLUMN correction_id uuid GENERATED ALWAYS AS (CASE WHEN entity_kind = 'purchase_correction' THEN entity_id END) STORED,
    ADD COLUMN transfer_id uuid GENERATED ALWAYS AS (CASE WHEN entity_kind = 'transfer' THEN entity_id END) STORED,
    ADD CONSTRAINT source_confirmations_entity_kind_check CHECK (entity_kind IN ('purchase', 'purchase_correction', 'transfer')),
    ADD CONSTRAINT source_confirmations_purchase_fk FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id),
    ADD CONSTRAINT source_confirmations_correction_fk FOREIGN KEY (household_id, correction_id) REFERENCES purchase_corrections(household_id, id),
    ADD CONSTRAINT source_confirmations_transfer_fk FOREIGN KEY (household_id, transfer_id) REFERENCES transfers(household_id, id);

CREATE UNIQUE INDEX source_confirmations_active_source_unique
    ON source_confirmations(household_id, source_document_id, source_reference)
    WHERE status = 'confirmed';
