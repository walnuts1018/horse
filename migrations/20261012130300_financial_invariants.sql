CREATE FUNCTION check_item_allocation_total() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_item_id uuid;
    item_amount bigint;
    allocation_total numeric;
BEGIN
    IF TG_TABLE_NAME = 'purchase_items' THEN
        target_item_id := COALESCE(NEW.id, OLD.id);
    ELSE
        target_item_id := COALESCE(NEW.item_id, OLD.item_id);
    END IF;
    SELECT amount_yen INTO item_amount FROM purchase_items WHERE id = target_item_id;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;
    SELECT COALESCE(sum(amount_yen), 0) INTO allocation_total FROM item_allocations WHERE item_id = target_item_id;
    IF allocation_total <> item_amount THEN
        RAISE EXCEPTION 'item % allocations do not match item amount', target_item_id USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER purchase_item_allocation_total
AFTER INSERT OR UPDATE OR DELETE ON purchase_items
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_item_allocation_total();

CREATE CONSTRAINT TRIGGER item_allocation_total
AFTER INSERT OR UPDATE OR DELETE ON item_allocations
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_item_allocation_total();

CREATE FUNCTION check_purchase_payment_total() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_purchase_id uuid;
    item_total numeric;
    payment_total numeric;
    payment_count bigint;
BEGIN
    IF TG_TABLE_NAME = 'purchases' THEN
        target_purchase_id := COALESCE(NEW.id, OLD.id);
    ELSIF TG_TABLE_NAME = 'purchase_items' THEN
        target_purchase_id := COALESCE(NEW.purchase_id, OLD.purchase_id);
    ELSE
        target_purchase_id := COALESCE(NEW.purchase_id, OLD.purchase_id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM purchases WHERE id = target_purchase_id) THEN
        RETURN NULL;
    END IF;
    SELECT COALESCE(sum(amount_yen), 0) INTO item_total FROM purchase_items WHERE purchase_id = target_purchase_id;
    SELECT count(*), COALESCE(sum(amount_yen), 0) INTO payment_count, payment_total FROM purchase_payments WHERE purchase_id = target_purchase_id;
    IF payment_count > 0 AND payment_total <> item_total THEN
        RAISE EXCEPTION 'purchase % payments do not match item total', target_purchase_id USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER purchase_item_payment_total
AFTER INSERT OR UPDATE OR DELETE ON purchase_items
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_purchase_payment_total();

CREATE CONSTRAINT TRIGGER purchase_payment_total
AFTER INSERT OR UPDATE OR DELETE ON purchase_payments
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_purchase_payment_total();

CREATE CONSTRAINT TRIGGER purchase_total_payment_total
AFTER INSERT OR UPDATE OR DELETE ON purchases
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_purchase_payment_total();
