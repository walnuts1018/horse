use chrono::{Duration, Utc};
use diesel::{ExpressionMethods, QueryDsl, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::schema::public::{household_invitations, household_members, members};
use horse_test_infra::{TestPostgres, apply_migrations};
use household_application::{
    create_household::CreateHouseholdCommand,
    invitation::{MemberId, RevokeInvitationCommand},
};
use uuid::Uuid;

#[tokio::test]
async fn another_household_owner_cannot_access_invitation_without_its_token() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = horse_postgres_common::connect(postgres.connection_url())
        .await
        .unwrap();

    let invited_household_owner = Uuid::now_v7();
    let other_household_owner = Uuid::now_v7();
    {
        let mut conn = pool.get().await.unwrap();
        for (id, subject, name) in [
            (
                invited_household_owner,
                "invitation-boundary-invited-owner",
                "Invited household owner",
            ),
            (
                other_household_owner,
                "invitation-boundary-other-owner",
                "Other household owner",
            ),
        ] {
            insert_into(members::table)
                .values((
                    members::id.eq(id),
                    members::oidc_subject.eq(subject),
                    members::display_name.eq(name),
                ))
                .execute(&mut conn)
                .await
                .unwrap();
        }
    }

    let invited_household = household_postgres::create_household::persist_create_household(
        &pool,
        CreateHouseholdCommand {
            actor_id: invited_household_owner.into(),
            name: "Invited household".to_owned(),
            member_names: Vec::new(),
        },
    )
    .await
    .unwrap();
    let invited_household_id = Uuid::parse_str(&invited_household.household_id).unwrap();
    let other_household = household_postgres::create_household::persist_create_household(
        &pool,
        CreateHouseholdCommand {
            actor_id: other_household_owner.into(),
            name: "Other household".to_owned(),
            member_names: Vec::new(),
        },
    )
    .await
    .unwrap();
    let other_household_id = Uuid::parse_str(&other_household.household_id).unwrap();

    let invitation_id = Uuid::now_v7();
    let valid_token_hash = [7; 32];
    let now = Utc::now();
    {
        let mut conn = pool.get().await.unwrap();
        insert_into(household_invitations::table)
            .values((
                household_invitations::id.eq(invitation_id),
                household_invitations::household_id.eq(invited_household_id),
                household_invitations::token_hash.eq(valid_token_hash.to_vec()),
                household_invitations::expires_at.eq(now + Duration::days(1)),
                household_invitations::created_by.eq(invited_household_owner),
            ))
            .execute(&mut conn)
            .await
            .unwrap();
    }

    let listed = household_postgres::household_invitation::list_invitations(
        &pool,
        other_household_owner,
        invited_household_id,
        None,
        None,
        Some(10),
        None,
    )
    .await;
    assert!(matches!(
        listed,
        Err(household_postgres::household_invitation::HouseholdInvitationError::HouseholdNotFound)
    ));

    let revoked = household_postgres::household_invitation::revoke_invitation(
        &pool,
        RevokeInvitationCommand {
            actor_id: other_household_owner.to_string(),
            household_id: invited_household_id.to_string(),
            invitation_id: invitation_id.to_string(),
        },
        now,
    )
    .await;
    assert!(matches!(
        revoked,
        Err(household_postgres::household_invitation::HouseholdInvitationError::MemberNotFound)
    ));

    let accepted = household_postgres::household_invitation::accept_invitation_by_token(
        &pool,
        MemberId::from(other_household_owner),
        [8; 32],
        now,
    )
    .await;
    assert!(matches!(
        accepted,
        Err(household_postgres::household_invitation::HouseholdInvitationError::InvitationNotFound)
    ));

    let invitation_state = {
        let mut conn = pool.get().await.unwrap();
        household_invitations::table
            .filter(household_invitations::id.eq(invitation_id))
            .select((
                household_invitations::accepted_by,
                household_invitations::accepted_at,
                household_invitations::revoked_at,
            ))
            .first::<(
                Option<Uuid>,
                Option<chrono::DateTime<Utc>>,
                Option<chrono::DateTime<Utc>>,
            )>(&mut conn)
            .await
            .unwrap()
    };
    assert_eq!(invitation_state, (None, None, None));

    let other_household_membership = {
        let mut conn = pool.get().await.unwrap();
        household_members::table
            .filter(household_members::household_id.eq(invited_household_id))
            .filter(household_members::member_id.eq(other_household_owner))
            .select(household_members::member_id)
            .first::<Uuid>(&mut conn)
            .await
    };
    assert!(other_household_membership.is_err());
    assert_ne!(invited_household_id, other_household_id);

    postgres.terminate().await.unwrap();
}
