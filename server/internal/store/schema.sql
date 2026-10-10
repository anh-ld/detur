-- detur schema. Timestamps are fixed-width UTC text (YYYY-MM-DDTHH:MM:SS.sssZ,
-- same as strftime('%Y-%m-%dT%H:%M:%fZ')) so text comparison orders them.

CREATE TABLE IF NOT EXISTS apps (
    id                      TEXT PRIMARY KEY,
    name                    TEXT NOT NULL,
    api_key_hash            TEXT NOT NULL,
    ios_app_id              TEXT,
    android_package         TEXT,
    android_cert_fingerprint TEXT,
    match_threshold         INTEGER NOT NULL DEFAULT 850,
    match_window_minutes    INTEGER NOT NULL DEFAULT 15,
    created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- apps.sdk_version, sdk_seen_at, tag_links: addMissingColumns.

CREATE TABLE IF NOT EXISTS links (
    id             TEXT PRIMARY KEY,
    app_id         TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    key            TEXT NOT NULL,
    url            TEXT NOT NULL,
    ios            TEXT,
    android        TEXT,
    fallback_url   TEXT,
    expires_at     TEXT,
    expired_url    TEXT,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (app_id, key)
);

-- Global key resolution (GET /{key} pipeline) and the create-time conflict
-- check look up links by bare key; the UNIQUE(app_id, key) index cannot serve
-- a key-only lookup, and the NOCASE comparison needs a matching collation.
CREATE INDEX IF NOT EXISTS idx_links_key ON links (key COLLATE NOCASE);

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
    os_version  TEXT,
    pasted_link TEXT,
    destination TEXT NOT NULL,
    click_id    TEXT,
    is_bot      INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at  TEXT NOT NULL,
    matched_at  TEXT
);

-- clicks.kind, first_seen_at, ua_suspect, ip_hosting, hits_ip, hits_link, source, variant: addMissingColumns.

CREATE INDEX IF NOT EXISTS idx_clicks_created ON clicks (created_at);
CREATE INDEX IF NOT EXISTS idx_clicks_expires ON clicks (expires_at);
CREATE INDEX IF NOT EXISTS idx_clicks_clickid ON clicks (click_id);
CREATE INDEX IF NOT EXISTS idx_clicks_app_created ON clicks (app_id, is_bot, created_at);
CREATE INDEX IF NOT EXISTS idx_clicks_app ON clicks (app_id);
CREATE INDEX IF NOT EXISTS idx_clicks_dedup ON clicks (link_id, ip, created_at);

CREATE TABLE IF NOT EXISTS installs (
    id          TEXT PRIMARY KEY,
    app_id      TEXT NOT NULL,
    device_hash TEXT NOT NULL,
    click_id    TEXT,
    attribution TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (app_id, device_hash, click_id)
);
-- installs.link_id, platform, method, score, runner_up, fraud, fraud_action, fraud_link_id, variant: addMissingColumns.

CREATE INDEX IF NOT EXISTS idx_installs_app_attribution ON installs (app_id, attribution);
-- (app_id) index entries are (app_id, rowid): webhook delivery seeks app_id = ? AND rowid > cursor in order, no sort.
CREATE INDEX IF NOT EXISTS idx_installs_app ON installs (app_id);

