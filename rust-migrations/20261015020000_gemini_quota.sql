CREATE TABLE gemini_quota_reservation_locks (
    project_route text NOT NULL CHECK (project_route IN ('free', 'paid')),
    model text NOT NULL CHECK (length(model) > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_route, model)
);

CREATE TABLE gemini_quota_reservations (
    id uuid PRIMARY KEY,
    project_route text NOT NULL CHECK (project_route IN ('free', 'paid')),
    model text NOT NULL CHECK (length(model) > 0),
    reserved_at timestamptz NOT NULL,
    input_tokens bigint NOT NULL CHECK (input_tokens > 0),
    actual_input_tokens bigint CHECK (actual_input_tokens >= 0),
    settled_at timestamptz,
    CHECK ((actual_input_tokens IS NULL) = (settled_at IS NULL))
);

CREATE INDEX gemini_quota_reservations_route_model_time_idx
    ON gemini_quota_reservations (project_route, model, reserved_at);

CREATE TABLE gemini_quota_cooldowns (
    project_route text NOT NULL CHECK (project_route IN ('free', 'paid')),
    model text NOT NULL CHECK (length(model) > 0),
    blocked_until timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_route, model)
);
