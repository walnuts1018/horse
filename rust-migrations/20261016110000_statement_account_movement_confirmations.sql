ALTER TABLE source_confirmations
    ADD COLUMN account_movement_id uuid GENERATED ALWAYS AS (CASE WHEN entity_kind = 'account_movement' THEN entity_id END) STORED,
    DROP CONSTRAINT source_confirmations_entity_kind_check,
    ADD CONSTRAINT source_confirmations_entity_kind_check CHECK (entity_kind IN ('purchase', 'purchase_correction', 'transfer', 'account_income', 'account_movement')),
    ADD CONSTRAINT source_confirmations_account_movement_fk FOREIGN KEY (household_id, account_movement_id) REFERENCES account_movements(household_id, id);
