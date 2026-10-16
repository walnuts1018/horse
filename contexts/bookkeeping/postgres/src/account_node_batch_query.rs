use bookkeeping_application::account_history_query::{
    AccountIncomeProjection, AccountMovementProjection, AccountProjection,
};
use bookkeeping_application::account_node_batch_query::{
    AccountNodeBatchProjection, AccountNodeBatchQuery as AccountNodeBatchQueryPort,
    AccountNodeBatchQueryError, AccountOpeningBalanceProjection,
};
use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

pub struct PostgresAccountNodeBatchQuery {
    pool: PgPool,
}

impl PostgresAccountNodeBatchQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl AccountNodeBatchQueryPort for PostgresAccountNodeBatchQuery {
    fn load<'a>(
        &'a self,
        household_ids: &'a [Uuid],
        opening_balance_ids: &'a [Uuid],
        movement_ids: &'a [Uuid],
        income_ids: &'a [Uuid],
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<AccountNodeBatchProjection, AccountNodeBatchQueryError>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            if household_ids.is_empty()
                || (opening_balance_ids.is_empty()
                    && movement_ids.is_empty()
                    && income_ids.is_empty())
            {
                return Ok(AccountNodeBatchProjection::default());
            }
            let mut conn =
                self.pool.get().await.map_err(|error| {
                    AccountNodeBatchQueryError::Infrastructure(error.to_string())
                })?;

            let opening_balances = if opening_balance_ids.is_empty() {
                Vec::new()
            } else {
                public::accounts::table
                    .filter(public::accounts::household_id.eq_any(household_ids))
                    .filter(public::accounts::id.eq_any(opening_balance_ids))
                    .select((
                        public::accounts::id,
                        public::accounts::name,
                        public::accounts::kind,
                        public::accounts::card_last4,
                        public::accounts::pool_id,
                    ))
                    .load::<(Uuid, String, String, Option<String>, Option<Uuid>)>(&mut conn)
                    .await
                    .map_err(infrastructure)?
                    .into_iter()
                    .map(
                        |(account_id, account_name, account_kind, card_last4, pool_id)| {
                            AccountOpeningBalanceProjection {
                                account_id,
                                account_name,
                                account_kind,
                                card_last4,
                                pool_id,
                            }
                        },
                    )
                    .collect()
            };

            let movements = if movement_ids.is_empty() {
                Vec::new()
            } else {
                let rows = public::account_movements::table
                    .filter(public::account_movements::household_id.eq_any(household_ids))
                    .filter(public::account_movements::id.eq_any(movement_ids))
                    .select((
                        public::account_movements::id,
                        public::account_movements::household_id,
                        public::account_movements::kind,
                        public::account_movements::occurred_at,
                        public::account_movements::from_account_id,
                        public::account_movements::to_account_id,
                        public::account_movements::amount_yen,
                        public::account_movements::description,
                        public::account_movements::created_at,
                    ))
                    .load::<(
                        Uuid,
                        Uuid,
                        String,
                        chrono::DateTime<chrono::Utc>,
                        Uuid,
                        Uuid,
                        i64,
                        String,
                        chrono::DateTime<chrono::Utc>,
                    )>(&mut conn)
                    .await
                    .map_err(infrastructure)?;
                let account_ids = rows
                    .iter()
                    .flat_map(|row| [row.4, row.5])
                    .collect::<Vec<_>>();
                let accounts = load_accounts(&mut conn, household_ids, &account_ids).await?;
                rows.into_iter()
                    .map(
                        |(
                            id,
                            household_id,
                            kind,
                            occurred_at,
                            from_account_id,
                            to_account_id,
                            amount_yen,
                            description,
                            created_at,
                        )| {
                            let from_account =
                                accounts.get(&from_account_id).cloned().ok_or_else(|| {
                                    AccountNodeBatchQueryError::Infrastructure(
                                        "account movement source account is missing".to_owned(),
                                    )
                                })?;
                            let to_account =
                                accounts.get(&to_account_id).cloned().ok_or_else(|| {
                                    AccountNodeBatchQueryError::Infrastructure(
                                        "account movement destination account is missing"
                                            .to_owned(),
                                    )
                                })?;
                            Ok(AccountMovementProjection {
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
                        },
                    )
                    .collect::<Result<Vec<_>, AccountNodeBatchQueryError>>()?
            };

            let incomes = if income_ids.is_empty() {
                Vec::new()
            } else {
                public::account_incomes::table
                    .filter(public::account_incomes::household_id.eq_any(household_ids))
                    .filter(public::account_incomes::id.eq_any(income_ids))
                    .select((
                        public::account_incomes::id,
                        public::account_incomes::household_id,
                        public::account_incomes::account_id,
                        public::account_incomes::amount_yen,
                        public::account_incomes::occurred_at,
                        public::account_incomes::description,
                        public::account_incomes::created_at,
                    ))
                    .load::<(
                        Uuid,
                        Uuid,
                        Uuid,
                        i64,
                        chrono::DateTime<chrono::Utc>,
                        String,
                        chrono::DateTime<chrono::Utc>,
                    )>(&mut conn)
                    .await
                    .map_err(infrastructure)?
                    .into_iter()
                    .map(
                        |(
                            id,
                            household_id,
                            account_id,
                            amount_yen,
                            occurred_at,
                            description,
                            created_at,
                        )| {
                            AccountIncomeProjection {
                                id,
                                household_id,
                                account_id,
                                amount_yen,
                                occurred_at,
                                description,
                                created_at,
                            }
                        },
                    )
                    .collect()
            };

            Ok(AccountNodeBatchProjection {
                opening_balances,
                movements,
                incomes,
            })
        })
    }
}

async fn load_accounts(
    conn: &mut horse_postgres_common::AsyncPgConnection,
    household_ids: &[Uuid],
    account_ids: &[Uuid],
) -> Result<std::collections::HashMap<Uuid, AccountProjection>, AccountNodeBatchQueryError> {
    if account_ids.is_empty() {
        return Ok(std::collections::HashMap::new());
    }
    public::accounts::table
        .filter(public::accounts::household_id.eq_any(household_ids))
        .filter(public::accounts::id.eq_any(account_ids))
        .select((
            public::accounts::id,
            public::accounts::name,
            public::accounts::kind,
            public::accounts::card_last4,
            public::accounts::pool_id,
        ))
        .load::<(Uuid, String, String, Option<String>, Option<Uuid>)>(conn)
        .await
        .map_err(infrastructure)
        .map(|rows| {
            rows.into_iter()
                .map(|(id, name, kind, card_last4, pool_id)| {
                    (
                        id,
                        AccountProjection {
                            id,
                            name,
                            kind,
                            card_last4,
                            pool_id,
                        },
                    )
                })
                .collect()
        })
}

fn infrastructure(error: diesel::result::Error) -> AccountNodeBatchQueryError {
    AccountNodeBatchQueryError::Infrastructure(format!("{error:?}"))
}
