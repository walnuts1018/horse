use std::collections::BTreeMap;
use uuid::Uuid;

const OPENING_BALANCE_SOURCE_KIND: &str = "ACCOUNT_OPENING_BALANCE";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountKind {
    Card,
    Other,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountBalanceInput {
    pub account_id: Uuid,
    pub kind: AccountKind,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct JournalEntryBalanceInput {
    pub account_id: Uuid,
    pub source_kind: String,
    pub source_id: Uuid,
    pub occurred_at_micros: i64,
    pub debit_yen: i64,
    pub credit_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountBalance {
    pub account_id: Uuid,
    pub balance_yen: i64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccountBalancesError {
    BalanceOverflow,
}

pub fn calculate_account_balances(
    accounts: &[AccountBalanceInput],
    entries: &[JournalEntryBalanceInput],
) -> Result<Vec<AccountBalance>, AccountBalancesError> {
    let kinds = accounts
        .iter()
        .map(|account| (account.account_id, account.kind))
        .collect::<BTreeMap<_, _>>();
    let mut opening = BTreeMap::<Uuid, (i64, i128)>::new();
    let mut movements = BTreeMap::<Uuid, Vec<(i64, i128)>>::new();

    for entry in entries {
        let Some(kind) = kinds.get(&entry.account_id) else {
            continue;
        };
        let amount = match kind {
            AccountKind::Card => i128::from(entry.credit_yen) - i128::from(entry.debit_yen),
            AccountKind::Other => i128::from(entry.debit_yen) - i128::from(entry.credit_yen),
        };
        if entry.source_kind == OPENING_BALANCE_SOURCE_KIND && entry.source_id == entry.account_id {
            let value = opening
                .entry(entry.account_id)
                .or_insert((entry.occurred_at_micros, 0));
            value.0 = value.0.max(entry.occurred_at_micros);
            value.1 = value
                .1
                .checked_add(amount)
                .ok_or(AccountBalancesError::BalanceOverflow)?;
        } else {
            movements
                .entry(entry.account_id)
                .or_default()
                .push((entry.occurred_at_micros, amount));
        }
    }

    accounts
        .iter()
        .map(|account| {
            let (opening_at, opening_yen) = opening
                .get(&account.account_id)
                .map(|(at, amount)| (Some(*at), *amount))
                .unwrap_or((None, 0));
            let movement_yen = movements
                .get(&account.account_id)
                .into_iter()
                .flatten()
                .filter(|(at, _)| opening_at.is_none_or(|opening_at| *at >= opening_at))
                .try_fold(0_i128, |total, (_, amount)| {
                    total
                        .checked_add(*amount)
                        .ok_or(AccountBalancesError::BalanceOverflow)
                })?;
            let total_yen = opening_yen
                .checked_add(movement_yen)
                .ok_or(AccountBalancesError::BalanceOverflow)?;
            let balance_yen =
                i64::try_from(total_yen).map_err(|_| AccountBalancesError::BalanceOverflow)?;
            Ok(AccountBalance {
                account_id: account.account_id,
                balance_yen,
            })
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn account(id: u128, kind: AccountKind) -> AccountBalanceInput {
        AccountBalanceInput {
            account_id: Uuid::from_u128(id),
            kind,
        }
    }

    fn entry(
        account_id: Uuid,
        source_kind: &str,
        source_id: Uuid,
        occurred_at_micros: i64,
        debit_yen: i64,
        credit_yen: i64,
    ) -> JournalEntryBalanceInput {
        JournalEntryBalanceInput {
            account_id,
            source_kind: source_kind.into(),
            source_id,
            occurred_at_micros,
            debit_yen,
            credit_yen,
        }
    }

    #[test]
    fn opening_balance_includes_movements_at_its_timestamp_and_later() {
        let account_id = Uuid::from_u128(1);
        let balances = calculate_account_balances(
            &[account(1, AccountKind::Other)],
            &[
                entry(
                    account_id,
                    OPENING_BALANCE_SOURCE_KIND,
                    account_id,
                    10,
                    100,
                    0,
                ),
                entry(account_id, "TRANSFER", Uuid::from_u128(2), 9, 50, 0),
                entry(account_id, "TRANSFER", Uuid::from_u128(3), 10, 20, 0),
                entry(account_id, "TRANSFER", Uuid::from_u128(4), 11, 0, 5),
            ],
        )
        .unwrap();

        assert_eq!(balances[0].balance_yen, 115);
    }

    #[test]
    fn ordinary_movements_use_account_kind_debit_and_credit_signs() {
        let card_id = Uuid::from_u128(1);
        let other_id = Uuid::from_u128(2);
        let balances = calculate_account_balances(
            &[
                account(1, AccountKind::Card),
                account(2, AccountKind::Other),
            ],
            &[
                entry(card_id, "PURCHASE", Uuid::from_u128(3), 1, 60, 0),
                entry(card_id, "PAYMENT", Uuid::from_u128(4), 2, 0, 15),
                entry(other_id, "TRANSFER", Uuid::from_u128(5), 1, 60, 0),
                entry(other_id, "TRANSFER", Uuid::from_u128(6), 2, 0, 15),
            ],
        )
        .unwrap();

        assert_eq!(balances[0].balance_yen, -45);
        assert_eq!(balances[1].balance_yen, 45);
    }
}
