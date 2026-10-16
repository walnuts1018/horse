use crate::MAX_YEN;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SettlementInput {
    pub scheduled_yen: i64,
    pub paid_yen: i64,
    pub previous_owed_yen: i64,
    pub monthly_adjustment_yen: i64,
    pub advance_credit_yen: i64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SettlementAmounts {
    pub carry_yen: i64,
    pub amount_due_yen: i64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SettlementError {
    NegativeNonnegativeAmount,
    AmountOutOfRange,
    AmountOverflow,
}

pub fn calculate_settlement(input: SettlementInput) -> Result<SettlementAmounts, SettlementError> {
    if input.scheduled_yen < 0 || input.paid_yen < 0 || input.advance_credit_yen < 0 {
        return Err(SettlementError::NegativeNonnegativeAmount);
    }
    if [
        input.scheduled_yen,
        input.paid_yen,
        input.previous_owed_yen,
        input.monthly_adjustment_yen,
        input.advance_credit_yen,
    ]
    .into_iter()
    .any(|amount| !(-MAX_YEN..=MAX_YEN).contains(&amount))
    {
        return Err(SettlementError::AmountOutOfRange);
    }

    let amount_due_yen = input
        .scheduled_yen
        .checked_add(input.previous_owed_yen)
        .and_then(|amount| amount.checked_add(input.monthly_adjustment_yen))
        .and_then(|amount| amount.checked_sub(input.paid_yen))
        .and_then(|amount| amount.checked_sub(input.advance_credit_yen))
        .ok_or(SettlementError::AmountOverflow)?;
    if !(-MAX_YEN..=MAX_YEN).contains(&amount_due_yen) {
        return Err(SettlementError::AmountOutOfRange);
    }

    Ok(SettlementAmounts {
        carry_yen: input.previous_owed_yen,
        amount_due_yen,
    })
}

#[cfg(test)]
mod tests {
    use super::{SettlementAmounts, SettlementInput, calculate_settlement};

    #[test]
    fn scheduled_shortfall_and_previous_owed_are_added_to_amount_due() {
        let result = calculate_settlement(SettlementInput {
            scheduled_yen: 10_000,
            paid_yen: 8_000,
            previous_owed_yen: 2_000,
            monthly_adjustment_yen: 0,
            advance_credit_yen: 0,
        });

        assert_eq!(
            result,
            Ok(SettlementAmounts {
                carry_yen: 2_000,
                amount_due_yen: 4_000,
            })
        );
    }

    #[test]
    fn settlement_rejects_amounts_outside_the_supported_yen_range() {
        assert_eq!(
            calculate_settlement(SettlementInput {
                scheduled_yen: 1_000_000_000_001,
                paid_yen: 0,
                previous_owed_yen: 0,
                monthly_adjustment_yen: 0,
                advance_credit_yen: 0,
            }),
            Err(super::SettlementError::AmountOutOfRange)
        );
    }

    #[test]
    fn settlement_rejects_results_outside_the_supported_yen_range() {
        assert_eq!(
            calculate_settlement(SettlementInput {
                scheduled_yen: crate::MAX_YEN,
                paid_yen: 0,
                previous_owed_yen: crate::MAX_YEN,
                monthly_adjustment_yen: 0,
                advance_credit_yen: 0,
            }),
            Err(super::SettlementError::AmountOutOfRange)
        );
        assert_eq!(
            calculate_settlement(SettlementInput {
                scheduled_yen: 0,
                paid_yen: 0,
                previous_owed_yen: -crate::MAX_YEN,
                monthly_adjustment_yen: -crate::MAX_YEN,
                advance_credit_yen: 0,
            }),
            Err(super::SettlementError::AmountOutOfRange)
        );
    }
}
