ALTER TABLE mailbox_cursors
    ADD COLUMN lease_token uuid,
    ADD COLUMN lease_expires_at timestamptz,
    ADD CONSTRAINT mailbox_cursors_lease_check CHECK (
        (lease_token IS NULL AND lease_expires_at IS NULL)
        OR (lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)
    );
