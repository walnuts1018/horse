use async_graphql::{
    Context, Enum, ID, InputObject, InputValueError, InputValueResult, Result, Scalar, ScalarType,
    SimpleObject, Union, Value,
};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use chrono::NaiveDate;
use horse_postgres::{PgPool, policy as postgres_policy, policy_query as postgres_policy_query};
use policy_workflows::{budget, recurring_expense};
use uuid::Uuid;

use crate::{Long, MutationErrorCode, pagination::PageInfo};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Date(pub NaiveDate);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct YearMonth(String);

impl YearMonth {
    pub fn from_naive_date(value: NaiveDate) -> Self {
        Self(value.format("%Y-%m").to_string())
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[Scalar(name = "YearMonth")]
impl ScalarType for YearMonth {
    fn parse(value: Value) -> InputValueResult<Self> {
        match value {
            Value::String(value) => {
                let bytes = value.as_bytes();
                if bytes.len() == 7
                    && bytes[4] == b'-'
                    && bytes[..4].iter().all(u8::is_ascii_digit)
                    && bytes[5..].iter().all(u8::is_ascii_digit)
                    && NaiveDate::parse_from_str(&format!("{value}-01"), "%Y-%m-%d").is_ok()
                {
                    Ok(Self(value))
                } else {
                    Err(InputValueError::custom("YearMonth must use YYYY-MM"))
                }
            }
            value => Err(InputValueError::expected_type(value)),
        }
    }

    fn to_value(&self) -> Value {
        Value::String(self.0.clone())
    }
}

#[Scalar(name = "Date")]
impl ScalarType for Date {
    fn parse(value: Value) -> InputValueResult<Self> {
        match value {
            Value::String(value) => NaiveDate::parse_from_str(&value, "%Y-%m-%d")
                .map(Self)
                .map_err(|_| InputValueError::custom("Date must use YYYY-MM-DD")),
            value => Err(InputValueError::expected_type(value)),
        }
    }

    fn to_value(&self) -> Value {
        Value::String(self.0.format("%Y-%m-%d").to_string())
    }
}

#[derive(Clone, Debug, InputObject)]
pub struct CreateBudgetInput {
    pub household_id: ID,
    pub category_id: Option<ID>,
    pub name: String,
    pub amount_yen: Long,
    pub starts_on: Date,
    pub ends_on: Option<Date>,
}

#[derive(Clone, Debug, InputObject)]
pub struct UpdateBudgetInput {
    pub household_id: ID,
    pub budget_id: ID,
    pub base_revision: i32,
    pub category_id: Option<ID>,
    pub name: String,
    pub amount_yen: Long,
    pub starts_on: Date,
    pub ends_on: Option<Date>,
}

#[derive(Clone, Debug, InputObject)]
pub struct ArchiveBudgetInput {
    pub budget_id: ID,
    pub base_revision: i32,
    pub archived: bool,
}

#[derive(Clone, Debug, InputObject)]
pub struct CreateRecurringExpenseInput {
    pub household_id: ID,
    pub name: String,
    pub merchant: String,
    pub amount_yen: Long,
    pub category_id: Option<ID>,
    pub payment_account_id: Option<ID>,
    pub cadence: RecurringExpenseCadence,
    pub starts_on: Date,
    pub next_due_on: Date,
    pub ends_on: Option<Date>,
}

#[derive(Clone, Debug, InputObject)]
pub struct UpdateRecurringExpenseInput {
    pub household_id: ID,
    pub expense_id: ID,
    pub base_revision: i32,
    pub name: String,
    pub merchant: String,
    pub amount_yen: Long,
    pub category_id: Option<ID>,
    pub payment_account_id: Option<ID>,
    pub cadence: RecurringExpenseCadence,
    pub starts_on: Date,
    pub next_due_on: Date,
    pub ends_on: Option<Date>,
}

#[derive(Clone, Debug, InputObject)]
pub struct ArchiveRecurringExpenseInput {
    pub expense_id: ID,
    pub base_revision: i32,
    pub archived: bool,
}

#[derive(Clone, Debug, InputObject)]
pub struct MatchRecurringExpenseInput {
    pub household_id: ID,
    pub expense_id: ID,
    pub purchase_id: ID,
    pub base_revision: i32,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum RecurringExpenseCadence {
    Weekly,
    Monthly,
    Yearly,
}

impl From<RecurringExpenseCadence> for recurring_expense::Cadence {
    fn from(value: RecurringExpenseCadence) -> Self {
        match value {
            RecurringExpenseCadence::Weekly => Self::Weekly,
            RecurringExpenseCadence::Monthly => Self::Monthly,
            RecurringExpenseCadence::Yearly => Self::Yearly,
        }
    }
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "Budget")]
pub struct BudgetObject {
    pub id: ID,
    pub household_id: ID,
    pub category_id: Option<ID>,
    pub name: String,
    pub amount_yen: Long,
    pub actual_yen: Long,
    pub remaining_yen: Long,
    pub starts_on: Date,
    pub ends_on: Option<Date>,
    pub archived: bool,
    pub revision: i32,
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "RecurringExpense")]
pub struct RecurringExpenseObject {
    pub id: ID,
    pub household_id: ID,
    pub name: String,
    pub merchant: String,
    pub amount_yen: Long,
    pub currency: String,
    pub category_id: Option<ID>,
    pub payment_account_id: Option<ID>,
    pub cadence: RecurringExpenseCadence,
    pub starts_on: Date,
    pub next_due_on: Date,
    pub ends_on: Option<Date>,
    pub archived: bool,
    pub revision: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct BudgetEdge {
    pub cursor: String,
    pub node: BudgetObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct BudgetConnection {
    pub edges: Vec<BudgetEdge>,
    pub page_info: PageInfo,
    pub total_count: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecurringExpenseEdge {
    pub cursor: String,
    pub node: RecurringExpenseObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecurringExpenseConnection {
    pub edges: Vec<RecurringExpenseEdge>,
    pub page_info: PageInfo,
    pub total_count: i32,
}

macro_rules! success_type {
    ($name:ident, $object:ty) => {
        #[derive(Clone, Debug, SimpleObject)]
        pub struct $name {
            pub result: $object,
        }
    };
}

macro_rules! error_type {
    ($name:ident) => {
        #[derive(Clone, Debug, SimpleObject)]
        pub struct $name {
            pub code: MutationErrorCode,
            pub message: String,
        }

        impl From<MutationErrorCode> for $name {
            fn from(code: MutationErrorCode) -> Self {
                Self {
                    code,
                    message: policy_error_message(code).to_owned(),
                }
            }
        }
    };
}

success_type!(CreateBudgetSuccess, BudgetObject);
success_type!(UpdateBudgetSuccess, BudgetObject);
success_type!(ArchiveBudgetSuccess, BudgetObject);
success_type!(CreateRecurringExpenseSuccess, RecurringExpenseObject);
success_type!(UpdateRecurringExpenseSuccess, RecurringExpenseObject);
success_type!(ArchiveRecurringExpenseSuccess, RecurringExpenseObject);
success_type!(MatchRecurringExpenseSuccess, RecurringExpenseObject);

error_type!(CreateBudgetUserError);
error_type!(UpdateBudgetUserError);
error_type!(ArchiveBudgetUserError);
error_type!(CreateRecurringExpenseUserError);
error_type!(UpdateRecurringExpenseUserError);
error_type!(ArchiveRecurringExpenseUserError);
error_type!(MatchRecurringExpenseUserError);

macro_rules! result_union {
    ($name:ident, $success:ident, $error:ident) => {
        #[derive(Union)]
        pub enum $name {
            Success($success),
            UserError($error),
        }
    };
}

result_union!(
    CreateBudgetResult,
    CreateBudgetSuccess,
    CreateBudgetUserError
);
result_union!(
    UpdateBudgetResult,
    UpdateBudgetSuccess,
    UpdateBudgetUserError
);
result_union!(
    ArchiveBudgetResult,
    ArchiveBudgetSuccess,
    ArchiveBudgetUserError
);
result_union!(
    CreateRecurringExpenseResult,
    CreateRecurringExpenseSuccess,
    CreateRecurringExpenseUserError
);

macro_rules! result_error_conversion {
    ($union:ident, $error:ident) => {
        impl From<MutationErrorCode> for $union {
            fn from(code: MutationErrorCode) -> Self {
                Self::UserError($error::from(code))
            }
        }
    };
}

result_error_conversion!(CreateBudgetResult, CreateBudgetUserError);
result_error_conversion!(UpdateBudgetResult, UpdateBudgetUserError);
result_error_conversion!(ArchiveBudgetResult, ArchiveBudgetUserError);
result_error_conversion!(
    CreateRecurringExpenseResult,
    CreateRecurringExpenseUserError
);
result_error_conversion!(
    UpdateRecurringExpenseResult,
    UpdateRecurringExpenseUserError
);
result_error_conversion!(
    ArchiveRecurringExpenseResult,
    ArchiveRecurringExpenseUserError
);
result_error_conversion!(MatchRecurringExpenseResult, MatchRecurringExpenseUserError);
result_union!(
    UpdateRecurringExpenseResult,
    UpdateRecurringExpenseSuccess,
    UpdateRecurringExpenseUserError
);
result_union!(
    ArchiveRecurringExpenseResult,
    ArchiveRecurringExpenseSuccess,
    ArchiveRecurringExpenseUserError
);
result_union!(
    MatchRecurringExpenseResult,
    MatchRecurringExpenseSuccess,
    MatchRecurringExpenseUserError
);

pub async fn create_budget(
    ctx: &Context<'_>,
    input: CreateBudgetInput,
) -> Result<CreateBudgetResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match decode_node_id(&input.household_id, "Household") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let category_id = match optional_node_id(input.category_id.as_ref(), "Category") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let command = budget::CreateBudgetCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        id: Uuid::now_v7().to_string(),
        category_id: category_id.map(|id| id.to_string()),
        name: input.name,
        amount_yen: input.amount_yen.0,
        starts_on: input.starts_on.0,
        ends_on: input.ends_on.map(|value| value.0),
    };
    let pool = ctx.data::<PgPool>()?;
    match postgres_policy::persist_create_budget(pool, command).await {
        Ok(value) => {
            let result = load_budget_result(
                pool,
                actor.0,
                Uuid::parse_str(&value.id).map_err(crate::internal_error)?,
            )
            .await?;
            Ok(CreateBudgetSuccess { result }.into())
        }
        Err(error) => policy_error(error),
    }
}

pub async fn update_budget(
    ctx: &Context<'_>,
    input: UpdateBudgetInput,
) -> Result<UpdateBudgetResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match decode_node_id(&input.household_id, "Household") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let budget_id = match decode_node_id(&input.budget_id, "Budget") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let category_id = match optional_node_id(input.category_id.as_ref(), "Category") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let command = budget::UpdateBudgetCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        id: budget_id.to_string(),
        expected_revision: i64::from(input.base_revision),
        category_id: category_id.map(|id| id.to_string()),
        name: input.name,
        amount_yen: input.amount_yen.0,
        starts_on: input.starts_on.0,
        ends_on: input.ends_on.map(|value| value.0),
    };
    let pool = ctx.data::<PgPool>()?;
    match postgres_policy::persist_update_budget(pool, command).await {
        Ok(value) => {
            let result = load_budget_result(
                pool,
                actor.0,
                Uuid::parse_str(&value.id).map_err(crate::internal_error)?,
            )
            .await?;
            Ok(UpdateBudgetSuccess { result }.into())
        }
        Err(error) => policy_error(error),
    }
}

pub async fn archive_budget(
    ctx: &Context<'_>,
    input: ArchiveBudgetInput,
) -> Result<ArchiveBudgetResult> {
    let actor = crate::actor(ctx)?;
    let budget_id = match decode_node_id(&input.budget_id, "Budget") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let pool = ctx.data::<PgPool>()?;
    let household_id = match postgres_policy_query::budget_household_id(pool, budget_id).await {
        Ok(value) => value,
        Err(error) => {
            return policy_query_error(error);
        }
    };
    let command = budget::ArchiveBudgetCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        id: budget_id.to_string(),
        expected_revision: i64::from(input.base_revision),
        archived: input.archived,
    };
    match postgres_policy::persist_archive_budget(pool, command).await {
        Ok(value) => {
            let result = load_budget_result(
                pool,
                actor.0,
                Uuid::parse_str(&value.id).map_err(crate::internal_error)?,
            )
            .await?;
            Ok(ArchiveBudgetSuccess { result }.into())
        }
        Err(error) => policy_error(error),
    }
}

