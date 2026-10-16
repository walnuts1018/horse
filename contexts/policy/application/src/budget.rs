use chrono::NaiveDate;
use horse_household_change::UiChangeDescriptor;

const MAX_AMOUNT_YEN: i64 = 1_000_000_000_000;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetName(String);

impl BudgetName {
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct BudgetAmountYen(i64);

impl BudgetAmountYen {
    pub fn value(self) -> i64 {
        self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Budget {
    pub id: String,
    pub household_id: String,
    pub category_id: Option<String>,
    pub name: String,
    pub amount_yen: i64,
    pub starts_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
    pub archived: bool,
    pub revision: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateBudgetCommand {
    pub actor_id: String,
    pub household_id: String,
    pub id: String,
    pub category_id: Option<String>,
    pub name: String,
    pub amount_yen: i64,
    pub starts_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateBudgetCommand {
    pub actor_id: String,
    pub household_id: String,
    pub id: String,
    pub expected_revision: i64,
    pub category_id: Option<String>,
    pub name: String,
    pub amount_yen: i64,
    pub starts_on: NaiveDate,
    pub ends_on: Option<Option<NaiveDate>>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ArchiveBudgetCommand {
    pub actor_id: String,
    pub household_id: String,
    pub id: String,
    pub expected_revision: i64,
    pub archived: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateBudgetDecision {
    pub actor_id: String,
    pub id: String,
    pub household_id: String,
    pub category_id: Option<String>,
    pub name: BudgetName,
    pub amount_yen: BudgetAmountYen,
    pub starts_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
}

impl CreateBudgetDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        budget_change(
            "budget.updated",
            ["name", "amountYen", "categoryId", "startsOn", "endsOn"],
        )
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EndDateChange {
    Unchanged,
    Set(Option<NaiveDate>),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateBudgetDecision {
    pub actor_id: String,
    pub id: String,
    pub household_id: String,
    pub expected_revision: i64,
    pub category_id: Option<String>,
    pub name: BudgetName,
    pub amount_yen: BudgetAmountYen,
    pub starts_on: NaiveDate,
    pub ends_on: EndDateChange,
}

impl UpdateBudgetDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        budget_change(
            "budget.updated",
            ["name", "amountYen", "categoryId", "startsOn", "endsOn"],
        )
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ArchiveBudgetDecision {
    pub actor_id: String,
    pub id: String,
    pub household_id: String,
    pub expected_revision: i64,
    pub archived: bool,
}

impl ArchiveBudgetDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        budget_change("budget.archived", ["archived"])
    }
}

fn budget_change(
    event_type: &'static str,
    changed_fields: impl IntoIterator<Item = &'static str>,
) -> UiChangeDescriptor {
    UiChangeDescriptor::new(event_type, "budget", changed_fields)
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BudgetError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidId,
    InvalidCategoryId,
    InvalidName,
    InvalidAmount,
    InvalidDateRange,
    InvalidRevision,
}

pub trait BudgetOperations {
    type Error: From<BudgetError>;

    async fn persist_create(
        &mut self,
        decision: CreateBudgetDecision,
    ) -> Result<Budget, Self::Error>;
    async fn persist_update(
        &mut self,
        decision: UpdateBudgetDecision,
    ) -> Result<Budget, Self::Error>;
    async fn persist_archive(
        &mut self,
        decision: ArchiveBudgetDecision,
    ) -> Result<Budget, Self::Error>;
}

pub async fn create_budget<O: BudgetOperations>(
    operations: &mut O,
    command: CreateBudgetCommand,
) -> Result<Budget, O::Error> {
    operations.persist_create(decide_create(command)?).await
}

pub async fn update_budget<O: BudgetOperations>(
    operations: &mut O,
    command: UpdateBudgetCommand,
) -> Result<Budget, O::Error> {
    operations.persist_update(decide_update(command)?).await
}

pub async fn archive_budget<O: BudgetOperations>(
    operations: &mut O,
    command: ArchiveBudgetCommand,
) -> Result<Budget, O::Error> {
    operations.persist_archive(decide_archive(command)?).await
}

pub fn decide_create(command: CreateBudgetCommand) -> Result<CreateBudgetDecision, BudgetError> {
    validate_identity(&command.actor_id, &command.household_id, &command.id)?;
    let category_id = validate_optional_id(command.category_id)?;
    let name = BudgetName(validate_name(command.name)?);
    let amount_yen = BudgetAmountYen::try_from(command.amount_yen)?;
    validate_dates(command.starts_on, command.ends_on)?;
    Ok(CreateBudgetDecision {
        actor_id: command.actor_id,
        id: command.id,
        household_id: command.household_id,
        category_id,
        name,
        amount_yen,
        starts_on: command.starts_on,
        ends_on: command.ends_on,
    })
}

pub fn decide_update(command: UpdateBudgetCommand) -> Result<UpdateBudgetDecision, BudgetError> {
    validate_identity(&command.actor_id, &command.household_id, &command.id)?;
    validate_revision(command.expected_revision)?;
    let category_id = validate_optional_id(command.category_id)?;
    let name = BudgetName(validate_name(command.name)?);
    let amount_yen = BudgetAmountYen::try_from(command.amount_yen)?;
    if let Some(ends_on) = command.ends_on {
        validate_dates(command.starts_on, ends_on)?;
    }
    Ok(UpdateBudgetDecision {
        actor_id: command.actor_id,
        id: command.id,
        household_id: command.household_id,
        expected_revision: command.expected_revision,
        category_id,
        name,
        amount_yen,
        starts_on: command.starts_on,
        ends_on: command
            .ends_on
            .map_or(EndDateChange::Unchanged, EndDateChange::Set),
    })
}

pub fn decide_archive(command: ArchiveBudgetCommand) -> Result<ArchiveBudgetDecision, BudgetError> {
    validate_identity(&command.actor_id, &command.household_id, &command.id)?;
    validate_revision(command.expected_revision)?;
    Ok(ArchiveBudgetDecision {
        actor_id: command.actor_id,
        id: command.id,
        household_id: command.household_id,
        expected_revision: command.expected_revision,
        archived: command.archived,
    })
}

fn validate_identity(actor: &str, household: &str, id: &str) -> Result<(), BudgetError> {
    validate_actor_id(actor)?;
    if household.trim().is_empty() {
        return Err(BudgetError::InvalidHouseholdId);
    }
    validate_id(id)
}
fn validate_actor_id(value: &str) -> Result<(), BudgetError> {
    if value.trim().is_empty() {
        Err(BudgetError::InvalidActorId)
    } else {
        Ok(())
    }
}
fn validate_id(value: &str) -> Result<(), BudgetError> {
    if value.trim().is_empty() {
        Err(BudgetError::InvalidId)
    } else {
        Ok(())
    }
}
fn validate_optional_id(value: Option<String>) -> Result<Option<String>, BudgetError> {
    value
        .map(|id| {
            if id.trim().is_empty() {
                Err(BudgetError::InvalidCategoryId)
            } else {
                Ok(id)
            }
        })
        .transpose()
}
fn validate_name(value: String) -> Result<String, BudgetError> {
    let value = value.trim().to_owned();
    if value.is_empty() || value.chars().count() > 120 {
        Err(BudgetError::InvalidName)
    } else {
        Ok(value)
    }
}
impl TryFrom<i64> for BudgetAmountYen {
    type Error = BudgetError;

    fn try_from(value: i64) -> Result<Self, Self::Error> {
        if (1..=MAX_AMOUNT_YEN).contains(&value) {
            Ok(Self(value))
        } else {
            Err(BudgetError::InvalidAmount)
        }
    }
}
fn validate_dates(starts: NaiveDate, ends: Option<NaiveDate>) -> Result<(), BudgetError> {
    if ends.is_some_and(|end| end < starts) {
        Err(BudgetError::InvalidDateRange)
    } else {
        Ok(())
    }
}
fn validate_revision(value: i64) -> Result<(), BudgetError> {
    if value > 0 {
        Ok(())
    } else {
        Err(BudgetError::InvalidRevision)
    }
}
