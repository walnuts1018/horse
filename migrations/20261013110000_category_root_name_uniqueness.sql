DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM categories
        WHERE parent_id IS NULL
        GROUP BY household_id, name
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot enforce unique top-level category names while duplicates exist';
    END IF;
END;
$$;

ALTER TABLE categories DROP CONSTRAINT categories_household_id_parent_id_name_key;
ALTER TABLE categories ADD CONSTRAINT categories_household_id_parent_id_name_key UNIQUE NULLS NOT DISTINCT (household_id, parent_id, name);
