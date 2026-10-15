CREATE TABLE source_documents (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    sha256 text NOT NULL CHECK (length(sha256) = 64),
    object_key text NOT NULL UNIQUE,
    mime_type text NOT NULL CHECK (length(mime_type) BETWEEN 1 AND 255),
    source_kind text NOT NULL CHECK (source_kind IN ('receipt', 'order', 'email', 'card', 'bank')),
    original_filename text NOT NULL DEFAULT '',
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, sha256),
    FOREIGN KEY (household_id, created_by) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX source_documents_household_created_idx ON source_documents(household_id, created_at DESC, id DESC);

CREATE TABLE source_document_attachments (
    household_id uuid NOT NULL,
    parent_document_id uuid NOT NULL,
    child_document_id uuid NOT NULL,
    attachment_index integer NOT NULL CHECK (attachment_index >= 0),
    filename text NOT NULL,
    mime_type text NOT NULL CHECK (mime_type IN ('application/pdf', 'image/jpeg', 'image/png', 'image/webp')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (parent_document_id, attachment_index),
    UNIQUE (parent_document_id, child_document_id),
    CHECK (parent_document_id <> child_document_id),
    FOREIGN KEY (household_id, parent_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, child_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE
);

CREATE INDEX source_document_attachments_child_idx ON source_document_attachments(household_id, child_document_id);

CREATE TABLE mailbox_cursors (
    mailbox text PRIMARY KEY CHECK (length(mailbox) BETWEEN 1 AND 255),
    uid_validity bigint NOT NULL DEFAULT 0 CHECK (uid_validity BETWEEN 0 AND 4294967295),
    uid bigint NOT NULL DEFAULT 0 CHECK (uid BETWEEN 0 AND 4294967295),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO mailbox_cursors (mailbox) VALUES ('forwarding');

CREATE TABLE extraction_jobs (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    source_document_id uuid NOT NULL,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    provider text NOT NULL DEFAULT 'gemini_developer_api' CHECK (length(provider) BETWEEN 1 AND 128),
    model text NOT NULL DEFAULT '' CHECK (length(model) <= 256),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    request jsonb NOT NULL DEFAULT '{}'::jsonb,
    result jsonb,
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz,
    lease_token uuid,
    lease_expires_at timestamptz,
    error_code text,
    error_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, source_document_id, id),
    UNIQUE (household_id, source_document_id, version),
    UNIQUE (household_id, idempotency_key),
    FOREIGN KEY (household_id, source_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE,
    CHECK (
        (status = 'running' AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL AND finished_at IS NULL)
        OR (status <> 'running' AND lease_token IS NULL AND lease_expires_at IS NULL)
    ),
    CHECK (
        (status IN ('succeeded', 'failed', 'cancelled') AND finished_at IS NOT NULL)
        OR (status IN ('queued', 'running') AND finished_at IS NULL)
    ),
    CHECK ((status = 'succeeded') = (result IS NOT NULL)),
    CHECK (attempt_count <= max_attempts)
);

CREATE INDEX extraction_jobs_ready_idx ON extraction_jobs(available_at, created_at) WHERE status = 'queued';
CREATE INDEX extraction_jobs_expired_lease_idx ON extraction_jobs(lease_expires_at) WHERE status = 'running';

CREATE TABLE extraction_job_attempts (
    household_id uuid NOT NULL,
    source_document_id uuid NOT NULL,
    job_id uuid NOT NULL,
    attempt_no integer NOT NULL CHECK (attempt_no > 0),
    provider text NOT NULL CHECK (length(provider) BETWEEN 1 AND 128),
    model text NOT NULL DEFAULT '' CHECK (length(model) <= 256),
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    error_code text,
    error_message text,
    response_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (job_id, attempt_no),
    FOREIGN KEY (household_id, source_document_id, job_id) REFERENCES extraction_jobs(household_id, source_document_id, id) ON DELETE CASCADE,
    CHECK ((status = 'running' AND finished_at IS NULL) OR (status <> 'running' AND finished_at IS NOT NULL))
);

CREATE TABLE source_observations (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    source_document_id uuid NOT NULL,
    extraction_job_id uuid NOT NULL,
    source_reference text NOT NULL DEFAULT '' CHECK (length(source_reference) <= 256),
    source_hash text NOT NULL CHECK (length(source_hash) = 64),
    source_kind text NOT NULL CHECK (source_kind IN ('receipt', 'order', 'email', 'card', 'bank')),
    transaction_kind text NOT NULL CHECK (length(transaction_kind) BETWEEN 1 AND 128),
    external_order_id text NOT NULL DEFAULT '',
    external_transaction_id text NOT NULL DEFAULT '',
    merchant text NOT NULL DEFAULT '',
    occurred_at timestamptz,
    amount_yen bigint,
    card_last4 text NOT NULL DEFAULT '' CHECK (card_last4 = '' OR card_last4 ~ '^[0-9]{4}$'),
    currency_code text NOT NULL DEFAULT 'JPY' CHECK (currency_code ~ '^[A-Z]{3}$'),
    raw_observation jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, source_document_id, id),
    UNIQUE (household_id, source_document_id, source_reference, extraction_job_id),
    UNIQUE (household_id, source_document_id, id, source_reference),
    FOREIGN KEY (household_id, source_document_id, extraction_job_id) REFERENCES extraction_jobs(household_id, source_document_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, source_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE
);

CREATE INDEX source_observations_household_idx ON source_observations(household_id, created_at DESC);
CREATE INDEX source_observations_transaction_idx ON source_observations(household_id, external_transaction_id) WHERE external_transaction_id <> '';
CREATE INDEX source_observations_order_idx ON source_observations(household_id, external_order_id) WHERE external_order_id <> '';
CREATE INDEX source_observations_hash_idx ON source_observations(household_id, source_hash);
CREATE INDEX source_observations_amount_occurred_idx ON source_observations(household_id, amount_yen, occurred_at);

CREATE TABLE evidence_links (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    source_document_id uuid NOT NULL,
    observation_id uuid,
    source_reference text NOT NULL DEFAULT '' CHECK (length(source_reference) <= 256),
    entity_kind text NOT NULL CHECK (length(entity_kind) BETWEEN 1 AND 64),
    entity_id uuid NOT NULL,
    item_id uuid,
    evidence_region jsonb,
    observation jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (household_id, source_document_id, source_reference, entity_kind, entity_id, item_id),
    FOREIGN KEY (household_id, source_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, source_document_id, observation_id, source_reference) REFERENCES source_observations(household_id, source_document_id, id, source_reference) ON DELETE CASCADE,
    FOREIGN KEY (household_id, created_by) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX evidence_links_entity_idx ON evidence_links(household_id, entity_kind, entity_id);
CREATE INDEX evidence_links_document_idx ON evidence_links(household_id, source_document_id, source_reference);

CREATE TABLE source_confirmations (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    source_document_id uuid NOT NULL,
    observation_id uuid,
    source_reference text NOT NULL DEFAULT '' CHECK (length(source_reference) <= 256),
    entity_kind text NOT NULL CHECK (length(entity_kind) BETWEEN 1 AND 64),
    entity_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('confirmed', 'revoked')),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    changed_by uuid NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, idempotency_key),
    FOREIGN KEY (household_id, source_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, source_document_id, observation_id, source_reference) REFERENCES source_observations(household_id, source_document_id, id, source_reference) ON DELETE CASCADE,
    FOREIGN KEY (household_id, changed_by) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX source_confirmations_source_idx ON source_confirmations(household_id, source_document_id, source_reference, changed_at DESC);
CREATE INDEX source_confirmations_entity_idx ON source_confirmations(household_id, entity_kind, entity_id, changed_at DESC);

CREATE TABLE bank_statement_imports (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    source_document_id uuid NOT NULL,
    account_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'processing', 'ready', 'failed', 'cancelled')),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    content_hash text NOT NULL CHECK (length(content_hash) = 64),
    statement_start date,
    statement_end date,
    row_count integer NOT NULL DEFAULT 0 CHECK (row_count >= 0),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    error_code text,
    error_message text,
    available_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz,
    lease_token uuid,
    lease_expires_at timestamptz,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, source_document_id, id),
    UNIQUE (household_id, idempotency_key),
    UNIQUE (household_id, account_id, content_hash),
    FOREIGN KEY (household_id, source_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, account_id) REFERENCES accounts(household_id, id),
    FOREIGN KEY (household_id, created_by) REFERENCES household_members(household_id, member_id),
    CHECK (statement_end IS NULL OR statement_start IS NULL OR statement_end >= statement_start),
    CHECK (
        (status = 'processing' AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL AND finished_at IS NULL)
        OR (status <> 'processing' AND lease_token IS NULL AND lease_expires_at IS NULL)
    ),
    CHECK (
        (status IN ('ready', 'failed', 'cancelled') AND finished_at IS NOT NULL)
        OR (status IN ('queued', 'processing') AND finished_at IS NULL)
    ),
    CHECK (attempt_count <= max_attempts)
);

