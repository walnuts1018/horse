ALTER TABLE purchases
    ADD COLUMN date_precision text NOT NULL DEFAULT 'DATE_TIME'
    CHECK (date_precision IN ('DATE_TIME', 'DATE_ONLY'));
