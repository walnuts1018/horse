use std::collections::BTreeSet;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GetSettlementQuery {
    pub household_id: String,
    pub period: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SettlementQueryResult {
    pub household_id: String,
    pub period: String,
    pub status: String,
    pub revision: i64,
    pub created_at_micros: Option<i64>,
    pub lines: Vec<SettlementQueryLine>,
    pub evidence: Vec<SettlementEvidence>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SettlementQueryLine {
    pub member_id: String,
    pub member_name: String,
    pub scheduled_yen: i64,
    pub paid_yen: i64,
    pub carry_yen: i64,
    pub monthly_adjustment_yen: i64,
    pub advance_credit_yen: i64,
    pub amount_due_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SettlementEvidence {
    pub member_id: String,
    pub kind: String,
    pub label: String,
    pub amount_yen: i64,
    pub source_id: String,
    pub purchase_id: Option<String>,
    pub occurred_at_micros: Option<i64>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SettlementQueryError {
    InvalidPeriod,
    InvalidHouseholdId,
}

pub fn validate_queries(queries: &[GetSettlementQuery]) -> Result<(), SettlementQueryError> {
    for query in queries {
        if query.household_id.is_empty() {
            return Err(SettlementQueryError::InvalidHouseholdId);
        }
        let Some((year, month)) = query.period.split_once('-') else {
            return Err(SettlementQueryError::InvalidPeriod);
        };
        if year.len() != 4
            || month.len() != 2
            || year.parse::<u16>().is_err()
            || !matches!(month.parse::<u8>(), Ok(1..=12))
        {
            return Err(SettlementQueryError::InvalidPeriod);
        }
    }
    Ok(())
}

pub fn distinct_queries(queries: &[GetSettlementQuery]) -> Vec<GetSettlementQuery> {
    let mut seen = BTreeSet::new();
    queries
        .iter()
        .filter(|query| seen.insert((query.household_id.clone(), query.period.clone())))
        .cloned()
        .collect()
}