CREATE INDEX bank_statement_imports_ready_idx ON bank_statement_imports(available_at, created_at) WHERE status = 'queued';
CREATE INDEX bank_statement_imports_expired_lease_idx ON bank_statement_imports(lease_expires_at) WHERE status = 'processing';

CREATE TABLE bank_statement_import_attempts (
    household_id uuid NOT NULL,
    import_id uuid NOT NULL,
    attempt_no integer NOT NULL CHECK (attempt_no > 0),
    status text NOT NULL CHECK (status IN ('processing', 'succeeded', 'failed', 'cancelled')),
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    error_code text,
    error_message text,
    PRIMARY KEY (import_id, attempt_no),
    FOREIGN KEY (household_id, import_id) REFERENCES bank_statement_imports(household_id, id) ON DELETE CASCADE,
    CHECK ((status = 'processing' AND finished_at IS NULL) OR (status <> 'processing' AND finished_at IS NOT NULL))
);

CREATE TABLE bank_statement_rows (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    source_document_id uuid NOT NULL,
    import_id uuid NOT NULL,
    observation_id uuid,
    row_number integer NOT NULL CHECK (row_number > 0),
    source_reference text NOT NULL DEFAULT '' CHECK (length(source_reference) <= 256),
    external_transaction_id text NOT NULL DEFAULT '',
    transaction_kind text NOT NULL CHECK (length(transaction_kind) BETWEEN 1 AND 128),
    occurred_at timestamptz,
    value_date date,
    amount_yen bigint NOT NULL,
    balance_after_yen bigint,
    currency_code text NOT NULL DEFAULT 'JPY' CHECK (currency_code ~ '^[A-Z]{3}$'),
    description text NOT NULL DEFAULT '',
    merchant text NOT NULL DEFAULT '',
    raw_row jsonb NOT NULL DEFAULT '{}'::jsonb,
    row_hash text NOT NULL CHECK (length(row_hash) = 64),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (import_id, row_number),
    UNIQUE (household_id, import_id, id),
    FOREIGN KEY (household_id, source_document_id, import_id) REFERENCES bank_statement_imports(household_id, source_document_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, source_document_id, observation_id, source_reference) REFERENCES source_observations(household_id, source_document_id, id, source_reference)
);

