# API

JSON over HTTP at `/api/v1`. Authenticate with the session cookie, a personal token (`Authorization: Bearer ppc_…`, Settings → API & shortcuts), a Pocket Core device or user token, or a Pocket Core app-link token. Everything is per person: another person's ids answer 404. Money is in **minor units** (cents) of the parcel's `currency`; dates are `YYYY-MM-DD`, times RFC 3339. PUT bodies are partial: only the keys you send change.

Statuses: `pending` (nothing heard yet), `info_received` (label created), `in_transit`, `out_for_delivery`, `pickup` (waiting at a post office or locker), `delivered`, `exception` (a failed attempt, customs, damage), `returned` (going back to the sender), `expired` (tracking gave up).

## Overview

| Route | What |
|---|---|
| `GET /overview` | The home screen: `needs_you` (out for delivery, ready to pick up, arriving today), `on_the_way`, `problems` (exception or no news for `stale_days`), `returns`, `recent` (delivered in the last ten days), `finds` (shipping emails waiting), `counts`, `tracking {cainiao, seventeen}` |
| `GET /summary` | The short version Pocket AI reads: `parcels {today, pickup, problems, on_the_way, returns_due (≤7 days), delivered_today}` with `counts` (plus `emails_to_check`) |
| `GET /day/{date}` | One day for a journal (Pocket Day): `delivered`, `sent` (outgoing), `added`, `expected` and `steps` (every tracking step that day, with the parcel's title) |
| `GET /search?q&limit` | Parcels by name, tracking number, shop, order number, recipient or latest step |
| `GET /stats?months=12` | Per month (parcels, delivered, average days on the way), top shops and carriers, `spent` and `extra` per currency, `avg_days` |
| `GET /shops` | Shops ordered from, most used first (suggestions) |
| `GET /carriers` | Carriers known by name (`key`, `name`, `country`, `cainiao`), national posts after the companies |
| `POST /detect {number}` | What Parcels makes of a number: `number` (normalized), `valid`, `carrier`, `carrier_name`, `tracking_url`, `check_with` (`17track`, `cainiao` or "" = only email and by hand), `existing_id` when already followed |

## Parcels

| Route | What |
|---|---|
| `GET /parcels?view&q&shop&carrier&direction&status&limit&offset` | `{parcels, total}`. `view`: `active` (default; what needs you first, then soonest expected), `delivered`, `archived`, `returns` (a return deadline and not kept or sent back), `problems`, `all` |
| `POST /parcels` | `tracking_number` (spaces and dashes are fine) and/or `title` (one of the two is required), `carrier` (guessed from the number when left out), `direction` (`in`/`out`), `shop`, `order_number`, `ordered_on`, `price`, `extra_cost`, `currency`, `expected_on`, `expected_until`, `return_by`, `return_state`, `notes`, `contact_id`/`contact_name`, `wish_id`, `money_tx_id`, `mail_message_id`, `external_id` (another app sending the same `external_id` again gets the first parcel back) → the parcel, 201. A number already followed → 409 `{error, id}` |
| `GET /parcels/{id}` | The parcel with `events` (newest first), `attachments`, `carrier_name`, `status_label`, `tracking_url` (the carrier's page, else 17TRACK's), `can_check`, `arrives_in` (days), `stale`, and the pages it links to in the other apps (`mail_url`, `wish_url`, `money_url`, `extra_url`, `item_url`, `task_url`, `return_task_url`, `contact_url`) |
| `PUT /parcels/{id}` | Any field above, plus `status` (by hand), `archived`. An expected day typed here is kept: a carrier's estimate no longer replaces it |
| `DELETE /parcels/{id}` | With its timeline and papers; its calendar events go too |
| `POST /parcels/{id}/check` | Ask the tracking service now → `{parcel, changed, error?}` |
| `POST /parcels/{id}/delivered {at?}` | "It's here": delivered now (or at `at`) |
| `GET /parcels/{id}/events`, `POST /parcels/{id}/events` | The timeline; add a step `{description, status?, at?, location?}` (status guessed from the words when left out) |
| `DELETE /events/{id}` | Only steps added by hand |
| `POST /attachments`, `DELETE /attachments/{id}` | `{parcel_id, kind: document|url, ref (Pocket Documents id) or url, title}` |

Tracking: with a 17TRACK key (Settings → Tracking, per person) every number is registered once with 17TRACK (one unit of its quota) and read from then on; without one, AliExpress / Cainiao numbers (and China, Hong Kong, Singapore and Malaysia post numbers) are read from Cainiao's public tracking. Checks come round by status: out for delivery every 30 minutes, on the way every 2 hours, otherwise every 3–4 hours, once a day for a number nobody has heard of in a month.

## Shipping emails (Pocket Mail)

| Route | What |
|---|---|
| `GET /finds?state=new|added|updated|dismissed|all` | Emails that named a tracking number: `tracking_number`, `carrier`, `shop`, `order_number`, `title`, `status_hint`, `mail_url` |
| `POST /finds/{id}/add {title?, shop?}` | Follow it → the parcel, 201 |
| `POST /finds/{id}/dismiss` | Not a parcel to follow |
| `POST /mail/scan` | Look through new email now → `{ok, result: {read, found, added, updated}}` |

Every half hour Parcels reads the new receipts and updates in Pocket Mail (and searches for "tracking", "shipped", "pošiljka"). Numbers are found by their shape (UPU `RR123456785CN`, UPS `1Z…`, Cainiao `LP…`, DHL `JJD…`…), right after a tracking word in several languages, or in a carrier's tracking link; the person's AI reads only shipping emails the patterns found nothing in, and a number it gives that is not in the email is dropped. An email about a parcel already followed ("delivered", "ready for pick-up") becomes a step in its timeline.

## The other Pocket apps

| Route | What |
|---|---|
| `POST /parcels/{id}/wish {wish_id, mark_bought?}` | Link the wish it is (Pocket Wishlist); marks it bought there unless `mark_bought: false` or the setting is off. `wish_id: 0` unlinks |
| `POST /parcels/{id}/money {tx_id}` | Link the payment (Pocket Money; fills the price when empty). `{tx_id: 0}` unlinks. `{book_extra: true, account_id?, category_id?}` books `extra_cost` (customs, postage) as an expense |
| `POST /parcels/{id}/inventory {location?}` | Put what arrived on the shelves (Pocket Inventory) |
| `POST /parcels/{id}/task {kind: pickup|return}` | "Pick up …" (due today) or "Send back …" (due the day before the deadline) in Pocket Tasks |
| `GET /links/wishlist?q`, `/links/money/transactions?q&near=YYYY-MM-DD`, `/links/money/accounts`, `/links/money/categories?account_id`, `/links/calendar/calendars`, `/links/contacts?q`, `/links/documents?q` | Pickers: `{connected, items, detail?}` |
| `POST /links/calendar/sync` | Put expected delivery days (and return deadlines) on the calendar now |

Automatic, per person (Settings): a pick-up task when a parcel waits at a pick-up point, a send-back task when you choose to send one back, the delivered parcel on the shelves, the wish marked bought when a parcel is linked to it, delivery days and return deadlines as all-day events in Pocket Calendar.

## Pocket Core app links

Offers `parcels:read` (`GET /auth/me`, `/overview`, `/summary`, `/parcels`, `/parcels/{id}`, `/parcels/{id}/events`, `/search`, `/shops`, `/carriers`, `/stats`, `/day/{date}`, `POST /detect`) and `parcels:write` (`POST /parcels`, `PUT|DELETE /parcels/{id}`, `POST /parcels/{id}/events|check`). An app may change, remove or add steps only to parcels it added itself (`source` = its slug).

Uses Pocket Mail `mail:read`, Pocket Money `accounts:read transactions:read transactions:write`, Pocket Wishlist `wishes:read wishes:write`, Pocket Inventory `items:read items:write`, Pocket Calendar `calendar:read calendar:write`, Pocket Tasks `tasks:write`, Pocket Contacts `contacts:read`, Pocket Documents `documents:read`, and Pocket AI for inference.

Events on the Core bus (each with `user_email`, `parcel_id`, `title`, `tracking_number`, `carrier`, `shop`, `status`, `status_label`, `latest`, `location`, `expected_on`, `url`): `parcels.added`, `parcels.status_changed` (+ `previous_status`, `source`), `parcels.out_for_delivery`, `parcels.ready_for_pickup`, `parcels.delivered`, `parcels.problem`, `parcels.return_due` (`return_by`, `days_left`), `parcels.found_in_mail` (`count`).

## Settings, data

`GET/PUT /settings`, `GET /settings/system`, `POST /integrations/test {which}` (an app slug, `17track` — answers the quota left —, `cainiao`, `openai`, `notify`), `POST /notify/test`, `POST /notify/check` (send today's reminders now), `GET/POST/DELETE /tokens`, `GET /backup` (admin), `GET /export.json`, `GET /export.csv` (one row per parcel), `POST /import` (an export; importing the same file twice adds nothing). Health: `GET /healthz`, `/readyz`, `/metrics`.
