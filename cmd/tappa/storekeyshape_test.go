package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// storekeyshape_test.go -- THE §4.7 KEY WALL, MOVED OFF THE SQL TEXT AND ONTO THE
// GENERATED ARTIFACT.
//
// 🔴 WHY IT IS HERE AND NOT IN THE SQL SCAN, WHICH IS THE WHOLE LESSON OF FOUR
// AUDIT ROUNDS. internal/domain/tenant's TestPlaquesDB_NoShippedTagQuerySelectsTheKey
// reads db/queries/tags.sql and asks whether a statement hands `aes_key_ref` back to
// Go. Three successive designs of that scan -- absolute, directional,
// absolute-plus-allow-list -- were each beaten by a spelling nobody had listed:
//
//	round 2  )RETURNING with no space · )SELECT with no space · ON CONFLICT DO UPDATE
//	round 3  RETURNING * · RETURNING t.* · RETURNING tags.* · a trailing comment
//	         containing the word "returning"
//	round 4  AES_KEY_REF · Aes_Key_Ref · aes_key_reF (PostgreSQL folds unquoted
//	         identifiers to lower case) · bare `tags` · to_jsonb(tags) ·
//	         row_to_json(tags) · U&"\0061es_key_ref"
//
// SIX OF THE ROUND-4 SEVEN WERE RE-MEASURED HERE, each pasted into the SHIPPED
// loader and run through the real test: all six ESCAPED. The reason is not a bug in
// any one design -- it is that THE SET OF SQL SPELLINGS THAT RETURN A COLUMN IS NOT
// ENUMERABLE. Case folding, wildcards, whole-row functions, Unicode escapes, CTEs,
// aliases, ordinal references: a text scan is playing a game it cannot finish, and
// each round's "now it is complete" was falsified by the next reader.
//
// 🔴 THE GENERATED GO IS FINITE, IT IS OURS, AND go/ast CAN COUNT IT. Every one of
// those spellings has to arrive somewhere, and the somewhere is a FIELD ON A
// GENERATED RETURN TYPE. Measured by running `make gen` on the mutated queries and
// reading the output (probes reverted, reverts verified with cmp on both files):
//
//	RETURNING uid, created_at, AES_KEY_REF   ->  InsertUnassignedRow{Uid, CreatedAt,
//	                                             AesKeyRef []byte}
//	RETURNING uid, created_at, to_jsonb(tags) -> InsertUnassignedRow{Uid, CreatedAt,
//	                                             ToJsonb []byte}   <- THE WHOLE ROW
//	RETURNING *                              ->  the bespoke row type disappears and
//	                                             the signature becomes (Tag, error),
//	                                             scanning &i.AesKeyRef
//
// ⚠️ AND `ToJsonb` IS WHY A NAME SEARCH IS NOT ENOUGH EITHER: the field name gives
// nothing away. What changed in every case is the FIELD SET and the SHAPE. So this
// file pins those, not names.
//
// 🔴 THIS GATE IS DEFENCE IN DEPTH, NOT THE WALL. THE LOAD-BEARING HALF IS A
// PRIVILEGE (migration 00022, 2026-08-24): tappa_app has no SELECT on
// tags.aes_key_ref at all. That decision was taken because a security audit produced
// a leak THAT CHANGES NO SHAPE -- one substitution in GetTagForTenant's select list,
//
//	g.status  ->  to_jsonb(g)::text AS status
//
// which emits a row type BYTE-IDENTICAL to the shipped one and carries the whole row,
// wrapped key included, out through a field that was already a string. Measured: this
// inventory says "115 inventoried and matched", the []byte gate never looks, the SQL
// text wall sees nothing, redline-check exits 0 -- and the same mutation under the new
// privilege fails at runtime with `permission denied for table tags (SQLSTATE 42501)`.
//
// A static gate answers "what does a leak look like", and five rounds measured that
// question to be unbounded. The privilege system never asks it: if the column cannot
// be read, the expression does not matter.
//
// 🔴 WHAT THIS DOES **NOT** DO, because seven rounds of over-claiming is enough: it
// does not prove no key can reach Go, it does not see a shape-preserving
// substitution, and it does not see a call to resolve_tag_by_uid -- which returns the
// column and which the tap path cannot do without. Those two are covered by the
// privilege and by resolverCallSites below, respectively, and NEITHER is covered
// here. It proves that the GENERATED SURFACE is the one
// recorded -- 123 method signatures (2026-10-03) with their parameter and result structs expanded
// -- and that no []byte-carrying result is sourced from `tags`. A key reaching Go
// through raw SQL (internal/db/resolve.go does exactly that, legitimately), through a
// package that does not use sqlc, or through a hand-written scan is outside it.
//
// ⚠️ AND THE SENTENCE THAT USED TO SIT HERE DESCRIBED TWO GATES THAT NO LONGER EXIST
// (fifth audit): it claimed "the ten tags query functions return exactly the fields
// recorded below" and "no query over `tags` returns a NON-SCALAR". The rule was never
// non-scalar -- it was []byte -- and the gate had no notion of "over tags" at all; its
// own deletion note says so. Both are superseded by the inventory.
//
// The SQL scan is NOT deleted, for a reason that survives: it reads a different
// artifact, it costs nothing, and it fires on the spelling somebody types first. What
// it may not claim is completeness, and it does not.
//
// THE ONE LEGITIMATE READER IS NOT IN THIS ARTIFACT AT ALL, which is what makes the
// rule below narrow rather than allow-listed: resolve_tag_by_uid is a SECURITY
// DEFINER function (migration 00004) called with raw SQL from internal/db/resolve.go
// (line 254) and never through sqlc.
//
// ⚠️ THE COMMAND PUBLISHED HERE DID NOT PRODUCE THE RESULT PUBLISHED WITH IT, AND
// THAT IS THE FOURTH TIME THIS ROUND FAMILY HAS DONE IT (audit, 2026-08-24). It said
// `grep -rn "aes_key_ref" internal/store/*.go` "returns the INSERT parameter and
// nothing else". Re-run on this tree: ELEVEN hits -- one INSERT text inside a query
// constant, and TEN comment lines carried over from db/queries by sqlc. The
// CONCLUSION stands (no query READS the column); the evidence did not. Re-run here,
// with its output:
//
//	grep -rn "aes_key_ref" internal/store/*.go | grep -v "://"   -> 6 lines:
//	  tags.sql.go:266   INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status)
//	  querier.go:758 / :2123 / :2409 / :2467 / :3434   -- five copied doc comments
//
// i.e. exactly ONE statement text, and it is the INSERT. A published measurement has
// to be reproducible in the tree that publishes it.

// storeTagsDir is the generated package this file judges.
const storeDir = "internal/store"

//
// 🔴 IT PINS THE SHAPE, NOT THE NAME, AND BOTH HALVES ARE LOAD-BEARING. The field
// NAMES catch a column being added or removed; the field TYPES catch a column being
// swapped for one of a different kind. `ToJsonb []byte` is caught by neither name
// nor type in isolation -- it is caught because the SET gained a member.
//
// A row type listed as nil is one that must NOT exist (the query returns a scalar or
// a model type); today every tags query has a bespoke Row.
//
// ⚠️ ADDING A COLUMN TO A RETURNING CLAUSE TURNS THIS RED, ON PURPOSE, EVEN WHEN THE
// COLUMN IS HARMLESS. That is the trade: a one-line update here, against a class of
// silent escape that four audits have now paid for. The message says what to do.

