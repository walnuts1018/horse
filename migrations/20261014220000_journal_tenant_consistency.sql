ALTER TABLE journals
    ADD CONSTRAINT journals_household_id_id_key UNIQUE (household_id, id);

ALTER TABLE journal_entries
    ADD COLUMN household_id uuid;

UPDATE journal_entries AS entries
SET household_id = journals.household_id
FROM journals
WHERE journals.id = entries.journal_id;

SET CONSTRAINTS ALL IMMEDIATE;

ALTER TABLE journal_entries
    ALTER COLUMN household_id SET NOT NULL;

CREATE FUNCTION set_journal_entry_household_id() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    SELECT household_id
    INTO NEW.household_id
    FROM journals
    WHERE id = NEW.journal_id;

    RETURN NEW;
END;
$$;

CREATE TRIGGER journal_entries_set_household_id
BEFORE INSERT OR UPDATE OF journal_id ON journal_entries
FOR EACH ROW
EXECUTE FUNCTION set_journal_entry_household_id();

ALTER TABLE journal_entries
    DROP CONSTRAINT journal_entries_journal_id_fkey,
    ADD CONSTRAINT journal_entries_household_journal_fkey
        FOREIGN KEY (household_id, journal_id)
        REFERENCES journals (household_id, id) ON DELETE CASCADE,
    ADD CONSTRAINT journal_entries_household_account_fkey
        FOREIGN KEY (household_id, account_id)
        REFERENCES accounts (household_id, id),
    ADD CONSTRAINT journal_entries_household_pool_fkey
        FOREIGN KEY (household_id, pool_id)
        REFERENCES pools (household_id, id),
    ADD CONSTRAINT journal_entries_household_member_fkey
        FOREIGN KEY (household_id, member_id)
        REFERENCES household_members (household_id, member_id);

ALTER TABLE ledger_entries
    DROP CONSTRAINT ledger_entries_journal_id_fkey,
    ADD CONSTRAINT ledger_entries_household_journal_fkey
        FOREIGN KEY (household_id, journal_id)
        REFERENCES journals (household_id, id);
