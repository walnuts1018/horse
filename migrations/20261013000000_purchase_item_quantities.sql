ALTER TABLE purchase_items
    ADD COLUMN quantity_numerator bigint,
    ADD COLUMN quantity_denominator bigint,
    ADD COLUMN quantity_unit text,
    ADD COLUMN printed_unit_price_yen bigint,
    ADD COLUMN printed_unit_price_quantity_numerator bigint,
    ADD COLUMN printed_unit_price_quantity_denominator bigint,
    ADD COLUMN printed_unit_price_unit text,
    ADD CONSTRAINT purchase_items_quantity_presence_check CHECK (
        (quantity_numerator IS NULL AND quantity_denominator IS NULL AND quantity_unit IS NULL)
        OR (quantity_numerator IS NOT NULL AND quantity_numerator > 0 AND quantity_numerator <= 1000000000000
            AND quantity_denominator IS NOT NULL AND quantity_denominator > 0 AND quantity_denominator <= 1000000
            AND quantity_unit IS NOT NULL AND length(quantity_unit) BETWEEN 1 AND 32)
    ),
    ADD CONSTRAINT purchase_items_printed_unit_price_presence_check CHECK (
        (printed_unit_price_yen IS NULL AND printed_unit_price_quantity_numerator IS NULL
            AND printed_unit_price_quantity_denominator IS NULL AND printed_unit_price_unit IS NULL)
        OR (printed_unit_price_yen IS NOT NULL AND printed_unit_price_yen >= 0 AND printed_unit_price_yen <= 1000000000000
            AND printed_unit_price_quantity_numerator IS NOT NULL AND printed_unit_price_quantity_numerator > 0 AND printed_unit_price_quantity_numerator <= 1000000000000
            AND printed_unit_price_quantity_denominator IS NOT NULL AND printed_unit_price_quantity_denominator > 0 AND printed_unit_price_quantity_denominator <= 1000000
            AND printed_unit_price_unit IS NOT NULL AND length(printed_unit_price_unit) BETWEEN 1 AND 32)
    );