// storeSurface is THE INVENTORY: every *Queries method in internal/store, with its
// parameter types and its result types, and every result struct EXPANDED to its
// fields. 123 entries (2026-10-03, M10 WL-1), generated from the artifact and pinned verbatim;
// 125 since the activation ADR added the two consent queries.
//
// 🔴 IT REPLACES FIVE ROUNDS OF TYPE CLASSIFICATION, AND THE REASON IS A COUNT.
// Every previous design of this wall answered "what does a leak LOOK like":
//
//	round 1  the column name, case-sensitively
//	round 2  direction (INSERT in / RETURNING out), on whitespace boundaries
//	round 3  absolute text + an allow-list, plus a wildcard rule
//	round 4  the generated FIELD SETS of ten named tags row types
//	round 5  any []byte result, bare or as a field
//
// Five designs, five escapes, and the fifth audit produced three more in one sitting:
// `[][]byte` (a `:many` whole-row dump -- `SELECT to_jsonb(tags)` over a tenant --
// which is the NATURAL shape for that query and matched none of the []byte patterns),
// `json.RawMessage` (one line in sqlc.yaml's `overrides` renames the type and every
// pattern goes blind), and an EXISTING query repointed at `tags` while keeping the
// field name an exception was written for.
//
// The question "how does a leak look" has no finite answer. THE ARTIFACT DOES: 123
// methods, 20 files (2026-10-03), all generated by us. So the criterion is no longer "is this
// dangerous" but "IS THIS IN THE INVENTORY" -- the shape this repository has used
// three times and terminated three times (retireCallSites, deadlineWriters,
// sessionFields, and the constant-time inventory).
//
// WHAT A CHANGE COSTS: one line here, in a visible edit, at exactly the moment a
// human should be looking. A new query, a changed RETURNING, a new column on an
// existing row, a `overrides` entry, `[][]byte`, `json.RawMessage`, an alias, inner
// whitespace -- every one of them changes a signature or a field list and turns this
// red with a diff that names it.
//
// ⚠️ WHAT IT IS NOT: a claim that no key can reach Go. It says the generated surface
// is the one recorded. A key travelling through raw SQL (internal/db/resolve.go does
// exactly that, legitimately, via the SECURITY DEFINER resolver), through a hand-
// written scan, or through a package that does not use sqlc is outside it. Named
// here rather than discovered in a sixth audit.
//
// ⚠️ AND THREE THINGS THE FIFTH AUDIT EXPLICITLY DID NOT TRY, carried rather than
// implied: reading through a VIEW or a CTE, hiding bytes inside a pgtype wrapper, and
// a multi-value signature `(A, B, error)`.
//
// 🔴 THE CTE HALF WAS MEASURED AND THE PREDICTION WAS WRONG (audit, 2026-08-24). The
// sentence said those "would change a result type or a field list and land here". A
// CTE over resolve_tag_by_uid returns the envelope through a field that is ALREADY a
// string, so the result type does NOT change and it does NOT land here -- it lands in
// resolverCallSites. The pgtype and multi-value cases remain unmeasured.
//
// M10 WL-1 (2026-10-03, db/queries/branding.sql) added eight tenant_branding methods,
// recorded verbatim from the generated artifact: ClearTenantAccent, ClearTenantLogo,
// EnsureTenantBrand, GetTenantBrand, GetTenantBrandForUpdate, GetTenantLogo,
// SetTenantAccent, SetTenantLogo. One returns a []byte -- GetTenantLogoRow.Logo, the
// logo's bytes from tenant_branding, read by tenant AND digest. The two per-page reads
// carry no []byte field; internal/db's TestTenantBranding_PerPageReadsDoNotSelectTheLogo
// pins their select lists on the shipped text.
//
// M10 WL-8 (2026-10-03) added GetTenantPanelBrand, the panel chrome's per-request read:
// the business's name (tenants.name) beside GetTenantBrand's brand fields without the
// audit pair. No []byte field -- the same test pins its select list.
//
// M10 EM-7C (2026-10-09, db/queries/passwordresets.sql) added LockAdminForResetIssue
// and WithdrawPasswordReset, the recovery flow's one-live-link-per-account rule: an
// advisory lock keyed by the administrator and the retirement of a link recorded
// undelivered. Neither carries a []byte field nor calls a definer.
//
// M10 WL-13 (2026-10-09, db/queries/branding.sql) added GetTenantActivationBrand, the
// activation family's per-page read (GetTenantPanelBrand's fields plus the business's
// VIES verdict as a plain bool), and GetTenantActivationLogo, the activation wizard's
// logo bytes -- GetTenantLogo's statement with the VIES gate in its WHERE. The second
// returns a []byte, GetTenantActivationLogoRow.Logo: the logo's bytes from
// tenant_branding, read by tenant AND digest, exactly as GetTenantLogoRow.Logo. Neither
// calls a definer; internal/db's TestTenantBranding_PerPageReadsDoNotSelectTheLogo pins
// the first's select list and the second's positive control.
//
// M10 OP-15 phase B (written 2026-10-08, carried onto 00063 on 2026-10-20; ADR 0025 §3) widened TouchAdminSessionRow by two
// fields, the business's suspension -- tenants.suspended_at (*time.Time) and
// tenants.suspension_reason (*string, a code from a closed CHECK set) -- read by the
// panel's per-request statement so the write gate needs no second query. Harmless by
// this inventory's test: no []byte, no key, no model type, and neither column carries
// anything but a time and one of four words.
var storeSurface = map[string]string{
	// M11 API-6: the API's employee (employees.sql), K-25's budget and the owner's alert (audit.sql)
	// and the invitation queue (invitequeue.sql). No address is returned (has_email / same_email are
	// booleans computed in SQL), no code and no hash; ids, names, instants and counts.
	"CountPanelEmployeesByStatus":    "(context.Context, CountPanelEmployeesByStatusParams{LocationID *uuid.UUID; DepartmentID *uuid.UUID; FullName *string; TenantID uuid.UUID}) ([]CountPanelEmployeesByStatusRow{Status string; Everyone int64; Matching int64}, error)",
	"GetPanelPersonSummary":          "(context.Context, GetPanelPersonSummaryParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (GetPanelPersonSummaryRow{ID uuid.UUID; Status string; Role *string; InvitedAt *time.Time; ActivatedAt *time.Time; DeactivatedAt *time.Time; LiveSessions int64; FirstSessionAt *time.Time; SessionLastUsedAt *time.Time}, error)",
	"ListLastTapsForEmployees":       "(context.Context, ListLastTapsForEmployeesParams{EmployeeIds []uuid.UUID; TenantID uuid.UUID}) ([]ListLastTapsForEmployeesRow{EmployeeID uuid.UUID; OccurredAt time.Time; Verdict string; Channel string}, error)",
	"ListRecentTapsForEmployee":      "(context.Context, ListRecentTapsForEmployeeParams{TenantID uuid.UUID; EmployeeID *uuid.UUID}) ([]ListRecentTapsForEmployeeRow{OccurredAt time.Time; Type *string; Verdict string; Channel string; ReviewOutcome *string}, error)",
	"RetireUndeliveredInvites":       "(context.Context, RetireUndeliveredInvitesParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (int64, error)",
	"CountRecentSentInvites":         "(context.Context, CountRecentSentInvitesParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (int32, error)",
	"FailInviteRequest":              "(context.Context, FailInviteRequestParams{Due *time.Time; TenantID uuid.UUID; EmployeeID uuid.UUID; LeaseUntil *time.Time}) (int32, error)",
	"NextInviteRequestDue":           "(context.Context, uuid.UUID) (*time.Time, error)",
	"ClaimInviteRequest":             "(context.Context, uuid.UUID) (ClaimInviteRequestRow{EmployeeID uuid.UUID; LeaseUntil *time.Time; Attempts int32; RequestedAt time.Time}, error)",
	"CountAPIBudgetAlerts":           "(context.Context, CountAPIBudgetAlertsParams{TenantID uuid.UUID; KeyID string}) (int32, error)",
	"CountAPIDeactivations":          "(context.Context, CountAPIDeactivationsParams{TenantID uuid.UUID; KeyID uuid.UUID}) (CountAPIDeactivationsRow{Deactivations int32; Oldest time.Time}, error)",
	"CreateEmployeeFromAPI":          "(context.Context, CreateEmployeeFromAPIParams{ExternalRef *string; EmployeeCode *string; FirstName *string; LastName *string; FullName string; Email *string; TenantID uuid.UUID}) (CreateEmployeeFromAPIRow{ID uuid.UUID; ExternalRef *string; EmployeeCode *string; FirstName *string; LastName *string; FullName string; Status string; HasEmail bool}, error)",
	"DeferInviteRequest":             "(context.Context, DeferInviteRequestParams{Due *time.Time; TenantID uuid.UUID; EmployeeID uuid.UUID; LeaseUntil *time.Time}) (*time.Time, error)",
	"DeleteInviteRequest":            "(context.Context, DeleteInviteRequestParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (error)",
	"EnqueueInviteRequest":           "(context.Context, EnqueueInviteRequestParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (uuid.UUID, error)",
	"FindLinkableEmployeeByEmail":    "(context.Context, FindLinkableEmployeeByEmailParams{Email string; TenantID uuid.UUID}) (FindLinkableEmployeeByEmailRow{ID uuid.UUID; SameEmail bool}, error)",
	"FinishInviteRequest":            "(context.Context, FinishInviteRequestParams{TenantID uuid.UUID; EmployeeID uuid.UUID; LeaseUntil *time.Time}) (uuid.UUID, error)",
	"GetAPIEmployee":                 "(context.Context, GetAPIEmployeeParams{TenantID uuid.UUID; ID uuid.UUID}) (GetAPIEmployeeRow{ID uuid.UUID; ExternalRef *string; EmployeeCode *string; FirstName *string; LastName *string; FullName string; Status string; HasEmail bool; ActivatedAt *time.Time; CreatedAt time.Time; UpdatedAt time.Time; QueuedAt *time.Time; HasLiveInvite bool; LiveInviteAt time.Time; LastUndelivered bool; LastAttemptAt time.Time}, error)",
	"GetEmployeeInviteStatus":        "(context.Context, GetEmployeeInviteStatusParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (string, error)",
	"GetInviteRequest":               "(context.Context, GetInviteRequestParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (time.Time, error)",
	"GetLatestEmployeeByExternalRef": "(context.Context, GetLatestEmployeeByExternalRefParams{TenantID uuid.UUID; ExternalRef *string}) (uuid.UUID, error)",
	"GetLiveEmployeeByExternalRef":   "(context.Context, GetLiveEmployeeByExternalRefParams{Email *string; TenantID uuid.UUID; ExternalRef *string}) (GetLiveEmployeeByExternalRefRow{ID uuid.UUID; Status string; SameEmail bool}, error)",
	"IsAdministratorAddress":         "(context.Context, IsAdministratorAddressParams{TenantID uuid.UUID; Email string}) (bool, error)",
	"ListAPIBudgetAlerts":            "(context.Context, ListAPIBudgetAlertsParams{TenantID uuid.UUID; WindowSeconds int32}) ([]ListAPIBudgetAlertsRow{At time.Time; Target *string; KeyName string; Budget int32}, error)",
	"ListAPIEmployees":               "(context.Context, ListAPIEmployeesParams{TenantID uuid.UUID; After *uuid.UUID; PageSize int32}) ([]ListAPIEmployeesRow{ID uuid.UUID; ExternalRef *string; EmployeeCode *string; FirstName *string; LastName *string; FullName string; Status string; HasEmail bool; ActivatedAt *time.Time; CreatedAt time.Time; UpdatedAt time.Time; QueuedAt *time.Time; HasLiveInvite bool; LiveInviteAt time.Time; LastUndelivered bool; LastAttemptAt time.Time}, error)",
	"LockAPIDeactivationBudget":      "(context.Context, LockAPIDeactivationBudgetParams{KeyID uuid.UUID; TenantID uuid.UUID}) (LockAPIDeactivationBudgetRow{ID uuid.UUID; Unlocked bool}, error)",
	"LockSandboxPlaque":              "(context.Context, LockSandboxPlaqueParams{Uid string; TenantID uuid.UUID}) (LockSandboxPlaqueRow{ID uuid.UUID; Unlocked bool}, error)",
	"LockEmployeeForAPI":             "(context.Context, LockEmployeeForAPIParams{Email *string; TenantID uuid.UUID; ID uuid.UUID}) (LockEmployeeForAPIRow{ID uuid.UUID; ExternalRef *string; EmployeeCode *string; FirstName *string; LastName *string; FullName string; Status string; HasEmail bool; SameEmail bool; ActivatedAt *time.Time}, error)",
	"LockEmployeeForQueuedInvite":    "(context.Context, LockEmployeeForQueuedInviteParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (string, error)",
	"RenewInviteRequestLease":        "(context.Context, RenewInviteRequestLeaseParams{TenantID uuid.UUID; EmployeeID uuid.UUID; LeaseUntil *time.Time}) (*time.Time, error)",
	"RequeueInviteRequest":           "(context.Context, RequeueInviteRequestParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (error)",
	"UpdateEmployeeNamesFromAPI":     "(context.Context, UpdateEmployeeNamesFromAPIParams{FirstName *string; LastName *string; FullName string; EmployeeCode *string; TenantID uuid.UUID; ID uuid.UUID}) (uuid.UUID, error)",
	// M11 API-1c: the site's reads of its projection (db/queries/sitepunches.sql) and the manager's
	// record at a peer's venue -- ids, names, counts and instants; no key, no evidence.
	"ListSitePunchesForPeriod":           "(context.Context, ListSitePunchesForPeriodParams{TenantID uuid.UUID; FromAt time.Time; UntilAt time.Time; NewestFirst bool; RowLimit int32}) ([]ListSitePunchesForPeriodRow{TransactionID uuid.UUID; EmployerApiCode *string; EmployeeID *uuid.UUID; EmployeeRef *string; EmployeeCode *string; EmployeeName *string; SiteLocationID *uuid.UUID; LocationName *string; SiteTagUid *string; OccurredAt time.Time; Channel string; Type *string; Outcome string; CreatedAt time.Time}, error)",
	"ListSiteTagLastSeen":                "(context.Context, uuid.UUID) ([]ListSiteTagLastSeenRow{TagUid string; LastSeen time.Time}, error)",
	"InsertManualSiteTransaction":        "(context.Context, InsertManualSiteTransactionParams{TenantID uuid.UUID; Type string; OccurredAt time.Time; Trust int16; Note *string; EnteredBy uuid.UUID; SiteTenantID uuid.UUID; SiteLocationID uuid.UUID; EmployeeID uuid.UUID}) (InsertManualSiteTransactionRow{ID uuid.UUID; OccurredAt time.Time; CreatedAt time.Time; Type *string; Verdict string; Channel string; Trust *int16; Practice bool; Queued bool}, error)",
	"ListSitePlaqueGaps":                 "(context.Context, ListSitePlaqueGapsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListSitePlaqueGapsRow{TagUid string; LocationName *string; Records int64; CounterGaps int64; LargestGap int32}, error)",
	"ListSiteLostPlaqueAlerts":           "(context.Context, ListSiteLostPlaqueAlertsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListSiteLostPlaqueAlertsRow{TagUid string; LocationName *string; Refusals int64; LastAt time.Time}, error)",
	"AdvanceTagCounter":                  "(context.Context, AdvanceTagCounterParams{Ctr int32; TenantID uuid.UUID; Uid string}) (AdvanceTagCounterRow{Uid string; CtrGap int32}, error)",
	"AppendPolicyVersion":                "(context.Context, AppendPolicyVersionParams{TenantID uuid.UUID; PolicyID uuid.UUID; VersionNo int32; Document []byte; CreatedBy *uuid.UUID}) (AppendPolicyVersionRow{ID uuid.UUID; VersionNo int32; CreatedAt time.Time}, error)",
	"AssignTagToLocation":                "(context.Context, AssignTagToLocationParams{LocationID uuid.UUID; TenantID uuid.UUID; Uid string}) (AssignTagToLocationRow{Uid string; TenantID uuid.UUID; LocationID *uuid.UUID; LastCtr int32; Status string; RetiredAt *time.Time; ReplacedBy *string; CreatedAt time.Time; EncodedAt *time.Time}, error)",
	"AttachPolicyResource":               "(context.Context, AttachPolicyResourceParams{TenantID uuid.UUID; PolicyID uuid.UUID; Resource string}) (AttachPolicyResourceRow{ID uuid.UUID; Resource string}, error)",
	"CancelPendingInvitesForEmployee":    "(context.Context, CancelPendingInvitesForEmployeeParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) ([]uuid.UUID, error)",
	"ClearTenantAccent":                  "(context.Context, ClearTenantAccentParams{UpdatedBy uuid.UUID; TenantID uuid.UUID}) (int64, error)",
	"ClearTenantLogo":                    "(context.Context, ClearTenantLogoParams{UpdatedBy uuid.UUID; TenantID uuid.UUID}) (int64, error)",
	"CloseBillingPeriod":                 "(context.Context, CloseBillingPeriodParams{ClosedBy uuid.UUID; PeriodMonth pgtype.Date; TenantID uuid.UUID}) (CloseBillingPeriodRow{ID uuid.UUID; TenantID uuid.UUID; PeriodMonth pgtype.Date; PeriodFrom time.Time; PeriodTo time.Time; Timezone string; Plan string; FreePeriod bool; EmployeeCount int32; UnstampedEmployees int32; UnitPrice pgtype.Numeric; Currency string; AmountDue pgtype.Numeric; ClosedBy uuid.UUID; ClosedAt time.Time}, error)",
	"ConfirmRecentRemoval":               "(context.Context, ConfirmRecentRemovalParams{TenantID uuid.UUID; ActorID uuid.UUID; Action string; Target string; WindowSeconds int32}) (string, error)",
	"ConsumeInviteAndActivate":           "(context.Context, ConsumeInviteAndActivateParams{TenantID uuid.UUID; CodeHash string; ConsentBindingHash string}) (ConsumeInviteAndActivateRow{ID uuid.UUID; TenantID uuid.UUID; LocationID *uuid.UUID; DepartmentID *uuid.UUID; FullName string; Status string; InvitedAt *time.Time; ActivatedAt *time.Time; InviteID uuid.UUID}, error)",
	"ConsumePasswordResetAndSetPassword": "(context.Context, ConsumePasswordResetAndSetPasswordParams{PasswordHash string; TenantID uuid.UUID; TokenHash string}) (ConsumePasswordResetAndSetPasswordRow{AdminUserID uuid.UUID; TenantID uuid.UUID; ResetID uuid.UUID; RetiredCount int64}, error)",
	"CountAnomalySignals":                "(context.Context, CountAnomalySignalsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time}) (CountAnomalySignalsRow{Records int64; Judged int64; Unanswerable int64; DistanceOnly int64; OutsideVenue int64; OutsideVenueOnNetwork int64; CounterGaps int64; OtherVenue int64; Training int64; ManagerTyped int64; PartnerRecords int64; PartnerAnswerable int64}, error)",
	"CountDepartmentReferences":          "(context.Context, CountDepartmentReferencesParams{TenantID uuid.UUID; ID uuid.UUID}) (CountDepartmentReferencesRow{EmployeeCount int64; TransactionCount int64}, error)",
	"CountLocationReferences":            "(context.Context, CountLocationReferencesParams{TenantID uuid.UUID; ID uuid.UUID}) (CountLocationReferencesRow{DepartmentCount int64; EmployeeCount int64; TagCount int64; TransactionCount int64; HoldingCount int64}, error)",
	"CountMaskedOpenCheckIns":            "(context.Context, CountMaskedOpenCheckInsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time}) (CountMaskedOpenCheckInsRow{StillOpen int64; ClosedByItsCheckout int64}, error)",
	"CountOnTheFloorByVenue":             "(context.Context, CountOnTheFloorByVenueParams{TenantID uuid.UUID; Since time.Time}) ([]CountOnTheFloorByVenueRow{LocationID *uuid.UUID; SiteLocationID *uuid.UUID; LocationName *string; People int64}, error)",
	"CountPanelTransactionsByVerdict":    "(context.Context, CountPanelTransactionsByVerdictParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; LocationID *uuid.UUID; DepartmentID *uuid.UUID; EmployeeName *string; Channel *string}) (CountPanelTransactionsByVerdictRow{Records int64; Approved int64; Flagged int64; Rejected int64; Ignored int64; Manual int64}, error)",
	"CountPendingFlagged":                "(context.Context, CountPendingFlaggedParams{TenantID uuid.UUID; Cap int32}) (int32, error)",
	"CountPracticeTaps":                  "(context.Context, CountPracticeTapsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time}) (int64, error)",
	"CountRecordsAtPartnerVenues":        "(context.Context, CountRecordsAtPartnerVenuesParams{TenantID uuid.UUID; FromAt time.Time; UntilAt time.Time}) (int64, error)",
	"CountRecentInvites":                 "(context.Context, CountRecentInvitesParams{EmployeeID uuid.UUID; TenantID uuid.UUID}) (CountRecentInvitesRow{TenantLastHour int32; TenantLastDay int32; EmployeeLastHour int32}, error)",
	"CountReviewsSince":                  "(context.Context, CountReviewsSinceParams{TenantID uuid.UUID; Since time.Time}) (int32, error)",
	"CountTenantPlaques":                 "(context.Context, uuid.UUID) (CountTenantPlaquesRow{InService int64; InStock int64; ReadyToMount int64; Loaded int64}, error)",
	"CreateAdminSession":                 "(context.Context, CreateAdminSessionParams{TokenHash string; AdminUserID uuid.UUID; TenantID uuid.UUID}) (CreateAdminSessionRow{ID uuid.UUID; TenantID uuid.UUID; AdminUserID uuid.UUID; CreatedAt time.Time; LastUsedAt *time.Time; RevokedAt *time.Time}, error)",
	"CreateAdminUser":                    "(context.Context, CreateAdminUserParams{FullName string; Email *string; PasswordHash string; Role string; TenantID uuid.UUID}) (CreateAdminUserRow{ID uuid.UUID; TenantID uuid.UUID; FullName string; Role string; Status string; CreatedAt time.Time}, error)",
	"CreateDepartment":                   "(context.Context, CreateDepartmentParams{TenantID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; LocationID uuid.UUID}) (Department{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time; DayCutoff pgtype.Time}, error)",
	"CreateEmployee":                     "(context.Context, CreateEmployeeParams{TenantID uuid.UUID; FullName string; Role *string; Email *string; DepartmentID *uuid.UUID; LocationID uuid.UUID}) (CreateEmployeeRow{ID uuid.UUID; TenantID uuid.UUID; LocationID *uuid.UUID; DepartmentID *uuid.UUID; FullName string; Role *string; Status string; CreatedAt time.Time}, error)",
	"CreateInvite":                       "(context.Context, CreateInviteParams{TenantID uuid.UUID; EmployeeID uuid.UUID; CodeHash string; ExpiresAt time.Time}) (CreateInviteRow{ID uuid.UUID; TenantID uuid.UUID; EmployeeID uuid.UUID; CreatedAt time.Time; ExpiresAt time.Time; UsedAt *time.Time}, error)",
	"CreateLocation":                     "(context.Context, CreateLocationParams{Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; TenantID uuid.UUID}) (CreateLocationRow{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; CreatedAt time.Time}, error)",
	"CreatePasswordReset":                "(context.Context, CreatePasswordResetParams{TokenHash string; ExpiresAt time.Time; AdminUserID uuid.UUID; TenantID uuid.UUID}) (CreatePasswordResetRow{ID uuid.UUID; TenantID uuid.UUID; AdminUserID uuid.UUID; CreatedAt time.Time; ExpiresAt time.Time; RetiredCount int64}, error)",
	"CreateSession":                      "(context.Context, CreateSessionParams{TenantID uuid.UUID; EmployeeID uuid.UUID; TokenHash string; DeviceInfo *string}) (CreateSessionRow{ID uuid.UUID; TenantID uuid.UUID; EmployeeID uuid.UUID; DeviceInfo *string; CreatedAt time.Time; LastUsedAt *time.Time; RevokedAt *time.Time}, error)",
	"CreateTenant":                       "(context.Context, CreateTenantParams{ID uuid.UUID; Name string; VatNumber string; BusinessType string; Structure string; Timezone string; VatVerified *bool; VatCheckedAt *time.Time}) (CreateTenantRow{ID uuid.UUID; Name string; VatNumber string; BusinessType string; Structure string; Plan string; Timezone string; PricePerEmployeeMonth pgtype.Numeric; VatVerified *bool; VatCheckedAt *time.Time; CreatedAt time.Time}, error)",
	"CreateTenantPolicy":                 "(context.Context, CreateTenantPolicyParams{TenantID uuid.UUID; Name string; Enabled bool; CreatedBy *uuid.UUID}) (CreateTenantPolicyRow{ID uuid.UUID; Name string; Layer string; Enabled bool}, error)",
	"DeactivateEmployee":                 "(context.Context, DeactivateEmployeeParams{TenantID uuid.UUID; ID uuid.UUID}) (DeactivateEmployeeRow{ID uuid.UUID; FullName string; Status string; DeactivatedAt *time.Time}, error)",
	"DeleteDepartment":                   "(context.Context, DeleteDepartmentParams{TenantID uuid.UUID; ID uuid.UUID}) (DeleteDepartmentRow{ID uuid.UUID; LocationID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time}, error)",
	"DeleteLocation":                     "(context.Context, DeleteLocationParams{TenantID uuid.UUID; ID uuid.UUID}) (DeleteLocationRow{ID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; CreatedAt time.Time}, error)",
	"DetachPolicyResource":               "(context.Context, DetachPolicyResourceParams{TenantID uuid.UUID; PolicyID uuid.UUID; Resource string}) (DetachPolicyResourceRow{ID uuid.UUID; Resource string}, error)",
	"EnsureBaselinePolicy":               "(context.Context, EnsureBaselinePolicyParams{ID uuid.UUID; TenantID uuid.UUID; Name string}) (error)",
	"EnsureBaselinePolicyVersion":        "(context.Context, EnsureBaselinePolicyVersionParams{ID uuid.UUID; TenantID uuid.UUID; PolicyID uuid.UUID; Document []byte}) (error)",
	"EnsurePolicyAttachment":             "(context.Context, EnsurePolicyAttachmentParams{TenantID uuid.UUID; PolicyID uuid.UUID; Resource string}) (error)",
	"EnsureTenantBrand":                  "(context.Context, EnsureTenantBrandParams{TenantID uuid.UUID; UpdatedBy uuid.UUID}) (error)",
	"FirstChargeableMonthShift":          "(context.Context, FirstChargeableMonthShiftParams{Timezone string; TenantID uuid.UUID}) (FirstChargeableMonthShiftRow{CurrentMonth pgtype.Date; ProposedMonth pgtype.Date}, error)",
	"GetAdminByID":                       "(context.Context, GetAdminByIDParams{ID uuid.UUID; TenantID uuid.UUID}) (GetAdminByIDRow{ID uuid.UUID; TenantID uuid.UUID; FullName string; Email *string; Role string; Status string; CreatedAt time.Time; LastLoginAt *time.Time}, error)",
	"GetAdminForTenantChoice":            "(context.Context, GetAdminForTenantChoiceParams{ID uuid.UUID; TenantID uuid.UUID}) (GetAdminForTenantChoiceRow{ID uuid.UUID; TenantID uuid.UUID; Role string; FullName string; TenantName string}, error)",
	// PRE-EXISTING SYNC (not md.5): the three admin-password methods below shipped in
	// T73 (change own password) whose commit did not update this inventory, so this
	// test was already RED on the branch before ADR 0018's app_key_ref work. They are
	// recorded here verbatim from the generated artifact -- none returns a []byte over
	// `tags`, so none is a §4.7 finding -- purely to make the suite green again.
	"GetAdminPasswordHashByID":      "(context.Context, GetAdminPasswordHashByIDParams{ID uuid.UUID; TenantID uuid.UUID}) (GetAdminPasswordHashByIDRow{ID uuid.UUID; PasswordHash string; Status string}, error)",
	"GetBillingPeriod":              "(context.Context, GetBillingPeriodParams{TenantID uuid.UUID; PeriodMonth pgtype.Date}) (GetBillingPeriodRow{ID uuid.UUID; TenantID uuid.UUID; TenantName string; PeriodMonth pgtype.Date; PeriodFrom time.Time; PeriodTo time.Time; Timezone string; Plan string; FreePeriod bool; EmployeeCount int32; UnstampedEmployees int32; UnitPrice pgtype.Numeric; Currency string; AmountDue pgtype.Numeric; ClosedBy uuid.UUID; ClosedAt time.Time}, error)",
	"GetDepartmentShift":            "(context.Context, GetDepartmentShiftParams{TenantID uuid.UUID; ID uuid.UUID}) (GetDepartmentShiftRow{ID uuid.UUID; TenantID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool}, error)",
	"GetEmployeeActivationContext":  "(context.Context, GetEmployeeActivationContextParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (GetEmployeeActivationContextRow{ID uuid.UUID; FullName string; Status string; LocationID *uuid.UUID; TenantName string}, error)",
	"GetEmployeeEmail":              "(context.Context, GetEmployeeEmailParams{TenantID uuid.UUID; ID uuid.UUID}) (GetEmployeeEmailRow{ID uuid.UUID; Email *string}, error)",
	"GetEmployeeForTap":             "(context.Context, GetEmployeeForTapParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (GetEmployeeForTapRow{Status string; LocationID *uuid.UUID; DepartmentID *uuid.UUID; ActivatedAt *time.Time; TenantTimezone string; TenantBusinessType string; EffectiveDayCutoff pgtype.Time}, error)",
	"GetLastOpenTransaction":        "(context.Context, GetLastOpenTransactionParams{TenantID uuid.UUID; EmployeeID *uuid.UUID; At time.Time}) (Transaction{ID uuid.UUID; TenantID uuid.UUID; EmployeeID *uuid.UUID; LocationID *uuid.UUID; DepartmentID *uuid.UUID; TagUid *string; Ctr *int32; Type *string; OccurredAt time.Time; SourceIp *netip.Addr; IpMatch *bool; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; GpsMatch *bool; SunValid *bool; Trust *int16; Verdict string; Note *string; Channel string; EnteredBy *uuid.UUID; Practice bool; Queued bool; CreatedAt time.Time; PolicyVersionID *uuid.UUID; MatchedSid *string; PolicyLayer *string; PolicyContext []byte; SiteTenantID *uuid.UUID; SiteLocationID *uuid.UUID; SiteTagUid *string; BusinessDayStart *time.Time}, error)",
	"GetInviteRecipient":            "(context.Context, GetInviteRecipientParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (GetInviteRecipientRow{Email *string; FullName string; TenantName string; TenantVerified bool; IsAdminAddress bool}, error)",
	"GetLastTransactionForEmployee": "(context.Context, GetLastTransactionForEmployeeParams{TenantID uuid.UUID; EmployeeID *uuid.UUID}) (Transaction{ID uuid.UUID; TenantID uuid.UUID; EmployeeID *uuid.UUID; LocationID *uuid.UUID; DepartmentID *uuid.UUID; TagUid *string; Ctr *int32; Type *string; OccurredAt time.Time; SourceIp *netip.Addr; IpMatch *bool; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; GpsMatch *bool; SunValid *bool; Trust *int16; Verdict string; Note *string; Channel string; EnteredBy *uuid.UUID; Practice bool; Queued bool; CreatedAt time.Time; PolicyVersionID *uuid.UUID; MatchedSid *string; PolicyLayer *string; PolicyContext []byte; SiteTenantID *uuid.UUID; SiteLocationID *uuid.UUID; SiteTagUid *string; BusinessDayStart *time.Time}, error)",
	"GetLocationByIP":               "(context.Context, GetLocationByIPParams{TenantID uuid.UUID; Src netip.Addr}) (Location{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time; WifiSsid *string; ExternalRef *string; Active bool; DayCutoff pgtype.Time}, error)",
	"GetLocationForTap":             "(context.Context, GetLocationForTapParams{TenantID uuid.UUID; ID uuid.UUID}) (GetLocationForTapRow{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool}, error)",
	"GetLocationWiFi":               "(context.Context, GetLocationWiFiParams{TenantID uuid.UUID; ID uuid.UUID}) (GetLocationWiFiRow{ID uuid.UUID; TenantID uuid.UUID; Name string; WifiSsid *string}, error)",
	"GetPanelDepartment":            "(context.Context, GetPanelDepartmentParams{TenantID uuid.UUID; ID uuid.UUID}) (GetPanelDepartmentRow{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time; LocationName string}, error)",
	"GetPanelEmployeeForAction":     "(context.Context, GetPanelEmployeeForActionParams{TenantID uuid.UUID; ID uuid.UUID}) (GetPanelEmployeeForActionRow{ID uuid.UUID; FullName string; Status string; LocationID *uuid.UUID; DepartmentID *uuid.UUID; ExternalRef *string; LocationName *string; DepartmentName *string}, error)",
	"GetPanelLocation":              "(context.Context, GetPanelLocationParams{TenantID uuid.UUID; ID uuid.UUID}) (GetPanelLocationRow{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; CreatedAt time.Time; ExternalRef *string}, error)",
	"GetPolicyForUpdate":            "(context.Context, GetPolicyForUpdateParams{TenantID uuid.UUID; ID uuid.UUID}) (GetPolicyForUpdateRow{ID uuid.UUID; Name string; Layer string; Enabled bool}, error)",
	"GetPolicyVersionDocument":      "(context.Context, GetPolicyVersionDocumentParams{TenantID uuid.UUID; PolicyID uuid.UUID; VersionNo int32}) (GetPolicyVersionDocumentRow{ID uuid.UUID; VersionNo int32; CreatedAt time.Time; CreatedBy *uuid.UUID; Document []byte; PolicyName string; Layer string; Enabled bool; AuthorName *string; DocumentBytes int32}, error)",
	"GetPunchFeedCounter":           "(context.Context, uuid.UUID) (int64, error)",
	// M11 API-8: the directory's own half -- the walk's clock and the business's company code, and a
	// page of its employees: identities, refs, codes, names, the active flag. No e-mail, no bytes.
	// The head's CreatedAt (the API-8 review, O-1/D-5): the business's creation instant, the lowest
	// asOf a cursor may carry -- a timestamp, harmless.
	"GetDirectoryHead":         "(context.Context, uuid.UUID) (GetDirectoryHeadRow{AsOf time.Time; ApiCode *string; CreatedAt time.Time}, error)",
	"ListDirectoryEmployees":   "(context.Context, ListDirectoryEmployeesParams{TenantID uuid.UUID; AfterAt time.Time; AfterID uuid.UUID; PageSize int32}) ([]ListDirectoryEmployeesRow{ID uuid.UUID; ExternalRef *string; EmployeeCode *string; FirstName *string; LastName *string; FullName string; Active bool; UpdatedAt time.Time}, error)",
	"GetPunchFeedHead":         "(context.Context, GetPunchFeedHeadParams{TenantID uuid.UUID; AfterSeq int64}) (GetPunchFeedHeadRow{LastSeq int64; NextSeq int64}, error)",
	"GetPunchHistoryWindow":    "(context.Context, GetPunchHistoryWindowParams{AfterTime time.Time; AfterSeq int64; TenantID uuid.UUID; FromTime time.Time; ToTime time.Time; ScanSize int32; WindowSize int32}) (GetPunchHistoryWindowRow{Scanned int64; EndTime time.Time; EndSeq int64}, error)",
	"GetRosterCursorAnchor":    "(context.Context, GetRosterCursorAnchorParams{TenantID uuid.UUID; ID uuid.UUID}) (string, error)",
	"GetTagForTenant":          "(context.Context, GetTagForTenantParams{TenantID uuid.UUID; Uid string}) (GetTagForTenantRow{Uid string; TenantID uuid.UUID; LocationID *uuid.UUID; LastCtr int32; Status string; RetiredAt *time.Time; ReplacedBy *string; CreatedAt time.Time; EncodedAt *time.Time}, error)",
	"GetTenantAccount":         "(context.Context, uuid.UUID) (GetTenantAccountRow{ID uuid.UUID; Name string; VatNumber string; BusinessType string; Timezone string; VatVerified *bool; VatCheckedAt *time.Time; CreatedAt time.Time}, error)",
	"GetTenantActivationBrand": "(context.Context, uuid.UUID) (GetTenantActivationBrandRow{Name string; VatVerified bool; Accent *string; LogoSha256 *string; LogoMime *string; LogoWidth *int32; LogoHeight *int32}, error)",
	"GetTenantActivationLogo":  "(context.Context, GetTenantActivationLogoParams{TenantID uuid.UUID; LogoSha256 string}) (GetTenantActivationLogoRow{Logo []byte; LogoMime *string}, error)",
	"GetTenantBrand":           "(context.Context, uuid.UUID) (GetTenantBrandRow{Accent *string; LogoSha256 *string; LogoMime *string; LogoWidth *int32; LogoHeight *int32; UpdatedAt time.Time; UpdatedBy uuid.UUID}, error)",
	"GetTenantBrandForUpdate":  "(context.Context, uuid.UUID) (GetTenantBrandForUpdateRow{Accent *string; LogoSha256 *string; LogoMime *string; LogoWidth *int32; LogoHeight *int32; UpdatedAt time.Time; UpdatedBy uuid.UUID}, error)",
	"GetTenantClock":           "(context.Context, uuid.UUID) (GetTenantClockRow{ID uuid.UUID; Name string; Timezone string}, error)",
	"GetTenantLogo":            "(context.Context, GetTenantLogoParams{TenantID uuid.UUID; LogoSha256 string}) (GetTenantLogoRow{Logo []byte; LogoMime *string}, error)",
	"GetTenantPanelBrand":      "(context.Context, uuid.UUID) (GetTenantPanelBrandRow{Name string; Accent *string; LogoSha256 *string; LogoMime *string; LogoWidth *int32; LogoHeight *int32}, error)",
	// M11 API-4b: /v1's suspension checkpoint -- one boolean, read in the write's own transaction.
	"GetTenantSuspended":   "(context.Context, uuid.UUID) (bool, error)",
	"GetTransactionReview": "(context.Context, GetTransactionReviewParams{TenantID uuid.UUID; TransactionID uuid.UUID}) (GetTransactionReviewRow{ReviewerID uuid.UUID; Outcome string}, error)",
	// M11 API-11: the sandbox simulation's read of the tap it recorded -- the decision's columns, no
	// source address, no coordinate, no policy context.
	"GetTapOutcome":                    "(context.Context, GetTapOutcomeParams{TenantID uuid.UUID; ID uuid.UUID}) (GetTapOutcomeRow{ID uuid.UUID; EmployeeID *uuid.UUID; Verdict string; Type *string; Trust *int16; Channel string; OccurredAt time.Time; MatchedSid *string; Practice bool; LocationID *uuid.UUID; SiteLocationID *uuid.UUID; TagUid *string; SiteTagUid *string}, error)",
	"InsertManualTransaction":          "(context.Context, InsertManualTransactionParams{TenantID uuid.UUID; Type string; OccurredAt time.Time; Trust int16; Note *string; EnteredBy uuid.UUID; LocationID *uuid.UUID; EmployeeID uuid.UUID}) (InsertManualTransactionRow{ID uuid.UUID; OccurredAt time.Time; CreatedAt time.Time; Type *string; Verdict string; Channel string; Trust *int16; Practice bool; Queued bool}, error)",
	"InsertTransaction":                "(context.Context, InsertTransactionParams{TenantID uuid.UUID; EmployeeID *uuid.UUID; LocationID *uuid.UUID; DepartmentID *uuid.UUID; TagUid *string; Ctr *int32; Type *string; OccurredAt time.Time; SourceIp *netip.Addr; IpMatch *bool; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; GpsMatch *bool; SunValid *bool; Trust *int16; Verdict string; Note *string; Channel string; EnteredBy *uuid.UUID; Practice bool; Queued bool; PolicyVersionID *uuid.UUID; MatchedSid *string; PolicyLayer *string; PolicyContext []byte; SiteTenantID *uuid.UUID; SiteLocationID *uuid.UUID; SiteTagUid *string; BusinessDayStart *time.Time}) (Transaction{ID uuid.UUID; TenantID uuid.UUID; EmployeeID *uuid.UUID; LocationID *uuid.UUID; DepartmentID *uuid.UUID; TagUid *string; Ctr *int32; Type *string; OccurredAt time.Time; SourceIp *netip.Addr; IpMatch *bool; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; GpsMatch *bool; SunValid *bool; Trust *int16; Verdict string; Note *string; Channel string; EnteredBy *uuid.UUID; Practice bool; Queued bool; CreatedAt time.Time; PolicyVersionID *uuid.UUID; MatchedSid *string; PolicyLayer *string; PolicyContext []byte; SiteTenantID *uuid.UUID; SiteLocationID *uuid.UUID; SiteTagUid *string; BusinessDayStart *time.Time}, error)",
	"InsertTransactionReview":          "(context.Context, InsertTransactionReviewParams{TenantID uuid.UUID; ReviewerID uuid.UUID; Outcome string; Note *string; TransactionID uuid.UUID}) (InsertTransactionReviewRow{ID uuid.UUID; ReviewedAt time.Time}, error)",
	"InsertUnassigned":                 "(context.Context, InsertUnassignedParams{Uid string; TenantID uuid.UUID; AesKeyRef []byte; AppKeyRef []byte}) (InsertUnassignedRow{Uid string; CreatedAt time.Time}, error)",
	"InviteConsentMatches":             "(context.Context, InviteConsentMatchesParams{ConsentBindingHash string; CodeHash string; TenantID uuid.UUID}) (bool, error)",
	"ListAdminSessionsForAdmin":        "(context.Context, ListAdminSessionsForAdminParams{TenantID uuid.UUID; AdminUserID uuid.UUID}) ([]ListAdminSessionsForAdminRow{ID uuid.UUID; TenantID uuid.UUID; AdminUserID uuid.UUID; CreatedAt time.Time; LastUsedAt *time.Time; RevokedAt *time.Time}, error)",
	"ListAnomalyPeople":                "(context.Context, ListAnomalyPeopleParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListAnomalyPeopleRow{EmployeeID *uuid.UUID; EmployeeName *string; Records int64; DistanceOnly int64; OutsideVenue int64; CounterGaps int64; OtherVenue int64}, error)",
	"ListAnomalyPlaques":               "(context.Context, ListAnomalyPlaquesParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListAnomalyPlaquesRow{TagUid *string; LocationName *string; Records int64; CounterGaps int64; LargestGap int32}, error)",
	"ListAnomalyPlaqueJumps":           "(context.Context, ListAnomalyPlaqueJumpsParams{TenantID uuid.UUID; LookbackFrom time.Time; FromAt time.Time; ToAt time.Time; TagUids []string}) ([]ListAnomalyPlaqueJumpsRow{TagUid string; LocationName *string; RevealedCtr *int32; RevealedAt time.Time; Gap int32; PreviousCtr *int32; PreviousAt *time.Time}, error)",
	"ListAnomalyPolicySids":            "(context.Context, ListAnomalyPolicySidsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListAnomalyPolicySidsRow{MatchedSid *string; PolicyLayer *string; Records int64}, error)",
	"ListAnomalyVenues":                "(context.Context, ListAnomalyVenuesParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListAnomalyVenuesRow{LocationID uuid.UUID; LocationName *string; Records int64; Judged int64; Answerable int64; DistanceOnly int64; OutsideVenue int64; BothMatched int64; NetworkOnly int64; NeitherMatched int64}, error)",
	"ListBillingPeriods":               "(context.Context, ListBillingPeriodsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListBillingPeriodsRow{ID uuid.UUID; TenantID uuid.UUID; TenantName string; PeriodMonth pgtype.Date; PeriodFrom time.Time; PeriodTo time.Time; Timezone string; Plan string; FreePeriod bool; EmployeeCount int32; UnstampedEmployees int32; UnitPrice pgtype.Numeric; Currency string; AmountDue pgtype.Numeric; ClosedBy uuid.UUID; ClosedAt time.Time}, error)",
	"ListDepartmentsForTenant":         "(context.Context, uuid.UUID) ([]ListDepartmentsForTenantRow{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; Name string; LocationName string}, error)",
	"ListFlaggedForReview":             "(context.Context, ListFlaggedForReviewParams{TenantID uuid.UUID; CursorAt *time.Time; CursorID *uuid.UUID; PageSize int32}) ([]ListFlaggedForReviewRow{ID uuid.UUID; OccurredAt time.Time; Type *string; Trust *int16; Verdict string; Channel string; Practice bool; Queued bool; TagUid *string; Ctr *int32; IpMatch *bool; GpsMatch *bool; Note *string; PolicyLayer *string; LocationName *string; DepartmentName *string; EmployeeName *string; SiteLocationID *uuid.UUID; SiteTagUid *string; MatchedSid *string}, error)",
	"ListLivePasswordResetsForAdmin":   "(context.Context, ListLivePasswordResetsForAdminParams{TenantID uuid.UUID; AdminUserID uuid.UUID}) ([]ListLivePasswordResetsForAdminRow{ID uuid.UUID; TenantID uuid.UUID; AdminUserID uuid.UUID; CreatedAt time.Time; ExpiresAt time.Time}, error)",
	"ListLocationsForTenant":           "(context.Context, uuid.UUID) ([]Location{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time; WifiSsid *string; ExternalRef *string; Active bool; DayCutoff pgtype.Time}, error)",
	"ListOpenCheckIns":                 "(context.Context, ListOpenCheckInsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListOpenCheckInsRow{EmployeeName *string; LocationName *string; SiteLocationID *uuid.UUID; OccurredAt time.Time; ManagerTyped bool; WasMaskable bool}, error)",
	"ListPanelDepartments":             "(context.Context, ListPanelDepartmentsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPanelDepartmentsRow{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time; LocationName string}, error)",
	"ListPanelEmployees":               "(context.Context, ListPanelEmployeesParams{TenantID uuid.UUID; Status *string; LocationID *uuid.UUID; DepartmentID *uuid.UUID; FullName *string; CursorName *string; CursorID *uuid.UUID; PageSize int32}) ([]ListPanelEmployeesRow{ID uuid.UUID; FullName string; Status string; InvitedAt *time.Time; ActivatedAt *time.Time; DeactivatedAt *time.Time; LocationID *uuid.UUID; Role *string; LocationName *string; DepartmentName *string; LiveSessions int64; SessionLastUsedAt *time.Time}, error)",
	"ListPanelLocations":               "(context.Context, ListPanelLocationsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPanelLocationsRow{ID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; CreatedAt time.Time; ExternalRef *string; DepartmentCount int64}, error)",
	"ListPanelTransactions":            "(context.Context, ListPanelTransactionsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; LocationID *uuid.UUID; DepartmentID *uuid.UUID; EmployeeName *string; Verdict *string; Channel *string; CursorAt *time.Time; CursorID *uuid.UUID; PageSize int32}) ([]ListPanelTransactionsRow{ID uuid.UUID; OccurredAt time.Time; Type *string; Trust *int16; Verdict string; Channel string; Practice bool; Queued bool; TagUid *string; Ctr *int32; IpMatch *bool; GpsMatch *bool; Note *string; PolicyLayer *string; LocationName *string; DepartmentName *string; EmployeeName *string; ReviewOutcome *string; ReviewNote *string; SiteLocationID *uuid.UUID; SiteTagUid *string; MatchedSid *string}, error)",
	"ListPendingInvitesForEmployee":    "(context.Context, ListPendingInvitesForEmployeeParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) ([]ListPendingInvitesForEmployeeRow{ID uuid.UUID; TenantID uuid.UUID; EmployeeID uuid.UUID; CreatedAt time.Time; ExpiresAt time.Time}, error)",
	"ListPlaqueHistory":                "(context.Context, ListPlaqueHistoryParams{TenantID uuid.UUID; Target string; RowLimit int32}) ([]ListPlaqueHistoryRow{Action string; At time.Time; ActorName string; BySystem bool}, error)",
	"ListPolicyAttachments":            "(context.Context, ListPolicyAttachmentsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPolicyAttachmentsRow{PolicyID uuid.UUID; Resource string}, error)",
	"ListPolicyDepartmentTargets":      "(context.Context, ListPolicyDepartmentTargetsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPolicyDepartmentTargetsRow{ID uuid.UUID; Name string}, error)",
	"ListPolicySet":                    "(context.Context, uuid.UUID) ([]ListPolicySetRow{PolicyID uuid.UUID; Name string; Layer string; Enabled bool; VersionID uuid.UUID; VersionNo int32; Document []byte}, error)",
	"ListPolicyVenueTargets":           "(context.Context, ListPolicyVenueTargetsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPolicyVenueTargetsRow{ID uuid.UUID; Name string}, error)",
	"ListPolicyVersions":               "(context.Context, ListPolicyVersionsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPolicyVersionsRow{PolicyID uuid.UUID; Name string; Layer string; Enabled bool; VersionID *uuid.UUID; VersionNo *int32; VersionCreatedAt *time.Time; CreatedBy *uuid.UUID; AuthorName *string; DocumentBytes int32}, error)",
	"ListPunchEvents":                  "(context.Context, ListPunchEventsParams{TenantID uuid.UUID; AfterSeq int64; UptoSeq int64; Role string; PageSize int32}) ([]ListPunchEventsRow{Seq int64; PunchID uuid.UUID; Version int32; Status string; PunchTime time.Time; RecordedAt time.Time; Channel string; Direction string; EmployeeID uuid.UUID; EmployeeRef *string; EmployeeCode *string; EmployeeCompany *string; LocationID *uuid.UUID; LocationRef *string; LocationName *string; LocationCompany *string; TagUid *string}, error)",
	"ListPunchHistory":                 "(context.Context, ListPunchHistoryParams{TenantID uuid.UUID; S0 int64; FromTime time.Time; ToTime time.Time; AfterTime time.Time; AfterSeq int64; EndTime time.Time; EndSeq int64; Role string; PageSize int32}) ([]ListPunchHistoryRow{Seq int64; PunchID uuid.UUID; Version int32; Status string; PunchTime time.Time; RecordedAt time.Time; Channel string; Direction string; EmployeeID uuid.UUID; EmployeeRef *string; EmployeeCode *string; EmployeeCompany *string; LocationID *uuid.UUID; LocationRef *string; LocationName *string; LocationCompany *string; TagUid *string}, error)",
	"ListPublishedLegalDocuments":      "(context.Context) ([]ListPublishedLegalDocumentsRow{ID uuid.UUID; Slug string; Body string; PublishedAt time.Time}, error)",
	"ListRollEmployees":                "(context.Context, ListRollEmployeesParams{TenantID uuid.UUID; EmployeeIds []uuid.UUID}) ([]ListRollEmployeesRow{ID uuid.UUID; Role *string; HomeLocationID *uuid.UUID; HomeVenue *string}, error)",
	"ListSessionsForEmployee":          "(context.Context, ListSessionsForEmployeeParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) ([]ListSessionsForEmployeeRow{ID uuid.UUID; TenantID uuid.UUID; EmployeeID uuid.UUID; DeviceInfo *string; CreatedAt time.Time; LastUsedAt *time.Time; RevokedAt *time.Time}, error)",
	"ListTagLastSeen":                  "(context.Context, uuid.UUID) ([]ListTagLastSeenRow{TagUid *string; LastSeen time.Time}, error)",
	"ListTagsForTenant":                "(context.Context, uuid.UUID) ([]ListTagsForTenantRow{Uid string; TenantID uuid.UUID; LocationID *uuid.UUID; LastCtr int32; Status string; RetiredAt *time.Time; ReplacedBy *string; CreatedAt time.Time; EncodedAt *time.Time}, error)",
	"ListTapsTakenTogether":            "(context.Context, ListTapsTakenTogetherParams{TenantID uuid.UUID; MinDays int32; RowLimit int32; FromAt time.Time; ToAt time.Time; Zone string; WithinSeconds float64}) ([]ListTapsTakenTogetherRow{FirstEmployeeName *string; SecondEmployeeName *string; LocationName *string; LocationID uuid.UUID; Together int64; Days int64; ClosestSeconds int32; TogetherDays []pgtype.Date}, error)",
	"ListWorkedShiftEvents":            "(context.Context, ListWorkedShiftEventsParams{TenantID uuid.UUID; FromAt time.Time; UntilAt time.Time; RowLimit int32}) ([]ListWorkedShiftEventsRow{EmployeeID *uuid.UUID; OccurredAt time.Time; Type *string; Verdict string; Channel string; LocationID *uuid.UUID; SiteLocationID *uuid.UUID; CreatedAt time.Time; BusinessDayStart *time.Time; ReviewOutcome *string; EmployeeName *string; LocationName *string; LocationShiftStart pgtype.Time; LocationShiftEnd pgtype.Time; LocationOvernight *bool; DepartmentShiftStart pgtype.Time; DepartmentShiftEnd pgtype.Time; DepartmentOvernight *bool}, error)",
	"LockAdminForResetIssue":           "(context.Context, LockAdminForResetIssueParams{AdminUserID uuid.UUID; TenantID uuid.UUID}) (error)",
	"LockEmployeeForEmailChange":       "(context.Context, LockEmployeeForEmailChangeParams{TenantID uuid.UUID; ID uuid.UUID}) (LockEmployeeForEmailChangeRow{ID uuid.UUID; Email *string}, error)",
	"LockEmployeeForTap":               "(context.Context, LockEmployeeForTapParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (error)",
	"LockTenantForInviteLimits":        "(context.Context, uuid.UUID) (error)",
	"MarkAdminLoggedIn":                "(context.Context, MarkAdminLoggedInParams{ID uuid.UUID; TenantID uuid.UUID}) (MarkAdminLoggedInRow{ID uuid.UUID; LastLoginAt *time.Time}, error)",
	"MarkTagEncoded":                   "(context.Context, MarkTagEncodedParams{Uid string; TenantID uuid.UUID}) (MarkTagEncodedRow{Uid string; EncodedAt *time.Time}, error)",
	"MoveEmployee":                     "(context.Context, MoveEmployeeParams{TenantID uuid.UUID; ID uuid.UUID; LocationID uuid.UUID; DepartmentID *uuid.UUID}) (MoveEmployeeRow{ID uuid.UUID; FullName string; LocationID *uuid.UUID; DepartmentID *uuid.UUID}, error)",
	"NextPolicyVersionNo":              "(context.Context, NextPolicyVersionNoParams{TenantID uuid.UUID; PolicyID uuid.UUID}) (int32, error)",
	"PolicyNameTaken":                  "(context.Context, PolicyNameTakenParams{TenantID uuid.UUID; Name string}) (bool, error)",
	"PreviewBillingPeriod":             "(context.Context, PreviewBillingPeriodParams{PeriodMonth pgtype.Date; TenantID uuid.UUID}) (PreviewBillingPeriodRow{TenantID uuid.UUID; TenantName string; Plan string; Timezone string; PeriodMonth pgtype.Date; PeriodFrom time.Time; PeriodTo time.Time; FirstChargeableMonth pgtype.Date; FreePeriod bool; EmployeeCount int32; UnstampedEmployees int32; UnitPrice pgtype.Numeric; AmountDue pgtype.Numeric; PeriodHasEnded bool; PeriodIsAfterSignup bool}, error)",
	"RecordAuditEvent":                 "(context.Context, RecordAuditEventParams{TenantID uuid.UUID; ActorID *uuid.UUID; Action string; Target *string; Detail []byte}) (RecordAuditEventRow{ID uuid.UUID; At time.Time}, error)",
	"RecordInviteConsent":              "(context.Context, RecordInviteConsentParams{ConsentBindingHash string; CodeHash string; TenantID uuid.UUID}) (RecordInviteConsentRow{ID uuid.UUID; ConsentedAt *time.Time}, error)",
	"RenameTenantPolicy":               "(context.Context, RenameTenantPolicyParams{Name string; TenantID uuid.UUID; ID uuid.UUID}) (RenameTenantPolicyRow{ID uuid.UUID; Name string}, error)",
	"RetireTagForReplacement":          "(context.Context, RetireTagForReplacementParams{ReplacedBy string; TenantID uuid.UUID; Uid string}) (RetireTagForReplacementRow{Uid string; TenantID uuid.UUID; LocationID *uuid.UUID; LastCtr int32; Status string; RetiredAt *time.Time; ReplacedBy *string; CreatedAt time.Time; EncodedAt *time.Time}, error)",
	"RevokeAdminSession":               "(context.Context, RevokeAdminSessionParams{ID uuid.UUID; TenantID uuid.UUID}) (RevokeAdminSessionRow{ID uuid.UUID; RevokedAt *time.Time}, error)",
	"RevokeAdminSessionsForAdmin":      "(context.Context, RevokeAdminSessionsForAdminParams{TenantID uuid.UUID; AdminUserID uuid.UUID}) ([]uuid.UUID, error)",
	"RevokeOtherAdminSessionsForAdmin": "(context.Context, RevokeOtherAdminSessionsForAdminParams{TenantID uuid.UUID; AdminUserID uuid.UUID; ExceptSessionID uuid.UUID}) ([]uuid.UUID, error)",
	"RevokeSession":                    "(context.Context, RevokeSessionParams{ID uuid.UUID; TenantID uuid.UUID}) (RevokeSessionRow{ID uuid.UUID; RevokedAt *time.Time}, error)",
	"RevokeSessionsForEmployee":        "(context.Context, RevokeSessionsForEmployeeParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) ([]uuid.UUID, error)",
	"SecondsSinceLastRecordedTap":      "(context.Context, SecondsSinceLastRecordedTapParams{TenantID uuid.UUID; EmployeeID *uuid.UUID; WindowSeconds float64}) (float64, error)",
	"SetEmployeeEmail":                 "(context.Context, SetEmployeeEmailParams{Email *string; TenantID uuid.UUID; ID uuid.UUID}) (SetEmployeeEmailRow{ID uuid.UUID; Email *string}, error)",
	"SetOwnAdminPassword":              "(context.Context, SetOwnAdminPasswordParams{PasswordHash string; ID uuid.UUID; TenantID uuid.UUID}) (uuid.UUID, error)",
	"SetPolicyEnabled":                 "(context.Context, SetPolicyEnabledParams{Enabled bool; TenantID uuid.UUID; ID uuid.UUID}) (SetPolicyEnabledRow{ID uuid.UUID; Name string; Layer string; Enabled bool}, error)",
	"SetTenantAccent":                  "(context.Context, SetTenantAccentParams{Accent string; UpdatedBy uuid.UUID; TenantID uuid.UUID}) (int64, error)",
	"SetTenantLogo":                    "(context.Context, SetTenantLogoParams{Logo []byte; LogoSha256 string; LogoMime string; LogoWidth int32; LogoHeight int32; UpdatedBy uuid.UUID; TenantID uuid.UUID}) (int64, error)",
	"TenantHasAnyTransaction":          "(context.Context, uuid.UUID) (bool, error)",
	"TouchAdminSession":                "(context.Context, TouchAdminSessionParams{ID uuid.UUID; TenantID uuid.UUID}) (TouchAdminSessionRow{ID uuid.UUID; LastUsedAt *time.Time; AdminUserID uuid.UUID; Role string; FullName string; SuspendedAt *time.Time; SuspensionReason *string}, error)",
	"TouchSession":                     "(context.Context, TouchSessionParams{ID uuid.UUID; TenantID uuid.UUID}) (TouchSessionRow{ID uuid.UUID; LastUsedAt *time.Time}, error)",
	"UnmountTagFromWall":               "(context.Context, UnmountTagFromWallParams{TenantID uuid.UUID; Uid string}) (UnmountTagFromWallRow{Uid string; TenantID uuid.UUID; LocationID *uuid.UUID; LastCtr int32; Status string; RetiredAt *time.Time; ReplacedBy *string; CreatedAt time.Time; EncodedAt *time.Time}, error)",
	"UpdateDepartment":                 "(context.Context, UpdateDepartmentParams{Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; TenantID uuid.UUID; ID uuid.UUID}) (Department{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time; DayCutoff pgtype.Time}, error)",
	"UpdateLocation":                   "(context.Context, UpdateLocationParams{Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; TenantID uuid.UUID; ID uuid.UUID}) (UpdateLocationRow{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; CreatedAt time.Time}, error)",
	"UpdateTenantAccount":              "(context.Context, UpdateTenantAccountParams{Name string; BusinessType string; Timezone string; TenantID uuid.UUID}) (UpdateTenantAccountRow{ID uuid.UUID; Name string; VatNumber string; BusinessType string; Timezone string; VatVerified *bool; VatCheckedAt *time.Time; CreatedAt time.Time}, error)",
	"WithdrawPasswordReset":            "(context.Context, WithdrawPasswordResetParams{TenantID uuid.UUID; ID uuid.UUID}) (error)",
	"WithTx":                           "(pgx.Tx) (*Queries)",
	// M11 API-1b (migration 00043): InsertTransaction, GetLastOpenTransaction and
	// GetLastTransactionForEmployee carry transactions' three site columns -- the site
	// tenant (uuid), its venue (uuid) and the plaque's uid (text, already in tag_uid's
	// shape) -- in the params and the Transaction model. No key, no hash, no evidence column
	// joins: the model is transactions', which never held aes_key_ref.
	// M15-02 (migration 00064, ADR 0032 §A.2, §A.5): the business day. InsertTransaction,
	// GetLastOpenTransaction and GetLastTransactionForEmployee carry business_day_start (a
	// *time.Time, the frozen UTC start of a tap's business day) in the params and the Transaction
	// model; GetLastOpenTransaction takes @at (the instant the tap is decided at). The Location and
	// Department models gain day_cutoff (pgtype.Time, a wall-clock setting), named in their
	// full-row lists for the mapping; GetEmployeeForTap returns the resolved effective_day_cutoff.
	// The four Set*DayCutoff writes hand back an id and the stored time. No key, no hash, no
	// evidence: times and ids only, and none of the models is tags'.
	"SetDepartmentDayCutoff": "(context.Context, SetDepartmentDayCutoffParams{DayCutoff pgtype.Time; TenantID uuid.UUID; ID uuid.UUID}) (SetDepartmentDayCutoffRow{ID uuid.UUID; DayCutoff pgtype.Time}, error)",
	"SetEmployeeDayCutoff":   "(context.Context, SetEmployeeDayCutoffParams{DayCutoff pgtype.Time; TenantID uuid.UUID; ID uuid.UUID}) (SetEmployeeDayCutoffRow{ID uuid.UUID; DayCutoff pgtype.Time}, error)",
	"SetLocationDayCutoff":   "(context.Context, SetLocationDayCutoffParams{DayCutoff pgtype.Time; TenantID uuid.UUID; ID uuid.UUID}) (SetLocationDayCutoffRow{ID uuid.UUID; DayCutoff pgtype.Time}, error)",
	"SetTenantDayCutoff":     "(context.Context, SetTenantDayCutoffParams{DayCutoff pgtype.Time; TenantID uuid.UUID}) (SetTenantDayCutoffRow{ID uuid.UUID; DayCutoff pgtype.Time}, error)",
	// M11 API-1 (migrations 00037-00039): the external-ref writes of the panel (K-18). Each
	// returns an id and the ref -- no key, no model type.
	"LockEmployeeForExternalRefChange": "(context.Context, LockEmployeeForExternalRefChangeParams{TenantID uuid.UUID; ID uuid.UUID}) (LockEmployeeForExternalRefChangeRow{ID uuid.UUID; ExternalRef *string; LocationID *uuid.UUID}, error)",
	"LockLocationForExternalRefChange": "(context.Context, LockLocationForExternalRefChangeParams{TenantID uuid.UUID; ID uuid.UUID}) (LockLocationForExternalRefChangeRow{ID uuid.UUID; ExternalRef *string}, error)",
	"SetEmployeeExternalRef":           "(context.Context, SetEmployeeExternalRefParams{ExternalRef *string; TenantID uuid.UUID; ID uuid.UUID}) (SetEmployeeExternalRefRow{ID uuid.UUID; ExternalRef *string}, error)",
	"SetLocationExternalRef":           "(context.Context, SetLocationExternalRefParams{ExternalRef *string; TenantID uuid.UUID; ID uuid.UUID}) (SetLocationExternalRefRow{ID uuid.UUID; ExternalRef *string}, error)",
	// M11 API-3 (migration 00051): the integration keys. The hash goes IN (InsertAPIKey) and
	// never comes back -- no row type carries token_hash, and tappa_app cannot read the column.
	"InsertAPIKey":          "(context.Context, InsertAPIKeyParams{TenantID uuid.UUID; Name string; TokenHash string; PrefixHint string; CreatedBy uuid.UUID}) (InsertAPIKeyRow{ID uuid.UUID; CreatedAt time.Time}, error)",
	"ListAPIKeys":           "(context.Context, uuid.UUID) ([]ListAPIKeysRow{ID uuid.UUID; Name string; PrefixHint string; CreatedBy uuid.UUID; CreatedAt time.Time; LastUsedAt *time.Time; RevokedAt *time.Time}, error)",
	"RevokeAPIKey":          "(context.Context, RevokeAPIKeyParams{TenantID uuid.UUID; ID uuid.UUID}) (RevokeAPIKeyRow{ID uuid.UUID; Name string; PrefixHint string; RevokedAt *time.Time}, error)",
	"RevokeAllAPIKeys":      "(context.Context, uuid.UUID) ([]RevokeAllAPIKeysRow{ID uuid.UUID; Name string; PrefixHint string; RevokedAt *time.Time}, error)",
	"TouchAPIKey":           "(context.Context, TouchAPIKeyParams{TenantID uuid.UUID; ID uuid.UUID}) (int64, error)",
	"GetTenantAPIEnabledAt": "(context.Context, uuid.UUID) (*time.Time, error)",
	"GetAPIKeyRevokedAt":    "(context.Context, GetAPIKeyRevokedAtParams{TenantID uuid.UUID; ID uuid.UUID}) (*time.Time, error)",
	// M11 API-4 (migration 00052): the Idempotency-Key ledger -- a request hash goes in, a status and
	// a record's id come back; no body, no key.
	"DeleteExpiredIdempotency":       "(context.Context, uuid.UUID) (int64, error)",
	"DeleteExpiredIdempotencyForKey": "(context.Context, DeleteExpiredIdempotencyForKeyParams{TenantID uuid.UUID; KeyID uuid.UUID; IdemKey string}) (int64, error)",
	"ClaimIdempotency":               "(context.Context, ClaimIdempotencyParams{TenantID uuid.UUID; KeyID uuid.UUID; IdemKey string; RequestHash string}) (int64, error)",
	"GetIdempotency":                 "(context.Context, GetIdempotencyParams{TenantID uuid.UUID; KeyID uuid.UUID; IdemKey string}) (GetIdempotencyRow{RequestHash string; Status *int16; ResultRef *uuid.UUID}, error)",
	"RecordIdempotency":              "(context.Context, RecordIdempotencyParams{Status *int16; ResultRef *uuid.UUID; TenantID uuid.UUID; KeyID uuid.UUID; IdemKey string}) (int64, error)",
	// M11 API-5: PUT /v1/locations/{ref} -- a venue's name, ref and K-20 label go in; the row comes
	// back with its evidence columns only so the trail can say whether it has proof of place. No key,
	// no tag, nothing of a person.
	"LockLocationByExternalRef": "(context.Context, LockLocationByExternalRefParams{TenantID uuid.UUID; ExternalRef *string}) (LockLocationByExternalRefRow{ID uuid.UUID; Name string; ExternalRef *string; Active bool; CreatedAt time.Time; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric}, error)",
	"CreateLocationFromAPI":     "(context.Context, CreateLocationFromAPIParams{Name string; ExternalRef *string; Active bool; TenantID uuid.UUID}) (CreateLocationFromAPIRow{ID uuid.UUID; Name string; ExternalRef *string; Active bool; CreatedAt time.Time; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric}, error)",
	"UpdateLocationFromAPI":     "(context.Context, UpdateLocationFromAPIParams{Name string; Active bool; TenantID uuid.UUID; ID uuid.UUID}) (UpdateLocationFromAPIRow{ID uuid.UUID; Name string; ExternalRef *string; Active bool; CreatedAt time.Time; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric}, error)",
	"GetLocationForAPI":         "(context.Context, GetLocationForAPIParams{TenantID uuid.UUID; ID uuid.UUID}) (GetLocationForAPIRow{ID uuid.UUID; Name string; ExternalRef *string; Active bool; CreatedAt time.Time; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric}, error)",
	// M11 API-9 (table from 00048): the owner's webhook endpoint. The SEALED signing key goes in
	// (InsertWebhookEndpoint, RotateWebhookEndpointKey) and comes back on ONE read -- the one that
	// signs (GetWebhookEndpointForSigning); the panel's read names no key. No plaintext key anywhere.
	"GetWebhookEndpoint":           "(context.Context, uuid.UUID) (GetWebhookEndpointRow{ID uuid.UUID; Url string; Active bool; CreatedAt time.Time; RotatedAt *time.Time}, error)",
	"GetWebhookEndpointForSigning": "(context.Context, uuid.UUID) (GetWebhookEndpointForSigningRow{ID uuid.UUID; Url string; SigningKeySealed []byte; Active bool}, error)",
	"InsertWebhookEndpoint":        "(context.Context, InsertWebhookEndpointParams{TenantID uuid.UUID; ID uuid.UUID; Url string; SigningKeySealed []byte}) (time.Time, error)",
	"RotateWebhookEndpointKey":     "(context.Context, RotateWebhookEndpointKeyParams{SigningKeySealed []byte; TenantID uuid.UUID; ID uuid.UUID}) (*time.Time, error)",
	"ReaddressWebhookEndpoint":     "(context.Context, ReaddressWebhookEndpointParams{Url string; TenantID uuid.UUID; ID uuid.UUID}) (int64, error)",
	"SetWebhookEndpointActive":     "(context.Context, SetWebhookEndpointActiveParams{Active bool; TenantID uuid.UUID; ID uuid.UUID}) (int64, error)",
	"LockWebhookBucket":            "(context.Context, uuid.UUID) (LockWebhookBucketRow{ID uuid.UUID; Unlocked bool}, error)",
	"ReadWebhookBucket":            "(context.Context, ReadWebhookBucketParams{TenantID uuid.UUID; Actions []string; WindowSeconds int32}) (ReadWebhookBucketRow{Spent int32; Oldest time.Time}, error)",
	// M11 API-10 (table from 00048): the deliveries. A lease, a result (a closed class and a status --
	// never the remote's text), the event's snapshot columns (no address, coordinate, note or policy
	// context exist to read), the log's page and the resend. No key and no address: the one read
	// that carries the sealed key stays GetWebhookEndpointForSigning.
	"ClaimWebhookDelivery":         "(context.Context, ClaimWebhookDeliveryParams{LeaseSeconds int32; TenantID uuid.UUID}) (ClaimWebhookDeliveryRow{ID uuid.UUID; EndpointID uuid.UUID; PunchEventSeq int64; Attempt int32; LeaseUntil *time.Time}, error)",
	"GetPunchEventForDelivery":     "(context.Context, GetPunchEventForDeliveryParams{TenantID uuid.UUID; Seq int64}) (GetPunchEventForDeliveryRow{Seq int64; PunchID uuid.UUID; Version int32; Status string; PunchTime time.Time; RecordedAt time.Time; Channel string; Direction string; EmployeeID uuid.UUID; EmployeeRef *string; EmployeeCode *string; EmployeeCompany *string; LocationID *uuid.UUID; LocationRef *string; LocationName *string; LocationCompany *string; TagUid *string}, error)",
	"RecordWebhookDeliveryResult":  "(context.Context, RecordWebhookDeliveryResultParams{State string; DelaySeconds int32; LastStatus *int32; LastErrorClass *string; TenantID uuid.UUID; ID uuid.UUID; LeaseUntil *time.Time}) (RecordWebhookDeliveryResultRow{Attempt int32; State string; NextAttemptAt time.Time}, error)",
	"NextWebhookDeliveryDue":       "(context.Context, uuid.UUID) (NextWebhookDeliveryDueRow{Pending int32; DueInSeconds int64}, error)",
	"ListWebhookDeliveries":        "(context.Context, ListWebhookDeliveriesParams{TenantID uuid.UUID; BeforeAt time.Time; BeforeID uuid.UUID; RetentionDays int32; PageSize int32}) ([]ListWebhookDeliveriesRow{ID uuid.UUID; PunchEventSeq int64; Attempt int32; NextAttemptAt time.Time; LeaseUntil *time.Time; State string; LastStatus *int32; LastErrorClass *string; CreatedAt time.Time; DeliveredAt *time.Time; PunchID uuid.UUID; InFlight bool}, error)",
	"LockWebhookDeliveryForResend": "(context.Context, LockWebhookDeliveryForResendParams{TenantID uuid.UUID; ID uuid.UUID; RetentionDays int32}) (LockWebhookDeliveryForResendRow{ID uuid.UUID; EndpointID uuid.UUID; State string; InFlight bool}, error)",
	"ResendWebhookDelivery":        "(context.Context, ResendWebhookDeliveryParams{TenantID uuid.UUID; ID uuid.UUID}) (int64, error)",
	// T130 (00058): the retention. The log's and the resend's reads take the window's days (an int,
	// nothing is handed back that was not before); the sweep deletes and answers a count.
	"DeleteSettledWebhookDeliveries": "(context.Context, DeleteSettledWebhookDeliveriesParams{TenantID uuid.UUID; RetentionDays int32; MaxRows int32}) (int64, error)",
	// M11 API-10: an approval's site business (K-19's second tenant) -- an id, nothing of the record.
	"GetReviewedPunchSite": "(context.Context, GetReviewedPunchSiteParams{TenantID uuid.UUID; ID uuid.UUID}) (*uuid.UUID, error)",
}

