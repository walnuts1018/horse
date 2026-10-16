use std::collections::{BTreeMap, BTreeSet};

use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, JoinOnDsl, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum PurchaseDefaultsQueryError {
    Pool(String),
    Database(diesel::result::Error),
    HouseholdNotFound,
    CountOutOfRange,
}

impl From<diesel::result::Error> for PurchaseDefaultsQueryError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseDefaultProjection {
    pub merchant: String,
    pub payment_account_id: Uuid,
    pub purchase_count: i32,
    pub last_used_at: DateTime<Utc>,
}

type PurchasePayment = (Uuid, String, Uuid, DateTime<Utc>);

pub async fn purchase_defaults(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<Vec<PurchaseDefaultProjection>, PurchaseDefaultsQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseDefaultsQueryError::Pool(error.to_string()))?;
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
            .ok_or(PurchaseDefaultsQueryError::CountOutOfRange)?;
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
) -> Result<(), PurchaseDefaultsQueryError> {
    if household_postgres::household_access::active_member_role(conn, household_id, actor_id)
        .await?
        .is_some()
    {
        Ok(())
    } else {
        Err(PurchaseDefaultsQueryError::HouseholdNotFound)
    }
}
