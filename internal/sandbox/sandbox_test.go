package sandbox

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/sun"
)

// sandboxApp is a sandbox configuration New accepts: TAPPA_ENV=sandbox, a 32-byte KEK and the tap
// recorder's windows.
func sandboxApp() *config.Config {
	return &config.Config{Env: config.EnvSandbox, TagKEK: make([]byte, 32), GPSRadiusMeters: 150,
		Debounce: 60 * time.Second, Freshness: 180 * time.Second}
}

// TestNew_RefusesAConfigurationThatIsNotTheSandboxs: the simulation is built only for
// TAPPA_ENV=sandbox (which only the sandbox build accepts) and only with every dependency.
func TestNew_RefusesAConfigurationThatIsNotTheSandboxs(t *testing.T) {
	ok := Config{App: sandboxApp(), Data: &db.DB{}, Work: nopWork{}}
	if _, err := New(ok); err != nil {
		t.Fatalf("CONTROL: %v", err)
	}
	for _, env := range []string{config.EnvProd, config.EnvDev, config.EnvStaging, ""} {
		c := ok
		app := *sandboxApp()
		app.Env = env
		c.App = &app
		if _, err := New(c); !errors.Is(err, ErrNotSandbox) {
			t.Errorf("TAPPA_ENV=%q: %v, want ErrNotSandbox", env, err)
		}
	}
	for name, mutate := range map[string]func(*Config){
		"no configuration": func(c *Config) { c.App = nil }, "no database": func(c *Config) { c.Data = nil },
		"no work set":         func(c *Config) { c.Work = nil },
		"a short KEK":         func(c *Config) { app := *sandboxApp(); app.TagKEK = make([]byte, 16); c.App = &app },
		"no freshness window": func(c *Config) { app := *sandboxApp(); app.Freshness = 0; c.App = &app },
	} {
		c := ok
		mutate(&c)
		if _, err := New(c); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}

// TestNew_TheTapPathIsTheTapPagesCodeOnTheRunner (M11 API-11 fix 1): New builds the SUN pre-check,
// the tap recorder and the trail with the tap page's own constructors -- a *sun.Verifier, a
// *checkin.Service, an *audit.Recorder -- and the chip with sun.MintTapPath; and the database they
// run on is the runner's transaction, never the pool: a call without one is errOffTheRunner, and the
// pool double here fails the test if it is ever asked.
//
// MUTANT (measured): onRunner's two methods falling back to the pool when the context carries no
// runner transaction (the nested connection the fix removed) -- red here, and
// TestSandbox_SimulationsHoldOneConnectionEach red against a real pool.
func TestNew_TheTapPathIsTheTapPagesCodeOnTheRunner(t *testing.T) {
	s, err := New(Config{App: sandboxApp(), Data: poolDouble{t}, Work: nopWork{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.verifier.(*sun.Verifier); !ok {
		t.Errorf("the pre-check is a %T, want the tap page's *sun.Verifier", s.verifier)
	}
	if _, ok := s.taps.(*checkin.Service); !ok {
		t.Errorf("the recorder is a %T, want the tap page's *checkin.Service", s.taps)
	}
	if _, ok := s.trail.(*audit.Recorder); !ok {
		t.Errorf("the trail is a %T, want an *audit.Recorder", s.trail)
	}
	if reflect.ValueOf(s.mint).Pointer() != reflect.ValueOf(sun.MintTapPath).Pointer() {
		t.Error("the chip is not sun.MintTapPath")
	}
	route := onRunner{data: poolDouble{t}}
	ctx := context.Background()
	if _, err := route.GetTagByUID(ctx, VirtualUID(uuid.New())); !errors.Is(err, errOffTheRunner) {
		t.Errorf("a resolve without the runner's transaction: %v, want errOffTheRunner", err)
	}
	if err := route.WithTenant(ctx, uuid.New(), func(context.Context, pgx.Tx) error { return nil }); !errors.Is(err, errOffTheRunner) {
		t.Errorf("a transaction without the runner's: %v, want errOffTheRunner", err)
	}
	if _, err := s.verifier.PreviewWithoutReplayProtection(ctx, sun.Params{UID: VirtualUID(uuid.New()), Channel: sun.ChannelQR}); err == nil {
		t.Error("the pre-check answered without the runner's transaction")
	}
}

// poolDouble is a Database whose pool fails the test when asked.
type poolDouble struct{ t *testing.T }

func (p poolDouble) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	p.t.Error("the pool was asked for a transaction")
	return errors.New("never")
}

func (p poolDouble) GetTagByUIDOn(context.Context, pgx.Tx, string) (db.ResolvedTag, error) {
	p.t.Error("a resolve reached the database without the runner's transaction")
	return db.ResolvedTag{}, errors.New("never")
}

func (poolDouble) AdmitTagUID(string) error { return nil }

type nopWork struct{}

func (nopWork) Notice(uuid.UUID) {}

// TestKeys_EachBuildRefusesTheOthersPrefixBeforeTheDatabase (plan §5 API-11: "bir tt_test_ anahtarı
// prod'da biçim kontrolünde (DB'ye gitmeden) 401, bir tt_live_ anahtarı sandbox'ta 401"): the chain
// built with one build's prefix refuses a well-shaped key of the other's at Check -- the step before
// the address bucket and the resolver -- and the resolver here fails the test if it is ever asked.
func TestKeys_EachBuildRefusesTheOthersPrefixBeforeTheDatabase(t *testing.T) {
	mac := make([]byte, 32)
	for _, tc := range []struct{ build, other string }{{apikey.LivePrefix, apikey.SandboxPrefix}, {apikey.SandboxPrefix, apikey.LivePrefix}} {
		chain, err := apikey.NewAuthenticator(tc.build, mac, neverResolver{t}, neverDB{t})
		if err != nil {
			t.Fatal(err)
		}
		tok, err := apikey.ParseBearer("Bearer " + tc.other + strings.Repeat("A", 43))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := chain.Check(tok); !errors.Is(err, apikey.ErrMalformed) {
			t.Errorf("a %s chain checked a %s key: %v, want ErrMalformed", tc.build, tc.other, err)
		}
		own, err := apikey.ParseBearer("Bearer " + tc.build + strings.Repeat("A", 43))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := chain.Check(own); err != nil {
			t.Errorf("CONTROL: a %s chain refused its own shape: %v", tc.build, err)
		}
	}
}

type neverResolver struct{ t *testing.T }

func (n neverResolver) GetAPIKeyByHash(context.Context, string) (db.ResolvedAPIKey, error) {
	n.t.Error("the resolver was asked")
	return db.ResolvedAPIKey{}, errors.New("never")
}

type neverDB struct{ t *testing.T }

func (n neverDB) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	n.t.Error("the database was asked")
	return errors.New("never")
}

// TestPlaqueAt_HoldsTheResolvedRowToTheVenue (API-11 fix 1, the third eye's finding 3): the plaque a
// simulation taps is the row the resolver answers for the venue's virtual UID ONLY when that row is
// the venue's own business's plaque on that venue's wall. A row of another business at the UID --
// the five-byte collision, or a UID taken first -- and a row of the business elsewhere or in stock
// are "no plaque here" (pgx.ErrNoRows, the 404), never a plaque to touch. In the database a plaque
// on the venue's wall IS the venue's business's (tags_location_fk is (location_id, tenant_id)), so
// the tenant half is the belt; this pins it, because no row the schema admits can.
//
// MUTANTS (measured): the tenant comparison removed; the wall comparison removed -- each red.
func TestPlaqueAt_HoldsTheResolvedRowToTheVenue(t *testing.T) {
	site, other, venue, elsewhere := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		name   string
		row    db.ResolvedTag
		refuse bool
	}{
		{"the venue's plaque (CONTROL)", db.ResolvedTag{TenantID: site, LocationID: &venue}, false},
		{"another business's plaque on the venue's wall", db.ResolvedTag{TenantID: other, LocationID: &venue}, true},
		{"another business's plaque elsewhere", db.ResolvedTag{TenantID: other, LocationID: &elsewhere}, true},
		{"the business's plaque on another wall", db.ResolvedTag{TenantID: site, LocationID: &elsewhere}, true},
		{"the business's plaque in stock", db.ResolvedTag{TenantID: site}, true},
	} {
		s := &Simulation{data: resolvesTo{row: tc.row}}
		_, err := s.plaqueAt(context.Background(), nil, site, venue)
		if tc.refuse != errors.Is(err, pgx.ErrNoRows) || (!tc.refuse && err != nil) {
			t.Errorf("%s: %v, want refused %v", tc.name, err, tc.refuse)
		}
	}
}

// resolvesTo is a Database whose resolver answers row for every UID.
type resolvesTo struct {
	Database
	row db.ResolvedTag
}

func (r resolvesTo) GetTagByUIDOn(context.Context, pgx.Tx, string) (db.ResolvedTag, error) {
	return r.row, nil
}
