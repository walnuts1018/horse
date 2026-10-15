ALTER TABLE household_events
    ADD COLUMN stream_cursor bigint;

CREATE TABLE household_event_stream_cursors (
    household_id uuid PRIMARY KEY REFERENCES households(id) ON DELETE CASCADE,
    cursor bigint NOT NULL CHECK (cursor >= 0)
);

WITH ranked AS (
    SELECT cursor, household_id,
        row_number() OVER (PARTITION BY household_id ORDER BY cursor) AS stream_cursor
    FROM household_events
)
UPDATE household_events AS event
SET stream_cursor = ranked.stream_cursor
FROM ranked
WHERE event.cursor = ranked.cursor;

INSERT INTO household_event_stream_cursors (household_id, cursor)
SELECT household_id, max(stream_cursor)
FROM household_events
GROUP BY household_id;

UPDATE household_events
SET stream_cursor = 0
WHERE stream_cursor IS NULL;

ALTER TABLE household_events
    ALTER COLUMN stream_cursor SET NOT NULL,
    ADD CONSTRAINT household_events_household_stream_cursor_key UNIQUE (household_id, stream_cursor);

CREATE FUNCTION assign_household_event_stream_cursor() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO household_event_stream_cursors (household_id, cursor)
    VALUES (NEW.household_id, 1)
    ON CONFLICT (household_id)
    DO UPDATE SET cursor = household_event_stream_cursors.cursor + 1
    RETURNING cursor INTO NEW.stream_cursor;
    RETURN NEW;
END;
$$;

CREATE TRIGGER household_events_assign_stream_cursor
BEFORE INSERT ON household_events
FOR EACH ROW
EXECUTE FUNCTION assign_household_event_stream_cursor();

WITH corrections AS (
    SELECT event.cursor,
        greatest(purchase.revision, coalesce(event_max.revision, 0))
            + row_number() OVER (
                PARTITION BY event.household_id, event.entity_id
                ORDER BY event.cursor
            ) AS revision
    FROM household_events AS event
    JOIN purchases AS purchase
        ON purchase.household_id = event.household_id
        AND purchase.id = event.entity_id
    LEFT JOIN LATERAL (
        SELECT max(other.revision) AS revision
        FROM household_events AS other
        WHERE other.household_id = event.household_id
            AND other.entity_kind = 'purchase'
            AND other.entity_id = event.entity_id
            AND other.event_type <> 'purchase.corrected'
    ) AS event_max ON true
    WHERE event.entity_kind = 'purchase'
        AND event.event_type = 'purchase.corrected'
)
UPDATE household_events AS event
SET revision = corrections.revision
FROM corrections
WHERE event.cursor = corrections.cursor;

UPDATE purchases AS purchase
SET revision = greatest(purchase.revision, corrected.max_revision)
FROM (
    SELECT household_id, entity_id, max(revision) AS max_revision
    FROM household_events
    WHERE entity_kind = 'purchase'
        AND event_type = 'purchase.corrected'
    GROUP BY household_id, entity_id
) AS corrected
WHERE purchase.household_id = corrected.household_id
    AND purchase.id = corrected.entity_id;
