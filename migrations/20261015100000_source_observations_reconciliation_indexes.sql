CREATE INDEX source_observations_hash_idx ON source_observations(household_id, source_hash);
CREATE INDEX source_observations_amount_occurred_idx ON source_observations(household_id, amount_yen, occurred_at);
