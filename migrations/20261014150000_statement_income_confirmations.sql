ALTER TABLE document_confirmations
    ADD COLUMN account_income_id uuid,
    DROP CONSTRAINT document_confirmations_target_check,
    ADD CONSTRAINT document_confirmations_account_income_fk FOREIGN KEY (household_id, account_income_id) REFERENCES account_incomes(household_id, id),
    ADD CONSTRAINT document_confirmations_target_check CHECK (num_nonnulls(purchase_id, correction_id, account_movement_id, account_income_id) = 1);
