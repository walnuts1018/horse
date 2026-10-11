CREATE TABLE document_confirmations (
    source_document_id uuid PRIMARY KEY REFERENCES source_documents(id) ON DELETE CASCADE,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    purchase_id uuid NOT NULL,
    created_by uuid NOT NULL REFERENCES members(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id)
);
