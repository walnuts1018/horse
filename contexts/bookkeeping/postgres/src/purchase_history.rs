use diesel::{
    BoolExpressionMethods, ExpressionMethods, NullableExpressionMethods, QueryDsl,
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Clone, Debug)]
pub struct PurchaseHistoryItem {
    pub merchant: String,
    pub printed_name: String,
    pub variant_name: Option<String>,
    pub normalized_name: Option<String>,
    pub category: Option<String>,
}

#[derive(Debug)]
pub enum PurchaseHistoryError {
    Pool(String),
    Database(DieselError),
    InvalidHouseholdId,
}

impl From<DieselError> for PurchaseHistoryError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn recent_purchase_items(
    pool: &PgPool,
    household_id: &str,
) -> Result<Vec<PurchaseHistoryItem>, PurchaseHistoryError> {
    use public::{categories, product_variants, purchase_items, purchases};

    let household_id =
        Uuid::parse_str(household_id).map_err(|_| PurchaseHistoryError::InvalidHouseholdId)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseHistoryError::Pool(error.to_string()))?;
    let recent_purchase_ids = purchases::table
        .filter(purchases::household_id.eq(household_id))
        .filter(
            purchases::status
                .eq("POSTED")
                .or(purchases::status.eq("CLOSED")),
        )
        .order((purchases::occurred_at.desc(), purchases::id.desc()))
        .select(purchases::id)
        .limit(20)
        .load::<Uuid>(&mut conn)
        .await?;
    if recent_purchase_ids.is_empty() {
        return Ok(Vec::new());
    }

    let rows = purchase_items::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_items::purchase_id)))
        .left_join(
            product_variants::table.on(product_variants::id
                .nullable()
                .eq(purchase_items::product_variant_id)),
        )
        .left_join(categories::table.on(categories::id.nullable().eq(purchase_items::category_id)))
        .filter(purchase_items::purchase_id.eq_any(recent_purchase_ids))
        .order((
            purchases::occurred_at.desc(),
            purchases::id.desc(),
            purchase_items::created_at.desc(),
        ))
        .select((
            purchases::merchant,
            purchase_items::name,
            product_variants::name.nullable(),
            product_variants::normalized_name.nullable(),
            categories::name.nullable(),
        ))
        .load::<(
            String,
            String,
            Option<String>,
            Option<String>,
            Option<String>,
        )>(&mut conn)
        .await?;
    Ok(rows
        .into_iter()
        .map(
            |(merchant, printed_name, variant_name, normalized_name, category)| {
                PurchaseHistoryItem {
                    merchant,
                    printed_name,
                    variant_name,
                    normalized_name,
                    category,
                }
            },
        )
        .collect())
}
