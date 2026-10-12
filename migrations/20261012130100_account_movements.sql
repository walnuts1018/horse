CREATE TABLE account_movements (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    journal_id uuid NOT NULL UNIQUE REFERENCES journals(id),
    kind text NOT NULL CHECK (kind IN ('TRANSFER', 'CARD_BILL_PAYMENT')),
    occurred_at timestamptz NOT NULL,
    from_account_id uuid NOT NULL,
    to_account_id uuid NOT NULL,
    amount_yen bigint NOT NULL CHECK (amount_yen > 0),
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    CHECK (from_account_id <> to_account_id)
);

CREATE FUNCTION validate_account_movement_accounts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE household_id = NEW.household_id AND id = NEW.from_account_id)
       OR NOT EXISTS (SELECT 1 FROM accounts WHERE household_id = NEW.household_id AND id = NEW.to_account_id) THEN
        RAISE EXCEPTION 'account movement references an account outside its household' USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER account_movements_accounts_valid
BEFORE INSERT OR UPDATE OF household_id, from_account_id, to_account_id ON account_movements
FOR EACH ROW EXECUTE FUNCTION validate_account_movement_accounts();

CREATE FUNCTION prevent_account_movement_account_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM account_movements WHERE from_account_id = OLD.id OR to_account_id = OLD.id) THEN
        RAISE EXCEPTION 'account is referenced by an account movement' USING ERRCODE = '23503';
    END IF;
    RETURN OLD;
END;
$$;

CREATE TRIGGER accounts_movement_reference_guard
BEFORE DELETE ON accounts
FOR EACH ROW EXECUTE FUNCTION prevent_account_movement_account_delete();

CREATE INDEX account_movements_household_occurred_idx ON account_movements(household_id, occurred_at DESC, id DESC);
