CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);

-- ---------- Pocket Parcels: everything below is per person ----------

-- A parcel on its way to you (direction in) or one you sent (out).
CREATE TABLE parcels (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title            TEXT NOT NULL DEFAULT '',               -- what is inside, in the person's words
  tracking_number  TEXT NOT NULL DEFAULT '',               -- upper case, no spaces; '' = no number (a local delivery)
  carrier          TEXT NOT NULL DEFAULT '',               -- key from internal/track (china-post, dhl, ups…); '' = not known
  direction        TEXT NOT NULL DEFAULT 'in',             -- in | out
  shop             TEXT NOT NULL DEFAULT '',               -- where it was bought (in) or who gets it (out)
  order_number     TEXT NOT NULL DEFAULT '',
  ordered_on       TEXT NOT NULL DEFAULT '',               -- YYYY-MM-DD
  price            INTEGER,                                -- minor units of currency: what the order cost
  extra_cost       INTEGER,                                -- customs, VAT, postage paid on top
  currency         TEXT NOT NULL DEFAULT 'EUR',
  status           TEXT NOT NULL DEFAULT 'pending',        -- see internal/track: pending, info_received, in_transit, out_for_delivery, pickup, delivered, exception, returned, expired
  status_text      TEXT NOT NULL DEFAULT '',               -- the latest event, as the carrier said it
  status_at        TEXT NOT NULL DEFAULT '',               -- when that happened (RFC 3339)
  location         TEXT NOT NULL DEFAULT '',               -- where the latest event happened
  expected_on      TEXT NOT NULL DEFAULT '',               -- YYYY-MM-DD, earliest expected delivery day
  expected_until   TEXT NOT NULL DEFAULT '',               -- YYYY-MM-DD, latest ('' = a single day)
  expected_manual  INTEGER NOT NULL DEFAULT 0,             -- 1 = typed by the person, a carrier's estimate does not replace it
  delivered_at     TEXT NOT NULL DEFAULT '',
  return_by        TEXT NOT NULL DEFAULT '',               -- YYYY-MM-DD, last day to send it back
  return_state     TEXT NOT NULL DEFAULT '',               -- '' | keeping | returning | returned
  notes            TEXT NOT NULL DEFAULT '',
  archived         INTEGER NOT NULL DEFAULT 0,
  -- tracking
  provider         TEXT NOT NULL DEFAULT '',               -- what tracks it: 17track | cainiao | '' (email and by hand only)
  provider_ref     TEXT NOT NULL DEFAULT '',               -- the provider's own handle once registered (17TRACK carrier code)
  checked_at       TEXT NOT NULL DEFAULT '',
  next_check_at    TEXT NOT NULL DEFAULT '',
  check_error      TEXT NOT NULL DEFAULT '',
  -- where it came from
  source           TEXT NOT NULL DEFAULT 'manual',         -- manual | mail | import | demo | <acting app slug>
  external_id      TEXT NOT NULL DEFAULT '',
  mail_message_id  INTEGER,                                -- Pocket Mail message it was found in
  -- the other Pocket apps
  wish_id          INTEGER,                                -- Pocket Wishlist
  wish_title       TEXT NOT NULL DEFAULT '',
  money_tx_id      INTEGER,                                -- Pocket Money: the payment for the order
  money_tx_label   TEXT NOT NULL DEFAULT '',
  extra_tx_id      INTEGER,                                -- Pocket Money: the extra cost booked from here
  inventory_item_id INTEGER,                               -- Pocket Inventory
  contact_id       INTEGER,                                -- Pocket Contacts: who gets it (out) or sends it (in)
  contact_name     TEXT NOT NULL DEFAULT '',
  task_id          INTEGER,                                -- Pocket Tasks: the pick-up task
  return_task_id   INTEGER,                                -- Pocket Tasks: the send-it-back task
  cal_event_id     INTEGER,                                -- Pocket Calendar: the expected delivery day
  cal_synced       TEXT NOT NULL DEFAULT '',
  return_event_id  INTEGER,                                -- Pocket Calendar: the last day to return
  return_synced    TEXT NOT NULL DEFAULT '',
  notified_status  TEXT NOT NULL DEFAULT '',               -- last status a push went out for
  notified_on      TEXT NOT NULL DEFAULT '',               -- last daily reminder (returns, stuck parcels)
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX parcels_user ON parcels(user_id, archived, status);
CREATE UNIQUE INDEX parcels_user_number ON parcels(user_id, tracking_number) WHERE tracking_number != '';
CREATE INDEX parcels_due_check ON parcels(next_check_at) WHERE provider != '' AND archived = 0;
CREATE UNIQUE INDEX parcels_user_external ON parcels(user_id, source, external_id) WHERE external_id != '';

-- One step in a parcel's journey, from a carrier, an email or the person.
CREATE TABLE parcel_events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  parcel_id   INTEGER NOT NULL REFERENCES parcels(id) ON DELETE CASCADE,
  at          TEXT NOT NULL,                               -- RFC 3339 (the carrier's local time when it gave one)
  status      TEXT NOT NULL DEFAULT 'in_transit',
  description TEXT NOT NULL DEFAULT '',
  location    TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT 'manual',              -- 17track | cainiao | mail | manual | demo | <app slug>
  fingerprint TEXT NOT NULL,                               -- the same step from the same source is kept once
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX parcel_events_parcel ON parcel_events(user_id, parcel_id, at);
CREATE UNIQUE INDEX parcel_events_fp ON parcel_events(parcel_id, fingerprint);

-- A shipping email found in Pocket Mail: a parcel to add, or news about one.
CREATE TABLE mail_finds (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  mail_message_id INTEGER NOT NULL,
  subject         TEXT NOT NULL DEFAULT '',
  from_name       TEXT NOT NULL DEFAULT '',
  from_addr       TEXT NOT NULL DEFAULT '',
  received_at     TEXT NOT NULL DEFAULT '',
  tracking_number TEXT NOT NULL DEFAULT '',
  carrier         TEXT NOT NULL DEFAULT '',
  shop            TEXT NOT NULL DEFAULT '',
  order_number    TEXT NOT NULL DEFAULT '',
  title           TEXT NOT NULL DEFAULT '',
  status_hint     TEXT NOT NULL DEFAULT '',                -- shipped | out_for_delivery | delivered | pickup… when the email says so
  state           TEXT NOT NULL DEFAULT 'new',             -- new | added | updated | dismissed
  parcel_id       INTEGER REFERENCES parcels(id) ON DELETE SET NULL,
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX mail_finds_user ON mail_finds(user_id, state, received_at);
CREATE UNIQUE INDEX mail_finds_msg ON mail_finds(user_id, mail_message_id, tracking_number);

-- A paper or a link kept with a parcel: the invoice, the customs form, the return label.
CREATE TABLE attachments (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  parcel_id   INTEGER NOT NULL REFERENCES parcels(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL DEFAULT 'url',                 -- document (Pocket Documents) | url
  ref         INTEGER,                                     -- Pocket Documents id
  url         TEXT NOT NULL DEFAULT '',
  title       TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX attachments_parcel ON attachments(user_id, parcel_id);
