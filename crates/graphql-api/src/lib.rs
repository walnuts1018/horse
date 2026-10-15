use async_graphql::{
    Context, EmptySubscription, Enum, Error, ID, Interface, Object, Result,
    Schema as GraphqlSchema, SimpleObject, Union,
};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use horse_postgres::{PgPool, mail_forwarding as postgres_mail_forwarding};
use household_workflows::mail_forwarding;
use uuid::Uuid;

pub mod account_balances;
pub mod account_creation;
pub mod account_mutations;
pub mod card_last4;
pub mod catalog_mutations;
pub mod category;
pub mod funding;
pub mod household_membership;
pub mod ledger_entry;
pub mod member_pool;
pub mod pagination;
pub mod scalars;
pub use scalars::{DateTime, Long};

pub type Schema = GraphqlSchema<QueryRoot, MutationRoot, EmptySubscription>;

#[derive(Clone, Debug)]
pub struct Config {
    pub mailbox_domain: String,
    pub local_part_prefix: String,
    pub mail_forwarding_enabled: bool,
}

#[derive(Clone, Copy, Debug)]
pub struct Actor(pub Uuid);

pub fn schema(pool: PgPool, config: Config) -> Schema {
    GraphqlSchema::build(QueryRoot, MutationRoot, EmptySubscription)
        .data(pool)
        .data(config)
        .register_output_type::<MutationError>()
        .finish()
}

pub struct QueryRoot;

#[Object]
impl QueryRoot {
    async fn account_balances(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
    ) -> Result<Vec<account_balances::AccountBalance>> {
        account_balances::resolve_account_balances(ctx, household_id).await
    }

    async fn household_mail_forwarding(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
    ) -> Result<Option<HouseholdMailForwardingStatus>> {
        let actor = actor(ctx)?;
        let household_id = decode_household_id(&household_id)?;
        match postgres_mail_forwarding::active(
            ctx.data::<PgPool>()?,
            mail_forwarding::Command {
                actor_id: actor.0.to_string(),
                household_id: household_id.to_string(),
            },
        )
        .await
        {
            Ok(status) => Ok(Some(status.into())),
            Err(postgres_mail_forwarding::MailForwardingError::ForwardingNotFound) => Ok(None),
            Err(postgres_mail_forwarding::MailForwardingError::HouseholdNotFound) => Ok(None),
            Err(error) => Err(internal_error(error)),
        }
    }
}

pub struct MutationRoot;

#[Object]
impl MutationRoot {
    async fn issue_household_mail_forwarding(
        &self,
        ctx: &Context<'_>,
        input: HouseholdMailForwardingInput,
    ) -> Result<IssueHouseholdMailForwardingResult> {
        let config = ctx.data::<Config>()?;
        if !config.mail_forwarding_enabled {
            return Ok(HouseholdMailForwardingUserError::disabled(
                "メール転送先の発行は現在利用できません。",
            )
            .into());
        }
        let actor = actor(ctx)?;
        match issue_or_rotate(ctx, actor, input, false).await {
            Ok(address) => Ok(IssueHouseholdMailForwardingSuccess { result: address }.into()),
            Err(error) => Ok(error.into()),
        }
    }

    async fn rotate_household_mail_forwarding(
        &self,
        ctx: &Context<'_>,
        input: HouseholdMailForwardingInput,
    ) -> Result<RotateHouseholdMailForwardingResult> {
        let config = ctx.data::<Config>()?;
        if !config.mail_forwarding_enabled {
            return Ok(HouseholdMailForwardingUserError::disabled(
                "メール転送先の交換は現在利用できません。",
            )
            .into());
        }
        let actor = actor(ctx)?;
        match issue_or_rotate(ctx, actor, input, true).await {
            Ok(address) => Ok(RotateHouseholdMailForwardingSuccess { result: address }.into()),
            Err(error) => Ok(error.into()),
        }
    }