pub async fn create_recurring_expense(
    ctx: &Context<'_>,
    input: CreateRecurringExpenseInput,
) -> Result<CreateRecurringExpenseResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match decode_node_id(&input.household_id, "Household") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let category_id = match optional_node_id(input.category_id.as_ref(), "Category") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let payment_account_id = match optional_node_id(input.payment_account_id.as_ref(), "Account") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let command = recurring_expense::CreateRecurringExpenseCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        id: Uuid::now_v7().to_string(),
        name: input.name,
        merchant: input.merchant,
        amount_yen: input.amount_yen.0,
        category_id: category_id.map(|id| id.to_string()),
        payment_account_id: payment_account_id.map(|id| id.to_string()),
        cadence: input.cadence.into().as_str().to_owned(),
        starts_on: input.starts_on.0,
        next_due_on: input.next_due_on.0,
        ends_on: input.ends_on.map(|value| value.0),
    };
    match postgres_policy::persist_create_recurring_expense(ctx.data::<PgPool>()?, command).await {
        Ok(value) => Ok(CreateRecurringExpenseSuccess {
            result: value.into(),
        }
        .into()),
        Err(error) => policy_error(error),
    }
}

pub async fn update_recurring_expense(
    ctx: &Context<'_>,
    input: UpdateRecurringExpenseInput,
) -> Result<UpdateRecurringExpenseResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match decode_node_id(&input.household_id, "Household") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let expense_id = match decode_node_id(&input.expense_id, "RecurringExpense") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let category_id = match optional_node_id(input.category_id.as_ref(), "Category") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let payment_account_id = match optional_node_id(input.payment_account_id.as_ref(), "Account") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let command = recurring_expense::UpdateRecurringExpenseCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        id: expense_id.to_string(),
        expected_revision: i64::from(input.base_revision),
        name: input.name,
        merchant: input.merchant,
        amount_yen: input.amount_yen.0,
        category_id: category_id.map(|id| id.to_string()),
        payment_account_id: payment_account_id.map(|id| id.to_string()),
        cadence: input.cadence.into().as_str().to_owned(),
        starts_on: input.starts_on.0,
        next_due_on: input.next_due_on.0,
        ends_on: input.ends_on.map(|value| value.0),
    };
    match postgres_policy::persist_update_recurring_expense(ctx.data::<PgPool>()?, command).await {
        Ok(value) => Ok(UpdateRecurringExpenseSuccess {
            result: value.into(),
        }
        .into()),
        Err(error) => policy_error(error),
    }
}

