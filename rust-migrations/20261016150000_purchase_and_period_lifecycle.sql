UPDATE purchases
SET status = 'POSTED'
WHERE status = 'CLOSED';

ALTER TABLE purchases
    DROP CONSTRAINT purchases_status_check,
    ADD CONSTRAINT purchases_status_check CHECK (status IN ('DRAFT', 'POSTED'));

ALTER TABLE settlement_run_purchase_snapshots
    DROP CONSTRAINT settlement_run_purchase_snapshots_check;

ALTER TABLE settlement_run_purchase_snapshots
    RENAME COLUMN expected_revision TO purchase_revision;

ALTER TABLE settlement_run_purchase_snapshots
    DROP COLUMN revision;
