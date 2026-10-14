CREATE TABLE gemini_quota_cooldowns (
    project_route text NOT NULL CHECK (project_route IN ('free', 'paid')),
    model text NOT NULL CHECK (length(model) > 0),
    blocked_until timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_route, model)
);
