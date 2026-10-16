use async_graphql::{Context, Error, ID, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use cross_context_application::statement_refund_correction_query::{
    StatementRefundCorrectionQuery, StatementRefundCorrectionQueryError,
};
use uuid::Uuid;

use crate::{Long, pagination::PageInfo};

#[derive(Clone, Debug, SimpleObject)]
pub struct StatementRefundCorrection {
    id: ID,
    amount_yen: Long,
    occurred_at: crate::DateTime,
    merchant: String,
    source_filename: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct StatementRefundCorrectionEdge {
    cursor: String,
    node: StatementRefundCorrection,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct StatementRefundCorrectionConnection {
    edges: Vec<StatementRefundCorrectionEdge>,
    page_info: PageInfo,
}

pub async fn graphql_statement_refund_corrections(
    ctx: &Context<'_>,
    household_id: ID,
    source_document_id: ID,
    source_reference: String,
    first: Option<i32>,
    after: Option<String>,
) -> Result<StatementRefundCorrectionConnection> {
    let actor = crate::actor(ctx)?;
    let household_id = decode_node_id(&household_id, "Household")?;
    let source_document_id = Uuid::parse_str(source_document_id.as_str())
        .map_err(|_| Error::new("invalid source document ID"))?;
    let after = after
        .as_deref()
        .filter(|value| !value.is_empty())
        .map(decode_cursor)
        .transpose()?;
    let query = ctx.data::<std::sync::Arc<dyn StatementRefundCorrectionQuery>>()?;
    let page = query
        .load(
            actor.0,
            household_id,
            source_document_id,
            source_reference,
            first,
            after,
        )
        .await
        .map_err(map_query_error)?;
    let edges = page
        .edges
        .into_iter()
        .map(|row| {
            let id = encode_node_id("PurchaseCorrection", row.correction_id)?;
            Ok(StatementRefundCorrectionEdge {
                cursor: encode_cursor(row.correction_id),
                node: StatementRefundCorrection {
                    id,
                    amount_yen: Long(row.amount_yen),
                    occurred_at: row.occurred_at.into(),
                    merchant: row.merchant,
                    source_filename: row.source_filename,
                },
            })
        })
        .collect::<Result<Vec<_>>>()?;
    let start_cursor = edges.first().map(|edge| edge.cursor.clone());
    let end_cursor = edges.last().map(|edge| edge.cursor.clone());
    Ok(StatementRefundCorrectionConnection {
        edges,
        page_info: PageInfo {
            has_next_page: page.has_next_page,
            has_previous_page: after.is_some(),
            start_cursor,
            end_cursor,
        },
    })
}

fn map_query_error(error: StatementRefundCorrectionQueryError) -> Error {
    match error {
        StatementRefundCorrectionQueryError::ResourceUnavailable => Error::new("not found"),
        StatementRefundCorrectionQueryError::InvalidInput => {
            Error::new("invalid statement refund query")
        }
        StatementRefundCorrectionQueryError::SourceDocumentNotFound
        | StatementRefundCorrectionQueryError::StatementNotFound => {
            Error::new("statement refund not found")
        }
        StatementRefundCorrectionQueryError::Infrastructure(_) => crate::internal_error(error),
    }
}

fn decode_node_id(value: &ID, expected_kind: &str) -> Result<Uuid> {
    let decoded = URL_SAFE_NO_PAD
        .decode(value.as_str())
        .map_err(|_| Error::new("invalid node ID"))?;
    let separator = decoded
        .iter()
        .position(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid node ID"))?;
    let (kind, raw_id) = decoded.split_at(separator);
    let raw_id = &raw_id[1..];
    if kind != expected_kind.as_bytes() {
        return Err(Error::new("invalid node ID"));
    }
    Uuid::parse_str(std::str::from_utf8(raw_id).map_err(|_| Error::new("invalid node ID"))?)
        .map_err(|_| Error::new("invalid node ID"))
}

fn encode_node_id(kind: &str, id: Uuid) -> Result<ID> {
    crate::catalog_mutations::encode_node_id(kind, &id.to_string())
        .map_err(|_| crate::internal_error("failed to encode node ID"))
}

fn decode_cursor(value: &str) -> Result<Uuid> {
    let decoded = URL_SAFE_NO_PAD
        .decode(value)
        .map_err(|_| Error::new("invalid cursor"))?;
    let separator = decoded
        .iter()
        .position(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid cursor"))?;
    let (kind, raw_id) = decoded.split_at(separator);
    let raw_id = &raw_id[1..];
    if kind != b"PurchaseCorrection" {
        return Err(Error::new("invalid cursor"));
    }
    Uuid::parse_str(std::str::from_utf8(raw_id).map_err(|_| Error::new("invalid cursor"))?)
        .map_err(|_| Error::new("invalid cursor"))
}

fn encode_cursor(id: Uuid) -> String {
    URL_SAFE_NO_PAD.encode(format!("PurchaseCorrection:{id}"))
}
