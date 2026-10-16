use horse_household_change::UiChangeDescriptor;
use uuid::Uuid;

macro_rules! product_id {
    ($name:ident) => {
        #[derive(Debug, Clone, PartialEq, Eq)]
        pub struct $name(String);

        impl $name {
            fn new(value: String) -> Self {
                Self(value)
            }

            pub fn as_str(&self) -> &str {
                &self.0
            }
        }
    };
}

product_id!(ActorId);
product_id!(HouseholdId);
product_id!(CategoryId);
product_id!(ConceptId);

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductName(String);

impl ProductName {
    fn new(value: String) -> Self {
        Self(value)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NormalizedProductName(String);

impl NormalizedProductName {
    fn from_name(name: &ProductName) -> Self {
        Self(normalize_text(name.as_str()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateConceptCommand {
    pub actor_id: String,
    pub household_id: String,
    pub name: String,
    pub category_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateConceptDecision {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub name: ProductName,
    pub category_id: Option<CategoryId>,
}

impl CreateConceptDecision {
    pub fn normalized_name(&self) -> NormalizedProductName {
        NormalizedProductName::from_name(&self.name)
    }

    pub fn ui_change(&self) -> UiChangeDescriptor {
        UiChangeDescriptor::new(
            "product.concept.created",
            "product_concept",
            ["name", "categoryId"],
        )
    }
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
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub concept_id: ConceptId,
    pub name: ProductName,
    pub brand: Option<String>,
    pub package_size: Option<String>,
    pub barcode: Option<String>,
}

impl CreateVariantDecision {
    pub fn normalized_name(&self) -> NormalizedProductName {
        NormalizedProductName::from_name(&self.name)
    }

    pub fn ui_change(&self) -> UiChangeDescriptor {
        UiChangeDescriptor::new(
            "product.variant.created",
            "product_variant",
            ["name", "conceptId"],
        )
    }
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
    let actor_id = validated_id(command.actor_id)
        .map(ActorId::new)
        .map_err(|_| CreateProductError::InvalidActorId)?;
    let household_id = validated_id(command.household_id)
        .map(HouseholdId::new)
        .map_err(|_| CreateProductError::InvalidHouseholdId)?;
    let category_id = command
        .category_id
        .map(|id| {
            validated_id(id)
                .map(CategoryId::new)
                .map_err(|_| CreateProductError::InvalidCategoryId)
        })
        .transpose()?;
    let name = ProductName::new(validated_name(command.name)?);
    Ok(CreateConceptDecision {
        actor_id,
        household_id,
        name,
        category_id,
    })
}

pub fn decide_create_variant(
    command: CreateVariantCommand,
) -> Result<CreateVariantDecision, CreateProductError> {
    let actor_id = validated_id(command.actor_id)
        .map(ActorId::new)
        .map_err(|_| CreateProductError::InvalidActorId)?;
    let household_id = validated_id(command.household_id)
        .map(HouseholdId::new)
        .map_err(|_| CreateProductError::InvalidHouseholdId)?;
    let concept_id = validated_id(command.concept_id)
        .map(ConceptId::new)
        .map_err(|_| CreateProductError::InvalidConceptId)?;
    if command
        .barcode
        .as_deref()
        .is_some_and(|barcode| barcode.trim().is_empty())
    {
        return Err(CreateProductError::InvalidBarcode);
    }
    let name = ProductName::new(validated_name(command.name)?);
    Ok(CreateVariantDecision {
        actor_id,
        household_id,
        concept_id,
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

fn validated_id(value: String) -> Result<String, ()> {
    Uuid::parse_str(&value).map_err(|_| ())?;
    Ok(value)
}
