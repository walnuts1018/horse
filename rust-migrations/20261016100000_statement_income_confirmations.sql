ALTER TABLE source_confirmations
    ADD COLUMN account_income_id uuid GENERATED ALWAYS AS (CASE WHEN entity_kind = 'account_income' THEN entity_id END) STORED,
    DROP CONSTRAINT source_confirmations_entity_kind_check,
    ADD CONSTRAINT source_confirmations_entity_kind_check CHECK (entity_kind IN ('purchase', 'purchase_correction', 'transfer', 'account_income')),
    ADD CONSTRAINT source_confirmations_account_income_fk FOREIGN KEY (household_id, account_income_id) REFERENCES account_incomes(household_id, id);
