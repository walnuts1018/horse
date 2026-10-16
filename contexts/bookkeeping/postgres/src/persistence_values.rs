use bookkeeping_domain::purchase_commands::{DatePrecision, PurchaseOrigin, QuantityUnit};

pub(super) const fn date_precision(value: DatePrecision) -> &'static str {
    match value {
        DatePrecision::DateTime => "DATE_TIME",
        DatePrecision::DateOnly => "DATE_ONLY",
    }
}

pub(super) const fn purchase_origin(value: PurchaseOrigin) -> &'static str {
    match value {
        PurchaseOrigin::AiAssisted => "AI",
        PurchaseOrigin::Manual => "MANUAL",
        PurchaseOrigin::Unknown => "UNKNOWN",
    }
}

pub(super) const fn quantity_unit(value: QuantityUnit) -> &'static str {
    match value {
        QuantityUnit::Piece => "個",
        QuantityUnit::Bottle => "本",
        QuantityUnit::Sheet => "枚",
        QuantityUnit::Bag => "袋",
        QuantityUnit::Box => "箱",
        QuantityUnit::Gram => "g",
        QuantityUnit::Kilogram => "kg",
        QuantityUnit::Milliliter => "ml",
        QuantityUnit::Liter => "L",
    }
}

pub(super) fn parse_quantity_unit(value: &str) -> Option<QuantityUnit> {
    match value {
        "個" => Some(QuantityUnit::Piece),
        "本" => Some(QuantityUnit::Bottle),
        "枚" => Some(QuantityUnit::Sheet),
        "袋" => Some(QuantityUnit::Bag),
        "箱" => Some(QuantityUnit::Box),
        "g" => Some(QuantityUnit::Gram),
        "kg" => Some(QuantityUnit::Kilogram),
        "ml" => Some(QuantityUnit::Milliliter),
        "L" => Some(QuantityUnit::Liter),
        _ => None,
    }
}
