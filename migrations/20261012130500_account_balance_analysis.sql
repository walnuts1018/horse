CREATE INDEX journal_entries_account_journal_idx
ON journal_entries(account_id, journal_id)
WHERE account_id IS NOT NULL;
