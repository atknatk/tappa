package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// TestNew_RefusesAConfigurationThatIsNotTheSandboxs: the simulation is built only for
// TAPPA_ENV=sandbox (which only the sandbox build accepts) and only with every dependency.
func TestNew_RefusesAConfigurationThatIsNotTheSandboxs(t *testing.T) {
	ok := Config{Env: config.EnvSandbox, Data: &db.DB{}, Verifier: nopPreview{}, Taps: nopRecorder{}, Trail: nopTrail{}, KEK: make([]byte, 32)}
	if _, err := New(ok); err != nil {
		t.Fatalf("CONTROL: %v", err)
	}
	for _, env := range []string{config.EnvProd, config.EnvDev, config.EnvStaging, ""} {
		c := ok
		c.Env = env
		if _, err := New(c); !errors.Is(err, ErrNotSandbox) {
			t.Errorf("TAPPA_ENV=%q: %v, want ErrNotSandbox", env, err)
		}
	}
	for name, mutate := range map[string]func(*Config){
		"no database": func(c *Config) { c.Data = nil }, "no pre-check": func(c *Config) { c.Verifier = nil },
		"no recorder": func(c *Config) { c.Taps = nil }, "no trail": func(c *Config) { c.Trail = nil },
		"a short KEK": func(c *Config) { c.KEK = make([]byte, 16) },
	} {
		c := ok
		mutate(&c)
		if _, err := New(c); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}

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

type (
	nopPreview  struct{ Previewer }
	nopRecorder struct{ Recorder }
	nopTrail    struct{ Trail }
)
