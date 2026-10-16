use chrono::NaiveDate;
use horse_household_change::UiChangeDescriptor;

const MAX_AMOUNT_YEN: i64 = 1_000_000_000_000;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Cadence {
    Weekly,
    Monthly,
    Yearly,
}

impl Cadence {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Weekly => "WEEKLY",
            Self::Monthly => "MONTHLY",
            Self::Yearly => "YEARLY",
        }
    }
    pub fn parse(value: &str) -> Result<Self, RecurringExpenseError> {
        match value {
            "WEEKLY" => Ok(Self::Weekly),
            "MONTHLY" => Ok(Self::Monthly),
            "YEARLY" => Ok(Self::Yearly),
            _ => Err(RecurringExpenseError::InvalidCadence),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecurringExpense {
    pub id: String,
    pub household_id: String,
    pub name: String,
    pub merchant: String,
    pub amount_yen: i64,
    pub currency: String,
    pub category_id: Option<String>,
    pub payment_account_id: Option<String>,
    pub cadence: Cadence,
    pub starts_on: NaiveDate,
    pub next_due_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
    pub archived: bool,
    pub revision: i64,
}

pub const CURRENCY: &str = "JPY";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateRecurringExpenseCommand {
    pub actor_id: String,
    pub household_id: String,
    pub id: String,
    pub name: String,
    pub merchant: String,
    pub amount_yen: i64,
    pub category_id: Option<String>,
    pub payment_account_id: Option<String>,
    pub cadence: String,
    pub starts_on: NaiveDate,
    pub next_due_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateRecurringExpenseCommand {
    pub actor_id: String,
    pub household_id: String,
    pub id: String,
    pub expected_revision: i64,
    pub name: String,
    pub merchant: String,
    pub amount_yen: i64,
    pub category_id: Option<String>,
    pub payment_account_id: Option<String>,
    pub cadence: String,
    pub starts_on: NaiveDate,
    pub next_due_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ArchiveRecurringExpenseCommand {
    pub actor_id: String,
    pub household_id: String,
    pub id: String,
    pub expected_revision: i64,
    pub archived: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MatchRecurringExpenseCommand {
    pub actor_id: String,
    pub household_id: String,
    pub id: String,
    pub purchase_id: String,
    pub expected_revision: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecurringExpenseDecision {
    pub actor_id: String,
    pub id: String,
    pub household_id: Option<String>,
    pub expected_revision: Option<i64>,
    pub name: Option<String>,
    pub merchant: Option<String>,
    pub amount_yen: Option<i64>,
    pub category_id: Option<String>,
    pub payment_account_id: Option<String>,
    pub cadence: Option<Cadence>,
    pub starts_on: Option<NaiveDate>,
    pub next_due_on: Option<NaiveDate>,
    pub ends_on: Option<Option<NaiveDate>>,
    pub archived: Option<bool>,
}

impl RecurringExpenseDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        if self.archived.is_some() {
            UiChangeDescriptor::new(
                "recurring_expense.archived",
                "recurring_expense",
                ["archived"],
            )
        } else {
            UiChangeDescriptor::new(
                "recurring_expense.updated",
                "recurring_expense",
                [
                    "name",
                    "merchant",
                    "amountYen",
                    "categoryId",
                    "paymentAccountId",
                    "cadence",
                    "startsOn",
                    "nextDueOn",
                    "endsOn",
                ],
            )
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MatchRecurringExpenseDecision {
    pub actor_id: String,
    pub household_id: String,
    pub id: String,
    pub purchase_id: String,
    pub expected_revision: i64,
}

impl MatchRecurringExpenseDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        UiChangeDescriptor::new(
            "recurring_expense.matched",
            "recurring_expense",
            ["nextDueOn"],
        )
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RecurringExpenseError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidId,
    InvalidCategoryId,
    InvalidPaymentAccountId,
    InvalidName,
    InvalidMerchant,
    InvalidAmount,
    InvalidCadence,
    InvalidDateRange,
    InvalidRevision,
    InvalidPurchaseId,
}

pub trait RecurringExpenseOperations {
    type Error: From<RecurringExpenseError>;

    async fn persist_create(
        &mut self,
        decision: RecurringExpenseDecision,
    ) -> Result<RecurringExpense, Self::Error>;
    async fn persist_update(
        &mut self,
        decision: RecurringExpenseDecision,
    ) -> Result<RecurringExpense, Self::Error>;
    async fn persist_archive(
        &mut self,
        decision: RecurringExpenseDecision,
    ) -> Result<RecurringExpense, Self::Error>;
    async fn persist_match(
        &mut self,
        decision: MatchRecurringExpenseDecision,
    ) -> Result<RecurringExpense, Self::Error>;
}

pub async fn create_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: CreateRecurringExpenseCommand,
) -> Result<RecurringExpense, O::Error> {
    operations.persist_create(decide_create(command)?).await
}

pub async fn update_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: UpdateRecurringExpenseCommand,
) -> Result<RecurringExpense, O::Error> {
    operations.persist_update(decide_update(command)?).await
}

pub async fn archive_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: ArchiveRecurringExpenseCommand,
) -> Result<RecurringExpense, O::Error> {
    operations.persist_archive(decide_archive(command)?).await
}

pub async fn match_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: MatchRecurringExpenseCommand,
) -> Result<RecurringExpense, O::Error> {
    operations.persist_match(decide_match(command)?).await
}

pub fn decide_match(
    command: MatchRecurringExpenseCommand,
) -> Result<MatchRecurringExpenseDecision, RecurringExpenseError> {
    identity(&command.actor_id, Some(&command.household_id), &command.id)?;
    if command.purchase_id.trim().is_empty() {
        return Err(RecurringExpenseError::InvalidPurchaseId);
    }
    if command.expected_revision <= 0 {
        return Err(RecurringExpenseError::InvalidRevision);
    }
    Ok(MatchRecurringExpenseDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        id: command.id,
        purchase_id: command.purchase_id,
        expected_revision: command.expected_revision,
    })
}

pub fn next_due_on(
    starts_on: NaiveDate,
    due_on: NaiveDate,
    cadence: Cadence,
) -> Result<NaiveDate, RecurringExpenseError> {
    use chrono::Datelike;
    let next = match cadence {
        Cadence::Weekly => due_on.checked_add_days(chrono::Days::new(7)),
        Cadence::Monthly => due_on
            .with_day(1)
            .and_then(|date| date.checked_add_months(chrono::Months::new(1)))
            .and_then(|month_start| {
                let last_day = month_start
                    .checked_add_months(chrono::Months::new(1))?
                    .pred_opt()?
                    .day();
                month_start.with_day(starts_on.day().min(last_day))
            }),
        Cadence::Yearly => NaiveDate::from_ymd_opt(due_on.year() + 1, starts_on.month(), 1)
            .and_then(|year_start| {
                let last_day = year_start
                    .checked_add_months(chrono::Months::new(1))?
                    .pred_opt()?
                    .day();
                year_start.with_day(starts_on.day().min(last_day))
            }),
    }
    .ok_or(RecurringExpenseError::InvalidDateRange)?;
    Ok(next)
}

pub fn decide_create(
    command: CreateRecurringExpenseCommand,
) -> Result<RecurringExpenseDecision, RecurringExpenseError> {
    identity(&command.actor_id, Some(&command.household_id), &command.id)?;
    let fields = validate_fields(
        command.name,
        command.merchant,
        command.category_id,
        command.payment_account_id,
        command.amount_yen,
        &command.cadence,
    )?;
    validate_dates(command.starts_on, command.next_due_on, command.ends_on)?;
    Ok(RecurringExpenseDecision {
        actor_id: command.actor_id,
        id: command.id,
        household_id: Some(command.household_id),
        expected_revision: None,
        name: Some(fields.name),
        merchant: Some(fields.merchant),
        amount_yen: Some(command.amount_yen),
        category_id: fields.category_id,
        payment_account_id: fields.payment_account_id,
        cadence: Some(fields.cadence),
        starts_on: Some(command.starts_on),
        next_due_on: Some(command.next_due_on),
        ends_on: Some(command.ends_on),
        archived: None,
    })
}

pub fn decide_update(
    command: UpdateRecurringExpenseCommand,
) -> Result<RecurringExpenseDecision, RecurringExpenseError> {
    identity(&command.actor_id, Some(&command.household_id), &command.id)?;
    if command.expected_revision <= 0 {
        return Err(RecurringExpenseError::InvalidRevision);
    }
    let fields = validate_fields(
        command.name,
        command.merchant,
        command.category_id,
        command.payment_account_id,
        command.amount_yen,
        &command.cadence,
    )?;
    validate_dates(command.starts_on, command.next_due_on, command.ends_on)?;
    Ok(RecurringExpenseDecision {
        actor_id: command.actor_id,
        id: command.id,
        household_id: Some(command.household_id),
        expected_revision: Some(command.expected_revision),
        name: Some(fields.name),
        merchant: Some(fields.merchant),
        amount_yen: Some(command.amount_yen),
        category_id: fields.category_id,
        payment_account_id: fields.payment_account_id,
        cadence: Some(fields.cadence),
        starts_on: Some(command.starts_on),
        next_due_on: Some(command.next_due_on),
        ends_on: Some(command.ends_on),
        archived: None,
    })
}

pub fn decide_archive(
    command: ArchiveRecurringExpenseCommand,
) -> Result<RecurringExpenseDecision, RecurringExpenseError> {
    identity(&command.actor_id, Some(&command.household_id), &command.id)?;
    if command.expected_revision <= 0 {
        return Err(RecurringExpenseError::InvalidRevision);
    }
    Ok(RecurringExpenseDecision {
        actor_id: command.actor_id,
        id: command.id,
        household_id: Some(command.household_id),
        expected_revision: Some(command.expected_revision),
        name: None,
        merchant: None,
        amount_yen: None,
        category_id: None,
        payment_account_id: None,
        cadence: None,
        starts_on: None,
        next_due_on: None,
        ends_on: None,
        archived: Some(command.archived),
    })
}

fn identity(actor: &str, household: Option<&str>, id: &str) -> Result<(), RecurringExpenseError> {
    if actor.trim().is_empty() {
        return Err(RecurringExpenseError::InvalidActorId);
    }
    if household.is_some_and(|value| value.trim().is_empty()) {
        return Err(RecurringExpenseError::InvalidHouseholdId);
    }
    if id.trim().is_empty() {
        return Err(RecurringExpenseError::InvalidId);
    }
    Ok(())
}
struct ValidatedRecurringExpenseFields {
    name: String,
    merchant: String,
    category_id: Option<String>,
    payment_account_id: Option<String>,
    cadence: Cadence,
}

fn validate_fields(
    name: String,
    merchant: String,
    category: Option<String>,
    account: Option<String>,
    amount: i64,
    cadence: &str,
) -> Result<ValidatedRecurringExpenseFields, RecurringExpenseError> {
    let name = name.trim().to_owned();
    let merchant = merchant.trim().to_owned();
    if name.is_empty() || name.chars().count() > 160 {
        return Err(RecurringExpenseError::InvalidName);
    }
    if merchant.chars().count() > 240 {
        return Err(RecurringExpenseError::InvalidMerchant);
    }
    if !(1..=MAX_AMOUNT_YEN).contains(&amount) {
        return Err(RecurringExpenseError::InvalidAmount);
    }
    let category = validate_id(category, RecurringExpenseError::InvalidCategoryId)?;
    let account = validate_id(account, RecurringExpenseError::InvalidPaymentAccountId)?;
    Ok(ValidatedRecurringExpenseFields {
        name,
        merchant,
        category_id: category,
        payment_account_id: account,
        cadence: Cadence::parse(cadence)?,
    })
}
fn validate_id(
    value: Option<String>,
    error: RecurringExpenseError,
) -> Result<Option<String>, RecurringExpenseError> {
    value
        .map(|id| {
            if id.trim().is_empty() {
                Err(error)
            } else {
                Ok(id)
            }
        })
        .transpose()
}
fn validate_dates(
    starts: NaiveDate,
    due: NaiveDate,
    ends: Option<NaiveDate>,
) -> Result<(), RecurringExpenseError> {
    if due < starts || ends.is_some_and(|end| end < starts) {
        Err(RecurringExpenseError::InvalidDateRange)
    } else {
        Ok(())
    }
}
