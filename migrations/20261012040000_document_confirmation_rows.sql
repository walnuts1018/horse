ALTER TABLE document_confirmations DROP CONSTRAINT document_confirmations_pkey;
ALTER TABLE document_confirmations ADD COLUMN source_reference text NOT NULL DEFAULT '';
ALTER TABLE document_confirmations ADD CONSTRAINT document_confirmations_pkey PRIMARY KEY (source_document_id, source_reference);
ALTER TABLE document_confirmations ADD CONSTRAINT document_confirmations_source_reference_length CHECK (length(source_reference) <= 256);
