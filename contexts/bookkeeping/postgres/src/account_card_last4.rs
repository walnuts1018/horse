use bookkeeping_application::account_card_last4::{
    SetCardLast4Command, SetCardLast4Error, decide_set_card_last4,
};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedSetCardLast4 {
    pub id: String,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<String>,
}

#[derive(Debug)]
pub enum SetCardLast4PersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    AccountNotFound,
    InvalidWorkflow(SetCardLast4Error),
    InvalidDecision,
}

impl From<DieselError> for SetCardLast4PersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_set_card_last4(
    pool: &PgPool,
    actor_id: &str,
    account_id: &str,
    card_last4: Option<String>,
) -> Result<PersistedSetCardLast4, SetCardLast4PersistenceError> {
    let actor_id = parse_uuid(actor_id, "actor_id")?;
    let account_id = parse_uuid(account_id, "account_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SetCardLast4PersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedSetCardLast4, SetCardLast4PersistenceError, _>(async move |conn| {
            use public::accounts::dsl as accounts;
            use public::household_events::dsl as events;
            use public::household_members::dsl as members;
            use public::households::dsl as households;

            let row = accounts::accounts
                .filter(accounts::id.eq(account_id))
                .for_update()
                .select((
                    accounts::household_id,
                    accounts::pool_id,
                    accounts::name,
                    accounts::kind,
                    accounts::card_last4,
                ))
                .first::<(Uuid, Option<Uuid>, String, String, Option<String>)>(conn)
                .await
                .optional()?
                .ok_or(SetCardLast4PersistenceError::AccountNotFound)?;

            let member_exists = members::household_members
                .filter(members::household_id.eq(row.0))
                .filter(members::member_id.eq(actor_id))
                .filter(members::left_at.is_null())
                .select(members::member_id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .is_some();
            if !member_exists {
                return Err(SetCardLast4PersistenceError::AccountNotFound);
            }

            let decision = decide_set_card_last4(SetCardLast4Command {
                actor_id: actor_id.to_string(),
                account_id: account_id.to_string(),
                account_kind: row.3.clone(),
                current: row.4.clone(),
                next: card_last4,
            })
            .map_err(SetCardLast4PersistenceError::InvalidWorkflow)?;
            if decision.actor_id != actor_id.to_string()
                || decision.account_id != account_id.to_string()
            {
                return Err(SetCardLast4PersistenceError::InvalidDecision);
            }

            if let Some(event) = &decision.event {
                let updated = diesel::update(
                    accounts::accounts
                        .filter(accounts::id.eq(account_id))
                        .filter(accounts::household_id.eq(row.0)),
                )
                .set(accounts::card_last4.eq(decision.card_last4.as_deref()))
                .execute(&mut *conn)
                .await?;
                if updated != 1 {
                    return Err(SetCardLast4PersistenceError::AccountNotFound);
                }

                households::households
                    .filter(households::id.eq(row.0))
                    .for_update()
                    .select(households::id)
                    .first::<Uuid>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(SetCardLast4PersistenceError::AccountNotFound)?;

                diesel::insert_into(events::household_events)
                    .values((
                        events::household_id.eq(row.0),
                        events::event_type.eq(event.event_type),
                        events::entity_kind.eq("account"),
                        events::entity_id.eq(account_id),
                        events::revision.eq(i64::try_from(event.revision)
                            .map_err(|_| SetCardLast4PersistenceError::InvalidDecision)?),
                        events::changed_fields.eq(vec![Some(event.changed_fields[0].to_owned())]),
                        events::actor_id.eq(actor_id),
                        events::created_at.eq(now),
                    ))
                    .execute(&mut *conn)
                    .await?;
            }

            Ok(PersistedSetCardLast4 {
                id: account_id.to_string(),
                name: row.2,
                kind: row.3,
                card_last4: decision.card_last4,
                pool_id: row.1.map(|id| id.to_string()),
            })
        })
        .await
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, SetCardLast4PersistenceError> {
    Uuid::parse_str(value).map_err(|_| SetCardLast4PersistenceError::InvalidUuid(field))
}