// TestStoreSurface_IsTheOneRecorded is the inventory check.
//
// Both arms are load-bearing and both are mutation-checked: SURPLUS catches an added
// or changed method, MISSING catches a deleted one. The floor is EXACT rather than a
// minimum -- a floor with slack cannot notice the thing it exists for, which this
// file said 200 lines away while carrying `< 100` (fifth audit).
func TestStoreSurface_IsTheOneRecorded(t *testing.T) {
	t.Parallel()

	got := storeMethodSignatures(t)
	if len(got) != len(storeSurface) {
		t.Errorf("internal/store declares %d *Queries method(s); the inventory records %d",
			len(got), len(storeSurface))
	}
	for name, want := range storeSurface {
		have, ok := got[name]
		if !ok {
			t.Errorf("%s is in the inventory and NOT in internal/store. If the query was removed, "+
				"remove it here in the same edit; an inventory that describes a tree that no "+
				"longer exists stops being read", name)
			continue
		}
		if have != want {
			t.Errorf("%s\n  is:       %s\n  recorded: %s\nA signature changed. If the new shape "+
				"is harmless, update the inventory in the same edit and say why; if it carries "+
				"the KEK-wrapped key -- as []byte, [][]byte, json.RawMessage, a whole-row dump or "+
				"a model type -- it is a §4.7 breach", name, have, want)
		}
	}
	for name, have := range got {
		if _, ok := storeSurface[name]; !ok {
			t.Errorf("internal/store declares %s, which is NOT in the inventory:\n  %s\nA new "+
				"query has to be recorded, which is the moment to ask what it hands back", name, have)
		}
	}
	if !t.Failed() {
		t.Logf("%d *Queries method signature(s) inventoried and matched", len(got))
	}
}

