// Package fixtures provides stable, typed identifiers for the demo/test seed
// data in test/fixtures/seed.sql. Tests reference these named handles instead of
// magic UUID / hex strings, so a change to the seed becomes a compile-time
// mismatch rather than a silent test drift.
//
// seed.sql is the source of truth for what actually exists in the database; this
// file is the Go-side mirror and MUST stay in exact sync with it. Every KF id
// begins with 1..., every KM id with 2... -- the two tenants share no row.
package fixtures

import "github.com/google/uuid"

// Tenants.
var (
	// TenantKF is Kebab Factory Ltd. -- multi-location, 9 locations, no departments.
	TenantKF = uuid.MustParse("10000000-0000-4000-8000-000000000001")
	// TenantKM is Kebab Manufacturing Co. Ltd. -- single-location, 5 departments.
	TenantKM = uuid.MustParse("20000000-0000-4000-8000-000000000001")
)

// KF locations (9). Shifts are Malta local wall-clock; see seed.sql.
var (
	LocKFHamrun      = uuid.MustParse("10000000-0000-4000-8000-000000000101") // 10:00-22:00
	LocKFMellieha    = uuid.MustParse("10000000-0000-4000-8000-000000000102") // 10:00-22:00
	LocKFMsida       = uuid.MustParse("10000000-0000-4000-8000-000000000103") // 10:00-22:00
	LocKFValletta    = uuid.MustParse("10000000-0000-4000-8000-000000000104") // 10:00-22:00
	LocKFSanGwann    = uuid.MustParse("10000000-0000-4000-8000-000000000105") // 10:00-22:00
	LocKFStJulians   = uuid.MustParse("10000000-0000-4000-8000-000000000106") // 10:00-22:00, reference crew
	LocKFPaceville   = uuid.MustParse("10000000-0000-4000-8000-000000000107") // 11:00-23:00
	LocKFRustyBar    = uuid.MustParse("10000000-0000-4000-8000-000000000108") // 18:00-02:00, overnight
	LocKFHeadquarter = uuid.MustParse("10000000-0000-4000-8000-000000000109") // 09:00-17:00
)

// KM location (single facility).
var (
	LocKMPlant = uuid.MustParse("20000000-0000-4000-8000-000000000101") // 09:00-17:00 baseline
)

// KM departments (5). A filled department shift overrides the location baseline.
var (
	DeptKMGeneral        = uuid.MustParse("20000000-0000-4000-8000-000000000201") // 09:00-17:00
	DeptKMMeat           = uuid.MustParse("20000000-0000-4000-8000-000000000202") // 05:00-13:00
	DeptKMWarehouse      = uuid.MustParse("20000000-0000-4000-8000-000000000203") // 08:00-16:00
	DeptKMCentralKitchen = uuid.MustParse("20000000-0000-4000-8000-000000000204") // 06:00-14:00
	DeptKMPastry         = uuid.MustParse("20000000-0000-4000-8000-000000000205") // 04:00-12:00
)

// KF employees (25). St Julians ...301-...315 is the reference crew: 13 active,
// EmpKFGraceAttard is invited-only (practice-tap candidate), EmpKFPaulSpiteri is
// deactivated (section 5 row 4). EmpKFMarcoBugeja has a NULL email (physical invite).
var (
	EmpKFMariaBorg       = uuid.MustParse("10000000-0000-4000-8000-000000000301") // St Julians, Manager
	EmpKFJosephCamilleri = uuid.MustParse("10000000-0000-4000-8000-000000000302") // St Julians
	EmpKFAntoineVella    = uuid.MustParse("10000000-0000-4000-8000-000000000303") // St Julians
	EmpKFRitaZammit      = uuid.MustParse("10000000-0000-4000-8000-000000000304") // St Julians
	EmpKFDavidGrech      = uuid.MustParse("10000000-0000-4000-8000-000000000305") // St Julians
	EmpKFSofiaRossi      = uuid.MustParse("10000000-0000-4000-8000-000000000306") // St Julians
	EmpKFAhmedHassan     = uuid.MustParse("10000000-0000-4000-8000-000000000307") // St Julians
	EmpKFElenaPetrova    = uuid.MustParse("10000000-0000-4000-8000-000000000308") // St Julians
	EmpKFMarcoBugeja     = uuid.MustParse("10000000-0000-4000-8000-000000000309") // St Julians, NULL email
	EmpKFNadiaFarrugia   = uuid.MustParse("10000000-0000-4000-8000-000000000310") // St Julians
	EmpKFLiamOBrien      = uuid.MustParse("10000000-0000-4000-8000-000000000311") // St Julians
	EmpKFPriyaSharma     = uuid.MustParse("10000000-0000-4000-8000-000000000312") // St Julians
	EmpKFCarlosMendez    = uuid.MustParse("10000000-0000-4000-8000-000000000313") // St Julians
	EmpKFGraceAttard     = uuid.MustParse("10000000-0000-4000-8000-000000000314") // St Julians, invited
	EmpKFPaulSpiteri     = uuid.MustParse("10000000-0000-4000-8000-000000000315") // St Julians, deactivated
	EmpKFAndreaMicallef  = uuid.MustParse("10000000-0000-4000-8000-000000000316") // Rusty Bar, night shift
	EmpKFRobertaFenech   = uuid.MustParse("10000000-0000-4000-8000-000000000317") // Rusty Bar, night shift
	EmpKFIvanPetrov      = uuid.MustParse("10000000-0000-4000-8000-000000000318") // Rusty Bar, night shift
	EmpKFEmmaCaruana     = uuid.MustParse("10000000-0000-4000-8000-000000000319") // Hamrun
	EmpKFJuliaSciberras  = uuid.MustParse("10000000-0000-4000-8000-000000000320") // Mellieha
	EmpKFOmarAziz        = uuid.MustParse("10000000-0000-4000-8000-000000000321") // Msida
	EmpKFGeorgeMuscat    = uuid.MustParse("10000000-0000-4000-8000-000000000322") // Valletta
	EmpKFKevinBorg       = uuid.MustParse("10000000-0000-4000-8000-000000000323") // San Gwann
	EmpKFRyanGauci       = uuid.MustParse("10000000-0000-4000-8000-000000000324") // Paceville
	EmpKFStephaniePace   = uuid.MustParse("10000000-0000-4000-8000-000000000325") // Headquarter
)

