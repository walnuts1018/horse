ALTER TABLE households
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    ADD COLUMN default_allocation_version bigint NOT NULL DEFAULT 1 CHECK (default_allocation_version > 0);