// TestStoreSurface_NoByteCarryingQueryReadsTags binds the []byte results to their SQL.
//
// 🔴 THE INVENTORY ABOVE CANNOT SEE THIS ONE, AND THE FIFTH AUDIT MEASURED WHY. Its
// probe repointed an EXISTING query at `tags` (`g.aes_key_ref AS document`, JOIN tags)
// while keeping the Go field name -- and a signature pin compares SHAPES, so a query
// that keeps its exact field set while changing WHERE THE BYTES COME FROM is invisible
// to it. (The audit's own version happened to change the field count and does turn the
// inventory red -- measured -- but a careful edit would not.)
//
// So the three []byte-carrying results are bound to a fact about their SQL: none of
// them reads `tags`. That is checked against the generated query CONSTANT, which is
// the statement text sqlc actually shipped, not a copy of it.
//
// ⚠️ THE LIST IS DERIVED, NOT TYPED: the subject is every method whose result carries
// a []byte field, found by walking the artifact. If a fourth appears it is checked
// too, and if it reads `tags` it fails -- nobody has to remember to add it.
//
// ⚠️ COUNTED LIMITS, ALL UNDER-REPORT, AND THE FIRST TWO ARE MEASURED ESCAPES RATHER
// THAN HYPOTHETICALS (sixth audit, 2026-08-24). `tags` is matched as a word against a
// short list of spellings, and BOTH of these read the table directly, kept the field
// set identical, and PASSED:
//
//	(SELECT g2.aes_key_ref FROM "tags" g2 ...) AS document     -- quoted identifier
//	(SELECT aes_key_ref FROM tags, tenants LIMIT 1) AS document -- comma join
//
// (` tags)` and ` public.tags)` are absent from the list too.) Beyond those: a read
// reaching the table through a VIEW, a CTE alias defined elsewhere, or a SECURITY
// DEFINER function would not say `tags` at all. NONE of this is repaired by widening
// the list -- that is the chase five rounds lost. What closes the class is the
// privilege: as of 00022 tappa_app cannot SELECT the column, so a query written any
// of these ways fails at runtime. This gate is kept as a cheap second reading.
func TestStoreSurface_NoByteCarryingQueryReadsTags(t *testing.T) {
	t.Parallel()

	files, fset := parseStore(t)
	structs := map[string]*ast.StructType{}
	consts := map[string]string{}
	type qm struct{ name, result string }
	var methods []qm
	for _, f := range files {
		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok {
				for _, sp := range gd.Specs {
					switch v := sp.(type) {
					case *ast.TypeSpec:
						if st, ok := v.Type.(*ast.StructType); ok {
							structs[v.Name.Name] = st
						}
					case *ast.ValueSpec:
						for i, id := range v.Names {
							if i < len(v.Values) {
								if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
									consts[id.Name] = strings.ToLower(lit.Value)
								}
							}
						}
					}
				}
				continue
			}
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 || fd.Type.Results == nil {
				continue
			}
			if exprString(fset, fd.Recv.List[0].Type) != "*Queries" || len(fd.Type.Results.List) < 2 {
				continue
			}
			methods = append(methods, qm{fd.Name.Name, exprString(fset, fd.Type.Results.List[0].Type)})
		}
	}

	carriers := 0
	for _, m := range methods {
		st, ok := structs[strings.TrimPrefix(m.result, "[]")]
		if !ok {
			continue
		}
		carries := false
		for _, fl := range st.Fields.List {
			if exprString(fset, fl.Type) == "[]byte" {
				carries = true
			}
		}
		if !carries {
			continue
		}
		carriers++
		// sqlc names the constant after the method, lower-camel.
		key := strings.ToLower(m.name[:1]) + m.name[1:]
		sql, found := consts[key]
		if !found {
			t.Errorf("%s returns a []byte field and its query constant %q was not found; this "+
				"check cannot see what it reads", m.name, key)
			continue
		}
		for _, w := range []string{" tags ", " tags\n", "\ntags ", " tags(", " public.tags "} {
			if strings.Contains(sql, w) {
				t.Errorf("%s returns a []byte AND its statement reads `tags`. The only []byte "+
					"results this artifact may carry are policy documents; a byte column sourced "+
					"from `tags` is the KEK-wrapped key leaving the database (§4.7)", m.name)
				break
			}
		}
	}
	// 🔴 FIVE, MEASURED -- AND THE FIRST DRAFT OF THIS LINE SAID THREE, WHICH IS THE
	// EXACT DEFECT THE FIFTH AUDIT RAISED AS N-A5-8 AND WHICH I THEN REPEATED IN THE
	// SAME FILE WITHIN THE HOUR. Three []byte FIELDS exist; FIVE METHODS return a
	// struct containing one, because `Transaction` is returned by three readers:
	//
	//	GetPolicyVersionDocument  -> GetPolicyVersionDocumentRow.Document
	//	ListPolicySet             -> ListPolicySetRow.Document
	//	GetLastOpenTransaction    \
	//	GetLastTransactionForEmployee > Transaction.PolicyContext
	//	InsertTransaction         /
	//
	// A count in a message is a claim like any other; this one is now the output of the
	// walk above rather than a number I expected.
	//
	// 5 -> 6 (2026-10-03, M10 WL-1), measured by this walk:
	//
	//	GetTenantLogo             -> GetTenantLogoRow.Logo (tenant_branding.logo, ADR 0024 §4)
	//
	// the logo's re-encoded bytes, read by tenant and digest for the two logo routes.
	// Its statement reads tenant_branding; the word check above runs on it like on the
	// other five.
	//
	// 6 -> 7 (2026-10-09, M10 WL-13), measured by this walk:
	//
	//	GetTenantActivationLogo   -> GetTenantActivationLogoRow.Logo (tenant_branding.logo)
	//
	// the same bytes for the activation wizard's logo route, read by tenant AND digest
	// with the business's VIES verdict in the WHERE. Its statement reads tenant_branding
	// joined to tenants; the word check above runs on it like on the other six.
	//
	// 7 -> 8 (2026-10-15, M11 API-9), measured by this walk:
	//
	//	GetWebhookEndpointForSigning -> GetWebhookEndpointForSigningRow.SigningKeySealed
	//
	// the endpoint's signing key, SEALED under TAPPA_WEBHOOK_KEK with the endpoint's id as AAD
	// (ADR 0028 §6) -- CLAUDE.md §4.7's class, and the reason it is counted here. Its statement
	// reads webhook_endpoints by tenant; the word check above runs on it like on the other seven.
	if carriers != 8 {
		t.Errorf("%d method(s) return a []byte-carrying struct; EIGHT did when this was last pinned "+
			"(2026-10-15) -- two policy-document readers, three Transaction readers, the "+
			"two tenant logo readers (the logo routes' and the activation wizard's) and the webhook "+
			"endpoint's sealed-key reader. A ninth is either a new policy document or logo reader, "+
			"or a §4.7 finding", carriers)
	}
	if !t.Failed() {
		// 🔴 THE MESSAGE SAYS WHAT THE CHECK KNOWS, AND THE PREVIOUS ONE DID NOT. It
		// read "none reads `tags`" -- a claim about the SQL. Measured: the word match
		// below misses a QUOTED table name and a COMMA JOIN, both of which read tags
		// directly and both of which passed while this line printed that sentence as
		// evidence. A gate that prints a conclusion it cannot support is worse than a
		// silent one.
		t.Logf("%d []byte-carrying result(s) checked; none says `tags` in the spellings this "+
			"check knows (see its counted limits)", carriers)
	}
}