CREATE TABLE IF NOT EXISTS events (
    id         TEXT PRIMARY KEY,
    app_id     TEXT NOT NULL,
    event      TEXT NOT NULL,
    metadata   TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_events_created ON events (created_at);
CREATE INDEX IF NOT EXISTS idx_events_app ON events (app_id);

-- Analytics rollups: per-day counters with no device data, so they outlive the
-- clicks/events retention purge. day = YYYY-MM-DD (UTC).
-- kind: app (browser click sent to a store) | web (browser click sent to a web
-- page) | open (SDK universal-link open, app already installed).
CREATE TABLE IF NOT EXISTS click_days (
    app_id   TEXT NOT NULL,
    link_id  TEXT NOT NULL,
    day      TEXT NOT NULL,
    platform TEXT NOT NULL,
    kind     TEXT NOT NULL,
    n        INTEGER NOT NULL,
    PRIMARY KEY (app_id, day, link_id, platform, kind)
);

-- click_sources: new browser clicks per in-app source (messenger, zalo, ...,
-- unknown-inapp). Own table: a source column in click_days would change its key.
CREATE TABLE IF NOT EXISTS click_sources (
    app_id  TEXT NOT NULL,
    link_id TEXT NOT NULL,
    day     TEXT NOT NULL,
    source  TEXT NOT NULL,
    n       INTEGER NOT NULL,
    PRIMARY KEY (app_id, day, link_id, source)
);

CREATE TABLE IF NOT EXISTS event_days (
    app_id TEXT NOT NULL,
    event  TEXT NOT NULL,
    day    TEXT NOT NULL,
    n      INTEGER NOT NULL,
    PRIMARY KEY (app_id, day, event)
);

-- Per-link conversions + retention. device_links maps an SDK device (hashed
-- device_id) to the first link the app tagged it with; d1/d7/d30 = already
-- counted for that mark. Holds device data: PurgeExpired drops rows 90 days
-- after first_seen. The two rollups below are counts only, never purged.
CREATE TABLE IF NOT EXISTS device_links (
    app_id     TEXT NOT NULL,
    device     TEXT NOT NULL,
    link_id    TEXT NOT NULL,
    first_seen TEXT NOT NULL,
    d1         INTEGER NOT NULL DEFAULT 0,
    d7         INTEGER NOT NULL DEFAULT 0,
    d30        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (app_id, device)
);

CREATE INDEX IF NOT EXISTS idx_device_links_first_seen ON device_links (first_seen);

-- link_event_days: SDK events from tagged devices, per link.
CREATE TABLE IF NOT EXISTS link_event_days (
    app_id  TEXT NOT NULL,
    link_id TEXT NOT NULL,
    event   TEXT NOT NULL,
    day     TEXT NOT NULL,
    n       INTEGER NOT NULL,
    PRIMARY KEY (app_id, day, link_id, event)
);

-- link_cohorts: devices first tagged to a link on day; dN = of those, devices
-- that sent a retention call exactly N days later.
CREATE TABLE IF NOT EXISTS link_cohorts (
    app_id  TEXT NOT NULL,
    link_id TEXT NOT NULL,
    day     TEXT NOT NULL,
    devices INTEGER NOT NULL DEFAULT 0,
    d1      INTEGER NOT NULL DEFAULT 0,
    d7      INTEGER NOT NULL DEFAULT 0,
    d30     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (app_id, day, link_id)
);

-- Fraud settings: one row per app; no row = fraud.Defaults (all tagged).
CREATE TABLE IF NOT EXISTS fraud_settings (
    app_id                  TEXT PRIMARY KEY,
    velocity_mode           TEXT NOT NULL,
    timing_mode             TEXT NOT NULL,
    user_agent_mode         TEXT NOT NULL,
    ip_mode                 TEXT NOT NULL,
    velocity_ip_max         INTEGER NOT NULL,
    velocity_link_max       INTEGER NOT NULL,
    velocity_window_minutes INTEGER NOT NULL,
    timing_short_seconds    INTEGER NOT NULL,
    timing_long_hours       INTEGER NOT NULL,
    fingerprint_max         INTEGER NOT NULL,
    fingerprint_window_days INTEGER NOT NULL
);

-- Velocity hit counter: raw tracked taps per minute. key_type ip (key = hashed
-- IP, IPv6 /64) | link (key = link id). bucket = YYYY-MM-DDTHH:MM (UTC).
-- Holds device data: PurgeExpired drops buckets older than 24h.
CREATE TABLE IF NOT EXISTS click_hits (
    app_id   TEXT NOT NULL,
    key_type TEXT NOT NULL,
    key      TEXT NOT NULL,
    bucket   TEXT NOT NULL,
    n        INTEGER NOT NULL,
    PRIMARY KEY (app_id, key_type, key, bucket)
);

CREATE INDEX IF NOT EXISTS idx_click_hits_bucket ON click_hits (bucket);

CREATE TABLE IF NOT EXISTS webhooks (
    id                    TEXT PRIMARY KEY,
    app_id                TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    url                   TEXT NOT NULL,
    secret                TEXT NOT NULL,
    types                 TEXT NOT NULL,
    cursor_installs       INTEGER NOT NULL DEFAULT 0,
    cursor_clicks         INTEGER NOT NULL DEFAULT 0,
    cursor_events         INTEGER NOT NULL DEFAULT 0,
    fails_installs        INTEGER NOT NULL DEFAULT 0,
    fails_clicks          INTEGER NOT NULL DEFAULT 0,
    fails_events          INTEGER NOT NULL DEFAULT 0,
    backoff_installs      TEXT,
    backoff_clicks        TEXT,
    backoff_events        TEXT,
    replay_until_installs INTEGER NOT NULL DEFAULT 0,
    replay_until_clicks   INTEGER NOT NULL DEFAULT 0,
    replay_until_events   INTEGER NOT NULL DEFAULT 0,
    enabled               INTEGER NOT NULL DEFAULT 1,
    created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_webhooks_app ON webhooks (app_id);

CREATE TABLE IF NOT EXISTS link_rules (
    id         TEXT PRIMARY KEY,
    link_id    TEXT NOT NULL REFERENCES links(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    name       TEXT NOT NULL,
    cond       TEXT NOT NULL,
    action     TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_link_rules_link ON link_rules (link_id, position);

CREATE TABLE IF NOT EXISTS variant_days (
    app_id   TEXT NOT NULL,
    link_id  TEXT NOT NULL,
    variant  TEXT NOT NULL,
    day      TEXT NOT NULL,
    clicks   INTEGER NOT NULL DEFAULT 0,
    installs INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (app_id, link_id, variant, day)
);

-- row_seq: highest rowid handed out per webhook stream table. TEXT primary keys = no AUTOINCREMENT: SQLite reuses
-- rowids after a tail delete, and the webhook cursor skips the new rows. Inserts take n + 1; triggers keep n >= any
-- rowid. Seeded from MAX(rowid) on every Open.
CREATE TABLE IF NOT EXISTS row_seq (
    name TEXT PRIMARY KEY,
    n    INTEGER NOT NULL
);
INSERT INTO row_seq (name, n) SELECT 'clicks', COALESCE(MAX(rowid), 0) FROM clicks WHERE true
    ON CONFLICT(name) DO UPDATE SET n = MAX(n, excluded.n);
INSERT INTO row_seq (name, n) SELECT 'installs', COALESCE(MAX(rowid), 0) FROM installs WHERE true
    ON CONFLICT(name) DO UPDATE SET n = MAX(n, excluded.n);
INSERT INTO row_seq (name, n) SELECT 'events', COALESCE(MAX(rowid), 0) FROM events WHERE true
    ON CONFLICT(name) DO UPDATE SET n = MAX(n, excluded.n);
CREATE TRIGGER IF NOT EXISTS clicks_row_seq AFTER INSERT ON clicks
BEGIN UPDATE row_seq SET n = MAX(n, NEW.rowid) WHERE name = 'clicks'; END;
CREATE TRIGGER IF NOT EXISTS installs_row_seq AFTER INSERT ON installs
BEGIN UPDATE row_seq SET n = MAX(n, NEW.rowid) WHERE name = 'installs'; END;
CREATE TRIGGER IF NOT EXISTS events_row_seq AFTER INSERT ON events
BEGIN UPDATE row_seq SET n = MAX(n, NEW.rowid) WHERE name = 'events'; END;
