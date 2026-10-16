use async_graphql::{Context, Result};
use bookkeeping_application::ledger_entry_batch_query::LedgerEntryBatchQuery;
use chrono::{Datelike, Duration, NaiveDate, Utc};
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

    let rows = ctx
        .data::<std::sync::Arc<dyn LedgerEntryBatchQuery>>()?
        .query(household_ids, ids)
        .await
        .map_err(crate::internal_error)?;

    let purchase_ids = rows
        .iter()
        .filter_map(|row| match row.source_kind.as_str() {
            "PURCHASE" | "purchase" => Some(row.source_id),
            "CORRECTION" | "correction" => row.correction_purchase_id,
            _ => None,
        })
        .collect::<std::collections::HashSet<_>>();
    let purchase_requests = purchase_ids
        .into_iter()
        .map(|id| ("Purchase".to_owned(), id))
        .collect::<Vec<_>>();
    let purchase_nodes =
        crate::node_purchase_batch::load(ctx, household_ids, &purchase_requests).await?;
    let financial_entry_ids = rows
        .iter()
        .filter(|row| {
            matches!(
                row.source_kind.as_str(),
                "FUNDING" | "funding" | "TRANSFER" | "transfer"
            )
        })
        .map(|row| row.id)
        .collect::<Vec<_>>();
    let now = Utc::now();
    let next_month = if now.month() == 12 {
        NaiveDate::from_ymd_opt(now.year() + 1, 1, 1)
    } else {
        NaiveDate::from_ymd_opt(now.year(), now.month() + 1, 1)
    }
    .ok_or_else(|| crate::internal_error("invalid next month"))?;
    let period_end = chrono::DateTime::<Utc>::from_naive_utc_and_offset(
        next_month
            .checked_add_signed(Duration::days(i64::from(now.day() - 1)))
            .ok_or_else(|| crate::internal_error("invalid next month date"))?
            .and_time(now.time()),
        Utc,
    );
    let financial = ctx
        .data::<std::sync::Arc<
            dyn cross_context_application::node_financial_batch::NodeFinancialBatchQuery,
        >>()?
        .load(
            cross_context_application::node_financial_batch::NodeFinancialBatchInput {
                household_ids: household_ids.to_vec(),
                funding_schedule_ids: Vec::new(),
                budget_ids: Vec::new(),
                loan_balance_ids: Vec::new(),
                ledger_entry_ids: financial_entry_ids,
                transfer_ids: Vec::new(),
                period_start: now,
                period_end,
            },
        )
        .await
        .map_err(crate::internal_error)?;

    let mut loaded = HashMap::with_capacity(rows.len());
    for row in rows {
        let id = row.id;
        let source_kind = row.source_kind;
        let source_id = row.source_id;
        let correction_purchase_id = row.correction_purchase_id;
        let occurred_at = row.occurred_at;
        let description = row.description;
        let amount_yen = row.amount_yen;
        let created_at = row.created_at;
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
                let projection = financial
                    .funding_ledger_entries
                    .get(&id)
                    .cloned()
                    .ok_or_else(|| {
                        crate::internal_error(format!("funding ledger entry {id} was not loaded"))
                    })?;
                ledger_entry::LedgerEntry::FundingLedgerEntry(
                    ledger_entry::FundingLedgerEntry::try_from(projection)
                        .map_err(crate::internal_error)?,
                )
            }
            "TRANSFER" | "transfer" => {
                let projection = financial
                    .transfer_ledger_entries
                    .get(&id)
                    .cloned()
                    .ok_or_else(|| {
                        crate::internal_error(format!("transfer ledger entry {id} was not loaded"))
                    })?;
                ledger_entry::LedgerEntry::try_from(projection).map_err(|error| {
                    crate::internal_error(format!("invalid transfer ledger entry: {error:?}"))
                })?
            }
            value => {
                return Err(crate::internal_error(format!(
                    "unknown ledger entry kind {value:?}"
                )));
            }
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