// storeMethodSignatures renders every *Queries method as "(params) (results)", with
// every result struct EXPANDED to its fields, so a field added to an existing row
// type changes the string even though the Go signature does not.
func storeMethodSignatures(t *testing.T) map[string]string {
	t.Helper()
	files, fset := parseStore(t)
	structs := map[string]*ast.StructType{}
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				if ts, ok := sp.(*ast.TypeSpec); ok {
					if st, ok := ts.Type.(*ast.StructType); ok {
						structs[ts.Name.Name] = st
					}
				}
			}
		}
	}
	expand := func(typ string) string {
		base := strings.TrimPrefix(typ, "[]")
		st, ok := structs[base]
		if !ok {
			return typ
		}
		var fs []string
		for _, fl := range st.Fields.List {
			for _, id := range fl.Names {
				fs = append(fs, id.Name+" "+exprString(fset, fl.Type))
			}
		}
		return typ + "{" + strings.Join(fs, "; ") + "}"
	}

	out := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
				continue
			}
			if exprString(fset, fd.Recv.List[0].Type) != "*Queries" {
				continue
			}
			var ps, rs []string
			if fd.Type.Params != nil {
				for _, p := range fd.Type.Params.List {
					n := len(p.Names)
					if n == 0 {
						n = 1
					}
					for i := 0; i < n; i++ {
						ps = append(ps, expand(exprString(fset, p.Type)))
					}
				}
			}
			if fd.Type.Results != nil {
				for _, r := range fd.Type.Results.List {
					n := len(r.Names)
					if n == 0 {
						n = 1
					}
					for i := 0; i < n; i++ {
						rs = append(rs, expand(exprString(fset, r.Type)))
					}
				}
			}
			out[fd.Name.Name] = "(" + strings.Join(ps, ", ") + ") (" + strings.Join(rs, ", ") + ")"
		}
	}
	if len(out) == 0 {
		t.Fatalf("no *Queries methods parsed under %s; the scan is reading the wrong package", storeDir)
	}
	return out
}

