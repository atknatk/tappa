package config_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// loadErrorVariables is every variable internal/config names, and whether a canary value is REFUSED
// by the rule that reads it (a refused variable's error must name it, or the row reached nothing).
// TestLoad_ACanaryValueReachesNoErrorText compares this list with the package's source in both
// directions, so a new variable is a red test until it has a row here.
var loadErrorVariables = map[string]bool{
	"DATABASE_URL":                    false,
	"DATABASE_MIGRATE_URL":            false,
	"TAPPA_ENV":                       true,
	"TAPPA_ADDR":                      false,
	"TAPPA_BASE_URL":                  true,
	"TAPPA_LOG_LEVEL":                 false,
	"TAPPA_LOG_FORMAT":                true,
	"TAPPA_DEV_TOOLS":                 true,
	"TAPPA_SESSION_HMAC_KEY":          true,
	"TAPPA_TAG_KEK":                   true,
	"TAPPA_TAG_KEK_PREVIOUS":          true,
	"TAPPA_INVITE_HMAC_KEY":           true,
	"TAPPA_API_KEY_HMAC_KEY":          true,
	"TAPPA_WEBHOOK_KEK":               true,
	"TAPPA_WEBHOOK_BLOCKED_ADDRESSES": true,
	"TAPPA_OPERATOR_DATABASE_URL":     false,
	"TAPPA_OPERATOR_TOTP_KEK":         true,
	"TAPPA_OPERATOR_TOKEN_HMAC_KEY":   true,
	"TAPPA_OPERATOR_HOST":             true,
	"TAPPA_API_HOST":                  true,
	"TAPPA_RETENTION_YEARS":           true,
	"TAPPA_TRUSTED_PROXIES":           true,
	"TAPPA_RESET_DELIVERY":            true,
	"TAPPA_INVITE_DELIVERY":           true,
	"TAPPA_SMTP_HOST":                 true,
	"TAPPA_SMTP_PORT":                 true,
	"TAPPA_SMTP_USERNAME":             false,
	"TAPPA_SMTP_PASSWORD":             false,
	"TAPPA_MAIL_FROM":                 true,
	"TAPPA_MAIL_REPLY_TO":             true,
	"TAPPA_GPS_RADIUS_M":              true,
	"TAPPA_DEBOUNCE_SECONDS":          true,
	"TAPPA_FRESHNESS_SECONDS":         true,
	"TAPPA_BUSINESS_DAY_FROM":         true,
	"TAPPA_ADMIN_IDENTITY":            true,
}

// Two fixture values, built into their URLs below so no line of this file is a URL with a credential in
// it: scripts/secretscan.sh cannot tell a fixture from a paste.
const (
	dsnCanary = "Canary-Pw-71"
	urlCanary = "Canary-Pw-72"
)

// loadCanaries are the shapes a wrong paste takes, each with the texts that would betray it: a parsed
// number comes back in %v's own spelling, so a canary's needles are not always the value itself.
var loadCanaries = []struct {
	value   string
	needles []string
}{
	{"postgres://tappa_app:" + dsnCanary + "@db.invalid:5432/tappa?password=" + dsnCanary, []string{dsnCanary, "db.invalid"}},
	{"CanaryWord", []string{"CanaryWord"}},
	{"  CanarySpaced  ", []string{"CanarySpaced"}},
	{"Q0FOQVJZa2V5", []string{"Q0FOQVJZa2V5"}},
	{"7777777", []string{"7777777", "7.777777e+06"}},
	{"2099-03-04T05:06:07Z", []string{"2099-03-04"}},
	{"2001-03-04T05:06:07Z", []string{"2001-03-04"}},
	{"::ffff:10.9.8.0/120", []string{"10.9.8"}},
	{"203.0.113.77/0", []string{"203.0.113.77"}},
	{"canaryhost.example", []string{"canaryhost"}},
	{"canary@mailbox.invalid", []string{"canary@", "mailbox.invalid"}},
	{"https://canaryuser:" + urlCanary + "@canaryurl.test/", []string{"canaryuser", urlCanary, "canaryurl"}},
}

// loadContexts are the modes in which the variables are read at all: the transport's settings only when a
// flow sends e-mail, the operator surface's only when one of the four is set, and the production
// rules only with TAPPA_ENV=prod.
func loadContexts() []struct {
	name string
	set  func(t *testing.T)
} {
	contexts := []struct {
		name string
		set  func(t *testing.T)
	}{
		{"base", func(*testing.T) {}},
		{"email on", func(t *testing.T) {
			setMail(t)
			t.Setenv("TAPPA_RESET_DELIVERY", config.ResetDeliveryEmail)
			t.Setenv("TAPPA_INVITE_DELIVERY", config.InviteDeliveryEmail)
		}},
		{"operator surface on", func(t *testing.T) { setOperator(t) }},
	}
	if !config.SandboxBuild() {
		contexts = append(contexts, struct {
			name string
			set  func(t *testing.T)
		}{"production", func(t *testing.T) { t.Setenv("TAPPA_ENV", config.EnvProd) }})
	}
	return contexts
}

