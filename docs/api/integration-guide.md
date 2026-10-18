# Taptime integration API — integration guide

Guide edition 1 (2026-10-16), for API version 1. This guide is served by the API itself, without a
key, at `GET /v1/guide.md`; its Turkish translation, the same document section by section, at
`GET /v1/guide.tr.md`. The machine-readable contract is the OpenAPI 3.1 document at
`GET /v1/openapi.yaml`; this guide explains it, and where they say the same thing they agree.

Every example answer in this guide is an answer the API gave: they come from one story, told in
sections 4 to 9, run against the real service.

<a id="contents"></a>
## Contents

- [1. Overview](#overview)
- [2. Authentication and keys](#auth)
- [3. The common contract](#common)
- [4. Locations](#locations)
- [5. Employees and activation](#employees)
- [6. The punch feed](#feed)
- [7. The history query](#history)
- [8. The directory](#directory)
- [9. Webhooks](#webhooks)
- [10. Suspension](#suspension)
- [11. Contract differences](#differences)
- [12. Changes to this guide](#changes)

<a id="overview"></a>
## 1. Overview

<a id="overview-what"></a>
### 1.1 What the API does

Taptime records working time by touch: an employee touches a plaque mounted at a location with
their own phone. The integration API lets a business's HR or ERP system:

- write the business's **locations** and **employees** (`PUT`, `DELETE`), so that Taptime knows the
  same places and people your system does, under your own references;
- read the business's **punches** from a feed that never loses one (`GET /v1/punches`), or a time
  range of them (the history query);
- read the **directory** of the employees the business may know of (`GET /v1/directory`);
- receive each punch as it happens, by **webhook**.

A request reads and writes **one business only — the one its key belongs to**. No request names a
business, and no parameter widens what a key can reach.

<a id="overview-companies"></a>
### 1.2 Businesses, companies and groups

In Taptime each company is its own **business**, with its own key, its own feed, its own webhook
and its own company code (the `company` field of a punch, for example `KF` or `KM`). Taptime sets
the company code when it enables the API for the business.

Two businesses can belong to one **group of companies** (Taptime sets the group up). In a group:

- an employee of one company can punch at the other company's plaques, and the other way round;
- such a punch **belongs to the employer** — the employer's rules judge it, a manager of the
  employer approves it if it needs approval, and corrections are made at the employer;
- it is in the employer's feed (scope `employer`) **and** in the feed of the company whose plaque
  was touched (scope `location`), with the same `id` in both — see [section 6.2](#feed-scopes);
- each company's directory also lists the other company's employees — see [section 8](#directory).

The story of this guide has two companies of one group: **KF**, whose location `KF-VLT`
("Valletta") has a plaque, and **KM**, whose departments are its locations: in Taptime **a KM
department is a location**, and its plaque is mounted there (`DEP-1`, "Production"). An employee of
KM who works a shift at KF's Valletta touches KF's plaque there.

<a id="overview-host"></a>
### 1.3 Where to send requests

The API has a host of its own: the `servers` URL of the OpenAPI document. Every path is under
`/v1`. The examples below write that address as `$TAPTIME_API` and the key as `$TAPTIME_KEY`; they
are placeholders, not values. On any other Taptime host `/v1` does not exist (404), and the API's
host answers nothing but `/v1` (404). The API sets no cookie and reads none.

<a id="overview-sandbox"></a>
### 1.4 Sandbox

The sandbox is a separate Taptime for testing an integration: its own domain, its own build of the
server, its own database and its own operator. No production business, key or punch is in it, and
nothing in it reaches production. **Its domain is not chosen yet**: this section will name the
sandbox's hosts when it is. Until then, test against a business Taptime enables for the purpose.

- **Keys start with `tt_test_`** and are issued in the sandbox's Taptime panel. A `tt_test_` key is
  refused in production (401) and a `tt_live_` key is refused in the sandbox (401), before anything
  is looked up.
- **Taptime enables the API there too**, business by business ([2.1](#auth-enable)): until the
  sandbox's operator does, a valid key is 403 `api_disabled`.
- **No e-mail leaves the sandbox**: an activation link is shown in the sandbox's panel, never sent.
- **Everything in this guide holds there**, with one addition: POST /v1/sandbox/taps simulates an
  employee's touch of a location's plaque, because the sandbox has no wall and no phone. Its own
  OpenAPI document is GET /v1/sandbox/openapi.json on the sandbox's API host.

The body names the employee and the location by your references, and the channel (`nfc` or `qr`):

```sh
curl -sS -X POST "$TAPTIME_API/v1/sandbox/taps" \
  -H "Authorization: Bearer $TAPTIME_KEY" \
  -H "Content-Type: application/json" \
  --data-binary '{"employeeRef":"E-1042","locationRef":"KF-VLT","channel":"nfc"}'
```

From the plaque on, the tap is Taptime's own: the plaque's signed read is checked, its counter is
advanced, the rules decide, the record is written, and a punch it makes is in the feed and is sent
to your webhook. The answer (`201`) carries the `verdict` (`ok`, `flag`, `reject` or `ignored`),
the `punchId` the feed and the webhook carry for it, and whether the tap is a punch now (`inFeed`).
An `Idempotency-Key` works as on every write ([3.3](#idempotency)).

- A location created through the API has no address range or coordinate until its owner sets them
  in the panel, so its taps are `flag` and wait for approval ([4](#locations)). The request's own
  address is the tap's address and there is no GPS: give the location your caller's address range
  to see `ok`.
- The same person again within 60 seconds is `ignored`, as in production ([6.1](#feed-what)).
- A location gets its virtual plaque on its own business's first simulated tap there. A location of
  the other company of your group ([1.2](#overview-companies)) can be touched by your employee — the
  group touch — once that company has simulated a tap there itself: the sandbox never provisions a
  plaque in another business. The group touch itself is recorded as a real one is: it advances that
  company's plaque counter and appears in its location feed.

<a id="overview-scope"></a>
### 1.5 What the API does not do (version 1)

- It does not write punches, and it does not cancel or correct them: corrections are made in the
  Taptime panel, by the employer, and a v1 punch is never changed (`version` stays `1`).
- It has no endpoints for departments as such (a KM department is a location), and it cannot delete
  a location.
- It does not manage the webhook: the endpoint, its address and its signing key are the business
  owner's, in the Taptime panel.
- A location has no time zone of its own: the business's time zone is every location's.
- A partner company cannot approve another company's employee's punch.
- It has no OAuth: a key is the only credential.

<a id="auth"></a>
## 2. Authentication and keys

<a id="auth-enable"></a>
### 2.1 Taptime enables the API

The API works for a business only after Taptime enables it for that business (and sets its
company code). Until then — and again if Taptime disables it — every request with a valid key is
answered **403 `api_disabled`**, and the business's webhook sends nothing; the business's punches
are still recorded in its feed. A business owner cannot create a key while the API is disabled.

<a id="auth-keys"></a>
### 2.2 Keys

The business's **owner** creates keys in the Taptime panel, section **API keys**. A key looks like
`tt_live_` followed by 43 base64url characters; it is shown **once**, when it is created, and never
again. The panel lists each key by its name, its first characters and when it was last used. The
owner can revoke one key, or every key at once.

Send the key in every request (except the three documents) as

```http
Authorization: Bearer <key>
```

- A key of another shape is refused before anything else is looked up: **401 `unauthorized`**.
- An unknown key and a revoked key get the **same** 401, with
  `WWW-Authenticate: Bearer realm="taptime"`. A revocation, and Taptime's disabling of the API,
  take effect on the very next request: nothing is cached.
- Keep the key out of logs, URLs and headers other than `Authorization`.
- **Never put a key or another secret into `X-Request-Id`.** Taptime may write the `X-Request-Id`
  you send into its logs as the request's id: a value of 1 to 64 characters, each one of `A`–`Z`,
  `a`–`z`, `0`–`9`, `-`, `_` and `.`, that contains none of `tt_live_`, `tt_test_` and `tt_whsec_`
  (in any case). Such a value is written as it is — a key's characters without its prefix
  included. Any other value — longer, with another character, or with one of the three prefixes — is
  replaced by an id of Taptime's own: keep to the set and the length if you want to match your id
  with Taptime's support.

A business may have several keys (one per system is a good rule). Each key has its own request
budget and its own deactivation budget, and its own Idempotency-Keys — see
[section 3.3](#idempotency) and [section 3.4](#rate-limits).

<a id="common"></a>
## 3. The common contract

<a id="errors"></a>
### 3.1 Errors

Every refusal is a JSON body of exactly three fields:

<!-- error: BadRequest/limit -->
```json
{"code": "invalid_request", "message": "limit must be a whole number from 1 to 1000.", "details": {"max": 1000, "parameter": "limit"}}
```

- `code` is the machine-readable answer — one of the codes below, and nothing else.
- `message` is a sentence for a person. It never repeats a value the request sent.
- `details` is always an object, possibly empty; its keys are listed after the codes.

Each operation of the OpenAPI document lists, in `x-error-codes`, exactly the codes it can answer.

| Code | Status | Meaning |
|---|---|---|
| `invalid_request` | 400 | A parameter, a header or a body the contract refuses. |
| `body_too_large` | 400 | The request body is over 65536 bytes. |
| `unauthorized` | 401 | No key, or a key that is not valid. |
| `api_disabled` | 403 | The API is not enabled for this business. |
| `tenant_suspended` | 403 | The business is suspended and this change is not accepted. |
| `not_found` | 404 | No such resource (also: an unknown path, or a method the path does not take). |
| `email_unavailable` | 409 | Another active employee of the business has this e-mail address. |
| `already_activated` | 409 | The employee has activated already. |
| `idempotency_key_reused` | 422 | The Idempotency-Key was used with a different request. |
| `validation_failed` | 422 | A body field, or the path's reference, the contract refuses. |
| `timezone_mismatch` | 422 | A location's time zone must be its business's. |
| `rate_limited` | 429 | Too many requests; wait `Retry-After` seconds. |
| `deactivation_budget_exceeded` | 429 | This key deactivated its hourly budget of employees; wait `Retry-After` seconds. |
| `unavailable` | 503 | The request could not be completed on Taptime's side; retry. |

The keys of `details`:

| Key | Where |
|---|---|
| `field` | `validation_failed`, `timezone_mismatch`: the body field at fault, or `ref` for the path's reference. |
| `reason` | Why — see the next table. |
| `parameter` | The query parameter at fault (`limit`, `cursor`, `scope`, `updatedSince`). |
| `parameters` | The query parameters at fault together (`from` and `to`). |
| `allowed` | The names the endpoint accepts. |
| `header` | The header at fault (`Content-Encoding`, `Idempotency-Key`). |
| `max` | The largest `limit`. |
| `maxBytes` | The largest body. |
| `maxHours` | The longest history range, in hours. |
| `retryAfterSeconds` | The same as `Retry-After`. |
| `businessTimezone` | The business's own time zone (`timezone_mismatch`). |

The values of `reason`:

| Reason | Code | Meaning |
|---|---|---|
| `required` | `validation_failed` | A required field is missing (or, for resend-activation, the employee has no address). |
| `type` | `validation_failed` | A field has the wrong JSON type. |
| `invalid` | `validation_failed` | A field's value is outside its rule (a length, an address, a reference's pattern). |
| `mismatch` | `validation_failed` | `externalRef` in the body is not the reference in the path. |
| `unknown_field` | `validation_failed` | The body names a field the endpoint does not take. |
| `duplicate_field` | `validation_failed` | The body names one field twice. |
| `hidden_character` | `validation_failed` | A new or changed name carries an invisible or control character. |
| `administrator_address` | `validation_failed` | The e-mail address is an administrator's of the business. |
| `malformed_json` | `invalid_request` | The body is not one JSON object. |
| `invalid_utf8` | `invalid_request` | The body is not UTF-8, or a string carries an unpaired surrogate escape. |
| `unencoded_plus` | `invalid_request` | An instant's `+` was sent unencoded (it arrives as a space). |

A **503 `unavailable`** is Taptime's failure, never yours: retry it after a short, growing wait. A
write retried with the same Idempotency-Key is safe (a failed write recorded nothing). A request
that is not answered within 30 seconds is answered 503.

<a id="paging"></a>
### 3.2 Pages and `limit`

Every list is paged. `limit` is a whole number from 1 to 1000; without it a page holds up to 500.
Anything else — 0, 1001, a sign, a fraction — is **400 `invalid_request`** with
`details.parameter` `limit`; a second `limit` is a repeated parameter, answered like one
([3.7](#query)). Each list has its own cursor and its own rule for the last page;
they are described with the lists ([5.6](#employees-read), [6.4](#feed-paging),
[7](#history), [8.2](#directory-walk)).

<a id="idempotency"></a>
### 3.3 Idempotency-Key

Every write (`PUT`, `DELETE`, `POST`) may carry an `Idempotency-Key` header: one value of 1 to 255
visible ASCII characters (a second header, a space or a non-ASCII character is 400 with
`details.header` `Idempotency-Key`). Repeat a write with the same key within 24 hours and:

- the **same request** is answered with the first request's status, without doing the work again,
  and with the header `Idempotent-Replayed: true`. The body is the resource **as it is now** (a
  rename in between shows); if the resource no longer exists, the replay is 404 `not_found`;
- a **different request** is **422 `idempotency_key_reused`**;
- two requests with one key at the same time are answered one after the other.

"The same request" is **byte for byte**: the method, the path exactly as sent (its
percent-encoding included) and the body's bytes — a write takes no query string
([3.7](#query)). Other whitespace in the JSON makes a **different** request — 422.
Headers are **not** part of it: a different `Content-Type` or `User-Agent` with the same key is a
replay. Two spellings of one reference (`%41BC` and `ABC`) are one location, but two requests.

Idempotency-Keys are kept per integration key: two keys of one business do not share them. A write
that failed (any 4xx or 503) recorded nothing — its key can be used again.

<a id="rate-limits"></a>
### 3.4 Rate limits

| Limit | Value |
|---|---|
| Requests per client address per minute | 1200 |
| Requests per key per minute | 600 |
| Deactivations per key per hour | 20 |

The address limit counts the requests from one address — the three documents, and every request
whose key is well formed — before the key is even looked up; the key limit counts a key's requests
from every address. Over either:
**429 `rate_limited`** with `Retry-After` (seconds; today the whole minute). No other rate-limit
header is sent. Deactivations have their own budget — see [section 5.5](#employees-deactivate).

<a id="limits"></a>
### 3.5 Limits at a glance

These are the OpenAPI document's `info.x-limits`, and the service's own values.

| Key | Value | What |
|---|---|---|
| `requestsPerAddressPerMinute` | 1200 | Requests per client address per minute. |
| `requestsPerKeyPerMinute` | 600 | Requests per key per minute. |
| `deactivationsPerKeyPerHour` | 20 | Deactivations per key per hour. |
| `maxBodyBytes` | 65536 | The largest request body. |
| `defaultPageSize` | 500 | A page without `limit`. |
| `maxPageSize` | 1000 | The largest `limit`. |
| `idempotencyHours` | 24 | How long an Idempotency-Key is kept. |
| `requestTimeoutSeconds` | 30 | A request not answered by then is 503. |
| `feedWindowPositions` | 10000 | The feed positions one page reads at most. |
| `historyMaxHours` | 745 | The longest history range (31 days and one hour). |
| `directoryOverlapMinutes` | 10 | How far before `updatedSince` the directory reads. |
| `webhookTimeoutSeconds` | 10 | How long a webhook try waits for an answer. |
| `webhookAttempts` | 6 | Tries of one webhook delivery. |
| `webhookTestMessagesPerHour` | 10 | Test messages and "send again now" per business per hour. |
| `invitesPerBusinessPerHour` | 50 | Activation e-mails a business sends per hour. |
| `invitesPerBusinessPerDay` | 300 | Activation e-mails a business sends per day. |
| `invitesPerEmployeePerHour` | 3 | Activation e-mails one employee is sent per hour. |
| `invitationQueueHours` | 72 | How long a queued activation e-mail waits at most. |
| `invitationSendAttempts` | 5 | Tries of one activation e-mail. |

<a id="bodies"></a>
### 3.6 Request bodies

- At most **65536 bytes**: a larger body is **400 `body_too_large`** (`details.maxBytes`). A body
  that is compressed or otherwise encoded (`Content-Encoding` other than `identity`) is 400.
- One **JSON object**, in **UTF-8**: anything else is 400 `invalid_request` (`reason`
  `malformed_json`; `invalid_utf8` for a body that is not UTF-8 or a string with an unpaired
  surrogate escape such as `\ud800`).
- Exactly the documented fields, **each at most once**, spelled exactly (`Name` is not `name`):
  any other field is **422 `validation_failed`** with `reason` `unknown_field` and the accepted
  names in `details.allowed`; a field named twice is 422 `duplicate_field` — the last value does
  not win.
- Send `Content-Type: application/json`.

<a id="query"></a>
### 3.7 Query parameters

Every `GET` takes exactly its documented parameters, each at most once, spelled exactly: any other
parameter (`Scope`, `curser`), a repeated one and a query string that does not parse (`%zz`, a `;`
between parameters) are **400 `invalid_request`**, with the accepted names in `details.allowed`.
A misspelt parameter is never ignored. `GET /v1/employees/{ref}` and the three documents take no
parameter at all: any is 400, with `details.allowed` empty. A write (`PUT`, `DELETE`, `POST`) takes
**no query string at all**: anything after the `?` — a parameter, one that does not parse, an empty
`&` — is **400 `invalid_request`** with `details.allowed` empty, and nothing is written. There is no
`dryRun` or the like: a `PUT` sent with `?dryRun=1` is refused, not carried out. A refused write
spends no Idempotency-Key ([3.3](#idempotency)): send the same key again without the query.

Instants in a query string (`from`, `to`, `updatedSince`) are RFC 3339 —
`YYYY-MM-DDThh:mm:ss`, an optional `.fraction` after a dot, then `Z` or an offset `±hh:mm` — with
upper-case `T` and `Z` and at most microsecond precision (a finer instant is refused, not rounded).
**Encode the `+` of an offset as `%2B`**: unencoded it arrives as a space, and the answer is 400
with `reason` `unencoded_plus`.

<a id="identifiers"></a>
### 3.8 Identifiers, references and times

- Taptime's identifiers: `emp_` (employee), `loc_` (location), `pch_` (punch) and `dlv_` (webhook
  delivery) followed by a UUID; `tag_` followed by the plaque's 14 hexadecimal characters. Keep
  them as opaque strings.
- **Your references** (`externalRef`, the `{ref}` of a path) are 1 to 64 characters of `A-Z`,
  `a-z`, `0-9`, `.`, `_`, `~` and `-`, starting with a letter or a digit. In a path a reference may
  be percent-encoded: it is decoded **once** and must then match (`%41BC` is `ABC`; an encoded
  `/`, a control character or a double encoding such as `%2541` is 422 with `field` `ref`).
- Every instant the API answers is RFC 3339 in **UTC**. A business's time zone matters for its
  reports, not for the API.
- A field that has no value is `null`, never absent.

<a id="headers"></a>
### 3.9 Response headers

Every `/v1` answer carries `Cache-Control: no-store`, `X-Content-Type-Options: nosniff` and
`Referrer-Policy: no-referrer` — except the two guides, which may be kept and revalidated
(`Cache-Control: no-cache`, an `ETag`, 304 on a matching `If-None-Match`). A 429 carries
`Retry-After`, a 401 `WWW-Authenticate`, a replayed write `Idempotent-Replayed: true`.

<a id="locations"></a>
## 4. Locations

`PUT /v1/locations/{ref}` creates or updates one of the business's locations under your reference.

| Field | Type | |
|---|---|---|
| `name` | string, required | 1 to 80 characters once surrounding spaces are removed. |
| `active` | boolean, required | A label only — see below. |
| `timezone` | string, optional | When sent, it must be the business's time zone. |
| `externalRef` | string, optional | When sent, it must be the reference in the path. |

- An unknown reference **creates** the location: **201**. A known one is renamed or relabelled:
  **200**. A request that matches the location as it is changes nothing: 200, and nothing is
  written.
- A `PUT` sends the whole location: `name` and `active` every time. Its two optional fields hold
  nothing of their own — `timezone` is the business's, `externalRef` the path's — so leaving them
  out changes nothing.
- A location is **never matched by its name**. A location that already exists in Taptime (set up
  in the panel) is reached through the API only after its owner fills in its **External ref** in
  the Taptime panel; until then the same reference would create a second location.
- **`timezone`**: a location has no time zone of its own. A `timezone` that is not the business's
  is **422 `timezone_mismatch`**, and `details.businessTimezone` names the business's.
- **`active` is a label.** It is stored and answered; it changes nothing about how taps at the
  location are judged — an inactive location's taps are judged like an active one's.
- **Taps at a location the API created wait for approval.** The proof that a tap happened at a
  location — its network address ranges and its coordinate — is set in the Taptime panel only, never
  through the API. Until it is, every tap there is recorded and **waits for a manager's
  approval**, and it reaches the feed only if it is approved ([section 6.1](#feed-what)).
- **Names.** Emoji, flags and every script's letters are accepted. A soft hyphen, the direction
  marks U+200E, U+200F and U+061C and a byte-order mark at either end are silently removed. A **new
  or changed** name cannot carry a control character, a line or paragraph separator, a direction
  override or isolate (U+202A–U+202E, U+2066–U+2069), a zero-width space (U+200B), a word joiner
  (U+2060) or an inner U+FEFF, nor a joiner (U+200C, U+200D) that is not between two visible
  characters or a tag character outside a flag: 422 `validation_failed`, `reason`
  `hidden_character` (a length refusal is `invalid`). A name already on file is accepted as it is.
- While the business is suspended this endpoint is 403 `tenant_suspended`
  ([section 10](#suspension)).

In the story, KF puts its location:

```sh
curl -sS -X PUT "$TAPTIME_API/v1/locations/KF-VLT" \
  -H "Authorization: Bearer $TAPTIME_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: loc-KF-VLT-v1" \
  --data-binary '{"name":"Valletta","active":true,"timezone":"Europe/Malta"}'
```

<!-- example: putLocation 201 -->
```json
{
  "id": "loc_30258476-2ce5-4a5d-a7dd-36651c3019dd",
  "externalRef": "KF-VLT",
  "name": "Valletta",
  "timezone": "Europe/Malta",
  "active": true,
  "createdAt": "2026-10-16T09:13:12.861739Z"
}
```

KM puts its department `DEP-1` ("Production") the same way: a KM department is a location.

<a id="employees"></a>
## 5. Employees and activation

<a id="employees-put"></a>
### 5.1 Creating and updating an employee

`PUT /v1/employees/{ref}` creates, updates or deactivates one employee under your reference.

| Field | Type | |
|---|---|---|
| `firstName` | string, required | |
| `lastName` | string, required | Together with `firstName` at most 120 characters; spaces are collapsed. |
| `email` | string or `null`, required | The activation e-mail's address; `null` for none. The key must be present. |
| `active` | boolean, required | `false` deactivates ([5.5](#employees-deactivate)). |
| `employeeCode` | string or `null`, optional | 1 to 64 characters. Left out, it becomes `null`. |
| `externalRef` | string, optional | When sent, it must be the reference in the path. |

- **An unknown reference first binds** an employee the business already has in Taptime: an
  employee who is not deactivated, has no reference and has the e-mail address sent (compared
  without regard to case; in this business only, never in a partner company). The binding answers **200**. If the
  stored address differs only in case, the binding is followed by an address change.
- Otherwise the reference **creates** a new employee: **201**, and when `email` is an address an
  activation e-mail is **queued** ([5.2](#employees-activation)).
- A known reference is updated: **200**. A request that matches the employee as they are changes
  nothing: 200.
- **A `PUT` replaces, it does not patch**: a left-out `employeeCode` becomes `null`. An employee
  whose `employeeCode` is `77`, put without `employeeCode`, is answered with `employeeCode: null` —
  and their next punches and a partner company's directory carry `null` too. Send every field you
  hold, every time. (`externalRef`, the other optional field, holds nothing of its own: left out,
  it is the reference in the path.)
- **The address**: one plain ASCII address (no display name, no spaces, at most 254 characters).
  An address of an administrator of the business is **422** (`reason` `administrator_address`). An
  address another **active** employee of the business holds is **409 `email_unavailable`**: nothing
  is created, changed or sent. A deactivated employee's address is free again.
- **Changing the address**: every activation link sent to the old address stops working, in the
  same change. An employee who has not activated is sent a new activation e-mail; **an activated
  employee is not sent one**.
- **Re-hiring**: `active: true` on a reference whose employee is deactivated creates a **new
  employee** — a new `id` under the same `externalRef`; the earlier employee and their history stay
  as they were. Key your records by `id`.
- **Names** follow the location name rule ([section 4](#locations)), and a tab or a line break
  inside a name is `hidden_character` too.
- **What these answers tell**: 409 `email_unavailable`, 422 `administrator_address` and a binding
  by address tell the key's holder whose an address is **inside its own business** — the key that
  writes the business's employees. They tell nothing about any other business.

```sh
curl -sS -X PUT "$TAPTIME_API/v1/employees/E-1042" \
  -H "Authorization: Bearer $TAPTIME_KEY" \
  -H "Content-Type: application/json" \
  --data-binary '{"firstName":"Jane","lastName":"Example","email":"jane.example@example.com","active":true,"employeeCode":"1042"}'
```

<!-- example: putEmployee 201 -->
```json
{
  "id": "emp_8973fbc8-5e76-48e2-a80b-67d5b10baa91",
  "externalRef": "E-1042",
  "employeeCode": "1042",
  "firstName": "Jane",
  "lastName": "Example",
  "fullName": "Jane Example",
  "active": true,
  "activated": false,
  "activatedAt": null,
  "hasEmail": true,
  "activationEmail": {"status": "queued", "at": "2026-10-16T09:13:12.905861Z"},
  "createdAt": "2026-10-16T09:13:12.891784Z",
  "updatedAt": "2026-10-16T09:13:12.902916Z"
}
```

No answer of the API carries an e-mail address: `hasEmail` says whether there is one.

<a id="employees-activation"></a>
### 5.2 Activation

A new employee with an address gets an **activation e-mail automatically**. The e-mail is sent
from a queue: the request is never refused or slowed by Taptime's sending limits — the e-mail is
delayed instead — and the link in it is created when the e-mail is sent.

The employee opens the link on their phone and then **touches a plaque of their own company**
(never a partner company's), **with NFC** — a QR code cannot activate. That first touch completes
the activation and **is not a punch**: the employee's first punch is their next touch.

<a id="employees-email-state"></a>
### 5.3 The activation e-mail's state

`activationEmail.status` is one of four values, with `activationEmail.at` its time:

| Status | Meaning |
|---|---|
| `queued` | The e-mail waits to be sent — also while a failed send is being retried. |
| `sent` | An e-mail was delivered and its link still works. |
| `undelivered` | The last attempt did not reach the employee and nothing is queued: send `resend-activation`. |
| `none` | None of those — never asked, or the link was used, expired or replaced. |

A queued e-mail waits **at most 72 hours** — behind a sending limit, or while Taptime has disabled
the API for the business (the queue stops while the API is disabled and goes on when it is enabled
again; the 72 hours run either way). A real send failure is tried **5 times**. When either runs
out, the status is `undelivered`; the answer does not say which, because the remedy is the same:
`resend-activation`.

A business sends at most **50 activation e-mails an hour and 300 a day**, and one employee is sent
at most 3 an hour. A first synchronisation that creates more than about 900 employees at once
therefore cannot send every e-mail within 72 hours: the rest become `undelivered`. Spread a large
first synchronisation over several days, or send `resend-activation` for the employees that end
`undelivered`.

<a id="employees-resend"></a>
### 5.4 `resend-activation`

`POST /v1/employees/{ref}/resend-activation` asks for the activation e-mail to be sent again.

- It always answers **202** with the employee — also when it changes nothing: while an e-mail is
  queued, or one was sent in the last hour and its link still works, the request has no effect. It
  never cancels a link when it is asked; the new link, when one is sent, replaces the older ones.
- An employee who has activated is **409 `already_activated`**: a new phone is set up in the
  Taptime panel.
- An employee without an address is 422 (`field` `email`, `reason` `required`).
- It is accepted while the business is suspended.

<a id="employees-deactivate"></a>
### 5.5 Deactivation

`DELETE /v1/employees/{ref}`, or `PUT` with `active: false`, deactivates the employee: their taps
are refused from then on, and their history stays.

- **The deactivation budget**: one key may deactivate **20 employees an hour**. Past that, the
  answer is **429 `deactivation_budget_exceeded`** with `Retry-After` — the seconds until the oldest
  of the hour's deactivations leaves the window — and the business's owner sees an alert in the
  panel. This code is not `rate_limited`, so your system can tell a budget from a request limit.
- An employee already deactivated is answered as they are: 200, nothing spent — except while the
  business is suspended.
- A reference the business never held is **404 `not_found`** — but for a `DELETE` while the
  business is suspended (below).
- While the business is suspended, every `DELETE` is 403 `tenant_suspended` — also for an employee
  already deactivated and for a reference the business never held — and so is every `PUT` with
  `active: false` that lands on an existing employee, one already deactivated included. A `PUT` with
  `active: false` on a reference the business never held lands on no employee: it stays 404
  ([section 10](#suspension)).

<!-- example: deleteEmployee 200 -->
```json
{
  "id": "emp_32a024b8-e86f-4110-a4da-c05ee0acd9b7",
  "externalRef": "E-1043",
  "employeeCode": null,
  "firstName": "Sam",
  "lastName": "Placeholder",
  "fullName": "Sam Placeholder",
  "active": false,
  "activated": false,
  "activatedAt": null,
  "hasEmail": false,
  "activationEmail": {"status": "none", "at": null},
  "createdAt": "2026-10-16T09:13:12.918861Z",
  "updatedAt": "2026-10-16T09:13:12.95631Z"
}
```

<a id="employees-read"></a>
### 5.6 Reading employees

- `GET /v1/employees` pages through **every** employee of the business — active and deactivated,
  and also those created in the Taptime panel and not bound to a reference yet (`externalRef`
  `null`; their `firstName` and `lastName` are `null` and `fullName` is their name). It takes only
  `limit` and `cursor`. A full page (`limit` employees) carries `nextCursor`; pass it as `cursor`.
  A shorter page is the last, and its `nextCursor` is `null`.
- `GET /v1/employees/{ref}` answers the active employee under the reference or, when none is
  active, the most recently deactivated one; 404 when the business never held the reference.

<a id="feed"></a>
## 6. The punch feed

`GET /v1/punches?scope=…` reads the business's punches in the order its feed received them, and
never loses one.

<a id="feed-what"></a>
### 6.1 What is a punch

A punch is a tap Taptime **counts**:

- a tap judged `ok`, by NFC (`nfc`) or by QR code (`qr`) — it enters the feed when it is recorded;
- a tap judged `flag` (recorded, waiting for a manager's approval) **once a manager of the
  employer approves it** — it enters the feed at the approval, once. A rejected flag never enters.

Nothing else is a punch, and nothing else is in the feed:

- the **activation touch** ([5.2](#employees-activation));
- a **manual record** a manager enters in the Taptime panel — manual records are your system's;
- a **refused** tap (for example a deactivated employee's, or a touch of a **lost or retired
  plaque**, the business's own or a partner company's) and a **practice** tap;
- a **repeat** of the same person within **60 seconds** — in the instants the phones declared
  **or** on Taptime's own clock, whichever is closer: it is ignored — per person, not per plaque,
  and across the two companies' plaques too. Should a client keep taps in an offline queue and send
  two back to back, the two are close on Taptime's clock, so the second is ignored even when the
  instants they declare are further apart.

**Time.** `punchTime` is the instant **of the tap**: the instant the employee's phone declared,
when it declared one, and otherwise Taptime's own instant of the tap. Taptime's tap page declares
none today, so today every `punchTime` is Taptime's instant. `recordedAt` is the instant the punch
entered the feed (for an approved flag, the approval's). A client that declares an instant —
should one, for example, keep a tap made offline in a queue and send it later with the instant it
was made (no Taptime client does today: the tap page keeps no queue) — is judged by it: up to
**120 seconds** late (by default; the business can change this) the tap is judged like a live one;
later than that it needs a manager's approval; more than **72 hours** in the past, or in the
future, it is refused. A tap sent at once
declares no instant: its `punchTime` is Taptime's own instant of the tap, milliseconds from its
`recordedAt` — as in the story, whose taps are live.

<a id="feed-scopes"></a>
### 6.2 Scopes

`scope` is **required**: one of

- `employer` — the punches of **this business's own employees**, at its own plaques and at a
  partner company's (there `location` names the partner's location, `location.company` the
  partner's code);
- `location` — every punch **at this business's plaques**, a partner company's employees' included
  (there `employee` names the partner's employee, `employee.company` the partner's code).

A punch of a business's own employee at its own plaque is in both scopes. A punch of a group
employee at a partner company's plaque is in the employer's `employer` scope and in the partner's
`location` scope — **the same `id` in both**. The partner is told who and where: the employee's
`id`, `externalRef`, `employeeCode` and `company`, the location's `id`, `externalRef`, `name` and
`company`, and `tagId` — never an e-mail address, a coordinate, a network address, a note or why a
decision was made.

The decision on such a punch is the **employer's**: its rules judge it, and a rule the partner set
for its own location does not apply to the employer's employee there — with one exception Taptime
sets and no business can change: a QR punch at a partner company's location counts only when it
comes **from that location's own network**; otherwise it waits for the employer's approval.

When the two companies leave the group (Taptime ends the membership), punches across them stop: an
NFC touch whose plaque counter was advanced before the end is still recorded; a later NFC touch is
refused without a record (and without advancing the counter); a QR punch and a manual record
across them are refused from the end on.

<a id="feed-punch"></a>
### 6.3 The punch

| Field | |
|---|---|
| `id` | `pch_…` — the punch, the same in both scopes and both companies' feeds. |
| `version` | Always `1` in v1. |
| `status` | Always `valid` in v1: no punch is cancelled (`voided` is never produced). |
| `punchTime` | The instant of the tap (UTC): the one the phone declared, or Taptime's own when it declared none — today always Taptime's ([6.1](#feed-what)). |
| `recordedAt` | The instant the punch entered the feed (UTC); for an approved flag, the approval's. |
| `channel` | `nfc` or `qr`. |
| `direction` | `in` or `out`, as Taptime computed it at the **employer**. |
| `employee` | `id`, `externalRef`, `employeeCode`, `company` — as they were at the punch. |
| `location` | `id`, `externalRef`, `name`, `company` — as they were at the punch. |
| `tagId` | `tag_…`, the plaque touched — also for a QR punch. |

- The `employee` and `location` fields are the values **at the punch**: a later rename or a new
  reference does not change a punch already in the feed.
- An employee not bound to a reference yet punches with `employee.externalRef: null`; your system
  can match them later by `id` (the directory and `GET /v1/employees` answer it).
- `location` is always an object. Its `id`, `externalRef` and `name` are `null` for a plaque not
  mounted at a location (`company` still names the plaque's company); all four are `null` for a
  punch at a partner's location that was approved after the two companies' group membership ended.
- `direction` is Taptime's own: it toggles on the employee's last open entry at the employer, not
  on the calendar day (a 02:00 exit of an 18:00 shift is an `out`). Your system does not have to
  work it out.
- An employee created through the API has no home location in Taptime, so Taptime's own reports do
  not mark their punches as "at another location".

<a id="feed-paging"></a>
### 6.4 Reading the feed

```text
cursor = load(scope)
repeat:
    page = GET /v1/punches?scope=<scope>&limit=1000[&cursor=<cursor>]
    for punch in page.data:
        upsert(punch.id, punch.version, punch)
    cursor = page.nextCursor
    save(scope, cursor)
    if page.hasMore is false:
        wait, then poll again with the same cursor
```

- Start without `cursor` (the beginning of the feed). Each page's `nextCursor` is the cursor of the
  next request. **`nextCursor` is always present** — on an empty page too, where it is the position
  you have caught up to — and it **never goes back**.
- A page can hold **fewer punches than `limit`, even none, while `hasMore` is `true`**: a page reads
  at most 10000 positions of the feed, and a scope can be sparse. Read on while `hasMore` is true.
  `hasMore: false` means you have every punch the feed held when the page was read; poll again
  later with the same `nextCursor`.
- **Keep one cursor per scope.** A cursor (`cur_…`) is a position in the business's feed, which the
  two scopes share; a cursor carried to the other scope — or to another business — is **not
  refused** when it is inside that feed's range, and the punches before it are **skipped
  silently**. Never carry one.
- A cursor past the end of the feed is **400**. For a cursor the server handed out, that happens
  only if Taptime's data was restored from a backup without its counters being moved on: stop and
  tell Taptime — do not wait silently.
- Deduplicate by `id` + `version`: the feed answers each punch once per scope, but a webhook can
  deliver it again ([section 9.7](#webhook-duplicates)).

In the story, KF reads its `location` scope: Joe, an employee of KM, touched in at Valletta, and so
did Jane (KF).

<!-- example: listPunches 200 feed -->
```json
{
  "data": [
    {
      "id": "pch_00d216d4-3012-4d76-9c35-8f24028b432a",
      "version": 1,
      "status": "valid",
      "punchTime": "2026-10-16T09:13:13.008183Z",
      "recordedAt": "2026-10-16T09:13:13.051707Z",
      "channel": "nfc",
      "direction": "in",
      "employee": {"id": "emp_42f028c7-242a-4903-89df-8fde6176926b", "externalRef": "M-2001", "employeeCode": "2001", "company": "KM"},
      "location": {"id": "loc_30258476-2ce5-4a5d-a7dd-36651c3019dd", "externalRef": "KF-VLT", "name": "Valletta", "company": "KF"},
      "tagId": "tag_0DD4F2FE3B9A6A"
    },
    {
      "id": "pch_04ce3ad0-fb32-477d-9d5d-7c0e9b079b41",
      "version": 1,
      "status": "valid",
      "punchTime": "2026-10-16T09:13:13.083662Z",
      "recordedAt": "2026-10-16T09:13:13.116875Z",
      "channel": "nfc",
      "direction": "in",
      "employee": {"id": "emp_8973fbc8-5e76-48e2-a80b-67d5b10baa91", "externalRef": "E-1042", "employeeCode": "1042", "company": "KF"},
      "location": {"id": "loc_30258476-2ce5-4a5d-a7dd-36651c3019dd", "externalRef": "KF-VLT", "name": "Valletta", "company": "KF"},
      "tagId": "tag_0DD4F2FE3B9A6A"
    }
  ],
  "nextCursor": "cur_2",
  "hasMore": false
}
```

<a id="feed-working-time"></a>
### 6.5 Working time and `scope=location`

> **Caution — do not compute a partner company's employee's working time from `scope=location`.**
> A partner company's employee's punches at your locations do not include the events in between —
> their punches at their employer's own plaques. Pairing in and out from this scope alone can count
> working time **too long**. This holds **with `direction` too**: every punch's direction is right,
> but what is missing are the events in between — the pair of an `in` at your location may never
> appear at your location, or may be an `out` at another location. For a group employee's working
> time, the employer company's data — its `employer` scope — is the record.

In the story, Joe (KM) starts a shift at KF's Valletta and ends it at KM's Production plaque, a
little over a minute later (the story runs in real time, and a repeat within 60 seconds would be no
punch — [6.1](#feed-what)). KF's
`location` scope receives only his `in` (above). The `out` is a punch at a KM location: it is in
KM's `employer` and `location` scopes. KM reads its `employer` scope as a history range and sees
both:

<!-- example: listPunches 200 history -->
```json
{
  "data": [
    {
      "id": "pch_00d216d4-3012-4d76-9c35-8f24028b432a",
      "version": 1,
      "status": "valid",
      "punchTime": "2026-10-16T09:13:13.008183Z",
      "recordedAt": "2026-10-16T09:13:13.051707Z",
      "channel": "nfc",
      "direction": "in",
      "employee": {"id": "emp_42f028c7-242a-4903-89df-8fde6176926b", "externalRef": "M-2001", "employeeCode": "2001", "company": "KM"},
      "location": {"id": "loc_30258476-2ce5-4a5d-a7dd-36651c3019dd", "externalRef": "KF-VLT", "name": "Valletta", "company": "KF"},
      "tagId": "tag_0DD4F2FE3B9A6A"
    },
    {
      "id": "pch_f01406af-dfcc-4fc0-bc4e-c3d982645fe8",
      "version": 1,
      "status": "valid",
      "punchTime": "2026-10-16T09:14:14.253191Z",
      "recordedAt": "2026-10-16T09:14:14.261497Z",
      "channel": "nfc",
      "direction": "out",
      "employee": {"id": "emp_42f028c7-242a-4903-89df-8fde6176926b", "externalRef": "M-2001", "employeeCode": "2001", "company": "KM"},
      "location": {"id": "loc_4b6d2cc5-44e7-42c0-9830-abd1b8e370bb", "externalRef": "DEP-1", "name": "Production", "company": "KM"},
      "tagId": "tag_DF83F315A119DF"
    }
  ],
  "nextCursor": "hst_2_1792142054253191_2_4b83bfee122c0b1d",
  "hasMore": false
}
```

The Taptime panel of the location's company (KF) lists a group employee's touches at its plaques
but does not compute their working time either: working time is the employer's.

<a id="history"></a>
## 7. The history query

`GET /v1/punches?scope=…&from=…&to=…` answers the scope's punches whose `punchTime` is in
[`from`, `to`).

- `from` and `to` go **together** (one without the other is 400); `from` must be before `to`, and
  they may be at most **31 days and one hour** apart — a calendar month in any time zone, a
  daylight-saving change included. Anything else is 400 (`details.maxHours` 745).
- They are RFC 3339 instants as in [section 3.7](#query) — encode the `+` of an offset as `%2B`.
- Punches come in `punchTime` order; equal instants in feed order.
- Paging is the feed's: `nextCursor`, `hasMore`, possibly empty pages while `hasMore` is true. A
  history cursor (`hst_…`) is bound to the business, the `scope`, the `from` and the `to` it was
  issued for: with any other it is 400 — the cross-scope trap of the feed cursor does not exist
  here. A feed cursor (`cur_…`) with `from`/`to`, or a history cursor without them, is 400.
- **A history read is a snapshot.** Its first page fixes the feed position it stands at; its pages
  answer the punches the feed held then. A punch that enters the feed **later with an earlier
  `punchTime`** — a flag approved days after the tap, a tap a client declared late — is **not** in that
  read, even inside its range, and asking the same range again later can answer **more**. To
  receive every punch for certain, read the **feed** ([section 6](#feed)); use the history query to
  look back.

<a id="directory"></a>
## 8. The directory

`GET /v1/directory` answers the employees the business may know of: **its own** and, when it
belongs to a group, **the partner companies'**. A business in no group reads its own only.

<a id="directory-entry"></a>
### 8.1 The entry

Each entry carries `id`, `company`, `externalRef`, `employeeCode`, `firstName`, `lastName`,
`fullName`, `active` (`false`: deactivated) and `updatedAt` — **no e-mail address**. An employee
created in the Taptime panel and not named by an integration has `firstName` and `lastName` `null`;
`fullName` is always present.

<!-- example: listDirectory 200 -->
```json
{
  "data": [
    {"id": "emp_32a024b8-e86f-4110-a4da-c05ee0acd9b7", "company": "KF", "externalRef": "E-1043", "employeeCode": null, "firstName": "Sam", "lastName": "Placeholder", "fullName": "Sam Placeholder", "active": false, "updatedAt": "2026-10-16T09:13:12.95631Z"},
    {"id": "emp_8973fbc8-5e76-48e2-a80b-67d5b10baa91", "company": "KF", "externalRef": "E-1042", "employeeCode": "1042", "firstName": "Jane", "lastName": "Example", "fullName": "Jane Example", "active": true, "updatedAt": "2026-10-16T09:13:13.00516Z"},
    {"id": "emp_42f028c7-242a-4903-89df-8fde6176926b", "company": "KM", "externalRef": "M-2001", "employeeCode": "2001", "firstName": "Joe", "lastName": "Sample", "fullName": "Joe Sample", "active": true, "updatedAt": "2026-10-16T09:13:13.006277Z"}
  ],
  "nextCursor": null,
  "hasMore": false,
  "asOf": "2026-10-16T09:14:14.298977Z"
}
```

<a id="directory-walk"></a>
### 8.2 Walks, `updatedSince` and `asOf`

- The directory is **paged** (`limit` up to 1000). Entries come in (`updatedAt`, `id`) order.
- A **walk** is the pages from a request without `cursor` to the page whose `hasMore` is `false`.
  Ask for the next page with `cursor=nextCursor` and **the same `updatedSince`** (or none): another
  `updatedSince`, or a cursor of another business, is 400. `nextCursor` is set only while `hasMore`
  is `true`, and `null` at the end.
- Every page of a walk carries the walk's **`asOf`** (the instant its first page was read). When a
  walk ends, start the next one with **`updatedSince=asOf`**. The server reads from **10 minutes
  before** `updatedSince`, so a change committed late is not missed.
- **Expect entries you already have** — within a walk too (an employee changed while you read moves
  to the end of the order): overwrite by `id`. A change is in a later walk unless the write that
  made it stayed open more than 10 minutes.
- **Key by `id`**: a re-hired person's deactivated entry and new active one can share an
  `externalRef`.

<a id="directory-full"></a>
### 8.3 Read the whole directory periodically

Two changes reach **no** incremental read, because they change no employee:

- a company **joining or leaving the group**: an incremental read neither brings the new partner's
  employees nor says the leaving partner's are gone;
- a company's **`company` code changing**: an incremental read leaves its employees with the old
  code.

Neither is signalled. Read the **whole directory** (a walk without `updatedSince`) **periodically —
daily, for instance** — and after a group change you know of; incremental reads are for the changes
in between.

<a id="directory-clock"></a>
### 8.4 A `nextCursor` refused after the server's clock stepped back

Rarely, a `nextCursor` the server itself handed out is refused with 400 — after the server's clock
stepped back. **Retry the same request after a short, growing wait** (seconds); if the 400
persists, start the walk again with the same `updatedSince`. Restarting at once does not help (the
new walk meets the same entry); the block lasts as long as the clock's step, and nothing is lost.
The directory stays open while the business is suspended.

<a id="webhooks"></a>
## 9. Webhooks

<a id="webhook-setup"></a>
### 9.1 Setting up the endpoint

A business has **one** webhook endpoint. Its **owner** sets it up in the Taptime panel (section
API keys, part Webhook); the API cannot. The address must be:

- `https://` and a DNS name — not an IP address;
- without a port (or with `:443`), without a user name or password, without a query string (`?`)
  and without a fragment (`#`);
- without an internationalised name (`xn--`) and without a trailing dot.

The panel stores the address in one spelling (a lower-case host, no `:443`). The address is checked again **when
Taptime connects**: a name that resolves to a private, loopback, link-local or reserved address, or
to Taptime's own, is refused (`blocked_address`) — even if it once resolved to a public one.
Taptime connects with **TLS 1.2 or newer** and checks the certificate: it must be valid for the
endpoint's name and chain to a publicly trusted certificate authority — with an older TLS, a
self-signed certificate or a private authority's, every try fails. Taptime **presents no client
certificate**: a receiver that asks for one (mutual TLS) fails every try too — the panel and the
delivery log name it a failed secure connection. Redirects are **not followed**;
an answer must come within **10 seconds**; the answer's body is not read.

<a id="webhook-key"></a>
### 9.2 The signing key

Taptime signs every request with the endpoint's **signing key**, `tt_whsec_` followed by 43
characters. The panel shows it **once** — when the endpoint is created and when the key is rotated
— and never again. After a rotation **no request is signed with the old key any more**: switch your
receiver to the new key at once (a receiver that refuses the new signature makes deliveries fail,
and they are retried on the schedule of [section 9.6](#webhook-retries)).

<a id="webhook-verify"></a>
### 9.3 Verifying the signature

Every request carries `X-Taptime-Signature: sha256=<hex>`: the lower-case hexadecimal
HMAC-SHA256 of the request body's **raw bytes**, keyed by the signing key **as text** (the
`tt_whsec_` prefix included). There is no timestamp header.

```text
received = header("X-Taptime-Signature")
expected = "sha256=" + lower_hex(hmac_sha256(key = utf8(signing_key), message = raw_body))
if not constant_time_equal(received, expected):
    answer 401 and stop
event = parse_json(raw_body)
if event.event == "ping":
    answer 204 and stop
if already_stored(event.id, event.version):
    answer 204 and stop
store(event)
answer 204
```

Compute the HMAC over the bytes **as received**, before parsing: a body parsed and serialised again
is not the same bytes. Compare in constant time.

Check your implementation against these known answers. The key is deliberately fake — it is
`tt_whsec_` and 43 `A`s, never a real key:

<!-- vector: webhook-signature -->
```text
key: tt_whsec_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
body: {"event":"ping"}
signature: sha256=1deefe0009103810c89395f2d078e1ba71edb003b3692a3ebca6d0866bf99935
body: (empty)
signature: sha256=88d05ff68b9d0e7585161401b08cb679fbf1701b3e28f683a3e96e4c33653576
body: {"event":"punch","deliveryId":"dlv_44721ce2-0f54-41ce-a757-7ee03cfec107","id":"pch_04ce3ad0-fb32-477d-9d5d-7c0e9b079b41","version":1,"status":"valid","punchTime":"2026-10-16T09:13:13.083662Z","recordedAt":"2026-10-16T09:13:13.116875Z","channel":"nfc","direction":"in","employee":{"id":"emp_8973fbc8-5e76-48e2-a80b-67d5b10baa91","externalRef":"E-1042","employeeCode":"1042","company":"KF"},"location":{"id":"loc_30258476-2ce5-4a5d-a7dd-36651c3019dd","externalRef":"KF-VLT","name":"Valletta","company":"KF"},"tagId":"tag_0DD4F2FE3B9A6A"}
signature: sha256=be925cb6fade218b39df6aa4ea0f1b78e52457c25068710ab32ad7ab6e8cb69c
```

The first known answer with `openssl` — it prints the first signature above:

```sh
printf '%s' '{"event":"ping"}' | openssl dgst -sha256 -hmac 'tt_whsec_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' | sed 's/^.*= /sha256=/'
```

Use this with the **fake key only**: `-hmac` (and `-macopt`) take the key on the command line, where
other users of the machine can read it (`ps`). With the real key, compute the HMAC in your
receiver's own language — its standard HMAC-SHA256, with the key read from your secret store — and
compare in constant time.

<a id="webhook-requests"></a>
### 9.4 The requests

Every request is a `POST` with three headers — `Content-Type: application/json`,
`User-Agent: Taptime-Webhooks/1` and `X-Taptime-Signature` — and a JSON body whose `event` says what
it is.

**A punch** (`event` `punch`) is the `GET /v1/punches` item ([section 6.3](#feed-punch)) with two
fields more: `event` and `deliveryId` (`dlv_…`). Every punch of the business's feed is posted once
(a punch in both of its scopes is one delivery); a punch between two companies of one group is
**two deliveries**, one to each company's endpoint. Jane's punch, as KF's endpoint receives it, is the third body of the vectors
above.

**A test message** (`event` `ping`) is `{"event":"ping","id":"<uuid>","sentAt":"<RFC 3339 UTC>"}`,
with the same headers and the same signature. The owner sends it from the panel, at most 10 an hour
— the same 10 that "Send again now" spends. It names no punch: **do not count it as one**.

<a id="webhook-answer"></a>
### 9.5 Answering

**Any 2xx is "delivered".** Anything else is a failed try: another status — a **3xx included**
(redirects are not followed) —, no answer within 10 seconds, a TLS or connection failure. Answer
quickly, after storing, and do the rest of your work afterwards.

<a id="webhook-retries"></a>
### 9.6 Retries

The first try is at once. After a failed try the next waits 1 minute, 5 minutes, 30 minutes, 2
hours, 6 hours; the **sixth** failed try makes the delivery **failed** (`dead`), and it is not
tried again by itself. Every failure is retried this way — a 4xx too (a receiver that has not
switched to a rotated key, a path being fixed).

| Try | Wait after the previous try failed (seconds) |
|---|---|
| 1 | 0 |
| 2 | 60 |
| 3 | 300 |
| 4 | 1800 |
| 5 | 7200 |
| 6 | 21600 |

The owner sees each delivery in the panel's **Deliveries** list — the punch, its state (Queued,
Sending, Retrying, Delivered, Failed), its tries and the last answer's class and status (never the
address or your answer's text) — and can send a waiting or failed delivery again with **Send again
now** (a failed one gets one more try). The failure classes are `timeout`, `tls`, `refused`,
`http_4xx`, `http_5xx` and `blocked_address`.

The list keeps a delivery for **90 days** after it arrived or was marked failed; then it is
deleted, and a failed one can no longer be sent again. A delivery that is still waiting is never
deleted. The punch itself stays in the feed.

<a id="webhook-duplicates"></a>
### 9.7 Duplicates and order

- **One punch can arrive more than once**: a retry, a request in flight when Taptime restarted, a
  "Send again now". Every try of one delivery carries the same `deliveryId` and the **same bytes**.
  **Deduplicate by `id` + `version`.**
- **There is no order guarantee**: a later punch can arrive before an earlier one, and a punch
  between two companies is two independent deliveries to two endpoints.

<a id="webhook-delays"></a>
### 9.8 Delays and gaps

- **After Taptime restarts** (every deploy included) waiting retries — and queued activation
  e-mails — wait until the business's next tap, approval or API request. The feed is not affected.
- **Endpoint switched off**: deliveries already queued **wait** (they are not dropped) and go at
  once when the owner switches the endpoint on again (or changes its address, or rotates its key)
  — none of which the owner can do while the business is suspended ([section 10](#suspension)).
  Punches recorded **while it is off** are never delivered.
- **API disabled by Taptime**: the same — queued deliveries wait, and go within 15 minutes of the
  API being enabled again (or at the business's next tap, approval or API request if Taptime
  restarted meanwhile); punches recorded while it is disabled get no delivery.
- An endpoint receives the punches recorded after it was created.

<a id="webhook-source"></a>
### 9.9 The feed is the record

A webhook is a fast notice, not the record: punches recorded while the endpoint was off are never
delivered, and a `dead` delivery does not come back by itself. **Read the feed regularly** with its
cursor ([section 6.4](#feed-paging)) and treat the webhook as "news now"; with deduplication by `id`
+ `version`, the two never count a punch twice.

<a id="suspension"></a>
## 10. Suspension

Taptime can suspend a business (for example over billing). A suspension is a **write lock** that
leaves reading — and the records — alone:

| Open while suspended | 403 `tenant_suspended` while suspended |
|---|---|
| every `GET` — the feed, the history query, the directory, the employees | `PUT /v1/locations/{ref}` |
| `PUT /v1/employees/{ref}` that creates a **new** employee, a re-hire included | every `PUT /v1/employees/{ref}` that lands on an existing employee — a change, a binding by address, `active: false` (also of an employee already deactivated), and one that changes nothing |
| `POST /v1/employees/{ref}/resend-activation` | every `DELETE /v1/employees/{ref}`, also of an employee already deactivated or of a reference never held |
| webhook deliveries, and the queue of activation e-mails | in the panel: creating a key; creating, re-addressing, rotating or switching on the endpoint; test messages; "Send again now" |
| taps are recorded and punches enter the feed (a group's too) | |
| in the panel: revoking a key, switching the endpoint off | |

<a id="differences"></a>
## 11. Contract differences

How Taptime differs from what an integration might assume. Each item links to where it is
explained.

1. The activation touch is **not a punch**: the first punch is the employee's second touch. Activation completes only at a plaque of the employee's **own company**. ([5.2](#employees-activation))
2. A tap judged `flag` enters the feed only **when a manager of the employer company approves it**, at the approval; a rejected one never comes. ([6.1](#feed-what))
3. In v1 `version` is always `1`, and `voided` is never produced. ([6.3](#feed-punch))
4. Extra fields: `channel` (`nfc` | `qr`) and `direction` (`in` | `out` — the direction Taptime computed at the employer); `tagId` is filled in on both channels. ([6.3](#feed-punch))
5. `punchTime` is the instant of the tap — the one the phone declared, or Taptime's own when it declared none (Taptime's tap page declares none today); a declared instant is `ok` up to 120 seconds late by default (the business can change this), and the ceiling is 72 hours; `recordedAt` is the instant the server recorded it. ([6.1](#feed-what))
6. A repeat touch within 60 seconds is not a punch — per person, **across the two companies' plaques too** — and the 60 seconds are counted in the declared instants or on Taptime's own clock, whichever is closer: should a client send two touches back to back from an offline queue, the second is lost, even when their declared instants are further apart. ([6.1](#feed-what))
7. A location's `timezone` must be the business's, or the answer is 422. ([4](#locations))
8. On a location, `active:false` is only a label. ([4](#locations))
9. A re-hire produces a new `employee.id`; the `externalRef` stays the same. ([5.1](#employees-put))
10. Taps at a location created through the API wait for approval until its network address ranges and coordinate are set in the Taptime panel. ([4](#locations))
11. The e-mail address must be ASCII and cannot be an administrator's of the business (422); held by another active employee of the business it is 409 `email_unavailable`. The activation e-mail always goes through the queue. ([5.1](#employees-put))
12. `resend-activation` has no effect (202) while an e-mail is queued or one was sent in the last hour and its link works. ([5.4](#employees-resend))
13. `resend-activation` for an activated employee is 409 `already_activated`. ([5.4](#employees-resend))
14. When an activated employee's e-mail address changes, no activation e-mail is sent. ([5.1](#employees-put))
15. Activation is by NFC only (a QR code cannot activate). ([5.2](#employees-activation))
16. Webhooks have no order guarantee; deduplicate by `id` + `version`. ([9.7](#webhook-duplicates))
17. Manual records do not enter the feed. ([6.1](#feed-what))
18. The history query (`from`/`to`) spans at most 31 days and one hour (a local calendar month, a daylight-saving change included), is paged, and is a snapshot — punches that arrive late are certain only in the feed. ([7](#history))
19. `updatedSince` (the directory) reads with a 10-minute overlap; entries that come back are overwritten by `id`. ([8.2](#directory-walk))
20. When Taptime restarts (every deploy included), waiting webhook retries and queued activation e-mails wait until the business's next tap or API request. The feed is not affected. ([9.8](#webhook-delays))
21. A punch of an employee not bound to a reference yet comes with `employee.externalRef: null`. ([6.3](#feed-punch))
22. Deactivation is limited by an hourly budget per key; past it the answer is 429. ([5.5](#employees-deactivate))
23. The API and the webhook work with the access Taptime enabled for the business. ([2.1](#auth-enable))
24. A punch of an employee without a home location gets no "different location" note. ([6.3](#feed-punch))
25. A punch across companies is decided by the **employer's** rules; a partner's rule for its own location does not apply to the employer's employee. **Exception:** a QR punch across companies counts only when it comes **from that location's network**; otherwise it waits for approval — the employer cannot switch this off. ([6.2](#feed-scopes))
26. When the group membership ends, punches across the companies stop: by NFC, a touch whose counter was advanced before the end is still recorded, a later NFC touch is refused without a record (the counter does not advance); QR and manual records are refused from the end on. ([6.2](#feed-scopes))
27. A punch across companies carries **the other company's identifiers**: the employee's `id`, `externalRef`, `employeeCode` and `company`, the location's `id`, `externalRef`, `name` and `company`, and `tagId`. A punch's fields are the values **at the punch** (a later rename does not change it). ([6.2](#feed-scopes))
28. A touch of another company's lost or retired plaque is refused; it is not a punch. ([6.1](#feed-what))
29. The directory (`GET /v1/directory`) is **paged** (`cursor`, at most 1000). ([8.2](#directory-walk))
30. In a KM shift that starts at a KF location and ends **at a KM plaque**, KF (`scope=location`) receives only the **in**; the out is a punch at a KM location and comes in both of KM's scopes. Every punch carries the `direction` Taptime computed **at the employer** (`in`/`out`); the integration does not have to work it out. The Taptime panel of the location's company lists a group employee's touches but does not compute their time ("time is the employer's"). ([6.5](#feed-working-time))
31. **An integration that computes working time with `scope=location`, beware:** a partner company's employee's punches at your locations do not include the employer's events in between (touches at the other company's plaques); pairing in and out from this scope alone can count the time **too long**. **This holds with `direction` too:** each punch's direction is right, but what is missing are the **events in between** — the pair of an `in` at your location may never appear there, or may be an `out` at another location. For a group employee's time, the employer company's data (its `employer` scope) is the record. ([6.5](#feed-working-time))

<a id="changes"></a>
## 12. Changes to this guide

| Edition | Date | Changes |
|---|---|---|
| 1 | 2026-10-16 | The first edition, for API version 1. |
| 2 | 2026-10-18 | §1.4 describes the sandbox: `tt_test_` keys, its own operator, no e-mail, and the tap simulation (its domain is not chosen yet). |
