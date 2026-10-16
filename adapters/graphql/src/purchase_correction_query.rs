use std::sync::Arc;

use async_graphql::{Context, Error, ID, Result};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use cross_context_application::purchase_correction_query::{
    PurchaseCorrectionQuery, PurchaseCorrectionQueryError,
};

use crate::{
    Actor,
    account_mutations::AccountObject,
    policy_mutations::YearMonth,
    purchase_mutations::{
        PurchaseCorrection, PurchaseCorrectionConnection, PurchaseCorrectionEdge,
        PurchaseItemCorrection, PurchasePayment, PurchaseWideCorrection, decode_purchase_id,
    },
    scalars::{DateTime, Long},
};

pub async fn graphql_purchase_corrections(
    ctx: &Context<'_>,
    purchase_id: ID,
    first: Option<i32>,
    after: Option<String>,
) -> Result<PurchaseCorrectionConnection> {
    let actor = ctx
        .data::<Actor>()
        .copied()
        .map_err(|_| Error::new("authentication required"))?;
    let purchase_id = decode_purchase_id(&purchase_id)?;
    let result = ctx
        .data::<Arc<dyn PurchaseCorrectionQuery>>()?
        .load(actor.0, purchase_id, first, after)
        .await
        .map_err(map_query_error)?;

    let edges = result
        .edges
        .into_iter()
        .map(|edge| {
            let payments = edge
                .node
                .payment_allocations
                .into_iter()
                .map(|payment| {
                    let kind = account_kind(&payment.account_kind)?;
                    let account_id = encode_node_id("Account", payment.account_id);
                    Ok(PurchasePayment {
                        payment_account_id: account_id.clone(),
                        account: AccountObject {
                            id: account_id,
                            name: payment.account_name,
                            kind,
                            card_last4: payment.account_card_last4,
                            pool_id: payment.account_pool_id.map(|id| encode_node_id("Pool", id)),
                        },
                        amount_yen: Long(payment.amount_yen),
                    })
                })
                .collect::<Result<Vec<_>>>()?;
            let id = encode_node_id("PurchaseCorrection", edge.node.id);
            let settlement_month = YearMonth::from_naive_date(edge.node.settlement_month);
            let occurred_at: DateTime = edge.node.occurred_at.into();
            let node = match edge.node.item_id {
                Some(item_id) => PurchaseCorrection::Item(PurchaseItemCorrection {
                    id,
                    item_id: encode_node_id("PurchaseItem", item_id),
                    amount_delta_yen: Long(edge.node.amount_delta_yen),
                    reason: edge.node.reason,
                    occurred_at,
                    settlement_month,
                    payment_allocations: payments,
                }),
                None => PurchaseCorrection::Wide(PurchaseWideCorrection {
                    id,
                    amount_delta_yen: Long(edge.node.amount_delta_yen),
                    reason: edge.node.reason,
                    occurred_at,
                    settlement_month,
                    payment_allocations: payments,
                }),
            };
            Ok(PurchaseCorrectionEdge {
                cursor: edge.cursor,
                node,
            })
        })
        .collect::<Result<Vec<_>>>()?;
    let total_count = i32::try_from(result.total_count).map_err(crate::internal_error)?;
    let start_cursor = edges.first().map(|edge| edge.cursor.clone());
    let end_cursor = edges.last().map(|edge| edge.cursor.clone());
    Ok(PurchaseCorrectionConnection {
        edges,
        page_info: crate::pagination::PageInfo {
            has_next_page: result.has_next_page,
            has_previous_page: result.has_previous_page,
            start_cursor,
            end_cursor,
        },
        total_count,
    })
}

fn map_query_error(error: PurchaseCorrectionQueryError) -> Error {
    match error {
        PurchaseCorrectionQueryError::PurchaseNotFound => Error::new("対象が見つかりません。"),
        PurchaseCorrectionQueryError::ResourceUnavailable => Error::new("purchase not found"),
        PurchaseCorrectionQueryError::CursorNotFound => Error::new("対象が見つかりません。"),
        PurchaseCorrectionQueryError::InvalidPageSize
        | PurchaseCorrectionQueryError::InvalidCursor => Error::new("入力を確認してください。"),
        PurchaseCorrectionQueryError::Infrastructure(error) => crate::internal_error(error),
    }
}

fn account_kind(value: &str) -> Result<crate::account_mutations::AccountKind> {
    match value {
        "BANK" => Ok(crate::account_mutations::AccountKind::Bank),
        "CASH" => Ok(crate::account_mutations::AccountKind::Cash),
        "CARD" => Ok(crate::account_mutations::AccountKind::Card),
        "OTHER" => Ok(crate::account_mutations::AccountKind::Other),
        _ => Err(crate::internal_error("invalid account kind")),
    }
}

fn encode_node_id(kind: &str, id: uuid::Uuid) -> ID {
    let mut bytes = Vec::with_capacity(kind.len() + 17);
    bytes.extend_from_slice(kind.as_bytes());
    bytes.push(b':');
    bytes.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(bytes))
}
