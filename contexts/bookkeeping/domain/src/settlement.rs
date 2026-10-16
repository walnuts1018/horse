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
    AmountOverflow,
}

pub fn calculate_settlement(input: SettlementInput) -> Result<SettlementAmounts, SettlementError> {
    if input.scheduled_yen < 0 || input.paid_yen < 0 || input.advance_credit_yen < 0 {
        return Err(SettlementError::NegativeNonnegativeAmount);
    }

    let amount_due_yen = input
        .scheduled_yen
        .checked_add(input.previous_owed_yen)
        .and_then(|amount| amount.checked_add(input.monthly_adjustment_yen))
        .and_then(|amount| amount.checked_sub(input.paid_yen))
        .and_then(|amount| amount.checked_sub(input.advance_credit_yen))
        .ok_or(SettlementError::AmountOverflow)?;

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
}
