CREATE TABLE budget_versions (
    budget_id uuid NOT NULL,
    household_id uuid NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    effective_from date NOT NULL CHECK (extract(day FROM effective_from) = 1),
    category_id uuid,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    amount_yen bigint NOT NULL CHECK (amount_yen > 0),
    starts_on date NOT NULL,
    ends_on date,
    archived boolean NOT NULL,
    PRIMARY KEY (budget_id, revision),
    FOREIGN KEY (household_id, budget_id) REFERENCES budgets(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, category_id) REFERENCES categories(household_id, id),
    CHECK (ends_on IS NULL OR ends_on >= starts_on)
);

CREATE INDEX budget_versions_period_idx ON budget_versions(household_id, budget_id, effective_from DESC, revision DESC);

INSERT INTO budget_versions (
    budget_id,
    household_id,
    revision,
    effective_from,
    category_id,
    name,
    amount_yen,
    starts_on,
    ends_on,
    archived
)
SELECT
    id,
    household_id,
    revision,
    date_trunc('month', starts_on)::date,
    category_id,
    name,
    amount_yen,
    starts_on,
    ends_on,
    archived
FROM budgets;