//
// 🔴 IT PINS THE SHAPE, NOT THE NAME, AND BOTH HALVES ARE LOAD-BEARING. The field
// NAMES catch a column being added or removed; the field TYPES catch a column being
// swapped for one of a different kind. `ToJsonb []byte` is caught by neither name
// nor type in isolation -- it is caught because the SET gained a member.
//
// A row type listed as nil is one that must NOT exist (the query returns a scalar or
// a model type); today every tags query has a bespoke Row.
//
// ⚠️ ADDING A COLUMN TO A RETURNING CLAUSE TURNS THIS RED, ON PURPOSE, EVEN WHEN THE
// COLUMN IS HARMLESS. That is the trade: a one-line update here, against a class of
// silent escape that four audits have now paid for. The message says what to do.
// 🔴 A SECOND GATE STOOD HERE AND IS DELETED AS REDUNDANT (fifth audit, 2026-08-24).
// It pinned the exact field sets of the ten NAMED tags row types. Every one of those field sets is now INSIDE the
// inventory above -- `InsertUnassignedRow{Uid string; CreatedAt time.Time}` is part of
// InsertUnassigned's recorded signature -- and its surplus arm is covered too, because
// a new row type only ever arrives with a new method.
//
// It is removed rather than kept because a check that duplicates its neighbour is how
// the neighbour stops being read; this file argued exactly that when it deleted the
// second anti-vacuity counter in internal/encode. What is lost is a narrower failure
// message, and the inventory's message names the method and prints both strings, which
// is the same information.

