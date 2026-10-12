ALTER TABLE purchase_member_burdens
    ADD COLUMN snapshot_source text NOT NULL DEFAULT 'POSTED' CHECK (snapshot_source IN ('POSTED', 'LEGACY_CURRENT_RULES'));

CREATE TABLE purchase_payment_funding_snapshots (
    household_id uuid NOT NULL,
    purchase_id uuid NOT NULL,
    payment_line_no integer NOT NULL,
    pool_id uuid NOT NULL,
    member_id uuid NOT NULL,
    funding_rule_version integer NOT NULL CHECK (funding_rule_version > 0),
    funding_weight bigint NOT NULL CHECK (funding_weight > 0),
    settlement_policy text CHECK (settlement_policy IN ('BALANCE_TO_STANDARD', 'REIMBURSE_PERSONAL')),
    snapshot_source text NOT NULL CHECK (snapshot_source IN ('POSTED', 'LEGACY_CURRENT_RULES')),
    PRIMARY KEY (purchase_id, payment_line_no, member_id),
    FOREIGN KEY (household_id, purchase_id) REFERENCES purchases(household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (purchase_id, payment_line_no) REFERENCES purchase_payments(purchase_id, line_no) ON DELETE CASCADE,
    FOREIGN KEY (household_id, pool_id, funding_rule_version) REFERENCES pool_funding_rules(household_id, pool_id, version),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members(household_id, member_id)
);

CREATE INDEX purchase_payment_funding_snapshots_household_purchase_idx ON purchase_payment_funding_snapshots(household_id, purchase_id);

INSERT INTO purchase_payments(household_id, purchase_id, line_no, account_id, amount_yen)
SELECT p.household_id, p.id, 1, p.payment_account_id, sum(i.amount_yen)
FROM purchases p
JOIN purchase_items i ON i.household_id = p.household_id AND i.purchase_id = p.id
WHERE p.status IN ('POSTED', 'CLOSED')
  AND p.payment_account_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM purchase_payments pp
      WHERE pp.household_id = p.household_id AND pp.purchase_id = p.id
  )
GROUP BY p.household_id, p.id, p.payment_account_id
HAVING sum(i.amount_yen) > 0;

WITH allocation_members AS (
    SELECT a.household_id, a.purchase_id, a.item_id, a.pool_id, a.amount_yen, p.funding_rule_version, r.owner_member_id AS member_id, 1::bigint AS funding_weight
    FROM item_allocations a
    JOIN purchases purchase ON purchase.household_id = a.household_id AND purchase.id = a.purchase_id AND purchase.status IN ('POSTED', 'CLOSED')
    JOIN pools p ON p.household_id = a.household_id AND p.id = a.pool_id
    JOIN pool_funding_rules r ON r.household_id = a.household_id AND r.pool_id = a.pool_id AND r.version = p.funding_rule_version
    WHERE r.owner_member_id IS NOT NULL
      AND NOT EXISTS (
          SELECT 1
          FROM purchase_member_burdens b
          WHERE b.household_id = a.household_id AND b.purchase_id = a.purchase_id AND b.item_id = a.item_id AND b.pool_id = a.pool_id
      )
    UNION ALL
    SELECT a.household_id, a.purchase_id, a.item_id, a.pool_id, a.amount_yen, p.funding_rule_version, s.member_id, s.weight AS funding_weight
    FROM item_allocations a
    JOIN purchases purchase ON purchase.household_id = a.household_id AND purchase.id = a.purchase_id AND purchase.status IN ('POSTED', 'CLOSED')
    JOIN pools p ON p.household_id = a.household_id AND p.id = a.pool_id
    JOIN pool_funding_rules r ON r.household_id = a.household_id AND r.pool_id = a.pool_id AND r.version = p.funding_rule_version AND r.owner_member_id IS NULL
    JOIN pool_funding_shares s ON s.household_id = r.household_id AND s.pool_id = r.pool_id AND s.version = r.version
    WHERE NOT EXISTS (
        SELECT 1
        FROM purchase_member_burdens b
        WHERE b.household_id = a.household_id AND b.purchase_id = a.purchase_id AND b.item_id = a.item_id AND b.pool_id = a.pool_id
    )
), weighted AS (
    SELECT *, amount_yen::numeric * funding_weight AS weighted_amount,
        sum(funding_weight) OVER (PARTITION BY item_id, pool_id) AS total_weight
    FROM allocation_members
), base_amounts AS (
    SELECT *, floor(weighted_amount / total_weight)::bigint AS base_amount,
        mod(weighted_amount, total_weight) AS remainder
    FROM weighted
), ranked AS (
    SELECT *,
        sum(base_amount) OVER (PARTITION BY item_id, pool_id) AS allocated_amount,
        row_number() OVER (PARTITION BY item_id, pool_id ORDER BY remainder DESC, member_id) AS remainder_rank
    FROM base_amounts
)
INSERT INTO purchase_member_burdens(household_id, purchase_id, item_id, pool_id, member_id, amount_yen, funding_rule_version, funding_weight, snapshot_source)
SELECT household_id, purchase_id, item_id, pool_id, member_id,
    base_amount + CASE WHEN remainder_rank <= amount_yen - allocated_amount THEN 1 ELSE 0 END,
    funding_rule_version, funding_weight, 'LEGACY_CURRENT_RULES'
FROM ranked;

INSERT INTO purchase_payment_funding_snapshots(household_id, purchase_id, payment_line_no, pool_id, member_id, funding_rule_version, funding_weight, settlement_policy, snapshot_source)
SELECT payment.household_id, payment.purchase_id, payment.line_no, account.pool_id, rule.owner_member_id, rule.version, 1, rule.settlement_policy, 'LEGACY_CURRENT_RULES'
FROM purchase_payments payment
JOIN purchases purchase ON purchase.household_id = payment.household_id AND purchase.id = payment.purchase_id AND purchase.status IN ('POSTED', 'CLOSED')
JOIN accounts account ON account.household_id = payment.household_id AND account.id = payment.account_id
JOIN pools pool ON pool.household_id = payment.household_id AND pool.id = account.pool_id
JOIN pool_funding_rules rule ON rule.household_id = pool.household_id AND rule.pool_id = pool.id AND rule.version = pool.funding_rule_version
WHERE rule.owner_member_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM purchase_payment_funding_snapshots snapshot
      WHERE snapshot.purchase_id = payment.purchase_id AND snapshot.payment_line_no = payment.line_no
  )
UNION ALL
SELECT payment.household_id, payment.purchase_id, payment.line_no, account.pool_id, share.member_id, rule.version, share.weight, rule.settlement_policy, 'LEGACY_CURRENT_RULES'
FROM purchase_payments payment
JOIN purchases purchase ON purchase.household_id = payment.household_id AND purchase.id = payment.purchase_id AND purchase.status IN ('POSTED', 'CLOSED')
JOIN accounts account ON account.household_id = payment.household_id AND account.id = payment.account_id
JOIN pools pool ON pool.household_id = payment.household_id AND pool.id = account.pool_id
JOIN pool_funding_rules rule ON rule.household_id = pool.household_id AND rule.pool_id = pool.id AND rule.version = pool.funding_rule_version AND rule.owner_member_id IS NULL
JOIN pool_funding_shares share ON share.household_id = rule.household_id AND share.pool_id = rule.pool_id AND share.version = rule.version
WHERE NOT EXISTS (
    SELECT 1
    FROM purchase_payment_funding_snapshots snapshot
    WHERE snapshot.purchase_id = payment.purchase_id AND snapshot.payment_line_no = payment.line_no
);
