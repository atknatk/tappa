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
// inventory says "112 inventoried and matched", the []byte gate never looks, the SQL
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
// recorded -- 112 method signatures with their parameter and result structs expanded
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
// fields. 112 entries, generated from the artifact and pinned verbatim.
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
// The question "how does a leak look" has no finite answer. THE ARTIFACT DOES: 112
// methods, 19 files, all generated by us. So the criterion is no longer "is this
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
var storeSurface = map[string]string{
	"AdvanceTagCounter":                  "(context.Context, AdvanceTagCounterParams{Ctr int32; TenantID uuid.UUID; Uid string}) (AdvanceTagCounterRow{Uid string; CtrGap int32}, error)",
	"AppendPolicyVersion":                "(context.Context, AppendPolicyVersionParams{TenantID uuid.UUID; PolicyID uuid.UUID; VersionNo int32; Document []byte; CreatedBy *uuid.UUID}) (AppendPolicyVersionRow{ID uuid.UUID; VersionNo int32; CreatedAt time.Time}, error)",
	"AssignTagToLocation":                "(context.Context, AssignTagToLocationParams{LocationID uuid.UUID; TenantID uuid.UUID; Uid string}) (AssignTagToLocationRow{Uid string; TenantID uuid.UUID; LocationID *uuid.UUID; LastCtr int32; Status string; RetiredAt *time.Time; ReplacedBy *string; CreatedAt time.Time}, error)",
	"AttachPolicyResource":               "(context.Context, AttachPolicyResourceParams{TenantID uuid.UUID; PolicyID uuid.UUID; Resource string}) (AttachPolicyResourceRow{ID uuid.UUID; Resource string}, error)",
	"CancelPendingInvitesForEmployee":    "(context.Context, CancelPendingInvitesForEmployeeParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) ([]uuid.UUID, error)",
	"CloseBillingPeriod":                 "(context.Context, CloseBillingPeriodParams{ClosedBy uuid.UUID; PeriodMonth pgtype.Date; TenantID uuid.UUID}) (CloseBillingPeriodRow{ID uuid.UUID; TenantID uuid.UUID; PeriodMonth pgtype.Date; PeriodFrom time.Time; PeriodTo time.Time; Timezone string; Plan string; FreePeriod bool; EmployeeCount int32; UnstampedEmployees int32; UnitPrice pgtype.Numeric; Currency string; AmountDue pgtype.Numeric; ClosedBy uuid.UUID; ClosedAt time.Time}, error)",
	"ConfirmRecentRemoval":               "(context.Context, ConfirmRecentRemovalParams{TenantID uuid.UUID; ActorID uuid.UUID; Action string; Target string; WindowSeconds int32}) (string, error)",
	"ConsumeInviteAndActivate":           "(context.Context, ConsumeInviteAndActivateParams{TenantID uuid.UUID; CodeHash string}) (ConsumeInviteAndActivateRow{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; DepartmentID *uuid.UUID; FullName string; Status string; InvitedAt *time.Time; ActivatedAt *time.Time; InviteID uuid.UUID}, error)",
	"ConsumePasswordResetAndSetPassword": "(context.Context, ConsumePasswordResetAndSetPasswordParams{PasswordHash string; TenantID uuid.UUID; TokenHash string}) (ConsumePasswordResetAndSetPasswordRow{AdminUserID uuid.UUID; TenantID uuid.UUID; ResetID uuid.UUID; RetiredCount int64}, error)",
	"CountAnomalySignals":                "(context.Context, CountAnomalySignalsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time}) (CountAnomalySignalsRow{Records int64; Judged int64; Unanswerable int64; DistanceOnly int64; OutsideVenue int64; OutsideVenueOnNetwork int64; CounterGaps int64; OtherVenue int64; Training int64; ManagerTyped int64}, error)",
	"CountDepartmentReferences":          "(context.Context, CountDepartmentReferencesParams{TenantID uuid.UUID; ID uuid.UUID}) (CountDepartmentReferencesRow{EmployeeCount int64; TransactionCount int64}, error)",
	"CountLocationReferences":            "(context.Context, CountLocationReferencesParams{TenantID uuid.UUID; ID uuid.UUID}) (CountLocationReferencesRow{DepartmentCount int64; EmployeeCount int64; TagCount int64; TransactionCount int64}, error)",
	"CountMaskedOpenCheckIns":            "(context.Context, CountMaskedOpenCheckInsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time}) (CountMaskedOpenCheckInsRow{StillOpen int64; ClosedLater int64}, error)",
	"CountPendingFlagged":                "(context.Context, CountPendingFlaggedParams{TenantID uuid.UUID; Cap int32}) (int32, error)",
	"CountPracticeTaps":                  "(context.Context, CountPracticeTapsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time}) (int64, error)",
	"CountTenantPlaques":                 "(context.Context, uuid.UUID) (CountTenantPlaquesRow{InService int64; InStock int64; Loaded int64}, error)",
	"CreateAdminSession":                 "(context.Context, CreateAdminSessionParams{TokenHash string; AdminUserID uuid.UUID; TenantID uuid.UUID}) (CreateAdminSessionRow{ID uuid.UUID; TenantID uuid.UUID; AdminUserID uuid.UUID; CreatedAt time.Time; LastUsedAt *time.Time; RevokedAt *time.Time}, error)",
	"CreateAdminUser":                    "(context.Context, CreateAdminUserParams{FullName string; Email *string; PasswordHash string; Role string; TenantID uuid.UUID}) (CreateAdminUserRow{ID uuid.UUID; TenantID uuid.UUID; FullName string; Role string; Status string; CreatedAt time.Time}, error)",
	"CreateDepartment":                   "(context.Context, CreateDepartmentParams{TenantID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; LocationID uuid.UUID}) (Department{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time}, error)",
	"CreateEmployee":                     "(context.Context, CreateEmployeeParams{TenantID uuid.UUID; FullName string; Role *string; Email *string; DepartmentID *uuid.UUID; LocationID uuid.UUID}) (CreateEmployeeRow{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; DepartmentID *uuid.UUID; FullName string; Role *string; Status string; CreatedAt time.Time}, error)",
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
	"FirstChargeableMonthShift":          "(context.Context, FirstChargeableMonthShiftParams{Timezone string; TenantID uuid.UUID}) (FirstChargeableMonthShiftRow{CurrentMonth pgtype.Date; ProposedMonth pgtype.Date}, error)",
	"GetAdminByID":                       "(context.Context, GetAdminByIDParams{ID uuid.UUID; TenantID uuid.UUID}) (GetAdminByIDRow{ID uuid.UUID; TenantID uuid.UUID; FullName string; Email *string; Role string; Status string; CreatedAt time.Time; LastLoginAt *time.Time}, error)",
	"GetAdminForTenantChoice":            "(context.Context, GetAdminForTenantChoiceParams{ID uuid.UUID; TenantID uuid.UUID}) (GetAdminForTenantChoiceRow{ID uuid.UUID; TenantID uuid.UUID; Role string; FullName string; TenantName string}, error)",
	"GetBillingPeriod":                   "(context.Context, GetBillingPeriodParams{TenantID uuid.UUID; PeriodMonth pgtype.Date}) (GetBillingPeriodRow{ID uuid.UUID; TenantID uuid.UUID; TenantName string; PeriodMonth pgtype.Date; PeriodFrom time.Time; PeriodTo time.Time; Timezone string; Plan string; FreePeriod bool; EmployeeCount int32; UnstampedEmployees int32; UnitPrice pgtype.Numeric; Currency string; AmountDue pgtype.Numeric; ClosedBy uuid.UUID; ClosedAt time.Time}, error)",
	"GetDepartmentShift":                 "(context.Context, GetDepartmentShiftParams{TenantID uuid.UUID; ID uuid.UUID}) (GetDepartmentShiftRow{ID uuid.UUID; TenantID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool}, error)",
	"GetEmployeeActivationContext":       "(context.Context, GetEmployeeActivationContextParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (GetEmployeeActivationContextRow{ID uuid.UUID; FullName string; Status string; LocationID uuid.UUID; TenantName string}, error)",
	"GetEmployeeForTap":                  "(context.Context, GetEmployeeForTapParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (GetEmployeeForTapRow{Status string; LocationID uuid.UUID; DepartmentID *uuid.UUID; ActivatedAt *time.Time; TenantTimezone string; TenantBusinessType string}, error)",
	"GetLastOpenTransaction":             "(context.Context, GetLastOpenTransactionParams{TenantID uuid.UUID; EmployeeID *uuid.UUID}) (Transaction{ID uuid.UUID; TenantID uuid.UUID; EmployeeID *uuid.UUID; LocationID *uuid.UUID; DepartmentID *uuid.UUID; TagUid *string; Ctr *int32; Type *string; OccurredAt time.Time; SourceIp *netip.Addr; IpMatch *bool; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; GpsMatch *bool; SunValid *bool; Trust *int16; Verdict string; Note *string; Channel string; EnteredBy *uuid.UUID; Practice bool; Queued bool; CreatedAt time.Time; PolicyVersionID *uuid.UUID; MatchedSid *string; PolicyLayer *string; PolicyContext []byte}, error)",
	"GetLastTransactionForEmployee":      "(context.Context, GetLastTransactionForEmployeeParams{TenantID uuid.UUID; EmployeeID *uuid.UUID}) (Transaction{ID uuid.UUID; TenantID uuid.UUID; EmployeeID *uuid.UUID; LocationID *uuid.UUID; DepartmentID *uuid.UUID; TagUid *string; Ctr *int32; Type *string; OccurredAt time.Time; SourceIp *netip.Addr; IpMatch *bool; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; GpsMatch *bool; SunValid *bool; Trust *int16; Verdict string; Note *string; Channel string; EnteredBy *uuid.UUID; Practice bool; Queued bool; CreatedAt time.Time; PolicyVersionID *uuid.UUID; MatchedSid *string; PolicyLayer *string; PolicyContext []byte}, error)",
	"GetLocationByIP":                    "(context.Context, GetLocationByIPParams{TenantID uuid.UUID; Src netip.Addr}) (Location{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time; WifiSsid *string}, error)",
	"GetLocationForTap":                  "(context.Context, GetLocationForTapParams{TenantID uuid.UUID; ID uuid.UUID}) (GetLocationForTapRow{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool}, error)",
	"GetLocationWiFi":                    "(context.Context, GetLocationWiFiParams{TenantID uuid.UUID; ID uuid.UUID}) (GetLocationWiFiRow{ID uuid.UUID; TenantID uuid.UUID; Name string; WifiSsid *string}, error)",
	"GetPanelDepartment":                 "(context.Context, GetPanelDepartmentParams{TenantID uuid.UUID; ID uuid.UUID}) (GetPanelDepartmentRow{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time; LocationName string}, error)",
	"GetPanelEmployeeForAction":          "(context.Context, GetPanelEmployeeForActionParams{TenantID uuid.UUID; ID uuid.UUID}) (GetPanelEmployeeForActionRow{ID uuid.UUID; FullName string; Status string; LocationID uuid.UUID; DepartmentID *uuid.UUID; LocationName *string; DepartmentName *string}, error)",
	"GetPanelLocation":                   "(context.Context, GetPanelLocationParams{TenantID uuid.UUID; ID uuid.UUID}) (GetPanelLocationRow{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; CreatedAt time.Time}, error)",
	"GetPolicyForUpdate":                 "(context.Context, GetPolicyForUpdateParams{TenantID uuid.UUID; ID uuid.UUID}) (GetPolicyForUpdateRow{ID uuid.UUID; Name string; Layer string; Enabled bool}, error)",
	"GetPolicyVersionDocument":           "(context.Context, GetPolicyVersionDocumentParams{TenantID uuid.UUID; PolicyID uuid.UUID; VersionNo int32}) (GetPolicyVersionDocumentRow{ID uuid.UUID; VersionNo int32; CreatedAt time.Time; CreatedBy *uuid.UUID; Document []byte; PolicyName string; Layer string; Enabled bool; AuthorName *string; DocumentBytes int32}, error)",
	"GetRosterCursorAnchor":              "(context.Context, GetRosterCursorAnchorParams{TenantID uuid.UUID; ID uuid.UUID}) (string, error)",
	"GetTagForTenant":                    "(context.Context, GetTagForTenantParams{TenantID uuid.UUID; Uid string}) (GetTagForTenantRow{Uid string; TenantID uuid.UUID; LocationID *uuid.UUID; LastCtr int32; Status string; RetiredAt *time.Time; ReplacedBy *string; CreatedAt time.Time}, error)",
	"GetTenantAccount":                   "(context.Context, uuid.UUID) (GetTenantAccountRow{ID uuid.UUID; Name string; VatNumber string; BusinessType string; Timezone string; VatVerified *bool; VatCheckedAt *time.Time; CreatedAt time.Time}, error)",
	"GetTenantClock":                     "(context.Context, uuid.UUID) (GetTenantClockRow{ID uuid.UUID; Name string; Timezone string}, error)",
	"GetTransactionReview":               "(context.Context, GetTransactionReviewParams{TenantID uuid.UUID; TransactionID uuid.UUID}) (GetTransactionReviewRow{ReviewerID uuid.UUID; Outcome string}, error)",
	"InsertManualTransaction":            "(context.Context, InsertManualTransactionParams{TenantID uuid.UUID; Type string; OccurredAt time.Time; Trust int16; Note *string; EnteredBy uuid.UUID; EmployeeID uuid.UUID}) (InsertManualTransactionRow{ID uuid.UUID; OccurredAt time.Time; CreatedAt time.Time; Type *string; Verdict string; Channel string; Trust *int16; Practice bool; Queued bool}, error)",
	"InsertTransaction":                  "(context.Context, InsertTransactionParams{TenantID uuid.UUID; EmployeeID *uuid.UUID; LocationID *uuid.UUID; DepartmentID *uuid.UUID; TagUid *string; Ctr *int32; Type *string; OccurredAt time.Time; SourceIp *netip.Addr; IpMatch *bool; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; GpsMatch *bool; SunValid *bool; Trust *int16; Verdict string; Note *string; Channel string; EnteredBy *uuid.UUID; Practice bool; Queued bool; PolicyVersionID *uuid.UUID; MatchedSid *string; PolicyLayer *string; PolicyContext []byte}) (Transaction{ID uuid.UUID; TenantID uuid.UUID; EmployeeID *uuid.UUID; LocationID *uuid.UUID; DepartmentID *uuid.UUID; TagUid *string; Ctr *int32; Type *string; OccurredAt time.Time; SourceIp *netip.Addr; IpMatch *bool; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; GpsMatch *bool; SunValid *bool; Trust *int16; Verdict string; Note *string; Channel string; EnteredBy *uuid.UUID; Practice bool; Queued bool; CreatedAt time.Time; PolicyVersionID *uuid.UUID; MatchedSid *string; PolicyLayer *string; PolicyContext []byte}, error)",
	"InsertTransactionReview":            "(context.Context, InsertTransactionReviewParams{TenantID uuid.UUID; ReviewerID uuid.UUID; Outcome string; Note *string; TransactionID uuid.UUID}) (InsertTransactionReviewRow{ID uuid.UUID; ReviewedAt time.Time}, error)",
	"InsertUnassigned":                   "(context.Context, InsertUnassignedParams{Uid string; TenantID uuid.UUID; AesKeyRef []byte}) (InsertUnassignedRow{Uid string; CreatedAt time.Time}, error)",
	"ListAdminSessionsForAdmin":          "(context.Context, ListAdminSessionsForAdminParams{TenantID uuid.UUID; AdminUserID uuid.UUID}) ([]ListAdminSessionsForAdminRow{ID uuid.UUID; TenantID uuid.UUID; AdminUserID uuid.UUID; CreatedAt time.Time; LastUsedAt *time.Time; RevokedAt *time.Time}, error)",
	"ListAnomalyPeople":                  "(context.Context, ListAnomalyPeopleParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListAnomalyPeopleRow{EmployeeID *uuid.UUID; EmployeeName *string; Records int64; DistanceOnly int64; OutsideVenue int64; CounterGaps int64; OtherVenue int64}, error)",
	"ListAnomalyPlaques":                 "(context.Context, ListAnomalyPlaquesParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListAnomalyPlaquesRow{TagUid *string; LocationName *string; Records int64; CounterGaps int64; LargestGap int32}, error)",
	"ListAnomalyPolicySids":              "(context.Context, ListAnomalyPolicySidsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListAnomalyPolicySidsRow{MatchedSid *string; PolicyLayer *string; Records int64}, error)",
	"ListAnomalyVenues":                  "(context.Context, ListAnomalyVenuesParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListAnomalyVenuesRow{LocationID *uuid.UUID; LocationName *string; Records int64; Judged int64; Answerable int64; DistanceOnly int64; OutsideVenue int64}, error)",
	"ListBillingPeriods":                 "(context.Context, ListBillingPeriodsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListBillingPeriodsRow{ID uuid.UUID; TenantID uuid.UUID; TenantName string; PeriodMonth pgtype.Date; PeriodFrom time.Time; PeriodTo time.Time; Timezone string; Plan string; FreePeriod bool; EmployeeCount int32; UnstampedEmployees int32; UnitPrice pgtype.Numeric; Currency string; AmountDue pgtype.Numeric; ClosedBy uuid.UUID; ClosedAt time.Time}, error)",
	"ListDepartmentsForTenant":           "(context.Context, uuid.UUID) ([]ListDepartmentsForTenantRow{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; Name string; LocationName string}, error)",
	"ListFlaggedForReview":               "(context.Context, ListFlaggedForReviewParams{TenantID uuid.UUID; CursorAt *time.Time; CursorID *uuid.UUID; PageSize int32}) ([]ListFlaggedForReviewRow{ID uuid.UUID; OccurredAt time.Time; Type *string; Trust *int16; Verdict string; Channel string; Practice bool; Queued bool; TagUid *string; Ctr *int32; IpMatch *bool; GpsMatch *bool; Note *string; PolicyLayer *string; LocationName *string; DepartmentName *string; EmployeeName *string}, error)",
	"ListLivePasswordResetsForAdmin":     "(context.Context, ListLivePasswordResetsForAdminParams{TenantID uuid.UUID; AdminUserID uuid.UUID}) ([]ListLivePasswordResetsForAdminRow{ID uuid.UUID; TenantID uuid.UUID; AdminUserID uuid.UUID; CreatedAt time.Time; ExpiresAt time.Time}, error)",
	"ListLocationsForTenant":             "(context.Context, uuid.UUID) ([]Location{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time; WifiSsid *string}, error)",
	"ListOpenCheckIns":                   "(context.Context, ListOpenCheckInsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; RowLimit int32}) ([]ListOpenCheckInsRow{EmployeeName *string; LocationName *string; OccurredAt time.Time; ManagerTyped bool; WasMaskable bool}, error)",
	"ListPanelDepartments":               "(context.Context, ListPanelDepartmentsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPanelDepartmentsRow{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time; LocationName string}, error)",
	"ListPanelEmployees":                 "(context.Context, ListPanelEmployeesParams{TenantID uuid.UUID; Status *string; LocationID *uuid.UUID; DepartmentID *uuid.UUID; FullName *string; CursorName *string; CursorID *uuid.UUID; PageSize int32}) ([]ListPanelEmployeesRow{ID uuid.UUID; FullName string; Status string; InvitedAt *time.Time; ActivatedAt *time.Time; DeactivatedAt *time.Time; LocationName *string; DepartmentName *string; LiveSessions int64; SessionLastUsedAt *time.Time}, error)",
	"ListPanelLocations":                 "(context.Context, ListPanelLocationsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPanelLocationsRow{ID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; CreatedAt time.Time; DepartmentCount int64}, error)",
	"ListPanelTransactions":              "(context.Context, ListPanelTransactionsParams{TenantID uuid.UUID; FromAt time.Time; ToAt time.Time; LocationID *uuid.UUID; DepartmentID *uuid.UUID; EmployeeName *string; Verdict *string; Channel *string; CursorAt *time.Time; CursorID *uuid.UUID; PageSize int32}) ([]ListPanelTransactionsRow{ID uuid.UUID; OccurredAt time.Time; Type *string; Trust *int16; Verdict string; Channel string; Practice bool; Queued bool; TagUid *string; Ctr *int32; IpMatch *bool; GpsMatch *bool; Note *string; PolicyLayer *string; LocationName *string; DepartmentName *string; EmployeeName *string; ReviewOutcome *string; ReviewNote *string}, error)",
	"ListPendingInvitesForEmployee":      "(context.Context, ListPendingInvitesForEmployeeParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) ([]ListPendingInvitesForEmployeeRow{ID uuid.UUID; TenantID uuid.UUID; EmployeeID uuid.UUID; CreatedAt time.Time; ExpiresAt time.Time}, error)",
	"ListPlaqueHistory":                  "(context.Context, ListPlaqueHistoryParams{TenantID uuid.UUID; Target string; RowLimit int32}) ([]ListPlaqueHistoryRow{Action string; At time.Time; ActorName string; BySystem bool}, error)",
	"ListPolicyAttachments":              "(context.Context, ListPolicyAttachmentsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPolicyAttachmentsRow{PolicyID uuid.UUID; Resource string}, error)",
	"ListPolicyDepartmentTargets":        "(context.Context, ListPolicyDepartmentTargetsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPolicyDepartmentTargetsRow{ID uuid.UUID; Name string}, error)",
	"ListPolicySet":                      "(context.Context, uuid.UUID) ([]ListPolicySetRow{PolicyID uuid.UUID; Name string; Layer string; Enabled bool; VersionID uuid.UUID; VersionNo int32; Document []byte}, error)",
	"ListPolicyVenueTargets":             "(context.Context, ListPolicyVenueTargetsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPolicyVenueTargetsRow{ID uuid.UUID; Name string}, error)",
	"ListPolicyVersions":                 "(context.Context, ListPolicyVersionsParams{TenantID uuid.UUID; RowLimit int32}) ([]ListPolicyVersionsRow{PolicyID uuid.UUID; Name string; Layer string; Enabled bool; VersionID *uuid.UUID; VersionNo *int32; VersionCreatedAt *time.Time; CreatedBy *uuid.UUID; AuthorName *string; DocumentBytes int32}, error)",
	"ListPublishedLegalDocuments":        "(context.Context) ([]ListPublishedLegalDocumentsRow{ID uuid.UUID; Slug string; Body string; PublishedAt time.Time}, error)",
	"ListSessionsForEmployee":            "(context.Context, ListSessionsForEmployeeParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) ([]ListSessionsForEmployeeRow{ID uuid.UUID; TenantID uuid.UUID; EmployeeID uuid.UUID; DeviceInfo *string; CreatedAt time.Time; LastUsedAt *time.Time; RevokedAt *time.Time}, error)",
	"ListTagLastSeen":                    "(context.Context, uuid.UUID) ([]ListTagLastSeenRow{TagUid *string; LastSeen time.Time}, error)",
	"ListTagsForTenant":                  "(context.Context, uuid.UUID) ([]ListTagsForTenantRow{Uid string; TenantID uuid.UUID; LocationID *uuid.UUID; LastCtr int32; Status string; RetiredAt *time.Time; ReplacedBy *string; CreatedAt time.Time}, error)",
	"ListTapsTakenTogether":              "(context.Context, ListTapsTakenTogetherParams{TenantID uuid.UUID; MinDays int32; RowLimit int32; FromAt time.Time; ToAt time.Time; Zone string; WithinSeconds float64}) ([]ListTapsTakenTogetherRow{FirstEmployeeName *string; SecondEmployeeName *string; LocationName *string; Together int64; Days int64; ClosestSeconds int32}, error)",
	"ListWorkedShiftEvents":              "(context.Context, ListWorkedShiftEventsParams{TenantID uuid.UUID; FromAt time.Time; UntilAt time.Time; RowLimit int32}) ([]ListWorkedShiftEventsRow{EmployeeID *uuid.UUID; OccurredAt time.Time; Type *string; Verdict string; Channel string; LocationID *uuid.UUID; ReviewOutcome *string; EmployeeName *string; LocationName *string; LocationShiftStart pgtype.Time; LocationShiftEnd pgtype.Time; LocationOvernight *bool; DepartmentShiftStart pgtype.Time; DepartmentShiftEnd pgtype.Time; DepartmentOvernight *bool}, error)",
	"LockEmployeeForTap":                 "(context.Context, LockEmployeeForTapParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) (error)",
	"MarkAdminLoggedIn":                  "(context.Context, MarkAdminLoggedInParams{ID uuid.UUID; TenantID uuid.UUID}) (MarkAdminLoggedInRow{ID uuid.UUID; LastLoginAt *time.Time}, error)",
	"MarkTagEncoded":                     "(context.Context, MarkTagEncodedParams{Uid string; TenantID uuid.UUID}) (MarkTagEncodedRow{Uid string; EncodedAt *time.Time}, error)",
	"MoveEmployee":                       "(context.Context, MoveEmployeeParams{TenantID uuid.UUID; ID uuid.UUID; LocationID uuid.UUID; DepartmentID *uuid.UUID}) (MoveEmployeeRow{ID uuid.UUID; FullName string; LocationID uuid.UUID; DepartmentID *uuid.UUID}, error)",
	"NextPolicyVersionNo":                "(context.Context, NextPolicyVersionNoParams{TenantID uuid.UUID; PolicyID uuid.UUID}) (int32, error)",
	"PolicyNameTaken":                    "(context.Context, PolicyNameTakenParams{TenantID uuid.UUID; Name string}) (bool, error)",
	"PreviewBillingPeriod":               "(context.Context, PreviewBillingPeriodParams{PeriodMonth pgtype.Date; TenantID uuid.UUID}) (PreviewBillingPeriodRow{TenantID uuid.UUID; TenantName string; Plan string; Timezone string; PeriodMonth pgtype.Date; PeriodFrom time.Time; PeriodTo time.Time; FirstChargeableMonth pgtype.Date; FreePeriod bool; EmployeeCount int32; UnstampedEmployees int32; UnitPrice pgtype.Numeric; AmountDue pgtype.Numeric; PeriodHasEnded bool; PeriodIsAfterSignup bool}, error)",
	"PublishLegalDocument":               "(context.Context, PublishLegalDocumentParams{Slug string; Body string; PublishedBy *uuid.UUID}) (PublishLegalDocumentRow{ID uuid.UUID; Slug string; Body string; PublishedAt time.Time}, error)",
	"RecordAuditEvent":                   "(context.Context, RecordAuditEventParams{TenantID uuid.UUID; ActorID *uuid.UUID; Action string; Target *string; Detail []byte}) (RecordAuditEventRow{ID uuid.UUID; At time.Time}, error)",
	"RenameTenantPolicy":                 "(context.Context, RenameTenantPolicyParams{Name string; TenantID uuid.UUID; ID uuid.UUID}) (RenameTenantPolicyRow{ID uuid.UUID; Name string}, error)",
	"RetireTagForReplacement":            "(context.Context, RetireTagForReplacementParams{ReplacedBy string; TenantID uuid.UUID; Uid string}) (RetireTagForReplacementRow{Uid string; TenantID uuid.UUID; LocationID *uuid.UUID; LastCtr int32; Status string; RetiredAt *time.Time; ReplacedBy *string; CreatedAt time.Time}, error)",
	"RevokeAdminSession":                 "(context.Context, RevokeAdminSessionParams{ID uuid.UUID; TenantID uuid.UUID}) (RevokeAdminSessionRow{ID uuid.UUID; RevokedAt *time.Time}, error)",
	"RevokeAdminSessionsForAdmin":        "(context.Context, RevokeAdminSessionsForAdminParams{TenantID uuid.UUID; AdminUserID uuid.UUID}) ([]uuid.UUID, error)",
	"RevokeSession":                      "(context.Context, RevokeSessionParams{ID uuid.UUID; TenantID uuid.UUID}) (RevokeSessionRow{ID uuid.UUID; RevokedAt *time.Time}, error)",
	"RevokeSessionsForEmployee":          "(context.Context, RevokeSessionsForEmployeeParams{TenantID uuid.UUID; EmployeeID uuid.UUID}) ([]uuid.UUID, error)",
	"SecondsSinceLastRecordedTap":        "(context.Context, SecondsSinceLastRecordedTapParams{TenantID uuid.UUID; EmployeeID *uuid.UUID; WindowSeconds float64}) (float64, error)",
	"SetPolicyEnabled":                   "(context.Context, SetPolicyEnabledParams{Enabled bool; TenantID uuid.UUID; ID uuid.UUID}) (SetPolicyEnabledRow{ID uuid.UUID; Name string; Layer string; Enabled bool}, error)",
	"TenantHasAnyTransaction":            "(context.Context, uuid.UUID) (bool, error)",
	"TouchAdminSession":                  "(context.Context, TouchAdminSessionParams{ID uuid.UUID; TenantID uuid.UUID}) (TouchAdminSessionRow{ID uuid.UUID; LastUsedAt *time.Time; AdminUserID uuid.UUID; Role string; FullName string}, error)",
	"TouchSession":                       "(context.Context, TouchSessionParams{ID uuid.UUID; TenantID uuid.UUID}) (TouchSessionRow{ID uuid.UUID; LastUsedAt *time.Time}, error)",
	"UnmountTagFromWall":                 "(context.Context, UnmountTagFromWallParams{TenantID uuid.UUID; Uid string}) (UnmountTagFromWallRow{Uid string; TenantID uuid.UUID; LocationID *uuid.UUID; LastCtr int32; Status string; RetiredAt *time.Time; ReplacedBy *string; CreatedAt time.Time}, error)",
	"UpdateDepartment":                   "(context.Context, UpdateDepartmentParams{Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; TenantID uuid.UUID; ID uuid.UUID}) (Department{ID uuid.UUID; TenantID uuid.UUID; LocationID uuid.UUID; Name string; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; CreatedAt time.Time}, error)",
	"UpdateLocation":                     "(context.Context, UpdateLocationParams{Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; TenantID uuid.UUID; ID uuid.UUID}) (UpdateLocationRow{ID uuid.UUID; TenantID uuid.UUID; Name string; StaticIps []netip.Prefix; GpsLat pgtype.Numeric; GpsLng pgtype.Numeric; ShiftStart pgtype.Time; ShiftEnd pgtype.Time; Overnight bool; WifiSsid *string; CreatedAt time.Time}, error)",
	"UpdateTenantAccount":                "(context.Context, UpdateTenantAccountParams{Name string; BusinessType string; Timezone string; TenantID uuid.UUID}) (UpdateTenantAccountRow{ID uuid.UUID; Name string; VatNumber string; BusinessType string; Timezone string; VatVerified *bool; VatCheckedAt *time.Time; CreatedAt time.Time}, error)",
	"WithTx":                             "(pgx.Tx) (*Queries)",
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
	if carriers != 5 {
		t.Errorf("%d method(s) return a []byte-carrying struct; FIVE did when this was pinned "+
			"(2026-08-24) -- two policy-document readers and three Transaction readers. A sixth "+
			"is either a new policy document or a §4.7 finding", carriers)
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
	if files != 17 {
		t.Fatalf("read %d .sql file(s) under db/queries; SEVENTEEN were there when this was "+
			"pinned (2026-08-24)", files)
	}
	// A file count alone does not prove the STATEMENT lines were reached; this does.
	// EXACT, by this file's own standard 130 lines down ("a floor with slack cannot
	// notice the thing it exists for"). It was `< 100` against a measured 111 -- eleven
	// queries of silent slack, in the same round that pinned parseStore to != 19.
	if named != 111 {
		t.Fatalf("%d named quer(ies) were seen across %d files; ONE HUNDRED AND ELEVEN were "+
			"there when this was pinned (2026-08-24). Update the number in the same edit that "+
			"adds or removes a query", named, files)
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
	if len(out) != 19 {
		t.Fatalf("parsed %d file(s) under %s; NINETEEN were generated when this floor was set "+
			"(2026-08-24). If sqlc genuinely emits a different number now, change this number "+
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