// resolverCallSites is the INVENTORY of production Go files that name a
// `resolve_*` SECURITY DEFINER function inside a SQL string.
//
// 🔴 IT EXISTS BECAUSE THE PRIVILEGE WALL HAS A DOOR AND THE DOOR IS SHIPPED. A
// sixth audit walked past migration 00022's REVOKE without naming aes_key_ref at
// all:
//
//	(SELECT to_jsonb(r)::text FROM resolve_tag_by_uid(g.uid) r LIMIT 1) AS status
//
// substituted for `g.status` in GetTagForTenant. Measured as tappa_app: the 44-byte
// envelope came back, the generated row type was BYTE-IDENTICAL, and the inventory,
// the []byte gate, the SQL text wall and redline-check were all green. The CTE form
// -- the shape those gates' own comments name as "would land here" -- passed too.
//
// 🔴 THE WALL'S SHAPE, CORRECTED FOR THE THIRD TIME AND THIS TIME BY COUNTING RATHER
// THAN BY CLAIMING (2026-08-24). Three successive versions said, in turn, that the
// application role "cannot read the key", then that it reads it "through EXACTLY ONE
// named path, and that path is INVENTORIED". The first was false because the tap path
// needs the key. The second was false because THE INVENTORY IS TEXT MATCHING AND IT
// WAS BEATEN TWICE IN ONE SITTING. What is true:
//
//  1. Every DIRECT expression over tags.aes_key_ref is refused, and the structural
//     gain is real. ⚠️ BUT THE CREDIT HAS TO BE SPLIT, AND AN EARLIER VERSION OF
//     THIS ENTRY DID NOT SPLIT IT (audit, 2026-08-24). It said "TEN OF ELEVEN
//     attempted shapes answered `permission denied`" and filed all ten under
//     "PRIVILEGE (migration 00022)". Measured, two of those ten are refused by
//     privileges this migration never touched:
//
//     CREATE VIEW … SELECT aes_key_ref  -> permission denied for SCHEMA public
//     SET ROLE tappa_resolver           -> permission denied to SET ROLE
//     SELECT aes_key_ref FROM tags      -> permission denied for TABLE tags  <- 00022
//
//     🔴 SO 00022's OWN SHARE IS EIGHT: the column itself, to_jsonb, row_to_json,
//     SELECT *, octet_length, a quoted correlated subselect, COPY TO STDOUT, and a
//     pg_temp SECURITY DEFINER of one's own. A ninth and tenth were already refused
//     before it existed, and the eleventh (resolve_tag_by_uid) RETURNS.
//     ⚠️ A LATER AUDIT ADDED THIRTY MORE DIRECT SHAPES -- ONLY tags, g.*, json_agg,
//     INSERT/UPDATE/MERGE … RETURNING, DECLARE CURSOR, LATERAL, md5(key::text),
//     COPY TO PROGRAM, (g).aes_key_ref, ORDER BY, WHERE … IS NOT NULL and more --
//     and every one was refused. The eight is 00022's share of the ORIGINAL eleven,
//     not a ceiling on what it refuses.
//
//  2. The key REMAINS READABLE through resolve_tag_by_uid, and THAT CANNOT BE
//     CLOSED: a tap arrives with no tenant context and must unwrap the envelope to
//     verify a SUN (ADR 0002 md. 7). The eleventh shape is this one.
//
//  3. 🔴 NO MECHANISM IS IN FORCE TODAY -- AND ONE EXISTS, IT WAS SIMPLY NOT TAKEN.
//     An earlier version of this list said "NOTHING MECHANICAL LIMITS THAT PATH TO
//     ONE CALLER", and that is FALSE (measured, 2026-08-24). EXECUTE on the function
//     is an ordinary privilege: inside one BEGIN ... ROLLBACK, creating a role,
//     granting it EXECUTE and revoking tappa_app's makes the call answer
//     `ERROR: permission denied for function resolve_tag_by_uid`. proacl before and
//     after the rollback were identical.
//
//     THE OPTION, NAMED SO IT CAN BE WEIGHED RATHER THAN REDISCOVERED: give the
//     context-free resolution pool its OWN role, holding EXECUTE, and revoke it from
//     tappa_app. COST: a SECOND POOL in internal/db, which touches FAZ B2c-2b's pool
//     design -- so it is an architectural decision and is DEFERRED, not done here.
//     ⚠️ AND A MEASURED WRINKLE FOR WHOEVER TAKES IT: pool.go's roleRefusal fires
//     only on Privileged() = Super || BypassRLS || OwnsScopedTable ||
//     InheritsPrivilege, so a plain NOSUPERUSER/NOBYPASSRLS resolution role would NOT
//     be refused at boot -- the gate would not notice it either way.
//
//     🔴 WHY THE WRONG WORD MATTERED: "cannot be closed" (true -- the tap needs the
//     envelope) and "nothing mechanical exists" (false) were written in one breath,
//     and the second makes an operator stop looking. Those are different sentences
//     and only the first is earned.
//
//     WHAT IS TRUE TODAY: the two arms below catch the PLAIN spelling -- the one a
//     developer would write -- and MEASURABLY DO NOT catch an evasive one. Four
//     escapes, three reproduced end to end:
//
//     (a) U&"\0072esolve_tag_by_uid" -- a Unicode-escaped function name. sqlc
//     accepts it, the generated row type is BYTE-IDENTICAL, BOTH arms pass,
//     and the gate logs "0 definer calls" on a file whose line calls one.
//     ⚠️ This is the SAME TRICK that beat round four's text wall on the COLUMN
//     name (U&"\0061es_key_ref"), which this file lists at the top.
//
//     (b) a SPLIT STRING LITERAL in a new Go file --
//     fmt.Sprintf("... FROM %stag_by_uid($1) r", "resolve_") -- which arm 2's
//     six fixed names cannot see. go build and redline-check both pass.
//
//     (c) a FUTURE definer with a new name (say resolve_venue_by_id) called from a
//     new Go file: arm 2's list is six literal names and arm 1 reads only
//     db/queries. Not reproduced; it needs a migration that does not exist.
//
//     (d) 🔴 ADDING A LINE TO resolverCallSites -- WHICH NEEDS NO EVASION AT ALL, and
//     which the previous count MISSED. Measured end to end: a new production
//     file reading the envelope through the definer, plus ONE line here, and
//     both arms pass while arm 2 LOGS "2 file(s) name a resolver, all
//     inventoried". That is the same hole round three found in
//     keyRefAllowedLines and closed with a size ratchet ("a visible edit is not
//     a red test"), and this list shipped without one. It has one now -- see
//     the ratchet in TestResolverAccess_TheCallSitesAreTheOnesInventoried --
//     so growing the list costs a second, deliberate edit.
//     ⚠️ The ratchet does NOT tell a legitimate second caller from a smuggled
//     one; it removes the SILENT version. That distinction is a human's.
//
//  4. ⚠️ AND THAT PATH CROSSES THE TENANT BOUNDARY, because tappa_resolver holds
//     BYPASSRLS -- so this is §4.5 as well as §4.7. ADR 0002 md. 7 scoped that
//     bypass to the RESOLUTION path, where the tenant is the answer rather than the
//     question; a panel query calling it is outside that decision.
//
// 🔴 SO THIS IS A COUNTED GAP, NOT A CLOSED ONE, and it is written that way on
// purpose. agent-brief.md's second stopping rule: a new channel is NOT closed, it is
// COUNTED -- "a counted gap is safer than one CLAIMED closed". Five text designs, one
// privilege and two inventory arms later, the honest remainder is this paragraph.
// Handed on to the M8-05 card's hand-over list, under its own title.
//
// 🔴 THE POINTER IS A TITLE, NOT A NUMBER, AND THAT IS A REPAIR. It said "ITEM 17";
// measured, the list's labels ran 1..16, 18, 19, 17, and CommonMark RENUMBERS an
// ordered list from its first item -- so a reader following "item 17" landed on a
// different entry. The list is in order now, and this pointer no longer depends on
// that staying true. (A number in one file pointing into another is exactly what
// broke here, and what broke the M64 mutation ids two rounds earlier.) Find it with:
//
//	grep -n "SAYILDI, KAPATILMADI" docs/plan/m8-deploy-pilot.md
var resolverCallSites = map[string][]string{
	"internal/db/resolve.go": {
		"resolve_admin_by_email",
		"resolve_admin_session_by_token_hash",
		"resolve_invite_by_code_hash",
		"resolve_password_reset_by_token_hash",
		"resolve_session_by_token_hash",
		"resolve_tag_by_uid",
	},
}