    async fn revoke_household_mail_forwarding(
        &self,
        ctx: &Context<'_>,
        input: HouseholdMailForwardingInput,
    ) -> Result<RevokeHouseholdMailForwardingResult> {
        let actor = actor(ctx)?;
        let household_id = match decode_household_id(&input.household_id) {
            Ok(value) => value,
            Err(_) => return Ok(bad_input_error().into()),
        };
        match postgres_mail_forwarding::revoke(
            ctx.data::<PgPool>()?,
            mail_forwarding::Command {
                actor_id: actor.0.to_string(),
                household_id: household_id.to_string(),
            },
        )
        .await
        {
            Ok(status) => Ok(RevokeHouseholdMailForwardingSuccess {
                result: status.into(),
            }
            .into()),
            Err(error) => match mutation_user_error(&error) {
                Some(error) => Ok(error.into()),
                None => Err(internal_error(error)),
            },
        }
    }

    async fn create_category(
        &self,
        ctx: &Context<'_>,
        input: category::CreateCategoryInput,
    ) -> Result<category::CreateCategoryResult> {
        category::create_category(ctx, input).await
    }

    async fn update_category_name(
        &self,
        ctx: &Context<'_>,
        input: category::UpdateCategoryNameInput,
    ) -> Result<category::UpdateCategoryNameResult> {
        category::update_category_name(ctx, input).await
    }

    async fn set_category_archived(
        &self,
        ctx: &Context<'_>,
        input: category::SetCategoryArchivedInput,
    ) -> Result<category::SetCategoryArchivedResult> {
        category::set_category_archived(ctx, input).await
    }

    async fn record_account_movement(
        &self,
        ctx: &Context<'_>,
        input: account_mutations::RecordAccountMovementInput,
    ) -> Result<account_mutations::RecordAccountMovementResult> {
        account_mutations::graphql_record_account_movement(ctx, input).await
    }

    async fn record_external_income(
        &self,
        ctx: &Context<'_>,
        input: account_mutations::RecordExternalIncomeInput,
    ) -> Result<account_mutations::RecordExternalIncomeResult> {
        account_mutations::graphql_record_external_income(ctx, input).await
    }

    async fn create_account(
        &self,
        ctx: &Context<'_>,
        input: account_creation::CreateAccountInput,
    ) -> Result<account_creation::CreateAccountResult> {
        account_creation::create_account(ctx, input).await
    }

    async fn record_funding(
        &self,
        ctx: &Context<'_>,
        input: funding::RecordFundingInput,
    ) -> Result<funding::RecordFundingResult> {
        funding::record_funding(ctx, input).await
    }

    async fn leave_household(
        &self,
        ctx: &Context<'_>,
        input: household_membership::LeaveHouseholdInput,
    ) -> Result<household_membership::LeaveHouseholdResult> {
        household_membership::leave_household(ctx, input).await
    }

    async fn remove_household_member(
        &self,
        ctx: &Context<'_>,
        input: household_membership::RemoveHouseholdMemberInput,
    ) -> Result<household_membership::RemoveHouseholdMemberResult> {
        household_membership::remove_household_member(ctx, input).await
    }

    async fn transfer_household_owner(
        &self,
        ctx: &Context<'_>,
        input: household_membership::TransferHouseholdOwnerInput,
    ) -> Result<household_membership::TransferHouseholdOwnerResult> {
        household_membership::transfer_household_owner(ctx, input).await
    }

    async fn create_tag(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::CreateTagInput,
    ) -> Result<catalog_mutations::CreateTagResult> {
        catalog_mutations::create_tag(ctx, input).await
    }

    async fn update_tag(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::UpdateTagInput,
    ) -> Result<catalog_mutations::UpdateTagResult> {
        catalog_mutations::update_tag(ctx, input).await
    }

    async fn archive_tag(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::ArchiveTagInput,
    ) -> Result<catalog_mutations::ArchiveTagResult> {
        catalog_mutations::archive_tag(ctx, input).await
    }

