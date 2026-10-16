use horse_household_change::UiChangeDescriptor;

pub fn category_event(event: &catalog_domain::category::CategoryEvent) -> UiChangeDescriptor {
    let (event_type, fields) = match event {
        catalog_domain::category::CategoryEvent::Created { .. } => {
            ("category.created", ["name", "parentId"].as_slice())
        }
        catalog_domain::category::CategoryEvent::NameUpdated { .. } => {
            ("category.name_updated", ["name"].as_slice())
        }
        catalog_domain::category::CategoryEvent::Archived { .. } => {
            ("category.archived", ["archived"].as_slice())
        }
        catalog_domain::category::CategoryEvent::Unarchived { .. } => {
            ("category.unarchived", ["archived"].as_slice())
        }
    };
    UiChangeDescriptor::new(event_type, "category", fields.iter().copied())
}

pub fn concept_created() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "product.concept.created",
        "product_concept",
        ["name", "categoryId"],
    )
}

pub fn variant_created() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "product.variant.created",
        "product_variant",
        ["name", "conceptId"],
    )
}

pub fn product_alias_learned() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "product.alias.learned",
        "product_alias",
        ["alias", "variantId", "merchant"],
    )
}

pub fn tag_created() -> UiChangeDescriptor {
    UiChangeDescriptor::new("tag.created", "tag", ["name"])
}

pub fn tag_updated(event: &catalog_domain::tag::TagEvent) -> UiChangeDescriptor {
    match event {
        catalog_domain::tag::TagEvent::NameUpdated { .. } => {
            UiChangeDescriptor::new("tag.updated", "tag", ["name"])
        }
        catalog_domain::tag::TagEvent::ArchiveStateChanged { .. } => {
            UiChangeDescriptor::new("tag.archived", "tag", ["archived"])
        }
        catalog_domain::tag::TagEvent::Created { .. } => tag_created(),
    }
}
