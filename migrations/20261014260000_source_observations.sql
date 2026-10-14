CREATE TABLE source_observations (
    household_id uuid NOT NULL,
    source_document_id uuid NOT NULL,
    source_reference text NOT NULL DEFAULT '',
    source_hash text NOT NULL,
    source_kind text NOT NULL CHECK (source_kind IN ('receipt', 'order', 'email', 'card', 'bank')),
    transaction_kind text NOT NULL,
    order_id text NOT NULL DEFAULT '',
    transaction_id text NOT NULL DEFAULT '',
    merchant text NOT NULL DEFAULT '',
    occurred_at timestamptz,
    amount_yen bigint NOT NULL,
    card_last4 text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_document_id, source_reference),
    FOREIGN KEY (household_id, source_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE
);

CREATE INDEX source_observations_household_idx ON source_observations(household_id);
CREATE INDEX source_observations_transaction_idx ON source_observations(household_id, transaction_id) WHERE transaction_id <> '';
CREATE INDEX source_observations_order_idx ON source_observations(household_id, order_id) WHERE order_id <> '';

WITH latest_success AS (
    SELECT DISTINCT ON (source_document_id)
        household_id,
        source_document_id,
        result
    FROM extraction_jobs
    WHERE status = 'succeeded' AND result IS NOT NULL
    ORDER BY source_document_id, version DESC
), statement_rows AS (
    SELECT
        latest_success.household_id,
        latest_success.source_document_id,
        COALESCE(NULLIF(statement."sourceReference", ''), 'row:' || statement."row") AS source_reference,
        source_documents.sha256 AS source_hash,
        source_documents.source_kind,
        statement.kind AS transaction_kind,
        COALESCE(statement."orderId", '') AS order_id,
        COALESCE(statement."transactionId", '') AS transaction_id,
        COALESCE(statement.merchant, '') AS merchant,
        NULLIF(statement."occurredAt", '')::timestamptz AS occurred_at,
        statement.amount::bigint AS amount_yen,
        COALESCE(statement."cardLast4", '') AS card_last4
    FROM latest_success
    JOIN source_documents ON source_documents.id = latest_success.source_document_id
    CROSS JOIN LATERAL jsonb_to_recordset(COALESCE(latest_success.result -> 'statements', '[]'::jsonb)) AS statement(
        "sourceReference" text,
        "row" text,
        kind text,
        "orderId" text,
        "transactionId" text,
        merchant text,
        "occurredAt" text,
        amount text,
        "cardLast4" text
    )
), receipt_rows AS (
    SELECT
        latest_success.household_id,
        latest_success.source_document_id,
        '' AS source_reference,
        source_documents.sha256 AS source_hash,
        source_documents.source_kind,
        'RECEIPT_PURCHASE' AS transaction_kind,
        COALESCE(latest_success.result #>> '{receipt,orderId}', '') AS order_id,
        COALESCE(latest_success.result #>> '{receipt,transactionId}', '') AS transaction_id,
        COALESCE(latest_success.result #>> '{receipt,merchant}', '') AS merchant,
        NULLIF(latest_success.result #>> '{receipt,purchasedAt}', '')::timestamptz AS occurred_at,
        (latest_success.result #>> '{receipt,total}')::bigint AS amount_yen,
        COALESCE((
            SELECT CASE WHEN count(DISTINCT payment."cardLast4") = 1 THEN min(payment."cardLast4") ELSE '' END
            FROM jsonb_to_recordset(COALESCE(latest_success.result #> '{receipt,payments}', '[]'::jsonb)) AS payment("cardLast4" text)
            WHERE COALESCE(payment."cardLast4", '') <> ''
        ), '') AS card_last4
    FROM latest_success
    JOIN source_documents ON source_documents.id = latest_success.source_document_id
    WHERE jsonb_array_length(COALESCE(latest_success.result #> '{receipt,items}', '[]'::jsonb)) > 0
)
INSERT INTO source_observations (
    household_id, source_document_id, source_reference, source_hash, source_kind,
    transaction_kind, order_id, transaction_id, merchant, occurred_at, amount_yen, card_last4
)
SELECT household_id, source_document_id, source_reference, source_hash, source_kind,
       transaction_kind, order_id, transaction_id, merchant, occurred_at, amount_yen, card_last4
FROM statement_rows
UNION ALL
SELECT household_id, source_document_id, source_reference, source_hash, source_kind,
       transaction_kind, order_id, transaction_id, merchant, occurred_at, amount_yen, card_last4
FROM receipt_rows;