CREATE INDEX bank_statement_rows_household_occurred_idx ON bank_statement_rows(household_id, occurred_at, id);
CREATE INDEX bank_statement_rows_external_transaction_idx ON bank_statement_rows(household_id, external_transaction_id) WHERE external_transaction_id <> '';
CREATE INDEX bank_statement_rows_hash_idx ON bank_statement_rows(household_id, row_hash);

CREATE TABLE statement_reconciliation_candidates (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL,
    import_id uuid NOT NULL,
    statement_row_id uuid NOT NULL,
    entity_kind text NOT NULL CHECK (length(entity_kind) BETWEEN 1 AND 64),
    entity_id uuid NOT NULL,
    score numeric(6, 5) NOT NULL CHECK (score BETWEEN 0 AND 1),
    rationale jsonb NOT NULL DEFAULT '{}'::jsonb,
    decision text NOT NULL DEFAULT 'pending' CHECK (decision IN ('pending', 'confirmed', 'rejected', 'superseded')),
    rank integer NOT NULL CHECK (rank > 0),
    decided_by uuid,
    decided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (statement_row_id, entity_kind, entity_id),
    UNIQUE (statement_row_id, rank),
    FOREIGN KEY (household_id, import_id, statement_row_id) REFERENCES bank_statement_rows(household_id, import_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, decided_by) REFERENCES household_members(household_id, member_id),
    CHECK (
        (decision = 'pending' AND decided_by IS NULL AND decided_at IS NULL)
        OR (decision <> 'pending' AND decided_by IS NOT NULL AND decided_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX statement_reconciliation_one_confirmation_idx ON statement_reconciliation_candidates(statement_row_id) WHERE decision = 'confirmed';
CREATE INDEX statement_reconciliation_candidates_rank_idx ON statement_reconciliation_candidates(household_id, statement_row_id, rank);
