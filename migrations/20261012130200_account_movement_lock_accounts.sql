CREATE OR REPLACE FUNCTION validate_account_movement_accounts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM accounts WHERE household_id = NEW.household_id AND id = NEW.from_account_id FOR KEY SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'account movement references an account outside its household' USING ERRCODE = '23503';
    END IF;
    PERFORM 1 FROM accounts WHERE household_id = NEW.household_id AND id = NEW.to_account_id FOR KEY SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'account movement references an account outside its household' USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$;