var loadVariableName = regexp.MustCompile(`\b(?:TAPPA|DATABASE)_[A-Z0-9_]*[A-Z0-9]\b`)

// variablesNamedInSource is every TAPPA_* and DATABASE_* this package's non-test source spells, in any
// string literal: a getenv's argument, a const's value, or an error's text. Comments do not count.
func variablesNamedInSource(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("%s: %v", fset.Position(lit.Pos()), err)
			}
			for _, name := range loadVariableName.FindAllString(text, -1) {
				seen[name] = true
			}
			return true
		})
	}
	var names []string
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// TestLoad_EveryVariableIsInTheCanaryTable is the inventory: the table above and the source name the
// same variables, so a new one cannot miss the canaries below.
//
// MUTANT (measured, 2026-11-06, reverted): `_ = os.Getenv("TAPPA_NEW_THING")` in Load -- red here;
// a table row with no variable behind it -- red here.
func TestLoad_EveryVariableIsInTheCanaryTable(t *testing.T) {
	named := variablesNamedInSource(t)
	if len(named) < 30 {
		t.Fatalf("the scan found %d variables (%v); it has gone blind", len(named), named)
	}
	for _, name := range named {
		if _, ok := loadErrorVariables[name]; !ok {
			t.Errorf("%s is named in internal/config and has no row in loadErrorVariables: a canary never reaches it (backlog T206)", name)
		}
	}
	for name := range loadErrorVariables {
		if !slices.Contains(named, name) {
			t.Errorf("loadErrorVariables has %s, which internal/config no longer names", name)
		}
	}
}

// loadWithLog runs Load and returns its error's text beside everything Load logged: a boot log line
// is the same place an error's text ends up.
func loadWithLog(t *testing.T) (errText, logText string) {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)
	if _, err := config.Load(); err != nil {
		errText = err.Error()
	}
	return errText, buf.String()
}

func checkNoNeedle(t *testing.T, what, name, errText, logText string, needles []string) {
	t.Helper()
	for _, needle := range needles {
		if strings.Contains(errText, needle) {
			t.Errorf("%s: %s: the error's text repeats the value (needle %q): %s", what, name, needle, errText)
		}
		if strings.Contains(logText, needle) {
			t.Errorf("%s: %s: Load's log repeats the value (needle %q): %s", what, name, needle, logText)
		}
	}
}

// TestLoad_ACanaryValueReachesNoErrorText (backlog T206): a value pasted into the wrong variable can be
// a connection string, and Load's error goes to the boot log. Every variable, every shape of value, every
// mode that reads it: no error and no log line repeats it.
//
// CONTROL: a variable marked refused has to be named by an error in at least one of its rows, so a
// row that reached no rule cannot pass as a row that repeated nothing.
//
// MUTANTS (measured, 2026-11-06, reverted): each of the sixteen sites that repeated a value, restored one
// at a time, is red: fourteen here, the dev-tools rule's in TestLoad_EveryVariableAtOnceRepeatsNothing,
// the sandbox build's in TestEnvForBuild_RepeatsNoValue.
func TestLoad_ACanaryValueReachesNoErrorText(t *testing.T) {
	contexts := loadContexts()
	for name, refused := range loadErrorVariables {
		t.Run(name, func(t *testing.T) {
			named := false
			for _, ctx := range contexts {
				for _, canary := range loadCanaries {
					t.Run(ctx.name, func(t *testing.T) {
						setRequired(t)
						ctx.set(t)
						t.Setenv(name, canary.value)
						errText, logText := loadWithLog(t)
						checkNoNeedle(t, ctx.name, name, errText, logText, canary.needles)
						if strings.Contains(errText, name) {
							named = true
						}
					})
				}
			}
			if refused && !named {
				t.Errorf("no canary was refused under %s's name, so its rule was never reached", name)
			}
		})
	}
}

// TestLoad_EveryVariableAtOnceRepeatsNothing is the row a one-at-a-time table cannot be: rules that read
// two variables (TAPPA_DEV_TOOLS=1 prints nothing of TAPPA_ENV any more), and every message Load joins.
func TestLoad_EveryVariableAtOnceRepeatsNothing(t *testing.T) {
	for _, canary := range loadCanaries {
		setRequired(t)
		for name := range loadErrorVariables {
			t.Setenv(name, canary.value)
		}
		errText, logText := loadWithLog(t)
		if errText == "" {
			t.Fatalf("CONTROL: every variable set to %d chars loaded cleanly", len(canary.value))
		}
		checkNoNeedle(t, "every variable", "joined", errText, logText, canary.needles)
	}

	setRequired(t)
	t.Setenv("TAPPA_ENV", "CanaryWord")
	t.Setenv("TAPPA_DEV_TOOLS", "1")
	errText, logText := loadWithLog(t)
	if !strings.Contains(errText, "TAPPA_DEV_TOOLS=1 is only allowed") {
		t.Fatalf("CONTROL: the dev-tools rule did not run: %s", errText)
	}
	checkNoNeedle(t, "dev tools beside a wrong environment", "TAPPA_ENV", errText, logText, []string{"CanaryWord"})
}
