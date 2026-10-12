ALTER TABLE idempotency_keys
    DROP CONSTRAINT idempotency_keys_pkey;

ALTER TABLE idempotency_keys
    ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (household_id, key, operation);
