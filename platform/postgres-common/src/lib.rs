use diesel::{ExpressionMethods, insert_into};
use diesel_async::{
    RunQueryDsl,
    pooled_connection::{AsyncDieselConnectionManager, PoolError, bb8::Pool},
};
use uuid::Uuid;

pub use diesel_async::AsyncPgConnection;
pub mod household_events;
pub mod schema;

pub type PgPool = Pool<AsyncPgConnection>;

pub async fn connect(database_url: impl Into<String>) -> Result<PgPool, PoolError> {
    let manager = AsyncDieselConnectionManager::<AsyncPgConnection>::new(database_url.into());
    Pool::builder().build(manager).await
}

pub use household_events::insert_ui_change;

pub async fn insert_evidence_link(
    conn: &mut AsyncPgConnection,
    link: evidence_application::EvidenceLink,
) -> Result<usize, diesel::result::Error> {
    use schema::public::evidence_links;

    let query = insert_into(evidence_links::table).values((
        evidence_links::id.eq(Uuid::now_v7()),
        evidence_links::household_id.eq(link.household_id),
        evidence_links::source_document_id.eq(link.source_document_id),
        evidence_links::observation_id.eq(None::<Uuid>),
        evidence_links::source_reference.eq(link.source_reference),
        evidence_links::entity_kind.eq(link.entity_kind),
        evidence_links::entity_id.eq(link.entity_id),
        evidence_links::item_id.eq(link.item_id),
        evidence_links::evidence_region.eq(link.evidence_region),
        evidence_links::observation.eq(link.observation),
        evidence_links::created_by.eq(link.actor_id),
        evidence_links::created_at.eq(link.created_at),
        evidence_links::purchase_id.eq(link.purchase_id),
        evidence_links::correction_id.eq(link.correction_id),
        evidence_links::transfer_id.eq(link.transfer_id),
    ));
    if link.ignore_duplicate {
        query.on_conflict_do_nothing().execute(conn).await
    } else {
        query.execute(conn).await
    }
}
