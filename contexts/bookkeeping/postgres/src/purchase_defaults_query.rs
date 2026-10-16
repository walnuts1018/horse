use std::collections::{BTreeMap, BTreeSet};

use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, JoinOnDsl, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use bookkeeping_application::purchase_defaults_query::{
    PurchaseDefaultProjection, PurchaseDefaultsQuery as PurchaseDefaultsQueryPort,
    PurchaseDefaultsQueryError as ApplicationQueryError,
};

use crate::{PgPool, schema::public};

pub struct PostgresPurchaseDefaultsQuery {
    pool: PgPool,
}

impl PostgresPurchaseDefaultsQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl PurchaseDefaultsQueryPort for PostgresPurchaseDefaultsQuery {
    fn query<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<Vec<PurchaseDefaultProjection>, ApplicationQueryError>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            purchase_defaults(&self.pool, actor_id, household_id)
                .await
                .map_err(|error| match error {
                    PurchaseDefaultsReadError::HouseholdNotFound => {
                        ApplicationQueryError::HouseholdNotFound
                    }
                    PurchaseDefaultsReadError::CountOutOfRange => {
                        ApplicationQueryError::CountOutOfRange
                    }
                    error @ (PurchaseDefaultsReadError::Pool(_)
                    | PurchaseDefaultsReadError::Database(_)) => {
                        ApplicationQueryError::Infrastructure(format!("{error:?}"))
                    }
                })
        })
    }
}

#[derive(Debug)]
pub enum PurchaseDefaultsReadError {
    Pool(String),
    Database(diesel::result::Error),
    HouseholdNotFound,
    CountOutOfRange,
}

impl From<diesel::result::Error> for PurchaseDefaultsReadError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

type PurchasePayment = (Uuid, String, Uuid, DateTime<Utc>);

pub async fn purchase_defaults(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<Vec<PurchaseDefaultProjection>, PurchaseDefaultsReadError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseDefaultsReadError::Pool(error.to_string()))?;
    require_member(&mut conn, actor_id, household_id).await?;

    use public::{purchase_payments, purchases};
    let payments = purchase_payments::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_payments::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::status.eq("POSTED"))
        .select((
            purchases::id,
            purchases::merchant,
            purchase_payments::account_id,
            purchases::occurred_at,
        ))
        .load::<PurchasePayment>(&mut conn)
        .await?;

    let unique_payments = payments
        .into_iter()
        .filter(|(_, merchant, _, _)| !merchant.is_empty())
        .collect::<BTreeSet<_>>();
    let mut aggregates = BTreeMap::<(String, Uuid), (i32, DateTime<Utc>)>::new();
    for (_, merchant, account_id, occurred_at) in unique_payments {
        let aggregate = aggregates
            .entry((merchant, account_id))
            .or_insert((0, occurred_at));
        aggregate.0 = aggregate
            .0
            .checked_add(1)
            .ok_or(PurchaseDefaultsReadError::CountOutOfRange)?;
        if occurred_at > aggregate.1 {
            aggregate.1 = occurred_at;
        }
    }

    let mut rows = aggregates
        .into_iter()
        .map(
            |((merchant, payment_account_id), (purchase_count, last_used_at))| {
                PurchaseDefaultProjection {
                    merchant,
                    payment_account_id,
                    purchase_count,
                    last_used_at,
                }
            },
        )
        .collect::<Vec<_>>();
    rows.sort_by(|left, right| {
        right
            .purchase_count
            .cmp(&left.purchase_count)
            .then_with(|| right.last_used_at.cmp(&left.last_used_at))
            .then_with(|| left.merchant.cmp(&right.merchant))
            .then_with(|| left.payment_account_id.cmp(&right.payment_account_id))
    });
    Ok(rows)
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<(), PurchaseDefaultsReadError> {
    if household_postgres::household_access::active_member_role(conn, household_id, actor_id)
        .await?
        .is_some()
    {
        Ok(())
    } else {
        Err(PurchaseDefaultsReadError::HouseholdNotFound)
    }
}
