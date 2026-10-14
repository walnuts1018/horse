ALTER TABLE transfers
    ADD CONSTRAINT transfers_household_id_unique UNIQUE (household_id, id);

ALTER TABLE document_confirmations
    ADD COLUMN transfer_id uuid,
    ADD CONSTRAINT document_confirmations_transfer_fk FOREIGN KEY (household_id, transfer_id) REFERENCES transfers(household_id, id),
    DROP CONSTRAINT document_confirmations_target_check,
    ADD CONSTRAINT document_confirmations_target_check CHECK (num_nonnulls(purchase_id, correction_id, account_movement_id, account_income_id, transfer_id) = 1);
