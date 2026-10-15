use crate::{MemberId, PurchaseId};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountingPeriodStatus {
    Open,
    Closed,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SettlementInput {
    pub member_id: MemberId,
    pub member_name: String,
    pub scheduled_yen: i64,
    pub paid_yen: i64,
    pub previous_owed_yen: i64,
    pub monthly_adjustment_yen: i64,
    pub advance_credit_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseToClose {
    pub id: PurchaseId,
    pub revision: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClosePeriodCommand {
    pub period: String,
    pub status: AccountingPeriodStatus,
    pub revision: u64,
    pub expected_revision: u64,
    pub has_draft_purchases: bool,
    pub actor_id: MemberId,
    pub settlements: Vec<SettlementInput>,
    pub posted_purchases: Vec<PurchaseToClose>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SettlementLine {
    pub member_id: MemberId,
    pub member_name: String,
    pub scheduled_yen: i64,
    pub paid_yen: i64,
    pub carry_yen: i64,
    pub monthly_adjustment_yen: i64,
    pub advance_credit_yen: i64,
    pub amount_due_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseClosure {
    pub purchase_id: PurchaseId,
    pub expected_revision: u64,
    pub revision: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PeriodClosedEvent {
    pub actor_id: MemberId,
    pub period: String,
    pub revision: u64,
    pub changed_fields: &'static [&'static str],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClosePeriodDecision {
    pub period: String,
    pub revision: u64,
    pub settlements: Vec<SettlementLine>,
    pub purchase_closures: Vec<PurchaseClosure>,
    pub event: PeriodClosedEvent,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ClosePeriodError {
    InvalidPeriod,
    PeriodAlreadyClosed,
    RevisionMismatch { expected: u64, actual: u64 },
    DraftPurchasesExist,
    EmptyMemberId,
    EmptyMemberName,
    DuplicateMember,
    DuplicatePurchase,
    NegativeNonnegativeAmount,
    AmountOverflow,
    RevisionOverflow,
}

pub fn decide_close_period(
    command: ClosePeriodCommand,
) -> Result<ClosePeriodDecision, ClosePeriodError> {
    validate_period(&command.period)?;
    if command.status == AccountingPeriodStatus::Closed {
        return Err(ClosePeriodError::PeriodAlreadyClosed);
    }
    if command.revision != command.expected_revision {
        return Err(ClosePeriodError::RevisionMismatch {
            expected: command.expected_revision,
            actual: command.revision,
        });
    }
    if command.has_draft_purchases {
        return Err(ClosePeriodError::DraftPurchasesExist);
    }

    let revision = command
        .revision
        .checked_add(1)
        .ok_or(ClosePeriodError::RevisionOverflow)?;

    let mut settlements = command
        .settlements
        .into_iter()
        .map(calculate_settlement)
        .collect::<Result<Vec<_>, _>>()?;
    settlements.sort_by(|left, right| left.member_id.cmp(&right.member_id));
    if settlements
        .windows(2)
        .any(|pair| pair[0].member_id == pair[1].member_id)
    {
        return Err(ClosePeriodError::DuplicateMember);
    }

    let mut purchase_ids = command
        .posted_purchases
        .iter()
        .map(|purchase| purchase.id.clone())
        .collect::<Vec<_>>();
    purchase_ids.sort();
    if purchase_ids.windows(2).any(|pair| pair[0] == pair[1]) {
        return Err(ClosePeriodError::DuplicatePurchase);
    }

    let purchase_closures = command
        .posted_purchases
        .into_iter()
        .map(|purchase| {
            Ok(PurchaseClosure {
                purchase_id: purchase.id,
                expected_revision: purchase.revision,
                revision: purchase
                    .revision
                    .checked_add(1)
                    .ok_or(ClosePeriodError::RevisionOverflow)?,
            })
        })
        .collect::<Result<Vec<_>, ClosePeriodError>>()?;

    Ok(ClosePeriodDecision {
        period: command.period.clone(),
        revision,
        settlements,
        purchase_closures,
        event: PeriodClosedEvent {
            actor_id: command.actor_id,
            period: command.period,
            revision,
            changed_fields: &["status"],
        },
    })
}

fn calculate_settlement(input: SettlementInput) -> Result<SettlementLine, ClosePeriodError> {
    if input.member_id.as_str().is_empty() {
        return Err(ClosePeriodError::EmptyMemberId);
    }
    if input.member_name.is_empty() {
        return Err(ClosePeriodError::EmptyMemberName);
    }
    if input.scheduled_yen < 0 || input.paid_yen < 0 || input.advance_credit_yen < 0 {
        return Err(ClosePeriodError::NegativeNonnegativeAmount);
    }
    let amount_due_yen = input
        .scheduled_yen
        .checked_add(input.previous_owed_yen)
        .and_then(|amount| amount.checked_add(input.monthly_adjustment_yen))
        .and_then(|amount| amount.checked_sub(input.paid_yen))
        .and_then(|amount| amount.checked_sub(input.advance_credit_yen))
        .ok_or(ClosePeriodError::AmountOverflow)?;

    Ok(SettlementLine {
        member_id: input.member_id,
        member_name: input.member_name,
        scheduled_yen: input.scheduled_yen,
        paid_yen: input.paid_yen,
        carry_yen: input.previous_owed_yen,
        monthly_adjustment_yen: input.monthly_adjustment_yen,
        advance_credit_yen: input.advance_credit_yen,
        amount_due_yen,
    })
}

fn validate_period(period: &str) -> Result<(), ClosePeriodError> {
    let bytes = period.as_bytes();
    if bytes.len() != 7
        || bytes[4] != b'-'
        || !bytes[..4].iter().all(u8::is_ascii_digit)
        || !bytes[5..].iter().all(u8::is_ascii_digit)
    {
        return Err(ClosePeriodError::InvalidPeriod);
    }
    let month = period[5..]
        .parse::<u8>()
        .map_err(|_| ClosePeriodError::InvalidPeriod)?;
    if month == 0 || month > 12 {
        return Err(ClosePeriodError::InvalidPeriod);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{MemberId, PurchaseId};

    fn member(value: &str) -> MemberId {
        MemberId::new(value).unwrap()
    }

    fn purchase(value: &str) -> PurchaseId {
        PurchaseId::new(value).unwrap()
    }

    fn command() -> ClosePeriodCommand {
        ClosePeriodCommand {
            period: "2025-02".into(),
            status: AccountingPeriodStatus::Open,
            revision: 3,
            expected_revision: 3,
            has_draft_purchases: false,
            actor_id: member("actor"),
            settlements: vec![SettlementInput {
                member_id: member("member-b"),
                member_name: "B".into(),
                scheduled_yen: 10_000,
                paid_yen: 8_000,
                previous_owed_yen: -500,
                monthly_adjustment_yen: 300,
                advance_credit_yen: 100,
            }],
            posted_purchases: vec![PurchaseToClose {
                id: purchase("purchase-1"),
                revision: 8,
            }],
        }
    }

    #[test]
    fn closing_calculates_snapshot_and_revisioned_transitions() {
        let decision = decide_close_period(command()).unwrap();

        assert_eq!(decision.revision, 4);
        assert_eq!(decision.settlements[0].amount_due_yen, 1_700);
        assert_eq!(decision.purchase_closures[0].revision, 9);
        assert_eq!(decision.event.actor_id.as_str(), "actor");
        assert_eq!(decision.event.changed_fields, &["status"]);
    }

    #[test]
    fn closing_rejects_stale_periods_drafts_and_duplicate_members() {
        let mut stale = command();
        stale.expected_revision -= 1;
        assert!(matches!(
            decide_close_period(stale),
            Err(ClosePeriodError::RevisionMismatch { .. })
        ));

        let mut with_draft = command();
        with_draft.has_draft_purchases = true;
        assert_eq!(
            decide_close_period(with_draft),
            Err(ClosePeriodError::DraftPurchasesExist)
        );

        let mut duplicate = command();
        duplicate.settlements.push(duplicate.settlements[0].clone());
        assert_eq!(
            decide_close_period(duplicate),
            Err(ClosePeriodError::DuplicateMember)
        );
    }

    #[test]
    fn closing_rejects_invalid_period_and_amount_overflow() {
        let mut invalid_period = command();
        invalid_period.period = "2025-13".into();
        assert_eq!(
            decide_close_period(invalid_period),
            Err(ClosePeriodError::InvalidPeriod)
        );

        let mut overflow = command();
        overflow.settlements[0].scheduled_yen = i64::MAX;
        overflow.settlements[0].previous_owed_yen = 1;
        assert_eq!(
            decide_close_period(overflow),
            Err(ClosePeriodError::AmountOverflow)
        );
    }
}
