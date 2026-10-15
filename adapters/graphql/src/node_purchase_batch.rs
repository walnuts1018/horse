use async_graphql::{Context, Result};
use diesel::{BoolExpressionMethods, ExpressionMethods, JoinOnDsl, QueryDsl};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{PgPool, schema::public};
use std::collections::{HashMap, HashSet};
use uuid::Uuid;

use crate::{ledger_entry, purchase_mutations};

type PurchaseCorrectionRow = (
    Uuid,
    Uuid,
    Uuid,
    Option<Uuid>,
    i64,
    String,
    chrono::NaiveDate,
    chrono::DateTime<chrono::Utc>,
);

pub(crate) async fn load(
    ctx: &Context<'_>,
    household_ids: &[Uuid],
    requested: &[(String, Uuid)],
) -> Result<HashMap<(String, Uuid), ledger_entry::Node>> {
    if household_ids.is_empty() || requested.is_empty() {
        return Ok(HashMap::new());
    }
    let pool = ctx.data::<PgPool>()?;
    let purchase_ids = requested
        .iter()
        .filter(|(kind, _)| kind == "Purchase")
        .map(|(_, id)| *id)
        .collect::<HashSet<_>>();
    let item_ids = requested
        .iter()
        .filter(|(kind, _)| kind == "PurchaseItem")
        .map(|(_, id)| *id)
        .collect::<HashSet<_>>();
    let correction_ids = requested
        .iter()
        .filter(|(kind, _)| kind == "PurchaseCorrection")
        .map(|(_, id)| *id)
        .collect::<HashSet<_>>();
    if purchase_ids.is_empty() && item_ids.is_empty() && correction_ids.is_empty() {
        return Ok(HashMap::new());
    }

    let mut conn = pool.get().await.map_err(crate::internal_error)?;
    let item_parents = if item_ids.is_empty() {
        Vec::new()
    } else {
        public::purchase_items::table
            .filter(public::purchase_items::household_id.eq_any(household_ids))
            .filter(public::purchase_items::id.eq_any(item_ids.iter().copied()))
            .select((
                public::purchase_items::household_id,
                public::purchase_items::purchase_id,
                public::purchase_items::id,
            ))
            .load::<(Uuid, Uuid, Uuid)>(&mut *conn)
            .await
            .map_err(crate::internal_error)?
    };
    let correction_rows = if correction_ids.is_empty() {
        Vec::new()
    } else {
        public::purchase_corrections::table
            .inner_join(
                public::journals::table.on(public::journals::id
                    .eq(public::purchase_corrections::journal_id)
                    .and(
                        public::journals::household_id
                            .eq(public::purchase_corrections::household_id),
                    )),
            )
            .filter(public::purchase_corrections::household_id.eq_any(household_ids))
            .filter(public::purchase_corrections::id.eq_any(correction_ids.iter().copied()))
            .select((
                public::purchase_corrections::household_id,
                public::purchase_corrections::purchase_id,
                public::purchase_corrections::id,
                public::purchase_corrections::item_id,
                public::purchase_corrections::amount_delta_yen,
                public::purchase_corrections::reason,
                public::purchase_corrections::effective_period_start,
                public::journals::occurred_at,
            ))
            .load::<PurchaseCorrectionRow>(&mut *conn)
            .await
            .map_err(crate::internal_error)?
    };
    drop(conn);
    let mut parent_ids = purchase_ids.clone();
    for (_, purchase_id, _) in &item_parents {
        parent_ids.insert(*purchase_id);
    }
    for (_, purchase_id, ..) in &correction_rows {
        parent_ids.insert(*purchase_id);
    }
    let parent_ids = parent_ids.into_iter().collect::<Vec<_>>();
    let projections = bookkeeping_postgres::purchase_command_result::load_purchase_command_results(
        pool,
        household_ids,
        &parent_ids,
    )
    .await
    .map_err(crate::internal_error)?;
    let mut purchase_nodes = HashMap::new();
    for projection in projections {
        let household_id = projection.household_id;
        let purchase_id = projection.id;
        let purchase = purchase_mutations::PurchaseObject::try_from(projection)
            .map_err(crate::internal_error)?;
        purchase_nodes.insert((household_id, purchase_id), purchase);
    }

    let mut result = HashMap::new();
    for (household_id, purchase_id, item_id) in item_parents {
        if !item_ids.contains(&item_id) {
            continue;
        }
        if let Some(purchase) = purchase_nodes.get(&(household_id, purchase_id))
            && let Some(item) = purchase.items.iter().find(|item| {
                crate::node_query::decode_global_node_id(&item.id, "PurchaseItem").ok()
                    == Some(item_id)
            })
        {
            result.insert(
                ("PurchaseItem".to_owned(), item_id),
                ledger_entry::Node::PurchaseItem(item.clone()),
            );
        }
    }
    for ((_, purchase_id), purchase) in purchase_nodes {
        if purchase_ids.contains(&purchase_id) {
            result.insert(
                ("Purchase".to_owned(), purchase_id),
                ledger_entry::Node::Purchase(purchase),
            );
        }
    }

    let payments = load_correction_payments(pool, household_ids, &correction_rows).await?;
    for (
        household_id,
        _,
        correction_id,
        item_id,
        amount_delta_yen,
        reason,
        settlement_month,
        occurred_at,
    ) in correction_rows
    {
        let payment_allocations = payments.get(&correction_id).cloned().unwrap_or_default();
        let id = crate::node_query::encode_global_node_id("PurchaseCorrection", correction_id);
        let settlement_month =
            crate::policy_mutations::YearMonth::from_naive_date(settlement_month);
        let occurred_at = occurred_at.into();
        let node = match item_id {
            Some(item_id) => ledger_entry::Node::PurchaseItemCorrection(
                purchase_mutations::PurchaseItemCorrection {
                    id,
                    item_id: crate::node_query::encode_global_node_id("PurchaseItem", item_id),
                    amount_delta_yen: crate::Long(amount_delta_yen),
                    reason,
                    occurred_at,
                    settlement_month,
                    payment_allocations,
                },
            ),
            None => ledger_entry::Node::PurchaseWideCorrection(
                purchase_mutations::PurchaseWideCorrection {
                    id,
                    amount_delta_yen: crate::Long(amount_delta_yen),
                    reason,
                    occurred_at,
                    settlement_month,
                    payment_allocations,
                },
            ),
        };
        if correction_ids.contains(&correction_id) && household_ids.contains(&household_id) {
            result.insert(("PurchaseCorrection".to_owned(), correction_id), node);
        }
    }
    Ok(result)
}