    async fn create_product_concept(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::CreateProductConceptInput,
    ) -> Result<catalog_mutations::CreateProductConceptResult> {
        catalog_mutations::create_product_concept(ctx, input).await
    }

    async fn create_product_variant(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::CreateProductVariantInput,
    ) -> Result<catalog_mutations::CreateProductVariantResult> {
        catalog_mutations::create_product_variant(ctx, input).await
    }

    async fn set_product_alias(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::SetProductAliasInput,
    ) -> Result<catalog_mutations::SetProductAliasResult> {
        catalog_mutations::set_product_alias(ctx, input).await
    }

    async fn set_account_card_last4(
        &self,
        ctx: &Context<'_>,
        input: card_last4::SetAccountCardLast4Input,
    ) -> Result<card_last4::SetAccountCardLast4Result> {
        card_last4::set_account_card_last4(ctx, input).await
    }
}

async fn issue_or_rotate(
    ctx: &Context<'_>,
    actor: Actor,
    input: HouseholdMailForwardingInput,
    rotate: bool,
) -> Result<HouseholdMailForwardingAddress, HouseholdMailForwardingUserError> {
    let household_id = decode_household_id(&input.household_id).map_err(|_| bad_input_error())?;
    let config = ctx.data::<Config>().map_err(|_| bad_input_error())?;
    let command = mail_forwarding::IssueCommand {
        command: mail_forwarding::Command {
            actor_id: actor.0.to_string(),
            household_id: household_id.to_string(),
        },
        mailbox_domain: config.mailbox_domain.clone(),
        local_part_prefix: config.local_part_prefix.clone(),
    };
    let result = if rotate {
        postgres_mail_forwarding::rotate(
            ctx.data::<PgPool>().map_err(|_| bad_input_error())?,
            command,
        )
        .await
    } else {
        postgres_mail_forwarding::issue(
            ctx.data::<PgPool>().map_err(|_| bad_input_error())?,
            command,
        )
        .await
    };
    result
        .map(|address| HouseholdMailForwardingAddress {
            address: address.address,
            status: HouseholdMailForwardingStatus {
                created_at: address.created_at.into(),
                active: true,
            },
        })
        .map_err(|error| mutation_user_error(&error).unwrap_or_else(bad_input_error))
}

pub(crate) fn actor(ctx: &Context<'_>) -> Result<Actor> {
    ctx.data::<Actor>()
        .copied()
        .map_err(|_| Error::new("authentication required"))
}

pub(crate) fn decode_household_id(id: &ID) -> Result<Uuid> {
    let bytes = URL_SAFE_NO_PAD
        .decode(id.as_str())
        .map_err(|_| Error::new("invalid Household node ID"))?;
    let separator = bytes
        .iter()
        .position(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid Household node ID"))?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != b"Household" || payload.len() != 16 {
        return Err(Error::new("invalid Household node ID"));
    }
    let id = Uuid::from_slice(payload).map_err(|_| Error::new("invalid Household node ID"))?;
    if id.is_nil() {
        return Err(Error::new("invalid Household node ID"));
    }
    Ok(id)
}

fn mutation_user_error(
    error: &postgres_mail_forwarding::MailForwardingError,
) -> Option<HouseholdMailForwardingUserError> {
    use postgres_mail_forwarding::MailForwardingError as E;
    match error {
        E::InvalidUuid(_) | E::Workflow(_) => Some(bad_input_error()),
        E::ForwardingNotFound | E::HouseholdNotFound => Some(HouseholdMailForwardingUserError {
            code: MutationErrorCode::NotFound,
            message: "対象が見つかりません。".to_owned(),
        }),
        E::Conflict => Some(HouseholdMailForwardingUserError {
            code: MutationErrorCode::Conflict,
            message: "最新の状態を読み込んでから再試行してください。".to_owned(),
        }),
        E::Pool(_) | E::Database(_) => None,
    }
}

fn bad_input_error() -> HouseholdMailForwardingUserError {
    HouseholdMailForwardingUserError {
        code: MutationErrorCode::BadInput,
        message: "入力を確認してください。".to_owned(),
    }
}

