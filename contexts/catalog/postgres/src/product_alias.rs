use catalog_application::workflow_failure::WorkflowFailure;
use catalog_domain::product_alias::{
    SetProductAliasCommand, SetProductAliasDecision, SetProductAliasError, decide_set_product_alias,
};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use horse_postgres_common::insert_ui_change;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedProductAlias {
    pub id: String,
    pub revision: u64,
}

#[derive(Debug)]
pub enum SetProductAliasPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    NumericOutOfRange(&'static str),
    HouseholdNotFound,
    ActorNotFound,
    VariantNotFound,
    InvalidDecision,
    InvalidWorkflow(SetProductAliasError),
}

impl From<DieselError> for SetProductAliasPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_set_product_alias(
    pool: &PgPool,
    command: SetProductAliasCommand,
) -> Result<
    PersistedProductAlias,
    WorkflowFailure<SetProductAliasError, SetProductAliasPersistenceError>,
> {
    let decision = decide_set_product_alias(command)
        .map_err(SetProductAliasPersistenceError::InvalidWorkflow)?;
    let ui_change = decision.ui_change();
    let household_id = parse_uuid(&decision.household_id, "household_id")?;
    let actor_id = parse_uuid(&decision.actor_id, "actor_id")?;
    let variant_id = parse_uuid(&decision.variant_id, "variant_id")?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SetProductAliasPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedProductAlias, SetProductAliasPersistenceError, _>(
            async move |conn| {
                use public::household_members::dsl as members;
                use public::households::dsl as households;
                use public::product_aliases::dsl as aliases;
                use public::product_variants::dsl as variants;

                households::households
                    .filter(households::id.eq(household_id))
                    .for_update()
                    .select(households::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .ok_or(SetProductAliasPersistenceError::HouseholdNotFound)?;

                let actor_role = members::household_members
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(actor_id))
                    .filter(members::left_at.is_null())
                    .select(members::role)
                    .first::<String>(&mut *conn)
                    .await
                    .optional()?;
                if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
                    return Err(SetProductAliasPersistenceError::ActorNotFound);
                }

                let variant_is_active = variants::product_variants
                    .filter(variants::household_id.eq(household_id))
                    .filter(variants::id.eq(variant_id))
                    .filter(variants::archived.eq(false))
                    .select(variants::id)
                    .first::<Uuid>(&mut *conn)
                    .await
                    .optional()?
                    .is_some();
                if !variant_is_active {
                    return Err(SetProductAliasPersistenceError::VariantNotFound);
                }

                validate_decision(&decision, actor_id, household_id, variant_id)?;
                let proposed_id = Uuid::now_v7();
                let row = diesel::insert_into(aliases::product_aliases)
                    .values((
                        aliases::id.eq(proposed_id),
                        aliases::household_id.eq(household_id),
                        aliases::variant_id.eq(variant_id),
                        aliases::merchant_key.eq(&decision.merchant_key),
                        aliases::alias_text.eq(&decision.alias_text),
                        aliases::normalized_alias.eq(&decision.normalized_alias),
                    ))
                    .on_conflict((
                        aliases::household_id,
                        aliases::merchant_key,
                        aliases::normalized_alias,
                    ))
                    .do_update()
                    .set((
                        aliases::variant_id.eq(variant_id),
                        aliases::alias_text.eq(&decision.alias_text),
                        aliases::revision.eq(aliases::revision + 1),
                        aliases::updated_at.eq(now),
                    ))
                    .returning((aliases::id, aliases::revision))
                    .get_result::<(Uuid, i64)>(&mut *conn)
                    .await?;
                let revision = u64::try_from(row.1)
                    .map_err(|_| SetProductAliasPersistenceError::InvalidDecision)?;
                let event_revision = i64::try_from(revision)
                    .map_err(|_| SetProductAliasPersistenceError::NumericOutOfRange("revision"))?;

                insert_ui_change(
                    conn,
                    household_id,
                    row.0,
                    event_revision,
                    actor_id,
                    chrono::Utc::now(),
                    ui_change,
                )
                .await?;

                Ok(PersistedProductAlias {
                    id: row.0.to_string(),
                    revision,
                })
            },
        )
        .await
        .map_err(map_workflow_failure)
}

fn map_workflow_failure(
    error: SetProductAliasPersistenceError,
) -> WorkflowFailure<SetProductAliasError, SetProductAliasPersistenceError> {
    match error {
        SetProductAliasPersistenceError::InvalidUuid(_)
        | SetProductAliasPersistenceError::NumericOutOfRange(_)
        | SetProductAliasPersistenceError::InvalidDecision => WorkflowFailure::InvalidInput,
        SetProductAliasPersistenceError::InvalidWorkflow(error) => WorkflowFailure::Decision(error),
        SetProductAliasPersistenceError::HouseholdNotFound
        | SetProductAliasPersistenceError::ActorNotFound
        | SetProductAliasPersistenceError::VariantNotFound => WorkflowFailure::NotFound,
        error @ (SetProductAliasPersistenceError::Pool(_)
        | SetProductAliasPersistenceError::Database(_)) => WorkflowFailure::Infrastructure(error),
    }
}

impl From<SetProductAliasPersistenceError>
    for WorkflowFailure<SetProductAliasError, SetProductAliasPersistenceError>
{
    fn from(error: SetProductAliasPersistenceError) -> Self {
        map_workflow_failure(error)
    }
}

fn validate_decision(
    decision: &SetProductAliasDecision,
    actor_id: Uuid,
    household_id: Uuid,
    variant_id: Uuid,
) -> Result<(), SetProductAliasPersistenceError> {
    if parse_uuid(&decision.actor_id, "actor_id")? != actor_id
        || parse_uuid(&decision.household_id, "household_id")? != household_id
        || parse_uuid(&decision.variant_id, "variant_id")? != variant_id
        || decision.merchant_key.is_empty()
        || decision.merchant_key.chars().count() > 240
        || decision.alias_text.trim().is_empty()
        || decision.alias_text.chars().count() > 240
        || decision.normalized_alias.is_empty()
    {
        return Err(SetProductAliasPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, SetProductAliasPersistenceError> {
    Uuid::parse_str(value).map_err(|_| SetProductAliasPersistenceError::InvalidUuid(field))
}
