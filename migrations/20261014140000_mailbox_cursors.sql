CREATE TABLE mailbox_cursors (
    mailbox text PRIMARY KEY CHECK (mailbox = 'forwarding'),
    uid_validity bigint NOT NULL DEFAULT 0 CHECK (uid_validity BETWEEN 0 AND 4294967295),
    uid bigint NOT NULL DEFAULT 0 CHECK (uid BETWEEN 0 AND 4294967295),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO mailbox_cursors (mailbox) VALUES ('forwarding');