// KM employees (11). All active. EmpKMJosephButtigieg has a NULL department and
// so resolves its shift from the KM Plant location baseline (section 5 own-shift split).
var (
	EmpKMAntoniaGrima    = uuid.MustParse("20000000-0000-4000-8000-000000000301") // General
	EmpKMFrankDebono     = uuid.MustParse("20000000-0000-4000-8000-000000000302") // General
	EmpKMSaviourBonnici  = uuid.MustParse("20000000-0000-4000-8000-000000000303") // Meat Production
	EmpKMDraganNikolic   = uuid.MustParse("20000000-0000-4000-8000-000000000304") // Meat Production
	EmpKMCharleneVella   = uuid.MustParse("20000000-0000-4000-8000-000000000305") // Warehouse & Supply
	EmpKMPeterFalzon     = uuid.MustParse("20000000-0000-4000-8000-000000000306") // Warehouse & Supply
	EmpKMRosariaSchembri = uuid.MustParse("20000000-0000-4000-8000-000000000307") // Central Kitchen
	EmpKMYusufDemir      = uuid.MustParse("20000000-0000-4000-8000-000000000308") // Central Kitchen
	EmpKMDorisMicallef   = uuid.MustParse("20000000-0000-4000-8000-000000000309") // Pastry & Bakery
	EmpKMAnaSilva        = uuid.MustParse("20000000-0000-4000-8000-000000000310") // Pastry & Bakery
	EmpKMJosephButtigieg = uuid.MustParse("20000000-0000-4000-8000-000000000311") // NULL department
)

// Tag UIDs (14-hex, char(14) in the DB). One active plaque per KF location plus
// the KM facility; TagKFStJuliansOld is retired with replaced_by = TagKFStJulians
// (the "Replace tag" audit chain), and TagKFHamrunLost is a lost plaque. These
// are text keys, so they are plain string constants.
const (
	TagKFHamrun       = "04AC7E55000101"
	TagKFHamrunLost   = "04AC7E550001FF" // status = lost
	TagKFMellieha     = "04AC7E55000201"
	TagKFMsida        = "04AC7E55000301"
	TagKFValletta     = "04AC7E55000401"
	TagKFSanGwann     = "04AC7E55000501"
	TagKFStJulians    = "04AC7E55000601"
	TagKFStJuliansOld = "04AC7E550006AA" // status = retired, replaced_by TagKFStJulians
	TagKFPaceville    = "04AC7E55000701"
	TagKFRustyBar     = "04AC7E55000801"
	TagKFHeadquarter  = "04AC7E55000901"
	TagKMPlant        = "04BE7E55000A01"
)

// Admin panel owners (one per tenant, M1-11). role='owner'. These are the panel
// identity, kept separate from employees (an employee never sees a password). The
// ...4xx range is the admin block (employees use ...3xx). The dev-only login
// password is AdminDevPassword below; its bcrypt hash lives in seed.sql.
var (
	AdminKFOwner = uuid.MustParse("10000000-0000-4000-8000-000000000401") // owner@kebabfactory.mt
	AdminKMOwner = uuid.MustParse("20000000-0000-4000-8000-000000000401") // owner@kebabmfg.mt
)

// AdminDevPassword is the DEV-ONLY plaintext whose bcrypt $2a$ hash seeds both
// owners above. It is NOT a real secret -- documented on purpose, same principle
// as the fake tag aes_key_ref: the demo panel login must work, but no real
// customer credential ever lives in the repo. Production owners (M8-07) never use
// it. Named here so a future admin-auth test can log in without a magic string.
const AdminDevPassword = "tappa-dev-only-changeme"
