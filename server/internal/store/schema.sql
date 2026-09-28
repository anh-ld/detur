-- detur schema. Timestamps are RFC3339 UTC text.

CREATE TABLE IF NOT EXISTS apps (
    id                      TEXT PRIMARY KEY,
    name                    TEXT NOT NULL,
    api_key_hash            TEXT NOT NULL,
    ios_app_id              TEXT,
    android_package         TEXT,
    android_cert_fingerprint TEXT,
    created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS links (
    id             TEXT PRIMARY KEY,
    app_id         TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    key            TEXT NOT NULL,
    url            TEXT NOT NULL,
    ios            TEXT,
    android        TEXT,
    fallback_url   TEXT,
    threshold      INTEGER NOT NULL DEFAULT 850,
    window_minutes INTEGER NOT NULL DEFAULT 15,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (app_id, key)
);

CREATE TABLE IF NOT EXISTS clicks (
    id          TEXT PRIMARY KEY,
    app_id      TEXT NOT NULL,
    link_id     TEXT NOT NULL,
    ip          TEXT,
    device      TEXT,
    locale      TEXT,
    timezone    TEXT,
    screen      TEXT,
    user_agent  TEXT,
    pasted_link TEXT,
    destination TEXT NOT NULL,
    click_id    TEXT,
    is_bot      INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_clicks_created ON clicks (created_at);
CREATE INDEX IF NOT EXISTS idx_clicks_expires ON clicks (expires_at);
CREATE INDEX IF NOT EXISTS idx_clicks_clickid ON clicks (click_id);
CREATE INDEX IF NOT EXISTS idx_clicks_app_created ON clicks (app_id, is_bot, created_at);

CREATE TABLE IF NOT EXISTS installs (
    id          TEXT PRIMARY KEY,
    app_id      TEXT NOT NULL,
    device_hash TEXT NOT NULL,
    click_id    TEXT,
    attribution TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (app_id, device_hash, click_id)
);

CREATE INDEX IF NOT EXISTS idx_installs_app_attribution ON installs (app_id, attribution);

CREATE TABLE IF NOT EXISTS events (
    id         TEXT PRIMARY KEY,
    app_id     TEXT NOT NULL,
    event      TEXT NOT NULL,
    metadata   TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_events_created ON events (created_at);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
