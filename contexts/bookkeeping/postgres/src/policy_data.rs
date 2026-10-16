use async_trait::async_trait;
use diesel_async::AsyncPgConnection;
use uuid::Uuid;

#[async_trait]
pub trait PolicyData: Send {
    async fn pool_funding_owner(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error>;

    async fn active_funding_schedule(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        member_id: Uuid,
        pool_id: Uuid,
    ) -> Result<Option<Uuid>, diesel::result::Error>;
}
