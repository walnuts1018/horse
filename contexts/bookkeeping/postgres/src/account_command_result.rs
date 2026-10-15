use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, QueryDsl, result::Error as DieselError};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountCommandAccount {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountMovementCommandResult {
    pub id: Uuid,
    pub household_id: Uuid,
    pub kind: String,
    pub occurred_at: DateTime<Utc>,
    pub from_account_id: Uuid,
    pub from_account: AccountCommandAccount,
    pub to_account_id: Uuid,
    pub to_account: AccountCommandAccount,
    pub amount_yen: i64,
    pub description: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountIncomeCommandResult {
    pub id: Uuid,
    pub household_id: Uuid,
    pub account_id: Uuid,
    pub account: AccountCommandAccount,
    pub occurred_at: DateTime<Utc>,
    pub amount_yen: i64,
    pub description: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug)]
pub enum AccountCommandResultError {
    Pool(String),
    Database(DieselError),
}

impl From<DieselError> for AccountCommandResultError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn load_account_movement_command_result(
    pool: &PgPool,
    household_id: Uuid,
    movement_id: Uuid,
) -> Result<AccountMovementCommandResult, AccountCommandResultError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| AccountCommandResultError::Pool(error.to_string()))?;
    use public::account_movements::dsl as movements;

    let (
        id,
        household_id,
        kind,
        occurred_at,
        from_account_id,
        to_account_id,
        amount_yen,
        description,
        created_at,
    ) = movements::account_movements
        .filter(movements::household_id.eq(household_id))
        .filter(movements::id.eq(movement_id))
        .select((
            movements::id,
            movements::household_id,
            movements::kind,
            movements::occurred_at,
            movements::from_account_id,
            movements::to_account_id,
            movements::amount_yen,
            movements::description,
            movements::created_at,
        ))
        .first::<(
            Uuid,
            Uuid,
            String,
            DateTime<Utc>,
            Uuid,
            Uuid,
            i64,
            String,
            DateTime<Utc>,
        )>(&mut *pooled)
        .await?;

    let from_account = load_account(&mut pooled, household_id, from_account_id).await?;
    let to_account = load_account(&mut pooled, household_id, to_account_id).await?;

    Ok(AccountMovementCommandResult {
        id,
        household_id,
        kind,
        occurred_at,
        from_account_id,
        from_account,
        to_account_id,
        to_account,
        amount_yen,
        description,
        created_at,
    })
}

pub async fn load_account_income_command_result(
    pool: &PgPool,
    household_id: Uuid,
    income_id: Uuid,
) -> Result<AccountIncomeCommandResult, AccountCommandResultError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| AccountCommandResultError::Pool(error.to_string()))?;
    use public::account_incomes::dsl as incomes;

    let (id, household_id, account_id, occurred_at, amount_yen, description, created_at) =
        incomes::account_incomes
            .filter(incomes::household_id.eq(household_id))
            .filter(incomes::id.eq(income_id))
            .select((
                incomes::id,
                incomes::household_id,
                incomes::account_id,
                incomes::occurred_at,
                incomes::amount_yen,
                incomes::description,
                incomes::created_at,
            ))
            .first::<(Uuid, Uuid, Uuid, DateTime<Utc>, i64, String, DateTime<Utc>)>(&mut *pooled)
            .await?;

    let account = load_account(&mut pooled, household_id, account_id).await?;

    Ok(AccountIncomeCommandResult {
        id,
        household_id,
        account_id,
        account,
        occurred_at,
        amount_yen,
        description,
        created_at,
    })
}

async fn load_account(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    account_id: Uuid,
) -> Result<AccountCommandAccount, AccountCommandResultError> {
    use public::accounts::dsl as accounts;

    let (id, name, kind, card_last4, pool_id) = accounts::accounts
        .filter(accounts::household_id.eq(household_id))
        .filter(accounts::id.eq(account_id))
        .select((
            accounts::id,
            accounts::name,
            accounts::kind,
            accounts::card_last4,
            accounts::pool_id,
        ))
        .first::<(Uuid, String, String, Option<String>, Option<Uuid>)>(conn)
        .await?;

    Ok(AccountCommandAccount {
        id,
        name,
        kind,
        card_last4,
        pool_id,
    })
}
