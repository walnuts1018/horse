#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StatementRefundObservation {
    pub source_reference: String,
    pub kind: String,
    pub occurred_at_unix_nanos: i128,
    pub merchant: String,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LinkStatementRefundCorrectionCommand {
    pub source_document_id: String,
    pub source_reference: String,
    pub current_statement: StatementRefundObservation,
    pub correction_id: String,
    pub correction_amount_delta_yen: i64,
    pub linked_source_document_id: String,
    pub linked_source_reference: String,
    pub linked_statement: StatementRefundObservation,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LinkStatementRefundCorrectionDecision {
    pub source_document_id: String,
    pub source_reference: String,
    pub correction_id: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LinkStatementRefundCorrectionError {
    InvalidSource,
    RefundDoesNotMatchCorrection,
}

pub fn decide_link_statement_refund_correction(
    command: LinkStatementRefundCorrectionCommand,
) -> Result<LinkStatementRefundCorrectionDecision, LinkStatementRefundCorrectionError> {
    if command.source_document_id.is_empty()
        || command.correction_id.is_empty()
        || command.source_reference.is_empty()
        || command.source_reference.len() > 256
        || command.linked_source_document_id.is_empty()
        || command.linked_source_reference.is_empty()
    {
        return Err(LinkStatementRefundCorrectionError::InvalidSource);
    }
    let current = &command.current_statement;
    let linked = &command.linked_statement;
    if current.source_reference != command.source_reference
        || linked.source_reference != command.linked_source_reference
        || current.kind != "REFUND"
        || linked.kind != "REFUND"
        || current.amount_yen >= 0
        || linked.amount_yen >= 0
        || current.merchant.trim().is_empty()
        || linked.merchant.trim().is_empty()
    {
        return Err(LinkStatementRefundCorrectionError::InvalidSource);
    }
    if command.source_document_id == command.linked_source_document_id
        || command.correction_amount_delta_yen >= 0
        || command.correction_amount_delta_yen != linked.amount_yen
        || current.amount_yen != linked.amount_yen
        || current.occurred_at_unix_nanos != linked.occurred_at_unix_nanos
        || current.merchant != linked.merchant
    {
        return Err(LinkStatementRefundCorrectionError::RefundDoesNotMatchCorrection);
    }
    Ok(LinkStatementRefundCorrectionDecision {
        source_document_id: command.source_document_id,
        source_reference: command.source_reference,
        correction_id: command.correction_id,
    })
}
