use catalog_application::product_learning::{
    ProductLearningDecision, product_alias_learned, product_variant_renamed,
};
use diesel::{dsl::now, prelude::*};
use diesel_async::RunQueryDsl;
use horse_postgres_common::insert_ui_change;
use uuid::Uuid;

use crate::{AsyncPgConnection, schema::public};

pub async fn persist_product_learning_in_transaction(
    conn: &mut AsyncPgConnection,
    decision: ProductLearningDecision,
) -> Result<(), diesel::result::Error> {
    if let Some((name, normalized_name)) = decision.variant_name {
        let revision = diesel::update(
            public::product_variants::table
                .filter(public::product_variants::household_id.eq(decision.household_id))
                .filter(public::product_variants::id.eq(decision.variant_id)),
        )
        .set((
            public::product_variants::name.eq(name.as_str()),
            public::product_variants::normalized_name.eq(normalized_name.as_str()),
            public::product_variants::revision.eq(public::product_variants::revision + 1),
            public::product_variants::updated_at.eq(now),
        ))
        .returning(public::product_variants::revision)
        .get_result::<i64>(conn)
        .await?;
        insert_ui_change(
            conn,
            decision.household_id,
            decision.variant_id,
            revision,
            decision.actor_id,
            chrono::Utc::now(),
            product_variant_renamed(),
        )
        .await?;
    }

    if !decision.record_correction {
        return Ok(());
    }
    if let Some((alias_text, normalized_alias)) = decision.alias {
        let row = diesel::insert_into(public::product_aliases::table)
            .values((
                public::product_aliases::id.eq(Uuid::now_v7()),
                public::product_aliases::household_id.eq(decision.household_id),
                public::product_aliases::variant_id.eq(decision.variant_id),
                public::product_aliases::merchant_key.eq(decision.merchant_key.as_str()),
                public::product_aliases::alias_text.eq(alias_text),
                public::product_aliases::normalized_alias.eq(normalized_alias),
            ))
            .on_conflict((
                public::product_aliases::household_id,
                public::product_aliases::merchant_key,
                public::product_aliases::normalized_alias,
            ))
            .do_update()
            .set((
                public::product_aliases::variant_id.eq(decision.variant_id),
                public::product_aliases::alias_text.eq(&decision.raw_name),
                public::product_aliases::revision.eq(public::product_aliases::revision + 1),
                public::product_aliases::updated_at.eq(now),
            ))
            .returning((
                public::product_aliases::id,
                public::product_aliases::revision,
            ))
            .get_result::<(Uuid, i64)>(conn)
            .await?;
        insert_ui_change(
            conn,
            decision.household_id,
            row.0,
            row.1,
            decision.actor_id,
            chrono::Utc::now(),
            product_alias_learned(),
        )
        .await?;
    }

    diesel::insert_into(public::product_corrections::table)
        .values((
            public::product_corrections::id.eq(Uuid::now_v7()),
            public::product_corrections::household_id.eq(decision.household_id),
            public::product_corrections::purchase_id.eq(decision.purchase_id),
            public::product_corrections::item_id.eq(decision.item_id),
            public::product_corrections::raw_name.eq(decision.raw_name),
            public::product_corrections::previous_product_variant_id
                .eq(decision.previous_variant_id),
            public::product_corrections::product_variant_id.eq(decision.variant_id),
            public::product_corrections::learning_scope.eq(decision.learning_scope.as_str()),
            public::product_corrections::merchant_key.eq(decision.merchant_key.as_str()),
            public::product_corrections::actor_id.eq(decision.actor_id),
        ))
        .execute(conn)
        .await?;
    Ok(())
}
