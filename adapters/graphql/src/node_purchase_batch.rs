use async_graphql::{Context, Result};
use std::collections::{HashMap, HashSet};
use uuid::Uuid;

use bookkeeping_application::purchase_node_batch_query::PurchaseNodeBatchQuery;

use crate::{ledger_entry, purchase_mutations};

pub(crate) async fn load(
    ctx: &Context<'_>,
    household_ids: &[Uuid],
    requested: &[(String, Uuid)],
) -> Result<HashMap<(String, Uuid), ledger_entry::Node>> {
    if household_ids.is_empty() || requested.is_empty() {
        return Ok(HashMap::new());
    }
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

    let query = ctx.data::<std::sync::Arc<dyn PurchaseNodeBatchQuery>>()?;
    let projection = query
        .query(
            household_ids,
            &purchase_ids.iter().copied().collect::<Vec<_>>(),
            &item_ids.iter().copied().collect::<Vec<_>>(),
            &correction_ids.iter().copied().collect::<Vec<_>>(),
        )
        .await
        .map_err(crate::internal_error)?;
    let item_parents = projection.item_parents;
    let correction_rows = projection.corrections;
    let mut household_by_purchase = HashMap::new();
    for parent in &item_parents {
        household_by_purchase.insert(parent.purchase_id, parent.household_id);
    }
    for correction in &correction_rows {
        household_by_purchase.insert(correction.purchase_id, correction.household_id);
    }
    let actor = crate::actor(ctx)?;
    let purchase_query = ctx.data::<std::sync::Arc<
        dyn bookkeeping_application::purchase_search_query::PurchaseSearchQuery,
    >>()?;
    let mut purchase_keys = household_by_purchase
        .into_iter()
        .map(|(purchase_id, household_id)| (household_id, purchase_id))
        .collect::<HashSet<_>>();
    for purchase_id in &purchase_ids {
        if !purchase_keys.iter().any(|(_, id)| id == purchase_id) {
            purchase_keys.extend(
                household_ids
                    .iter()
                    .copied()
                    .map(|household_id| (household_id, *purchase_id)),
            );
        }
    }
    use futures_util::{StreamExt, TryStreamExt};
    let projections = futures_util::stream::iter(purchase_keys)
        .map(|(household_id, purchase_id)| async move {
            match purchase_query
                .purchase(actor.0, household_id, purchase_id)
                .await
            {
                Ok(projection) => Ok(Some(((household_id, purchase_id), projection))),
                Err(
                    bookkeeping_application::purchase_search_query::PurchaseSearchQueryError::PurchaseNotFound
                    | bookkeeping_application::purchase_search_query::PurchaseSearchQueryError::HouseholdNotFound,
                ) => Ok(None),
                Err(error) => Err(crate::internal_error(error)),
            }
        })
        .buffer_unordered(16)
        .try_collect::<Vec<_>>()
        .await?;
    let mut purchase_nodes = HashMap::new();
    for ((household_id, purchase_id), projection) in projections.into_iter().flatten() {
        let purchase = purchase_mutations::PurchaseObject::try_from(projection)
            .map_err(crate::internal_error)?;
        purchase_nodes.insert((household_id, purchase_id), purchase);
    }

    let mut result = HashMap::new();
    for parent in item_parents {
        if !item_ids.contains(&parent.item_id) {
            continue;
        }
        if let Some(purchase) = purchase_nodes.get(&(parent.household_id, parent.purchase_id))
            && let Some(item) = purchase.items.iter().find(|item| {
                crate::node_query::decode_global_node_id(&item.id, "PurchaseItem").ok()
                    == Some(parent.item_id)
            })
        {
            result.insert(
                ("PurchaseItem".to_owned(), parent.item_id),
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

    let mut payments = HashMap::new();
    for payment in projection.correction_payments {
        let kind = match payment.account_kind.as_str() {
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
        let account_id = crate::node_query::encode_global_node_id("Account", payment.account_id);
        payments
            .entry(payment.correction_id)
            .or_insert_with(Vec::new)
            .push(purchase_mutations::PurchasePayment {
                payment_account_id: account_id.clone(),
                account: crate::account_mutations::AccountObject {
                    id: account_id,
                    name: payment.account_name,
                    kind,
                    card_last4: payment.card_last4,
                    pool_id: payment
                        .pool_id
                        .map(|id| crate::node_query::encode_global_node_id("Pool", id)),
                },
                amount_yen: crate::Long(payment.amount_yen),
            });
    }
    for correction in correction_rows {
        let payment_allocations = payments.get(&correction.id).cloned().unwrap_or_default();
        let id = crate::node_query::encode_global_node_id("PurchaseCorrection", correction.id);
        let settlement_month =
            crate::policy_mutations::YearMonth::from_naive_date(correction.settlement_month);
        let occurred_at = correction.occurred_at.into();
        let node = match correction.item_id {
            Some(item_id) => ledger_entry::Node::PurchaseItemCorrection(
                purchase_mutations::PurchaseItemCorrection {
                    id,
                    item_id: crate::node_query::encode_global_node_id("PurchaseItem", item_id),
                    amount_delta_yen: crate::Long(correction.amount_delta_yen),
                    reason: correction.reason,
                    occurred_at,
                    settlement_month,
                    payment_allocations,
                },
            ),
            None => ledger_entry::Node::PurchaseWideCorrection(
                purchase_mutations::PurchaseWideCorrection {
                    id,
                    amount_delta_yen: crate::Long(correction.amount_delta_yen),
                    reason: correction.reason,
                    occurred_at,
                    settlement_month,
                    payment_allocations,
                },
            ),
        };
        if correction_ids.contains(&correction.id)
            && household_ids.contains(&correction.household_id)
        {
            result.insert(("PurchaseCorrection".to_owned(), correction.id), node);
        }
    }
    Ok(result)
}