async fn load_correction_payments(
    pool: &PgPool,
    household_ids: &[Uuid],
    correction_rows: &[PurchaseCorrectionRow],
) -> Result<HashMap<Uuid, Vec<purchase_mutations::PurchasePayment>>> {
    let correction_ids = correction_rows.iter().map(|row| row.2).collect::<Vec<_>>();
    if correction_ids.is_empty() {
        return Ok(HashMap::new());
    }
    let mut conn = pool.get().await.map_err(crate::internal_error)?;
    let rows = public::purchase_correction_payments::table
        .inner_join(
            public::accounts::table.on(public::accounts::id
                .eq(public::purchase_correction_payments::account_id)
                .and(
                    public::accounts::household_id
                        .eq(public::purchase_correction_payments::household_id),
                )),
        )
        .filter(public::purchase_correction_payments::household_id.eq_any(household_ids))
        .filter(public::purchase_correction_payments::correction_id.eq_any(correction_ids))
        .order_by((
            public::purchase_correction_payments::correction_id,
            public::purchase_correction_payments::line_no,
        ))
        .select((
            public::purchase_correction_payments::correction_id,
            public::accounts::id,
            public::accounts::name,
            public::accounts::kind,
            public::accounts::card_last4,
            public::accounts::pool_id,
            public::purchase_correction_payments::amount_yen,
        ))
        .load::<(
            Uuid,
            Uuid,
            String,
            String,
            Option<String>,
            Option<Uuid>,
            i64,
        )>(&mut *conn)
        .await
        .map_err(crate::internal_error)?;
    let mut result = HashMap::new();
    for (correction_id, account_id, name, kind, card_last4, pool_id, amount_yen) in rows {
        let kind = match kind.as_str() {
            "BANK" => crate::account_mutations::AccountKind::Bank,
            "CASH" => crate::account_mutations::AccountKind::Cash,
            "CARD" => crate::account_mutations::AccountKind::Card,
            "OTHER" => crate::account_mutations::AccountKind::Other,
            value => {
                return Err(crate::internal_error(format!(
                    "invalid account kind {value}"
                )));
            }
        };
        result.entry(correction_id).or_insert_with(Vec::new).push(
            purchase_mutations::PurchasePayment {
                payment_account_id: crate::node_query::encode_global_node_id("Account", account_id),
                account: crate::account_mutations::AccountObject {
                    id: crate::node_query::encode_global_node_id("Account", account_id),
                    name,
                    kind,
                    card_last4,
                    pool_id: pool_id.map(|id| crate::node_query::encode_global_node_id("Pool", id)),
                },
                amount_yen: crate::Long(amount_yen),
            },
        );
    }
    Ok(result)
}
