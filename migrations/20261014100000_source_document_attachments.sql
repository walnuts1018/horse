ALTER TABLE source_documents
    ADD CONSTRAINT source_documents_household_id_id_key UNIQUE (household_id, id);

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
    CONSTRAINT source_document_attachments_parent_household_fkey FOREIGN KEY (household_id, parent_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE,
    CONSTRAINT source_document_attachments_child_household_fkey FOREIGN KEY (household_id, child_document_id) REFERENCES source_documents(household_id, id) ON DELETE CASCADE
);

CREATE INDEX source_document_attachments_child_idx ON source_document_attachments(child_document_id);