// TestResolverAccess_NoSqlcQueryNamesADefiner is the first arm.
//
// db/queries is where a definer call would be EASY to add and INVISIBLE to review --
// it is SQL, it looks like every other query, and sqlc will generate a Go method for
// it. Measured on this tree: ZERO statement lines in the seventeen files name a
// `resolve_*` function, and db/queries/resolve.sql declares NO queries at all (it is
// a documentation file recording why sqlc cannot express these calls; the raw SQL
// lives in internal/db/resolve.go). So the rule costs nothing and needs no exemption.
//
// ⚠️ COUNTED LIMITS, ALL UNDER-REPORT. It matches the prefix `resolve_` in statement
// text, so ALL of these pass: a Unicode-escaped name (U&"\0072esolve_tag_by_uid" --
// MEASURED, sqlc accepts it and this test logs "0 definer calls" on the file that
// calls one), a definer named something else, and a read through a view. It catches
// the PLAIN spelling and nothing more. See this file's header for why that is written
// as a counted gap rather than chased.
func TestResolverAccess_NoSqlcQueryNamesADefiner(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(repoRoot, "db", "queries")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	files, named := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files++
		body, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("reading %s: %v", e.Name(), rerr)
		}
		for i, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			// 🔴 THE MARKER IS COUNTED BEFORE THE COMMENT SKIP, AND THE FIRST VERSION
			// OF THIS LOOP HAD IT AFTER -- so `named` could never increment and the
			// log line printed "0 named queries" over seventeen files holding a
			// hundred. Pattern 2, written by me, in the round that closed two other
			// instances of it. The number is load-bearing here: it is what says the
			// scan reached statements at all.
			if strings.HasPrefix(trimmed, "-- name:") {
				named++
			}
			if strings.HasPrefix(trimmed, "--") {
				continue
			}
			if strings.Contains(strings.ToLower(trimmed), "resolve_") {
				t.Errorf("db/queries/%s:%d names a SECURITY DEFINER resolver in a statement:\n  %s\n"+
					"Those functions are owned by tappa_resolver, which holds BYPASSRLS, and "+
					"resolve_tag_by_uid RETURNS aes_key_ref -- so a query calling it reads the "+
					"KEK-wrapped key AND crosses the tenant boundary, whatever migration 00022's "+
					"column privilege says. The one production caller is internal/db/resolve.go; "+
					"if a second is genuinely needed, it goes there and into resolverCallSites",
					e.Name(), i+1, trimmed)
			}
		}
	}
	// 17 -> 18 on 2026-09-26 (M10 OP-6): db/queries/operator.sql, a `-- name:`-less
	// DOCUMENT like resolve.sql -- every statement in it is a comment, so it adds no named
	// query (the count below is unchanged) and no statement line this scan reads.
	// 18 -> 19 on 2026-10-03 (M10 WL-1): db/queries/branding.sql, eight named queries
	// over tenant_branding; none calls a definer.
	// 19 -> 20 on 2026-10-14 (M11 API-1c): db/queries/sitepunches.sql, four named queries
	// over site_punches (the site's projection); none calls a definer.
	// 20 -> 21 on 2026-10-15 (M11 API-3): db/queries/apikeys.sql, seven named queries over
	// api_keys and the tenants gate; none calls a definer (the seventh resolver is called by
	// hand from internal/db/resolve.go).
	// 21 -> 22 on 2026-10-15 (M11 API-4): db/queries/apiidempotency.sql, five named queries
	// over api_idempotency (the Idempotency-Key ledger); none calls a definer.
	// 22 -> 23 on 2026-10-15 (M11 API-6): db/queries/invitequeue.sql, the invitation queue's
	// named queries over invite_requests (and the person's status); none calls a definer.
	// 23 -> 24 on 2026-10-15 (M11 API-7): db/queries/punchfeed.sql, the punch feed's two reads
	// over feed_counters and punch_events; neither calls a definer.
	// 24 -> 25 on 2026-10-15 (M11 API-8): db/queries/directory.sql, the directory's own half over
	// tenants and employees; neither query calls a definer (holding_directory is called by hand from
	// internal/db/holding.go).
	// 25 -> 26 on 2026-10-15 (M11 API-9): db/queries/webhooks.sql, the owner's webhook endpoint and
	// the ping bucket; none calls a definer.
	if files != 26 {
		t.Fatalf("read %d .sql file(s) under db/queries; TWENTY-SIX were there when this was "+
			"pinned (2026-10-15; seventeen on 2026-08-24, plus operator.sql, branding.sql, sitepunches.sql, apikeys.sql, "+
			"apiidempotency.sql, invitequeue.sql, punchfeed.sql, directory.sql and webhooks.sql)", files)
	}
	// A file count alone does not prove the STATEMENT lines were reached; this does.
	// EXACT, by this file's own standard 130 lines down ("a floor with slack cannot
	// notice the thing it exists for"). It was `< 100` against a measured 111 -- eleven
	// queries of silent slack, in the same round that pinned parseStore to != 19.
	// PRE-EXISTING SYNC (not md.5): the pin was 111 at 2026-08-24; T73 (change own
	// password) added three admin-password queries and its commit did not bump this
	// number, so this test was already RED on the branch before ADR 0018's work. It
	// is 114 now (111 + T73's three). ADR 0018 / md.5 added ZERO queries -- it only
	// added a parameter to the existing InsertUnassigned.
	// 114 -> 122 on 2026-10-03 (M10 WL-1): branding.sql's eight queries.
	// 122 -> 121 on 2026-10-03 (M10 OP-10 phase B): db/queries/legal.sql's
	// PublishLegalDocument went with the M7-06 panel's legal screen (00027 revoked
	// tappa_app's INSERT; the operator publishes through op_publish_legal).
	// 121 -> 122 on 2026-10-03 (M10 WL-8): branding.sql's GetTenantPanelBrand, the
	// panel chrome's read; it calls no definer.
	// 122 -> 125 on 2026-10-06 (M10 EM-6): employees.sql's GetEmployeeEmail,
	// LockEmployeeForEmailChange and SetEmployeeEmail, the address read and change; none
	// calls a definer.
	// 125 -> 128 on 2026-10-09 (M10 EM-7B): invites.sql's LockTenantForInviteLimits,
	// CountRecentInvites and GetInviteRecipient, the e-mail route's limits and address
	// read; none calls a definer.
	// 128 -> 130 on 2026-10-09 (M10 EM-7C): passwordresets.sql's LockAdminForResetIssue
	// and WithdrawPasswordReset, the recovery flow's one live link per account; neither
	// calls a definer.
	// 130 -> 132 on 2026-10-09 (activation by NFC tap, from main by the merge that brought
	// it into m10-a1; main pinned it 128 -> 130 alone): invites.sql's RecordInviteConsent
	// and InviteConsentMatches; neither calls a definer.
	// 132 -> 134 on 2026-10-09 (M10 WL-13): branding.sql's GetTenantActivationBrand and
	// GetTenantActivationLogo, the activation family's brand read and the wizard's logo
	// bytes; neither calls a definer.
	// 134 -> 138 on 2026-10-11 (M11 API-1): employees.sql's LockEmployeeForExternalRefChange
	// and SetEmployeeExternalRef, locations.sql's LockLocationForExternalRefChange and
	// SetLocationExternalRef -- the panel's external-ref writes (K-18); none calls a definer.
	// 138 -> 143 on 2026-10-14 (M11 API-1c): sitepunches.sql's four reads of the site's
	// projection and transactions.sql's InsertManualSiteTransaction; none calls a definer (the
	// peer's venue is read through holding_site_locations by internal/db/holding.go, by hand).
	// 143 -> 150 on 2026-10-15 (M11 API-3): apikeys.sql's seven queries; none calls a definer.
	// 150 -> 155 on 2026-10-15 (M11 API-4): apiidempotency.sql's five queries; none calls a definer.
	// 155 -> 159 on 2026-10-15 (M11 API-5): locations.sql's four venue queries of PUT
	// /v1/locations; none calls a definer.
	// 159 -> 183 on 2026-10-15 (M11 API-6): employees.sql's nine (the API's employee: lookup,
	// K-18, lock, create, names, two reads, the administrator-address check), audit.sql's four
	// (K-25's lock and count, the alert's dedupe and list) and invitequeue.sql's eleven; none
	// calls a definer.
	// 183 -> 184 on 2026-10-15 (M11 API-6, the worker): invitequeue.sql's NextInviteRequestDue,
	// when the tenant's next deferred request is due; it calls no definer.
	// 184 -> 186 on 2026-10-15 (M11 API-6, second round): invitequeue.sql's FailInviteRequest
	// (a failed send counts a try; a ceiling does not) and invites.sql's RetireUndeliveredInvite
	// (the queue gives up on a link it never delivered); CountRecentLiveInvites became
	// CountRecentSentInvites. None calls a definer.
	// 186 -> 188 on 2026-10-15 (M11 API-7): punchfeed.sql's GetPunchFeedHead and ListPunchEvents,
	// the read end of the punch feed; neither calls a definer.
	// 188 -> 191 on 2026-10-15 (M11 API-7b): punchfeed.sql's GetPunchFeedCounter, GetPunchHistoryWindow
	// and ListPunchHistory, the history query's snapshot, window and page; none calls a definer.
	// 191 -> 193 on 2026-10-15 (M11 API-8): directory.sql's GetDirectoryHead and ListDirectoryEmployees,
	// the directory's own half; neither calls a definer (the peers' half is holding_directory, called
	// by internal/db/holding.go by hand).
	// 193 -> 201 on 2026-10-15 (M11 API-9): webhooks.sql's eight -- the endpoint's two reads, its four
	// writes and the bucket's lock and count; none calls a definer.
	// 201 -> 209 on 2026-10-15 (M11 API-10): webhooks.sql's seven delivery queries (the lease, the event,
	// the result, the next due, the log, the resend's lock and the resend) and reviews.sql's
	// GetReviewedPunchSite; none calls a definer.
	// 209 -> 210 on 2026-10-18 (M11 API-11): transactions.sql's GetTapOutcome, the sandbox simulation's
	// read of the tap it recorded (no evidence column); it calls no definer.
	// 210 -> 211 on 2026-10-18 (M11 API-11 fix 1): tags.sql's LockSandboxPlaque, the simulation's
	// per-plaque advisory lock over the caller's own tenant row; it calls no definer.
	// 211 -> 212 on 2026-10-18 (T130): webhooks.sql's DeleteSettledWebhookDeliveries, the retention
	// sweep over the caller's own tenant; it calls no definer.
	// 212 -> 213 on 2026-10-19 (GRP-1): transactions.sql's CountRecordsAtPartnerVenues, a count of the
	// caller's own rows that name a peer's venue; it calls no definer (holding_group is read by
	// internal/db/holding.go by hand).
	// 213 -> 214 on 2026-10-20 (M11 API-4b): tenants.sql's GetTenantSuspended, /v1's suspension
	// checkpoint's read of the caller's own tenant row; it calls no definer.
	// The M12 rows below were renumbered by one at the merge that brought API-4b from main.
	// 214 -> 215 on 2026-10-20 (M12-13): transactions.sql's CountOnTheFloorByVenue, The Pass's "on the
	// floor now" over the caller's own rows; it calls no definer (a partner venue's name is read by
	// internal/db/holding.go by hand).
	// 215 -> 216 on 2026-10-20 (M12-16): transactions.sql's CountPanelTransactionsByVerdict, the day's
	// records by verdict under the list's own filters; it calls no definer.
	// 216 -> 218 on 2026-10-20 (M12-21, M12-25; recounted at the merge that brought C3 beside C1):
	// employees.sql's ListRollEmployees, the Roll's role and home venue per listed person, and
	// reviews.sql's CountReviewsSince, today's decisions; neither calls a definer.
	// 218 -> 219 on 2026-10-20 (M12-24; recounted at the merge that brought C4 beside C1 and C3):
	// anomalies.sql's ListAnomalyPlaqueJumps, the counters around each listed plaque's largest gap
	// over the caller's own rows; it calls no definer.
	// 219 -> 223 on 2026-10-20 (M12-17..19; recounted at the merge that brought C2 beside C1, C3
	// and C4): employees.sql's CountPanelEmployeesByStatus (the chips) and GetPanelPersonSummary
	// (the person card), transactions.sql's ListLastTapsForEmployees (the roster's last tap) and
	// ListRecentTapsForEmployee (the card's strip); none calls a definer.
	// 223 -> 227 on 2026-10-21 (M15-02, migration 00064): the four business-day cutoff writes --
	// tenants.sql's SetTenantDayCutoff, locations.sql's SetLocationDayCutoff, departments.sql's
	// SetDepartmentDayCutoff and employees.sql's SetEmployeeDayCutoff -- each an UPDATE of the
	// caller's own row; none calls a definer.
	if named != 227 {
		t.Fatalf("%d named quer(ies) were seen across %d files; TWO HUNDRED AND TWENTY-SEVEN were "+
			"there when this was pinned (2026-10-21: 111 on 2026-08-24, + T73's three "+
			"admin-password queries, + WL-1's eight branding queries, less OP-10's removed "+
			"PublishLegalDocument, + WL-8's panel brand read, + EM-6's three address queries, "+
			"+ EM-7B's three invitation e-mail queries, + EM-7C's two recovery queries, "+
			"+ the activation tap's two consent queries, + WL-13's two activation brand queries, "+
			"+ API-1's four external-ref queries, + API-1c's five site queries, + API-3's seven key queries, + API-4's five ledger queries, + API-5's four venue queries, + API-6's twenty-seven employee, budget and queue queries, + API-7's two feed reads, + API-7b's three history reads, + API-8's two directory reads, + API-9's eight webhook queries, + API-10's eight delivery and approval queries, + API-11's one tap outcome read and its fix round's plaque lock, + T130's retention sweep, + GRP-1's partner-venue count, + API-4b's suspension read, + M12-13's floor count, + M12-16's verdict count, + M12-21's roll homes, + M12-25's decision count, + M12-24's plaque counters, + M12-17..19's four People reads, + M15-02's four cutoff writes). "+
			"Update the number in the same edit that adds or removes a query", named, files)
	}
	if !t.Failed() {
		t.Logf("%d .sql file(s) read, %d named queries, 0 definer calls", files, named)
	}
}

// TestResolverAccess_TheCallSitesAreTheOnesInventoried is the second arm: WHERE the
// raw SQL lives, counted with go/ast rather than trusted.
//
// This is the shape internal/encode uses for retireCallSites and sessionFields, and
// it is tested BOTH WAYS -- a new file naming a resolver fails as surplus, and a
// resolver disappearing from the inventoried file fails as missing.
//
// ⚠️ COUNTED LIMITS, AND THIS ARM HAD NONE WRITTEN UNTIL AN AUDIT ASKED (2026-08-24).
// It matches SIX FIXED FULL NAMES inside string literals, so:
//   - a SPLIT LITERAL defeats it -- fmt.Sprintf("... %stag_by_uid($1) ...",
//     "resolve_") in a new file passes both arms, go build and redline-check.
//     MEASURED end to end.
//   - a definer with a NAME NOT ON THE LIST (a future resolve_venue_by_id) is
//     invisible here, and arm 1 only reads db/queries -- so a new Go file calling it
//     is caught by nothing.
//   - it skips internal/store (generated; its only hits are copied doc comments) and
//     _test.go files.
//
// See the header: this is counted, not closed.
func TestResolverAccess_TheCallSitesAreTheOnesInventoried(t *testing.T) {
	t.Parallel()

	got := map[string][]string{}
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "node_modules" || base == ".tools" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(repoRoot, path)
		if rerr != nil {
			return rerr
		}
		if strings.HasPrefix(rel, "internal/store/") {
			return nil // generated; its only hits are copied doc comments
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		var found []string
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			for _, name := range []string{
				"resolve_admin_by_email", "resolve_admin_session_by_token_hash",
				"resolve_invite_by_code_hash", "resolve_password_reset_by_token_hash",
				"resolve_session_by_token_hash", "resolve_tag_by_uid",
			} {
				if strings.Contains(lit.Value, name) {
					found = append(found, name)
				}
			}
			return true
		})
		if len(found) > 0 {
			sort.Strings(found)
			got[filepath.ToSlash(rel)] = dedupe(found)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no file names a resolver in a string literal; the scan is not reading the tree")
	}

	for file, want := range resolverCallSites {
		have, ok := got[file]
		if !ok {
			t.Errorf("%s is inventoried as a resolver caller and no longer names one. If the raw "+
				"SQL moved, move this entry with it -- an inventory that describes a tree that "+
				"no longer exists stops being read", file)
			continue
		}
		if strings.Join(have, ",") != strings.Join(want, ",") {
			t.Errorf("%s names %v; the inventory records %v", file, have, want)
		}
	}
	for file, have := range got {
		if _, ok := resolverCallSites[file]; !ok {
			t.Errorf("%s names a SECURITY DEFINER resolver %v and is NOT inventoried. Those "+
				"functions bypass RLS and resolve_tag_by_uid returns the KEK-wrapped key, so a "+
				"second caller is a tenant-isolation and a §4.7 decision, not a refactor",
				file, have)
		}
	}
	// 🔴 THE INVENTORY'S OWN SIZE IS RATCHETED, AND ITS ABSENCE WAS THE FOURTH ESCAPE
	// (audit, 2026-08-24). Measured end to end: a new production file reading the
	// envelope through the definer, plus ONE line in resolverCallSites, and both arms
	// passed while this very Logf printed "2 file(s) name a resolver, all
	// inventoried" -- the gate ENDORSING a second caller that crosses the tenant
	// boundary. No Unicode escape, no split literal; just a line.
	//
	// Round three of this same task closed exactly this hole in keyRefAllowedLines,
	// for exactly this reason ("a visible edit is not a red test", mutation M26), and
	// this list shipped without the lesson. Growing it now costs a SECOND deliberate
	// edit, here, which is where a reviewer is looking.
	//
	// ⚠️ IT CANNOT TELL A LEGITIMATE SECOND CALLER FROM A SMUGGLED ONE. That is a
	// human's judgement and always was; what this removes is the SILENT version.
	if len(resolverCallSites) != 1 {
		t.Errorf("resolverCallSites holds %d entr(ies); ONE was the whole permission when this "+
			"ratchet was set (2026-08-24): internal/db/resolve.go. A second production caller of "+
			"a resolve_* definer reads the KEK-wrapped key AND crosses the tenant boundary "+
			"(tappa_resolver holds BYPASSRLS) -- say which caller, and why, and raise this "+
			"number in the same edit", len(resolverCallSites))
	}
	if !t.Failed() {
		t.Logf("%d file(s) name a resolver, all inventoried; inventory size %d",
			len(got), len(resolverCallSites))
	}
}

func dedupe(in []string) []string {
	out := in[:0:0]
	seen := map[string]bool{}
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// --- helpers -------------------------------------------------------------------

func parseStore(t *testing.T) (map[string]*ast.File, *token.FileSet) {
	t.Helper()
	dir := filepath.Join(repoRoot, storeDir)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	out := map[string]*ast.File{}
	for _, pkg := range pkgs {
		for path, f := range pkg.Files {
			out[filepath.Base(path)] = f
		}
	}
	// 🔴 THE FLOOR IS THE MEASURED NUMBER, NOT A NUMBER BELOW IT (audit, 2026-08-24).
	// It was 15 while the message said "nineteen were there" -- four files of silent
	// slack behind a figure presented as a floor. A floor with slack cannot notice the
	// thing it exists for; if a generated file disappears, that IS the event.
	//
	// 19 -> 20 (2026-10-03, M10 WL-1): db/queries/branding.sql adds branding.sql.go. The
	// eight methods it generates are in storeSurface, and GetTenantLogo is the sixth
	// []byte carrier counted in TestStoreSurface_NoByteCarryingQueryReadsTags.
	// 20 -> 21 (2026-10-14, M11 API-1c): db/queries/sitepunches.sql adds sitepunches.sql.go --
	// four reads of the site's projection, in storeSurface; none carries bytes.
	// 21 -> 22 (2026-10-15, M11 API-3): db/queries/apikeys.sql adds apikeys.sql.go -- seven
	// methods over api_keys and the tenants gate, in storeSurface; none carries bytes.
	// 22 -> 23 (2026-10-15, M11 API-4): db/queries/apiidempotency.sql adds apiidempotency.sql.go --
	// five methods over api_idempotency, in storeSurface; none carries bytes.
	// 23 -> 24 (2026-10-15, M11 API-6): db/queries/invitequeue.sql adds invitequeue.sql.go -- the
	// invitation queue's methods, in storeSurface; none carries bytes, none returns a code or a hash.
	// 24 -> 25 (2026-10-15, M11 API-7): db/queries/punchfeed.sql adds punchfeed.sql.go -- the punch
	// feed's two reads (the head and a page), in storeSurface; none carries bytes. (API-7b adds its
	// three history reads to the same file.)
	// 25 -> 26 (2026-10-15, M11 API-8): db/queries/directory.sql adds directory.sql.go -- the
	// directory's own half (the head and a page), in storeSurface; none carries bytes, none the e-mail.
	// 26 -> 27 (2026-10-15, M11 API-9): db/queries/webhooks.sql adds webhooks.sql.go -- the owner's
	// webhook endpoint (eight methods, in storeSurface). Two carry bytes, both the SEALED signing key
	// of webhook_endpoints (InsertWebhookEndpoint's parameter, GetWebhookEndpointForSigning's row);
	// neither reads tags.
	if len(out) != 27 {
		t.Fatalf("parsed %d file(s) under %s; TWENTY-SEVEN were generated when this floor was last set "+
			"(2026-10-15). If sqlc genuinely emits a different number now, change this number "+
			"in the same edit and say why -- do not widen it", len(out), storeDir)
	}
	return out, fset
}

// exprString renders a field's type the way it is written in the source.
func exprString(fset *token.FileSet, e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return "*" + exprString(fset, v.X)
	case *ast.SelectorExpr:
		return exprString(fset, v.X) + "." + v.Sel.Name
	case *ast.ArrayType:
		if v.Len == nil {
			return "[]" + exprString(fset, v.Elt)
		}
		return "[N]" + exprString(fset, v.Elt)
	case *ast.MapType:
		return "map[" + exprString(fset, v.Key) + "]" + exprString(fset, v.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{...}"
	case *ast.ChanType:
		return "chan"
	case *ast.FuncType:
		return "func"
	default:
		return fmt.Sprintf("%T", e)
	}
}