pub(crate) fn internal_error(error: impl std::fmt::Debug) -> Error {
    tracing::error!(error = ?error, "GraphQL operation failed");
    Error::new("internal server error")
}

#[derive(Clone, Debug, SimpleObject)]
pub struct HouseholdMailForwardingStatus {
    created_at: DateTime,
    active: bool,
}

impl From<mail_forwarding::Status> for HouseholdMailForwardingStatus {
    fn from(value: mail_forwarding::Status) -> Self {
        Self {
            created_at: value.created_at.into(),
            active: value.active,
        }
    }
}

#[derive(Clone, Debug, SimpleObject)]
pub struct HouseholdMailForwardingAddress {
    address: String,
    status: HouseholdMailForwardingStatus,
}

#[derive(Clone, Debug, async_graphql::InputObject)]
pub struct HouseholdMailForwardingInput {
    household_id: ID,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum MutationErrorCode {
    BadInput,
    Conflict,
    NotFound,
    MailForwardingDisabled,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct HouseholdMailForwardingUserError {
    code: MutationErrorCode,
    message: String,
}

impl HouseholdMailForwardingUserError {
    fn disabled(message: &str) -> Self {
        Self {
            code: MutationErrorCode::MailForwardingDisabled,
            message: message.to_owned(),
        }
    }
}

#[derive(Interface)]
#[graphql(
    field(name = "code", ty = "&MutationErrorCode"),
    field(name = "message", ty = "&String")
)]
pub enum MutationError {
    HouseholdMailForwardingUserError(HouseholdMailForwardingUserError),
    CreateCategoryUserError(category::CreateCategoryUserError),
    UpdateCategoryNameUserError(category::UpdateCategoryNameUserError),
    SetCategoryArchivedUserError(category::SetCategoryArchivedUserError),
    RecordAccountMovementUserError(account_mutations::RecordAccountMovementUserError),
    RecordExternalIncomeUserError(account_mutations::RecordExternalIncomeUserError),
    CreateAccountUserError(account_creation::CreateAccountUserError),
    RecordFundingUserError(funding::RecordFundingUserError),
    LeaveHouseholdUserError(household_membership::LeaveHouseholdUserError),
    RemoveHouseholdMemberUserError(household_membership::RemoveHouseholdMemberUserError),
    TransferHouseholdOwnerUserError(household_membership::TransferHouseholdOwnerUserError),
    CreateTagUserError(catalog_mutations::CreateTagUserError),
    UpdateTagUserError(catalog_mutations::UpdateTagUserError),
    ArchiveTagUserError(catalog_mutations::ArchiveTagUserError),
    CreateProductConceptUserError(catalog_mutations::CreateProductConceptUserError),
    CreateProductVariantUserError(catalog_mutations::CreateProductVariantUserError),
    SetProductAliasUserError(catalog_mutations::SetProductAliasUserError),
    SetAccountCardLast4UserError(card_last4::SetAccountCardLast4UserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct IssueHouseholdMailForwardingSuccess {
    result: HouseholdMailForwardingAddress,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RotateHouseholdMailForwardingSuccess {
    result: HouseholdMailForwardingAddress,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RevokeHouseholdMailForwardingSuccess {
    result: HouseholdMailForwardingStatus,
}

#[derive(Union)]
pub enum IssueHouseholdMailForwardingResult {
    Success(IssueHouseholdMailForwardingSuccess),
    UserError(HouseholdMailForwardingUserError),
}

#[derive(Union)]
pub enum RotateHouseholdMailForwardingResult {
    Success(RotateHouseholdMailForwardingSuccess),
    UserError(HouseholdMailForwardingUserError),
}

#[derive(Union)]
pub enum RevokeHouseholdMailForwardingResult {
    Success(RevokeHouseholdMailForwardingSuccess),
    UserError(HouseholdMailForwardingUserError),
}
