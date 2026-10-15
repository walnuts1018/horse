// @generated automatically by Diesel CLI.

pub mod public {
    diesel::table! {
        accounting_periods (household_id, period_start) {
            household_id -> Uuid,
            period_start -> Date,
            status -> Text,
            revision -> Int8,
            closed_by -> Nullable<Uuid>,
            closed_at -> Nullable<Timestamptz>,
        }
    }

    diesel::table! {
        accounts (id) {
            id -> Uuid,
            household_id -> Uuid,
            pool_id -> Nullable<Uuid>,
            name -> Text,
            kind -> Text,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        household_events (cursor) {
            cursor -> Int8,
            household_id -> Uuid,
            event_type -> Text,
            entity_kind -> Text,
            entity_id -> Uuid,
            revision -> Int8,
            changed_fields -> Array<Nullable<Text>>,
            actor_id -> Uuid,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        household_members (household_id, member_id) {
            household_id -> Uuid,
            member_id -> Uuid,
            role -> Text,
            joined_at -> Timestamptz,
        }
    }

    diesel::table! {
        households (id) {
            id -> Uuid,
            name -> Text,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
        }
    }

    diesel::table! {
        idempotency_records (household_id, operation, key) {
            household_id -> Uuid,
            operation -> Text,
            key -> Text,
            request_fingerprint -> Bytea,
            resource_kind -> Text,
            resource_id -> Uuid,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        journal_entries (journal_id, line_no) {
            household_id -> Uuid,
            journal_id -> Uuid,
            line_no -> Int4,
            account_id -> Nullable<Uuid>,
            pool_id -> Nullable<Uuid>,
            member_id -> Nullable<Uuid>,
            debit_yen -> Int8,
            credit_yen -> Int8,
            memo -> Text,
        }
    }

    diesel::table! {
        journals (id) {
            id -> Uuid,
            household_id -> Uuid,
            source_kind -> Text,
            source_id -> Uuid,
            occurred_at -> Timestamptz,
            created_by -> Uuid,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        ledger_entries (id) {
            id -> Uuid,
            household_id -> Uuid,
            journal_id -> Uuid,
            source_kind -> Text,
            source_id -> Uuid,
            occurred_at -> Timestamptz,
            description -> Text,
            amount_yen -> Int8,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        members (id) {
            id -> Uuid,
            oidc_subject -> Text,
            display_name -> Text,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        pool_funding_rules (household_id, pool_id, version) {
            household_id -> Uuid,
            pool_id -> Uuid,
            version -> Int4,
            owner_member_id -> Nullable<Uuid>,
            settlement_policy -> Nullable<Text>,
            created_by -> Uuid,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        pool_funding_shares (household_id, pool_id, version, member_id) {
            household_id -> Uuid,
            pool_id -> Uuid,
            version -> Int4,
            member_id -> Uuid,
            weight -> Int8,
        }
    }

    diesel::table! {
        pools (id) {
            id -> Uuid,
            household_id -> Uuid,
            name -> Text,
            kind -> Text,
            current_funding_version -> Nullable<Int4>,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        purchase_allocations (item_id, pool_id) {
            household_id -> Uuid,
            purchase_id -> Uuid,
            item_id -> Uuid,
            pool_id -> Uuid,
            amount_yen -> Int8,
        }
    }

    diesel::table! {
        purchase_items (id) {
            id -> Uuid,
            household_id -> Uuid,
            purchase_id -> Uuid,
            name -> Text,
            amount_yen -> Int8,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        purchase_member_burdens (item_id, pool_id, member_id) {
            household_id -> Uuid,
            purchase_id -> Uuid,
            item_id -> Uuid,
            pool_id -> Uuid,
            member_id -> Uuid,
            amount_yen -> Int8,
            funding_rule_version -> Int4,
            funding_weight -> Int8,
            settlement_policy -> Nullable<Text>,
        }
    }

    diesel::table! {
        purchase_payment_funding_snapshots (purchase_id, payment_line_no, member_id) {
            household_id -> Uuid,
            purchase_id -> Uuid,
            payment_line_no -> Int4,
            account_id -> Uuid,
            pool_id -> Uuid,
            member_id -> Uuid,
            funding_rule_version -> Int4,
            funding_weight -> Int8,
            settlement_policy -> Nullable<Text>,
        }
    }

    diesel::table! {
        purchase_payments (purchase_id, line_no) {
            household_id -> Uuid,
            purchase_id -> Uuid,
            line_no -> Int4,
            account_id -> Uuid,
            amount_yen -> Int8,
        }
    }

    diesel::table! {
        purchases (id) {
            id -> Uuid,
            household_id -> Uuid,
            occurred_at -> Timestamptz,
            merchant -> Text,
            status -> Text,
            revision -> Int8,
            created_by -> Uuid,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
        }
    }

    diesel::joinable!(accounting_periods -> households (household_id));
    diesel::joinable!(accounts -> households (household_id));
    diesel::joinable!(household_events -> households (household_id));
    diesel::joinable!(household_members -> households (household_id));
    diesel::joinable!(household_members -> members (member_id));
    diesel::joinable!(idempotency_records -> households (household_id));
    diesel::joinable!(journals -> households (household_id));
    diesel::joinable!(ledger_entries -> households (household_id));
    diesel::joinable!(pools -> households (household_id));
    diesel::joinable!(purchases -> households (household_id));

    diesel::allow_tables_to_appear_in_same_query!(
        accounting_periods,
        accounts,
        household_events,
        household_members,
        households,
        idempotency_records,
        journal_entries,
        journals,
        ledger_entries,
        members,
        pool_funding_rules,
        pool_funding_shares,
        pools,
        purchase_allocations,
        purchase_items,
        purchase_member_burdens,
        purchase_payment_funding_snapshots,
        purchase_payments,
        purchases,
    );
}
