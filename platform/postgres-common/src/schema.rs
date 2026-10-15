// @generated automatically by Diesel CLI.

pub mod public {
    diesel::table! {
        account_incomes (id) {
            id -> Uuid,
            household_id -> Uuid,
            journal_id -> Uuid,
            account_id -> Uuid,
            occurred_at -> Timestamptz,
            amount_yen -> Int8,
            description -> Text,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        account_movements (id) {
            id -> Uuid,
            household_id -> Uuid,
            journal_id -> Uuid,
            kind -> Text,
            occurred_at -> Timestamptz,
            from_account_id -> Uuid,
            to_account_id -> Uuid,
            amount_yen -> Int8,
            description -> Text,
            created_at -> Timestamptz,
        }
    }

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
            card_last4 -> Nullable<Text>,
        }
    }

    diesel::table! {
        allocation_defaults (household_id, version, pool_id) {
            household_id -> Uuid,
            version -> Int8,
            pool_id -> Uuid,
            weight -> Int8,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        allocation_rule_weights (rule_id, pool_id) {
            household_id -> Uuid,
            rule_id -> Uuid,
            pool_id -> Uuid,
            weight -> Int8,
        }
    }

    diesel::table! {
        allocation_rules (id) {
            id -> Uuid,
            household_id -> Uuid,
            category_id -> Nullable<Uuid>,
            merchant_key -> Nullable<Text>,
            revision -> Int8,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
            product_concept_id -> Nullable<Uuid>,
            product_variant_id -> Nullable<Uuid>,
        }
    }

    diesel::table! {
        bank_statement_import_attempts (import_id, attempt_no) {
            household_id -> Uuid,
            import_id -> Uuid,
            attempt_no -> Int4,
            status -> Text,
            started_at -> Timestamptz,
            finished_at -> Nullable<Timestamptz>,
            error_code -> Nullable<Text>,
            error_message -> Nullable<Text>,
        }
    }

    diesel::table! {
        bank_statement_imports (id) {
            id -> Uuid,
            household_id -> Uuid,
            source_document_id -> Uuid,
            account_id -> Uuid,
            status -> Text,
            idempotency_key -> Text,
            content_hash -> Text,
            statement_start -> Nullable<Date>,
            statement_end -> Nullable<Date>,
            row_count -> Int4,
            attempt_count -> Int4,
            max_attempts -> Int4,
            error_code -> Nullable<Text>,
            error_message -> Nullable<Text>,
            available_at -> Timestamptz,
            started_at -> Nullable<Timestamptz>,
            finished_at -> Nullable<Timestamptz>,
            lease_token -> Nullable<Uuid>,
            lease_expires_at -> Nullable<Timestamptz>,
            created_by -> Uuid,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
        }
    }

    diesel::table! {
        bank_statement_rows (id) {
            id -> Uuid,
            household_id -> Uuid,
            source_document_id -> Uuid,
            import_id -> Uuid,
            observation_id -> Nullable<Uuid>,
            row_number -> Int4,
            source_reference -> Text,
            external_transaction_id -> Text,
            transaction_kind -> Text,
            occurred_at -> Nullable<Timestamptz>,
            value_date -> Nullable<Date>,
            amount_yen -> Int8,
            balance_after_yen -> Nullable<Int8>,
            currency_code -> Text,
            description -> Text,
            merchant -> Text,
            raw_row -> Jsonb,
            row_hash -> Text,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        budgets (id) {
            id -> Uuid,
            household_id -> Uuid,
            category_id -> Nullable<Uuid>,
            name -> Text,
            amount_yen -> Int8,
            starts_on -> Date,
            ends_on -> Nullable<Date>,
            archived -> Bool,
            revision -> Int8,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
        }
    }

    diesel::table! {
        categories (id) {
            id -> Uuid,
            household_id -> Uuid,
            parent_id -> Nullable<Uuid>,
            name -> Text,
            archived -> Bool,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        correction_member_burdens (correction_id, pool_id, member_id) {
            household_id -> Uuid,
            correction_id -> Uuid,
            item_id -> Nullable<Uuid>,
            pool_id -> Uuid,
            member_id -> Uuid,
            amount_delta_yen -> Int8,
            funding_rule_version -> Int4,
            funding_weight -> Int8,
        }
    }

    diesel::table! {
        evidence_links (id) {
            id -> Uuid,
            household_id -> Uuid,
            source_document_id -> Uuid,
            observation_id -> Nullable<Uuid>,
            source_reference -> Text,
            entity_kind -> Text,
            entity_id -> Uuid,
            item_id -> Nullable<Uuid>,
            evidence_region -> Nullable<Jsonb>,
            observation -> Jsonb,
            created_by -> Uuid,
            created_at -> Timestamptz,
            purchase_id -> Nullable<Uuid>,
            correction_id -> Nullable<Uuid>,
            transfer_id -> Nullable<Uuid>,
        }
    }

    diesel::table! {
        extraction_job_attempts (job_id, attempt_no) {
            household_id -> Uuid,
            source_document_id -> Uuid,
            job_id -> Uuid,
            attempt_no -> Int4,
            provider -> Text,
            model -> Text,
            status -> Text,
            started_at -> Timestamptz,
            finished_at -> Nullable<Timestamptz>,
            error_code -> Nullable<Text>,
            error_message -> Nullable<Text>,
            response_metadata -> Jsonb,
        }
    }

    diesel::table! {
        extraction_jobs (id) {
            id -> Uuid,
            household_id -> Uuid,
            source_document_id -> Uuid,
            version -> Int4,
            status -> Text,
            provider -> Text,
            model -> Text,
            idempotency_key -> Text,
            request -> Jsonb,
            result -> Nullable<Jsonb>,
            attempt_count -> Int4,
            max_attempts -> Int4,
            available_at -> Timestamptz,
            started_at -> Nullable<Timestamptz>,
            finished_at -> Nullable<Timestamptz>,
            lease_token -> Nullable<Uuid>,
            lease_expires_at -> Nullable<Timestamptz>,
            error_code -> Nullable<Text>,
            error_message -> Nullable<Text>,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
            failure_count -> Int4,
            interaction_reply_preview -> Text,
        }
    }

    diesel::table! {
        funding_obligations (id) {
            id -> Uuid,
            household_id -> Uuid,
            schedule_id -> Nullable<Uuid>,
            member_id -> Uuid,
            pool_id -> Uuid,
            period_start -> Date,
            amount_yen -> Int8,
            paid_yen -> Int8,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        funding_schedules (id) {
            id -> Uuid,
            household_id -> Uuid,
            member_id -> Uuid,
            pool_id -> Uuid,
            amount_yen -> Int8,
            cadence -> Text,
            starts_on -> Date,
            ends_on -> Nullable<Date>,
            active -> Bool,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        gemini_quota_cooldowns (project_route, model) {
            project_route -> Text,
            model -> Text,
            blocked_until -> Timestamptz,
            updated_at -> Timestamptz,
        }
    }

    diesel::table! {
        gemini_quota_reservation_locks (project_route, model) {
            project_route -> Text,
            model -> Text,
            updated_at -> Timestamptz,
        }
    }

    diesel::table! {
        gemini_quota_reservations (id) {
            id -> Uuid,
            project_route -> Text,
            model -> Text,
            reserved_at -> Timestamptz,
            input_tokens -> Int8,
            actual_input_tokens -> Nullable<Int8>,
            settled_at -> Nullable<Timestamptz>,
        }
    }

    diesel::table! {
        household_event_stream_cursors (household_id) {
            household_id -> Uuid,
            cursor -> Int8,
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
            payload -> Jsonb,
            actor_id -> Uuid,
            created_at -> Timestamptz,
            stream_cursor -> Int8,
        }
    }

    diesel::table! {
        household_invitations (id) {
            id -> Uuid,
            household_id -> Uuid,
            token_hash -> Bytea,
            role -> Text,
            expires_at -> Timestamptz,
            created_by -> Uuid,
            created_at -> Timestamptz,
            revoked_at -> Nullable<Timestamptz>,
            accepted_by -> Nullable<Uuid>,
            accepted_at -> Nullable<Timestamptz>,
        }
    }

    diesel::table! {
        household_mail_forwardings (id) {
            id -> Uuid,
            household_id -> Uuid,
            token_hash -> Bytea,
            created_by -> Uuid,
            created_at -> Timestamptz,
            revoked_at -> Nullable<Timestamptz>,
        }
    }

    diesel::table! {
        household_members (household_id, member_id) {
            household_id -> Uuid,
            member_id -> Uuid,
            role -> Text,
            joined_at -> Timestamptz,
            left_at -> Nullable<Timestamptz>,
        }
    }

    diesel::table! {
        households (id) {
            id -> Uuid,
            name -> Text,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
            revision -> Int8,
            default_allocation_version -> Int8,
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
            idempotency_key -> Text,
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
            from_member_id -> Nullable<Uuid>,
            pool_id -> Nullable<Uuid>,
        }
    }

    diesel::table! {
        mailbox_cursors (mailbox) {
            mailbox -> Text,
            uid_validity -> Int8,
            uid -> Int8,
            updated_at -> Timestamptz,
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
        oidc_sessions (token_hash) {
            token_hash -> Bytea,
            member_id -> Uuid,
            expiry -> Timestamptz,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        oidc_states (state_hash) {
            state_hash -> Bytea,
            nonce -> Text,
            verifier -> Text,
            redirect_to -> Text,
            expires_at -> Timestamptz,
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
        product_aliases (id) {
            id -> Uuid,
            household_id -> Uuid,
            variant_id -> Uuid,
            merchant_key -> Text,
            alias_text -> Text,
            normalized_alias -> Text,
            revision -> Int8,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
        }
    }

    diesel::table! {
        product_concepts (id) {
            id -> Uuid,
            household_id -> Uuid,
            name -> Text,
            normalized_name -> Text,
            category_id -> Nullable<Uuid>,
            archived -> Bool,
            revision -> Int8,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
        }
    }

    diesel::table! {
        product_corrections (id) {
            id -> Uuid,
            household_id -> Uuid,
            purchase_id -> Uuid,
            item_id -> Uuid,
            raw_name -> Text,
            product_variant_id -> Uuid,
            learning_scope -> Text,
            merchant_key -> Text,
            actor_id -> Uuid,
            created_at -> Timestamptz,
            previous_product_variant_id -> Nullable<Uuid>,
        }
    }

    diesel::table! {
        product_variants (id) {
            id -> Uuid,
            household_id -> Uuid,
            concept_id -> Uuid,
            name -> Text,
            normalized_name -> Text,
            brand -> Text,
            package_size -> Text,
            barcode -> Nullable<Text>,
            archived -> Bool,
            revision -> Int8,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
        }
    }

    diesel::table! {
        purchase_allocations (item_id, pool_id) {
            household_id -> Uuid,
            purchase_id -> Uuid,
            item_id -> Uuid,
            pool_id -> Uuid,
            amount_yen -> Int8,
            weight -> Int8,
            rule_version -> Int8,
            rule_id -> Nullable<Uuid>,
        }
    }

    diesel::table! {
        purchase_correction_allocations (correction_id, line_no) {
            household_id -> Uuid,
            correction_id -> Uuid,
            line_no -> Int4,
            pool_id -> Uuid,
            amount_yen -> Int8,
        }
    }

    diesel::table! {
        purchase_correction_payments (correction_id, line_no) {
            household_id -> Uuid,
            correction_id -> Uuid,
            line_no -> Int4,
            account_id -> Uuid,
            amount_yen -> Int8,
        }
    }

    diesel::table! {
        purchase_corrections (id) {
            id -> Uuid,
            household_id -> Uuid,
            purchase_id -> Uuid,
            item_id -> Nullable<Uuid>,
            amount_delta_yen -> Int8,
            reason -> Text,
            effective_period_start -> Date,
            journal_id -> Uuid,
            created_by -> Uuid,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        purchase_item_tags (item_id, tag_id) {
            household_id -> Uuid,
            purchase_id -> Uuid,
            item_id -> Uuid,
            tag_id -> Uuid,
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
            category_id -> Nullable<Uuid>,
            revision -> Int8,
            field_revisions -> Jsonb,
            updated_at -> Timestamptz,
            allocation_override -> Bool,
            quantity_numerator -> Nullable<Int8>,
            quantity_denominator -> Nullable<Int8>,
            quantity_unit -> Nullable<Text>,
            printed_unit_price_yen -> Nullable<Int8>,
            printed_unit_price_quantity_numerator -> Nullable<Int8>,
            printed_unit_price_quantity_denominator -> Nullable<Int8>,
            printed_unit_price_unit -> Nullable<Text>,
            product_variant_id -> Nullable<Uuid>,
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
            snapshot_source -> Text,
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
            snapshot_source -> Text,
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
            payment_account_id -> Nullable<Uuid>,
            allocation_rule_version -> Int8,
            field_revisions -> Jsonb,
            date_precision -> Text,
            origin -> Text,
        }
    }

    diesel::table! {
        recurring_expense_matches (household_id, recurring_expense_id, due_on) {
            household_id -> Uuid,
            recurring_expense_id -> Uuid,
            due_on -> Date,
            purchase_id -> Uuid,
            matched_by -> Uuid,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        recurring_expenses (id) {
            id -> Uuid,
            household_id -> Uuid,
            name -> Text,
            merchant -> Text,
            amount_yen -> Int8,
            currency -> Text,
            category_id -> Nullable<Uuid>,
            payment_account_id -> Nullable<Uuid>,
            cadence -> Text,
            starts_on -> Date,
            next_due_on -> Date,
            ends_on -> Nullable<Date>,
            archived -> Bool,
            revision -> Int8,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
        }
    }

    diesel::table! {
        settlement_adjustments (id) {
            id -> Uuid,
            household_id -> Uuid,
            period_start -> Date,
            member_id -> Uuid,
            source_kind -> Text,
            source_id -> Uuid,
            amount_yen -> Int8,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        settlement_run_adjustments (settlement_run_id, member_id, adjustment_kind) {
            household_id -> Uuid,
            settlement_run_id -> Uuid,
            member_id -> Uuid,
            adjustment_kind -> Text,
            amount_yen -> Int8,
        }
    }

    diesel::table! {
        settlement_run_lines (settlement_run_id, member_id) {
            household_id -> Uuid,
            settlement_run_id -> Uuid,
            member_id -> Uuid,
            member_name -> Text,
            scheduled_yen -> Int8,
            paid_yen -> Int8,
            carry_yen -> Int8,
            monthly_adjustment_yen -> Int8,
            advance_credit_yen -> Int8,
            amount_due_yen -> Int8,
        }
    }

    diesel::table! {
        settlement_run_purchase_snapshots (settlement_run_id, purchase_id) {
            household_id -> Uuid,
            settlement_run_id -> Uuid,
            purchase_id -> Uuid,
            purchase_revision -> Int8,
        }
    }

    diesel::table! {
        settlement_runs (id) {
            id -> Uuid,
            household_id -> Uuid,
            period_start -> Date,
            revision -> Int8,
            status -> Text,
            funding_snapshot -> Jsonb,
            actor_id -> Uuid,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        shared_purchase_drafts (id) {
            id -> Uuid,
            household_id -> Uuid,
            revision -> Int8,
            payload -> Jsonb,
            created_at -> Timestamptz,
            updated_at -> Timestamptz,
        }
    }

    diesel::table! {
        source_confirmations (id) {
            id -> Uuid,
            household_id -> Uuid,
            source_document_id -> Uuid,
            observation_id -> Nullable<Uuid>,
            source_reference -> Text,
            entity_kind -> Text,
            entity_id -> Uuid,
            status -> Text,
            idempotency_key -> Text,
            changed_by -> Uuid,
            changed_at -> Timestamptz,
            purchase_id -> Nullable<Uuid>,
            correction_id -> Nullable<Uuid>,
            transfer_id -> Nullable<Uuid>,
            account_income_id -> Nullable<Uuid>,
            account_movement_id -> Nullable<Uuid>,
        }
    }

    diesel::table! {
        source_document_attachments (parent_document_id, attachment_index) {
            household_id -> Uuid,
            parent_document_id -> Uuid,
            child_document_id -> Uuid,
            attachment_index -> Int4,
            filename -> Text,
            mime_type -> Text,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        source_documents (id) {
            id -> Uuid,
            household_id -> Uuid,
            sha256 -> Text,
            object_key -> Text,
            mime_type -> Text,
            source_kind -> Text,
            original_filename -> Text,
            metadata -> Jsonb,
            created_by -> Uuid,
            created_at -> Timestamptz,
            analysis_object_key -> Nullable<Text>,
            analysis_mime_type -> Nullable<Text>,
            analysis_transform -> Nullable<Jsonb>,
        }
    }

    diesel::table! {
        source_observations (id) {
            id -> Uuid,
            household_id -> Uuid,
            source_document_id -> Uuid,
            extraction_job_id -> Uuid,
            source_reference -> Text,
            source_hash -> Text,
            source_kind -> Text,
            transaction_kind -> Text,
            external_order_id -> Text,
            external_transaction_id -> Text,
            merchant -> Text,
            occurred_at -> Nullable<Timestamptz>,
            amount_yen -> Nullable<Int8>,
            card_last4 -> Text,
            currency_code -> Text,
            raw_observation -> Jsonb,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        statement_reconciliation_candidates (id) {
            id -> Uuid,
            household_id -> Uuid,
            import_id -> Uuid,
            statement_row_id -> Uuid,
            entity_kind -> Text,
            entity_id -> Uuid,
            score -> Numeric,
            rationale -> Jsonb,
            decision -> Text,
            rank -> Int4,
            decided_by -> Nullable<Uuid>,
            decided_at -> Nullable<Timestamptz>,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        tags (id) {
            id -> Uuid,
            household_id -> Uuid,
            name -> Text,
            archived -> Bool,
            revision -> Int8,
            created_at -> Timestamptz,
        }
    }

    diesel::table! {
        transfers (id) {
            id -> Uuid,
            household_id -> Uuid,
            journal_id -> Uuid,
            from_member_id -> Uuid,
            to_member_id -> Uuid,
            kind -> Text,
            amount_yen -> Int8,
            description -> Text,
            payment_account_id -> Nullable<Uuid>,
            loan_id -> Nullable<Uuid>,
            created_by -> Uuid,
            created_at -> Timestamptz,
        }
    }

    diesel::joinable!(account_incomes -> households (household_id));
    diesel::joinable!(account_movements -> households (household_id));
    diesel::joinable!(accounting_periods -> households (household_id));
    diesel::joinable!(accounts -> households (household_id));
    diesel::joinable!(allocation_defaults -> households (household_id));
    diesel::joinable!(allocation_rules -> households (household_id));
    diesel::joinable!(budgets -> households (household_id));
    diesel::joinable!(categories -> households (household_id));
    diesel::joinable!(evidence_links -> households (household_id));
    diesel::joinable!(household_event_stream_cursors -> households (household_id));
    diesel::joinable!(household_events -> households (household_id));
    diesel::joinable!(household_invitations -> households (household_id));
    diesel::joinable!(household_mail_forwardings -> households (household_id));
    diesel::joinable!(household_mail_forwardings -> members (created_by));
    diesel::joinable!(household_members -> households (household_id));
    diesel::joinable!(household_members -> members (member_id));
    diesel::joinable!(idempotency_records -> households (household_id));
    diesel::joinable!(journals -> households (household_id));
    diesel::joinable!(ledger_entries -> households (household_id));
    diesel::joinable!(oidc_sessions -> members (member_id));
    diesel::joinable!(pools -> households (household_id));
    diesel::joinable!(product_concepts -> households (household_id));
    diesel::joinable!(purchases -> households (household_id));
    diesel::joinable!(recurring_expenses -> households (household_id));
    diesel::joinable!(shared_purchase_drafts -> households (household_id));
    diesel::joinable!(source_documents -> households (household_id));
    diesel::joinable!(tags -> households (household_id));

    diesel::allow_tables_to_appear_in_same_query!(
        account_incomes,
        account_movements,
        accounting_periods,
        accounts,
        allocation_defaults,
        allocation_rule_weights,
        allocation_rules,
        bank_statement_import_attempts,
        bank_statement_imports,
        bank_statement_rows,
        budgets,
        categories,
        correction_member_burdens,
        evidence_links,
        extraction_job_attempts,
        extraction_jobs,
        funding_obligations,
        funding_schedules,
        gemini_quota_cooldowns,
        gemini_quota_reservation_locks,
        gemini_quota_reservations,
        household_event_stream_cursors,
        household_events,
        household_invitations,
        household_mail_forwardings,
        household_members,
        households,
        idempotency_records,
        journal_entries,
        journals,
        ledger_entries,
        mailbox_cursors,
        members,
        oidc_sessions,
        oidc_states,
        pool_funding_rules,
        pool_funding_shares,
        pools,
        product_aliases,
        product_concepts,
        product_corrections,
        product_variants,
        purchase_allocations,
        purchase_correction_allocations,
        purchase_correction_payments,
        purchase_corrections,
        purchase_item_tags,
        purchase_items,
        purchase_member_burdens,
        purchase_payment_funding_snapshots,
        purchase_payments,
        purchases,
        recurring_expense_matches,
        recurring_expenses,
        settlement_adjustments,
        settlement_run_adjustments,
        settlement_run_lines,
        settlement_run_purchase_snapshots,
        settlement_runs,
        shared_purchase_drafts,
        source_confirmations,
        source_document_attachments,
        source_documents,
        source_observations,
        statement_reconciliation_candidates,
        tags,
        transfers,
    );
}
