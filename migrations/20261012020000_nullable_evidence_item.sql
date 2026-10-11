ALTER TABLE evidence_links DROP CONSTRAINT IF EXISTS evidence_links_pkey;
ALTER TABLE evidence_links ADD CONSTRAINT evidence_links_unique UNIQUE NULLS NOT DISTINCT (source_document_id, purchase_id, item_id);
