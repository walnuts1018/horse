#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateConceptCommand {
    pub actor_id: String,
    pub household_id: String,
    pub name: String,
    pub category_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateConceptDecision {
    pub actor_id: String,
    pub household_id: String,
    pub name: String,
    pub normalized_name: String,
    pub category_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateVariantCommand {
    pub actor_id: String,
    pub household_id: String,
    pub concept_id: String,
    pub name: String,
    pub brand: Option<String>,
    pub package_size: Option<String>,
    pub barcode: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateVariantDecision {
    pub actor_id: String,
    pub household_id: String,
    pub concept_id: String,
    pub name: String,
    pub normalized_name: String,
    pub brand: Option<String>,
    pub package_size: Option<String>,
    pub barcode: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CreateProductError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidCategoryId,
    InvalidConceptId,
    InvalidName,
    InvalidBarcode,
}

pub fn decide_create_concept(
    command: CreateConceptCommand,
) -> Result<CreateConceptDecision, CreateProductError> {
    validate_id(&command.actor_id).map_err(|_| CreateProductError::InvalidActorId)?;
    validate_id(&command.household_id).map_err(|_| CreateProductError::InvalidHouseholdId)?;
    if command
        .category_id
        .as_deref()
        .is_some_and(|id| validate_id(id).is_err())
    {
        return Err(CreateProductError::InvalidCategoryId);
    }
    let name = validated_name(command.name)?;
    Ok(CreateConceptDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        normalized_name: normalize_text(&name),
        name,
        category_id: command.category_id,
    })
}

pub fn decide_create_variant(
    command: CreateVariantCommand,
) -> Result<CreateVariantDecision, CreateProductError> {
    validate_id(&command.actor_id).map_err(|_| CreateProductError::InvalidActorId)?;
    validate_id(&command.household_id).map_err(|_| CreateProductError::InvalidHouseholdId)?;
    validate_id(&command.concept_id).map_err(|_| CreateProductError::InvalidConceptId)?;
    if command
        .barcode
        .as_deref()
        .is_some_and(|barcode| barcode.trim().is_empty())
    {
        return Err(CreateProductError::InvalidBarcode);
    }
    let name = validated_name(command.name)?;
    Ok(CreateVariantDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        concept_id: command.concept_id,
        normalized_name: normalize_text(&name),
        name,
        brand: trim_optional(command.brand),
        package_size: trim_optional(command.package_size),
        barcode: command.barcode.map(|barcode| barcode.trim().to_owned()),
    })
}

fn validated_name(value: String) -> Result<String, CreateProductError> {
    let name = value.trim();
    if name.is_empty() || name.chars().count() > 240 {
        return Err(CreateProductError::InvalidName);
    }
    Ok(name.to_owned())
}

fn normalize_text(value: &str) -> String {
    value
        .split_whitespace()
        .map(str::to_lowercase)
        .collect::<Vec<_>>()
        .join(" ")
}

fn trim_optional(value: Option<String>) -> Option<String> {
    value.map(|value| value.trim().to_owned())
}

fn validate_id(value: &str) -> Result<(), ()> {
    if value.is_empty() { Err(()) } else { Ok(()) }
}
