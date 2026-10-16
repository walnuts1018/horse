use async_graphql::{Context, Error, Result};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, NullableExpressionMethods, QueryDsl,
};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{PgPool, schema::public};
use std::collections::HashMap;
use uuid::Uuid;

use crate::ledger_entry::{self, Node};

/// Loads requested ledger-entry nodes after fetching their ledger rows in one query.
pub(crate) async fn load(
    ctx: &Context<'_>,
    household_ids: &[Uuid],
    ids: &[Uuid],
) -> Result<HashMap<Uuid, Node>> {
    if household_ids.is_empty() || ids.is_empty() {
        return Ok(HashMap::new());
    }

    use public::{ledger_entries as ledgers, purchase_corrections as corrections};
    let pool = ctx.data::<PgPool>()?;
    let mut conn = pool.get().await.map_err(crate::internal_error)?;
    let rows = ledgers::table
        .left_join(
            corrections::table.on(corrections::id
                .eq(ledgers::source_id)
                .and(corrections::household_id.eq(ledgers::household_id))
                .and(ledgers::source_kind.eq_any(["CORRECTION", "correction"]))),
        )
        .filter(ledgers::household_id.eq_any(household_ids))
        .filter(ledgers::id.eq_any(ids))
        .select((
            ledgers::id,
            ledgers::household_id,
            ledgers::source_kind,
            ledgers::source_id,
            corrections::purchase_id.nullable(),
            ledgers::occurred_at,
            ledgers::description,
            ledgers::amount_yen,
            ledgers::created_at,
        ))
        .load::<(
            Uuid,
            Uuid,
            String,
            Uuid,
            Option<Uuid>,
            chrono::DateTime<chrono::Utc>,
            String,
            i64,
            chrono::DateTime<chrono::Utc>,
        )>(&mut conn)
        .await
        .map_err(crate::internal_error)?;
    drop(conn);

    let purchase_ids = rows
        .iter()
        .filter_map(
            |(_, _, source_kind, source_id, correction_purchase_id, ..)| match source_kind.as_str()
            {
                "PURCHASE" | "purchase" => Some(*source_id),
                "CORRECTION" | "correction" => *correction_purchase_id,
                _ => None,
            },
        )
        .collect::<std::collections::HashSet<_>>();
    let purchase_requests = purchase_ids
        .into_iter()
        .map(|id| ("Purchase".to_owned(), id))
        .collect::<Vec<_>>();
    let purchase_nodes =
        crate::node_purchase_batch::load(ctx, household_ids, &purchase_requests).await?;
    let funding_ids = rows
        .iter()
        .filter(|row| matches!(row.2.as_str(), "FUNDING" | "funding"))
        .map(|row| row.0)
        .collect::<Vec<_>>();
    let funding_by_id = if funding_ids.is_empty() {
        HashMap::new()
    } else {
        cross_context_postgres::funding_command_result::load_funding_ledger_entry_results(
            pool,
            household_ids,
            &funding_ids,
        )
        .await
        .map_err(crate::internal_error)?
    };
    let transfer_ids = rows
        .iter()
        .filter(|row| matches!(row.2.as_str(), "TRANSFER" | "transfer"))
        .map(|row| row.0)
        .collect::<Vec<_>>();
    let transfer_by_id = if transfer_ids.is_empty() {
        HashMap::new()
    } else {
        bookkeeping_postgres::financial_command_result::load_transfer_ledger_entry_results(
            pool,
            household_ids,
            &transfer_ids,
        )
        .await
        .map_err(crate::internal_error)?
    };

    let mut loaded = HashMap::with_capacity(rows.len());
    for (
        id,
        _household_id,
        source_kind,
        source_id,
        correction_purchase_id,
        occurred_at,
        description,
        amount_yen,
        created_at,
    ) in rows
    {
        let currency = "JPY".to_owned();
        let entry = match source_kind.as_str() {
            "PURCHASE" | "purchase" => {
                let purchase = purchase_from_batch(&purchase_nodes, source_id)?;
                ledger_entry::purchase_ledger_entry(
                    id,
                    occurred_at,
                    description,
                    amount_yen,
                    currency,
                    created_at,
                    purchase,
                )
            }
            "CORRECTION" | "correction" => {
                let purchase_id = correction_purchase_id
                    .ok_or_else(|| crate::internal_error("correction has no parent purchase"))?;
                let purchase = purchase_from_batch(&purchase_nodes, purchase_id)?;
                ledger_entry::correction_ledger_entry(
                    id,
                    occurred_at,
                    description,
                    amount_yen,
                    currency,
                    created_at,
                    purchase,
                )
            }
            "FUNDING" | "funding" => {
                let projection = funding_by_id.get(&id).cloned().ok_or_else(|| {
                    crate::internal_error(format!("funding ledger entry {id} was not loaded"))
                })?;
                ledger_entry::LedgerEntry::FundingLedgerEntry(
                    ledger_entry::FundingLedgerEntry::try_from(projection)
                        .map_err(crate::internal_error)?,
                )
            }
            "TRANSFER" | "transfer" => {
                let projection = transfer_by_id.get(&id).cloned().ok_or_else(|| {
                    crate::internal_error(format!("transfer ledger entry {id} was not loaded"))
                })?;
                ledger_entry::LedgerEntry::try_from(projection).map_err(|error| {
                    crate::internal_error(format!("invalid transfer ledger entry: {error:?}"))
                })?
            }
            value => return Err(Error::new(format!("unknown ledger entry kind {value:?}"))),
        };
        loaded.insert(id, Node::LedgerEntry(entry));
    }
    Ok(loaded)
}

fn purchase_from_batch(
    purchases: &HashMap<(String, Uuid), Node>,
    purchase_id: Uuid,
) -> Result<crate::purchase_mutations::PurchaseObject> {
    match purchases.get(&("Purchase".to_owned(), purchase_id)) {
        Some(Node::Purchase(value)) => Ok(value.clone()),
        _ => Err(crate::internal_error(format!(
            "ledger entry source purchase {purchase_id} was not loaded"
        ))),
    }
}
