use bookkeeping_application::account_card_last4::{
    AccountCardLast4Projection, SetCardLast4Future, SetCardLast4Handler, SetCardLast4Request,
};
use bookkeeping_application::workflow_failure::WorkflowFailure;
use bookkeeping_domain::account_card_last4::{
    SetCardLast4Command, SetCardLast4Error, decide_set_card_last4,
};
use chrono::Utc;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use uuid::Uuid;

pub struct PostgresSetCardLast4Handler {
    pool: PgPool,
}

impl PostgresSetCardLast4Handler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl SetCardLast4Handler for PostgresSetCardLast4Handler {
    fn handle<'a>(&'a self, request: SetCardLast4Request) -> SetCardLast4Future<'a> {
        Box::pin(async move {
            let account = persist_set_card_last4(
                &self.pool,
                &request.actor_id.to_string(),
                &request.account_id.to_string(),
                request.card_last4,
            )
            .await
            .map_err(|failure| match failure {
                WorkflowFailure::Decision(error) => WorkflowFailure::Decision(error),
                WorkflowFailure::NotFound => WorkflowFailure::NotFound,
                WorkflowFailure::Conflict => WorkflowFailure::Conflict,
                WorkflowFailure::Infrastructure(error) => {
                    WorkflowFailure::Infrastructure(format!("{error:?}"))
                }
            })?;
            let id = Uuid::parse_str(&account.id)
                .map_err(|error| WorkflowFailure::Infrastructure(error.to_string()))?;
            let pool_id = account
                .pool_id
                .as_deref()
                .map(Uuid::parse_str)
                .transpose()
                .map_err(|error| WorkflowFailure::Infrastructure(error.to_string()))?;
            Ok(AccountCardLast4Projection {
                id,
                name: account.name,
                kind: account.kind,
                card_last4: account.card_last4,
                pool_id,
            })
        })
    }
}

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
) -> Result<PersistedSetCardLast4, WorkflowFailure<SetCardLast4Error, SetCardLast4PersistenceError>>
{
    persist_set_card_last4_inner(pool, actor_id, account_id, card_last4)
        .await
        .map_err(|error| match error {
            SetCardLast4PersistenceError::InvalidWorkflow(error) => {
                WorkflowFailure::Decision(error)
            }
            SetCardLast4PersistenceError::AccountNotFound => WorkflowFailure::NotFound,
            error => WorkflowFailure::Infrastructure(error),
        })
}

async fn persist_set_card_last4_inner(
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

            let actor_role = household_postgres::household_access::active_member_role(
                &mut *conn, row.0, actor_id,
            )
            .await?;
            if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
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

                if !household_postgres::household_access::lock_household_for_update(
                    &mut *conn, row.0,
                )
                .await?
                {
                    return Err(SetCardLast4PersistenceError::AccountNotFound);
                }

                horse_postgres_common::insert_ui_change(
                    &mut *conn,
                    row.0,
                    account_id,
                    i64::try_from(event.revision)
                        .map_err(|_| SetCardLast4PersistenceError::InvalidDecision)?,
                    actor_id,
                    Utc::now(),
                    bookkeeping_application::ui_changes::account_card_last4_updated(),
                )
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