pub async fn archive_recurring_expense(
    ctx: &Context<'_>,
    input: ArchiveRecurringExpenseInput,
) -> Result<ArchiveRecurringExpenseResult> {
    let actor = crate::actor(ctx)?;
    let expense_id = match decode_node_id(&input.expense_id, "RecurringExpense") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let household_id = match postgres_policy_query::recurring_expense_household_id(
        ctx.data::<PgPool>()?,
        expense_id,
    )
    .await
    {
        Ok(value) => value,
        Err(error) => {
            return policy_query_error(error);
        }
    };
    let command = recurring_expense::ArchiveRecurringExpenseCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        id: expense_id.to_string(),
        expected_revision: i64::from(input.base_revision),
        archived: input.archived,
    };
    match postgres_policy::persist_archive_recurring_expense(ctx.data::<PgPool>()?, command).await {
        Ok(value) => Ok(ArchiveRecurringExpenseSuccess {
            result: value.into(),
        }
        .into()),
        Err(error) => policy_error(error),
    }
}

pub async fn match_recurring_expense(
    ctx: &Context<'_>,
    input: MatchRecurringExpenseInput,
) -> Result<MatchRecurringExpenseResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match decode_node_id(&input.household_id, "Household") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let expense_id = match decode_node_id(&input.expense_id, "RecurringExpense") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let purchase_id = match decode_node_id(&input.purchase_id, "Purchase") {
        Ok(value) => value,
        Err(()) => return Ok(MutationErrorCode::BadInput.into()),
    };
    let command = recurring_expense::MatchRecurringExpenseCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        id: expense_id.to_string(),
        purchase_id: purchase_id.to_string(),
        expected_revision: i64::from(input.base_revision),
    };
    match postgres_policy::persist_match_recurring_expense(ctx.data::<PgPool>()?, command).await {
        Ok(value) => Ok(MatchRecurringExpenseSuccess {
            result: value.into(),
        }
        .into()),
        Err(error) => policy_error(error),
    }
}

