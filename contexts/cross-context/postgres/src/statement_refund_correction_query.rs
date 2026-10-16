use chrono::DateTime;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::RunQueryDsl;
use serde_json::Value;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone)]
pub struct StatementRefundCorrectionProjection {
    pub correction_id: Uuid,
    pub amount_yen: i64,
    pub occurred_at: DateTime<chrono::Utc>,
    pub merchant: String,
    pub source_filename: String,
}

#[derive(Debug, Clone)]
pub struct StatementRefundCorrectionConnectionProjection {
    pub edges: Vec<StatementRefundCorrectionProjection>,
    pub has_next_page: bool,
}

#[derive(Debug)]
pub enum StatementRefundCorrectionQueryError {
    Pool(String),
    Database(diesel::result::Error),
    InvalidInput,
    SourceDocumentNotFound,
    StatementNotFound,
}

impl From<diesel::result::Error> for StatementRefundCorrectionQueryError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct RefundStatement {
    reference: String,
    amount_yen: i64,
    occurred_at: DateTime<chrono::Utc>,
    merchant: String,
}

pub async fn load_statement_refund_corrections(
    pool: &PgPool,
    household_id: Uuid,
    source_document_id: Uuid,
    source_reference: &str,
    first: Option<i32>,
    after: Option<Uuid>,
) -> Result<StatementRefundCorrectionConnectionProjection, StatementRefundCorrectionQueryError> {
    use public::{
        evidence_links, extraction_jobs, purchase_corrections, source_confirmations,
        source_documents,
    };

    let page_size = first.unwrap_or(50);
    if !(1..=100).contains(&page_size)
        || source_reference.is_empty()
        || source_reference.len() > 256
    {
        return Err(StatementRefundCorrectionQueryError::InvalidInput);
    }

    let mut conn = pool
        .get()
        .await
        .map_err(|error| StatementRefundCorrectionQueryError::Pool(error.to_string()))?;
    source_documents::table
        .filter(source_documents::household_id.eq(household_id))
        .filter(source_documents::id.eq(source_document_id))
        .select(source_documents::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .ok_or(StatementRefundCorrectionQueryError::SourceDocumentNotFound)?;

    let extraction_result = extraction_jobs::table
        .filter(extraction_jobs::household_id.eq(household_id))
        .filter(extraction_jobs::source_document_id.eq(source_document_id))
        .filter(extraction_jobs::status.eq("succeeded"))
        .order_by(extraction_jobs::version.desc())
        .select(extraction_jobs::result)
        .first::<Option<Value>>(&mut *conn)
        .await
        .optional()?
        .flatten()
        .ok_or(StatementRefundCorrectionQueryError::StatementNotFound)?;
    let current = extraction_result
        .get("statements")
        .and_then(Value::as_array)
        .and_then(|statements| {
            statements
                .iter()
                .filter_map(|statement| {
                    if statement_reference(statement).as_deref() != Some(source_reference) {
                        return None;
                    }
                    crate::correct_purchase::statement_observation(statement, source_reference).ok()
                })
                .map(RefundStatement::from)
                .find(|statement| statement.reference == source_reference)
        })
        .filter(is_refund)
        .ok_or(StatementRefundCorrectionQueryError::StatementNotFound)?;

    let mut scan_after = after;
    let mut edges = Vec::with_capacity(page_size as usize);
    let mut has_next_page = false;
    loop {
        let mut query = evidence_links::table
            .filter(evidence_links::household_id.eq(household_id))
            .filter(evidence_links::entity_kind.eq("purchase_correction"))
            .filter(evidence_links::correction_id.is_not_null())
            .into_boxed();
        if let Some(cursor) = scan_after {
            query = query.filter(evidence_links::correction_id.lt(cursor));
        }
        let links = query
            .order_by(evidence_links::correction_id.desc())
            .limit(101)
            .select((
                evidence_links::source_document_id,
                evidence_links::source_reference,
                evidence_links::correction_id,
                evidence_links::observation,
            ))
            .load::<(Uuid, String, Option<Uuid>, Value)>(&mut *conn)
            .await?;
        if links.is_empty() {
            break;
        }

        let batch_len = links.len();
        for (linked_document_id, linked_reference, correction_id, observation) in links {
            let Some(correction_id) = correction_id else {
                continue;
            };
            scan_after = Some(correction_id);
            if linked_document_id == source_document_id {
                continue;
            }
            let linked_statement = observation.get("statement").unwrap_or(&observation);
            if statement_reference(linked_statement).as_deref() != Some(linked_reference.as_str()) {
                continue;
            }
            let linked =
                crate::correct_purchase::statement_observation(linked_statement, &linked_reference)
                    .ok()
                    .map(RefundStatement::from);
            if !linked.as_ref().is_some_and(|linked| {
                is_refund(linked)
                    && linked.amount_yen == current.amount_yen
                    && linked.occurred_at == current.occurred_at
                    && linked.merchant == current.merchant
            }) {
                continue;
            }
            let correction = purchase_corrections::table
                .filter(purchase_corrections::household_id.eq(household_id))
                .filter(purchase_corrections::id.eq(correction_id))
                .filter(purchase_corrections::amount_delta_yen.eq(current.amount_yen))
                .select(purchase_corrections::amount_delta_yen)
                .first::<i64>(&mut *conn)
                .await
                .optional()?;
            if correction.is_none() {
                continue;
            }
            let confirmed = source_confirmations::table
                .filter(source_confirmations::household_id.eq(household_id))
                .filter(source_confirmations::source_document_id.eq(linked_document_id))
                .filter(source_confirmations::source_reference.eq(linked_reference))
                .filter(source_confirmations::entity_kind.eq("purchase_correction"))
                .filter(source_confirmations::entity_id.eq(correction_id))
                .filter(source_confirmations::status.eq("confirmed"))
                .select(source_confirmations::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .is_some();
            if !confirmed {
                continue;
            }
            let linked_filename = source_documents::table
                .filter(source_documents::household_id.eq(household_id))
                .filter(source_documents::id.eq(linked_document_id))
                .select(source_documents::original_filename)
                .first::<String>(&mut *conn)
                .await
                .optional()?;
            let Some(linked_filename) = linked_filename else {
                continue;
            };
            if edges.len() == page_size as usize {
                has_next_page = true;
                return Ok(StatementRefundCorrectionConnectionProjection {
                    edges,
                    has_next_page,
                });
            }
            edges.push(StatementRefundCorrectionProjection {
                correction_id,
                amount_yen: current.amount_yen,
                occurred_at: current.occurred_at,
                merchant: current.merchant.clone(),
                source_filename: linked_filename,
            });
        }
        if batch_len < 101 {
            break;
        }
    }
    Ok(StatementRefundCorrectionConnectionProjection {
        edges,
        has_next_page,
    })
}

fn statement_reference(statement: &Value) -> Option<String> {
    statement
        .get("sourceReference")
        .and_then(Value::as_str)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
        .or_else(|| {
            statement
                .get("row")
                .and_then(Value::as_i64)
                .map(|row| format!("row:{row}"))
        })
}

impl From<bookkeeping_domain::statement_refund_correction::StatementRefundObservation>
    for RefundStatement
{
    fn from(
        value: bookkeeping_domain::statement_refund_correction::StatementRefundObservation,
    ) -> Self {
        let seconds = value.occurred_at_unix_nanos.div_euclid(1_000_000_000) as i64;
        let nanos = value.occurred_at_unix_nanos.rem_euclid(1_000_000_000) as u32;
        Self {
            reference: value.source_reference,
            amount_yen: value.amount_yen,
            occurred_at: DateTime::from_timestamp(seconds, nanos)
                .expect("parsed RFC3339 timestamp is representable"),
            merchant: value.merchant,
        }
    }
}

fn is_refund(statement: &RefundStatement) -> bool {
    statement.amount_yen < 0 && !statement.merchant.trim().is_empty()
}
