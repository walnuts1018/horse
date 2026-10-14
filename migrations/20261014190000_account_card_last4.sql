ALTER TABLE accounts ADD COLUMN card_last4 text;

ALTER TABLE accounts ADD CONSTRAINT accounts_card_last4_check CHECK (
    card_last4 IS NULL OR (kind = 'CARD' AND card_last4 ~ '^[0-9]{4}$')
);