pub async fn graphql_budgets(
    ctx: &Context<'_>,
    household_id: ID,
    period: YearMonth,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<BudgetConnection> {
    let actor = crate::actor(ctx)?;
    let household_id = decode_node_id(&household_id, "Household")
        .map_err(|_| async_graphql::Error::new("invalid household ID"))?;
    let after_id = decode_cursor(after.as_deref(), "Budget")?;
    let before_id = decode_cursor(before.as_deref(), "Budget")?;
    let page = postgres_policy_query::budgets(
        ctx.data::<PgPool>()?,
        actor.0,
        household_id,
        &period.0,
        after_id,
        before_id,
        first,
        last,
    )
    .await
    .map_err(policy_query_graphql_error)?;
    let edges = page
        .rows
        .into_iter()
        .map(|row| {
            let cursor = encode_cursor("Budget", &row.budget.id);
            BudgetEdge {
                cursor,
                node: row.into(),
            }
        })
        .collect::<Vec<_>>();
    let total_count =
        i32::try_from(page.total_count).map_err(|error| crate::internal_error(error))?;
    Ok(BudgetConnection {
        page_info: PageInfo {
            has_next_page: page.has_next_page,
            has_previous_page: page.has_previous_page,
            start_cursor: edges.first().map(|edge| edge.cursor.clone()),
            end_cursor: edges.last().map(|edge| edge.cursor.clone()),
        },
        edges,
        total_count,
    })
}

pub async fn graphql_recurring_expenses(
    ctx: &Context<'_>,
    household_id: ID,
    include_archived: bool,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<RecurringExpenseConnection> {
    let actor = crate::actor(ctx)?;
    let household_id = decode_node_id(&household_id, "Household")
        .map_err(|_| async_graphql::Error::new("invalid household ID"))?;
    let after_id = decode_cursor(after.as_deref(), "RecurringExpense")?;
    let before_id = decode_cursor(before.as_deref(), "RecurringExpense")?;
    let page = postgres_policy_query::recurring_expenses(
        ctx.data::<PgPool>()?,
        actor.0,
        household_id,
        include_archived,
        after_id,
        before_id,
        first,
        last,
    )
    .await
    .map_err(policy_query_graphql_error)?;
    let edges = page
        .rows
        .into_iter()
        .map(|value| RecurringExpenseEdge {
            cursor: encode_cursor("RecurringExpense", &value.id),
            node: value.into(),
        })
        .collect::<Vec<_>>();
    let total_count =
        i32::try_from(page.total_count).map_err(|error| crate::internal_error(error))?;
    Ok(RecurringExpenseConnection {
        page_info: PageInfo {
            has_next_page: page.has_next_page,
            has_previous_page: page.has_previous_page,
            start_cursor: edges.first().map(|edge| edge.cursor.clone()),
            end_cursor: edges.last().map(|edge| edge.cursor.clone()),
        },
        edges,
        total_count,
    })
}

impl From<postgres_policy_query::BudgetReadModel> for BudgetObject {
    fn from(value: postgres_policy_query::BudgetReadModel) -> Self {
        let actual_yen = value.actual_yen;
        let remaining_yen = value.remaining_yen;
        let projection = value.budget;
        Self {
            id: encode_node_id("Budget", &projection.id)
                .expect("persisted budget id must be a UUID")
                .into(),
            household_id: encode_node_id("Household", &projection.household_id)
                .expect("persisted household id must be a UUID")
                .into(),
            category_id: projection
                .category_id
                .as_deref()
                .and_then(|id| encode_node_id("Category", id).ok())
                .map(Into::into),
            name: projection.name,
            amount_yen: Long(projection.amount_yen),
            actual_yen: Long(actual_yen),
            remaining_yen: Long(remaining_yen),
            starts_on: Date(projection.starts_on),
            ends_on: projection.ends_on.map(Date),
            archived: projection.archived,
            revision: projection.revision as i32,
        }
    }
}

impl From<recurring_expense::RecurringExpense> for RecurringExpenseObject {
    fn from(value: recurring_expense::RecurringExpense) -> Self {
        Self {
            id: encode_node_id("RecurringExpense", &value.id)
                .expect("persisted recurring expense id must be a UUID")
                .into(),
            household_id: encode_node_id("Household", &value.household_id)
                .expect("persisted household id must be a UUID")
                .into(),
            name: value.name,
            merchant: value.merchant,
            amount_yen: Long(value.amount_yen),
            currency: value.currency,
            category_id: value
                .category_id
                .as_deref()
                .and_then(|id| encode_node_id("Category", id).ok())
                .map(Into::into),
            payment_account_id: value
                .payment_account_id
                .as_deref()
                .and_then(|id| encode_node_id("Account", id).ok())
                .map(Into::into),
            cadence: match value.cadence {
                recurring_expense::Cadence::Weekly => RecurringExpenseCadence::Weekly,
                recurring_expense::Cadence::Monthly => RecurringExpenseCadence::Monthly,
                recurring_expense::Cadence::Yearly => RecurringExpenseCadence::Yearly,
            },
            starts_on: Date(value.starts_on),
            next_due_on: Date(value.next_due_on),
            ends_on: value.ends_on.map(Date),
            archived: value.archived,
            revision: value.revision as i32,
        }
    }
}

fn decode_node_id(value: &ID, expected_kind: &str) -> std::result::Result<Uuid, ()> {
    let bytes = URL_SAFE_NO_PAD.decode(value.as_str()).map_err(|_| ())?;
    let separator = bytes.iter().position(|byte| *byte == b':').ok_or(())?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != expected_kind.as_bytes() || payload.len() != 16 {
        return Err(());
    }
    let id = Uuid::from_slice(payload).map_err(|_| ())?;
    if id.is_nil() {
        return Err(());
    }
    Ok(id)
}

fn decode_cursor(cursor: Option<&str>, expected_kind: &str) -> Result<Option<Uuid>> {
    let Some(cursor) = cursor.filter(|cursor| !cursor.is_empty()) else {
        return Ok(None);
    };
    let decoded = URL_SAFE_NO_PAD
        .decode(cursor)
        .map_err(|_| async_graphql::Error::new("invalid cursor"))?;
    let decoded =
        std::str::from_utf8(&decoded).map_err(|_| async_graphql::Error::new("invalid cursor"))?;
    let (kind, id) = decoded
        .split_once(':')
        .ok_or_else(|| async_graphql::Error::new("invalid cursor"))?;
    if kind != expected_kind {
        return Err(async_graphql::Error::new("invalid cursor type"));
    }
    Uuid::parse_str(id)
        .map(Some)
        .map_err(|_| async_graphql::Error::new("invalid cursor"))
}

fn encode_cursor(kind: &str, raw_id: &str) -> String {
    URL_SAFE_NO_PAD.encode(format!("{kind}:{raw_id}"))
}

fn optional_node_id(
    value: Option<&ID>,
    expected_kind: &str,
) -> std::result::Result<Option<Uuid>, ()> {
    value
        .map(|value| decode_node_id(value, expected_kind))
        .transpose()
}

fn encode_node_id(kind: &str, raw_id: &str) -> std::result::Result<String, ()> {
    let id = Uuid::parse_str(raw_id).map_err(|_| ())?;
    if id.is_nil() {
        return Err(());
    }
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    Ok(URL_SAFE_NO_PAD.encode(payload))
}

fn policy_error<T: From<MutationErrorCode>>(
    error: postgres_policy::PolicyPersistenceError,
) -> Result<T> {
    use postgres_policy::PolicyPersistenceError as Error;
    match error {
        Error::InvalidUuid(_) | Error::InvalidWorkflow => Ok(MutationErrorCode::BadInput.into()),
        Error::Conflict => Ok(MutationErrorCode::Conflict.into()),
        Error::HouseholdNotFound
        | Error::MemberNotFound
        | Error::BudgetNotFound
        | Error::RecurringExpenseNotFound
        | Error::PurchaseNotFound
        | Error::CategoryNotFound
        | Error::AccountNotFound => Ok(MutationErrorCode::NotFound.into()),
        error @ (Error::Pool(_) | Error::Database(_)) => Err(crate::internal_error(error)),
    }
}

fn policy_error_message(code: MutationErrorCode) -> &'static str {
    match code {
        MutationErrorCode::BadInput => "入力を確認してください。",
        MutationErrorCode::Conflict => "データが更新されています。再読み込みしてください。",
        MutationErrorCode::NotFound => "対象が見つかりません。",
        MutationErrorCode::MailForwardingDisabled => "操作を完了できませんでした。",
    }
}

async fn load_budget_result(
    pool: &PgPool,
    actor_id: Uuid,
    budget_id: Uuid,
) -> Result<BudgetObject> {
    postgres_policy_query::budget_for_current_period(pool, actor_id, budget_id)
        .await
        .map(Into::into)
        .map_err(crate::internal_error)
}

fn policy_query_error<T: From<MutationErrorCode>>(
    error: postgres_policy_query::PolicyQueryError,
) -> Result<T> {
    use postgres_policy_query::PolicyQueryError as Error;
    match error {
        Error::InvalidUuid | Error::InvalidPeriod | Error::InvalidPagination => {
            Ok(MutationErrorCode::BadInput.into())
        }
        Error::NotFound | Error::MemberNotFound => Ok(MutationErrorCode::NotFound.into()),
        error @ (Error::Pool(_) | Error::Database(_) | Error::AmountOverflow) => {
            Err(crate::internal_error(error))
        }
    }
}

fn policy_query_graphql_error(
    error: postgres_policy_query::PolicyQueryError,
) -> async_graphql::Error {
    use postgres_policy_query::PolicyQueryError as Error;
    match error {
        Error::InvalidUuid | Error::InvalidPeriod | Error::InvalidPagination => {
            async_graphql::Error::new("invalid query input")
        }
        Error::NotFound => async_graphql::Error::new("resource not found"),
        Error::MemberNotFound => async_graphql::Error::new("household not found"),
        error @ (Error::Pool(_) | Error::Database(_) | Error::AmountOverflow) => {
            crate::internal_error(error)
        }
    }
}
