use std::future::Future;
use std::pin::Pin;
use uuid::Uuid;

#[derive(Debug)]
pub enum PurchaseAnalyticsQueryError {
    HouseholdNotFound,
    InvalidPeriod,
    AmountOutOfRange,
    Infrastructure(String),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseAnalyticsProjection {
    pub dimension: String,
    pub id: String,
    pub name: String,
    pub period: String,
    pub amount_yen: i64,
    pub count: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchasePriceObservationProjection {
    pub item_id: String,
    pub occurred_at: chrono::DateTime<chrono::Utc>,
    pub amount_yen: i64,
    pub remaining_refundable_amount_yen: i64,
    pub effective_unit_price_numerator_yen: Option<String>,
    pub effective_unit_price_denominator: Option<String>,
    pub printed_unit_price_yen: Option<i64>,
    pub printed_unit_price_quantity_numerator: Option<String>,
    pub printed_unit_price_quantity_denominator: Option<String>,
    pub printed_unit_price_unit: Option<String>,
}

pub trait PurchaseAnalyticsQuery: Send + Sync {
    fn purchase_analytics<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        from: &'a str,
        to: &'a str,
    ) -> Pin<
        Box<
            dyn Future<
                    Output = Result<Vec<PurchaseAnalyticsProjection>, PurchaseAnalyticsQueryError>,
                > + Send
                + 'a,
        >,
    >;

    fn purchase_price_observations<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        from: &'a str,
        to: &'a str,
        item_ids: &'a [String],
    ) -> Pin<
        Box<
            dyn Future<
                    Output = Result<
                        Vec<PurchasePriceObservationProjection>,
                        PurchaseAnalyticsQueryError,
                    >,
                > + Send
                + 'a,
        >,
    >;
}
