CREATE TABLE tags (
    id uuid PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    archived boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, name)
);

CREATE TABLE purchase_item_tags (
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    item_id uuid NOT NULL,
    tag_id uuid NOT NULL,
    PRIMARY KEY (item_id, tag_id),
    FOREIGN KEY (household_id, purchase_id, item_id) REFERENCES purchase_items(household_id, purchase_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, tag_id) REFERENCES tags(household_id, id)
);

CREATE INDEX purchase_item_tags_tag_idx ON purchase_item_tags(household_id, tag_id, item_id);
