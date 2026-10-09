package config_test

// mail_test.go — M10 EM-3: the transactional e-mail settings (ADR 0022 §5). Two
// delivery modes, each a closed set; the transport's settings read ONLY when a flow is
// "email", and then fail-closed; every refusal names the variable and never the value;
// the sender rules are internal/mail's, asked rather than copied.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/mail"
)

// The values a valid transport configuration is built from. The two credentials are
// SENTINELS: obviously fake, and built so that NO three consecutive characters of either
// form an English fragment (consonant runs and digits — no "ent", "sen", "ion"), which is
// what lets noCredential search every 3-byte window of them in a message without matching
// the message's own words. Lower case and digits only: two character classes, below R7d's
// a0-token threshold.
const (
	shippedHost  = "email-smtp.eu-central-1.amazonaws.com"
	shippedFrom  = "Taptime <no-reply@taptime.mt>"
	sentinelUser = "jkq4wvz8xqp2zkv6"
	sentinelPass = "qzx7vjw9kpq3xzt8"
)

// credentialVariables is internal/config's own list of the two credential variables,
// checked once by name so the rest of this file never has to spell them.
func credentialVariables(t *testing.T) (user, pass string) {
	t.Helper()
	v := config.SMTPCredentialVariables()
	if !slices.Equal(v, []string{"TAPPA_SMTP_USERNAME", "TAPPA_SMTP_PASSWORD"}) {
		t.Fatalf("SMTPCredentialVariables() = %v, want the two credential variables of ADR 0022 §5", v)
	}
	return v[0], v[1]
}

// mailVariables is every variable of the transactional e-mail settings.
func mailVariables() []string {
	return append([]string{"TAPPA_RESET_DELIVERY", "TAPPA_INVITE_DELIVERY", "TAPPA_SMTP_HOST", "TAPPA_SMTP_PORT",
		"TAPPA_MAIL_FROM", "TAPPA_MAIL_REPLY_TO"}, config.SMTPCredentialVariables()...)
}

// setMail sets every transport setting to a valid value and leaves the two flows alone.
func setMail(t *testing.T) {
	t.Helper()
	user, pass := credentialVariables(t)
	t.Setenv("TAPPA_SMTP_HOST", shippedHost)
	t.Setenv("TAPPA_SMTP_PORT", "587")
	t.Setenv(user, sentinelUser)
	t.Setenv(pass, sentinelPass)
	t.Setenv("TAPPA_MAIL_FROM", shippedFrom)
	t.Setenv("TAPPA_MAIL_REPLY_TO", "")
}

// revealForTest reads a credential the one way internal/mail says it can be read —
// deliberate reflection (TestCredential_KnownLimitIsReflection) — so a test can tell
// the username from the password without the type offering a way to.
func revealForTest(t *testing.T, c mail.Credential) string {
	t.Helper()
	v := reflect.ValueOf(c)
	if v.NumField() != 1 || v.Field(0).Kind() != reflect.Pointer || v.Field(0).IsNil() {
		t.Fatal("PREMISE: mail.Credential is no longer one non-nil *string field; this reader has to change")
	}
	return v.Field(0).Elem().String()
}

// noValue fails when an error repeats any of the values, or a PREFIX of one: its first 8,
// 4 or 3 characters. 8 is the length internal/mail's own redaction tests search for; 4 and
// 3 are this file's (round 2, the third eye's X07: a refusal that leaked the first three
// bytes of the password passed an 8-character search). It is a PREFIX search and only
// that: the suffix or an inner part of a value is not looked for (the closing auditor's
// N12, N12b) — for the two credentials, noCredential does that. The refused non-credential
// values searched for here start with "qq" or "Qq", which no message of this package
// contains.
func noValue(t *testing.T, err error, values ...string) {
	t.Helper()
	for _, v := range values {
		if v == "" {
			continue
		}
		for _, frag := range []string{v, v[:min(8, len(v))], v[:min(4, len(v))], v[:min(3, len(v))]} {
			if strings.Contains(err.Error(), frag) {
				t.Errorf("the refusal repeats a value it was given (%d bytes long): %v", len(v), err)
			}
		}
	}
}

// noCredential fails when an error carries ANY 3-byte window of a credential — its
// prefix, its suffix (the closing auditor's N12), any inner part (N12b). It is meant for
// the two sentinels only, which are built for it (see the const block); a value with
// ordinary words in it would match the message's own.
func noCredential(t *testing.T, err error, creds ...string) {
	t.Helper()
	for _, c := range creds {
		for i := 0; i+3 <= len(c); i++ {
			if strings.Contains(err.Error(), c[i:i+3]) {
				t.Errorf("the refusal carries 3 bytes of a credential (offset %d of %d): %v", i, len(c), err)
				break
			}
		}
	}
}

// TestLoad_InviteDeliveryIsAClosedSetAndFailsClosed: TAPPA_INVITE_DELIVERY is {panel,
// email}, empty is panel, anything else is a startup failure that names the set and the
// ADR and does not repeat the value.
func TestLoad_InviteDeliveryIsAClosedSetAndFailsClosed(t *testing.T) {
	setRequired(t)
	c, err := config.Load()
	if err != nil {
		t.Fatalf("an unset TAPPA_INVITE_DELIVERY must load: %v", err)
	}
	if c.InviteDelivery != config.InviteDeliveryPanel {
		t.Errorf("InviteDelivery = %q, want %q", c.InviteDelivery, config.InviteDeliveryPanel)
	}

	setMail(t)
	for spelling, want := range map[string]string{
		"panel": config.InviteDeliveryPanel, "PANEL": config.InviteDeliveryPanel, " panel ": config.InviteDeliveryPanel,
		"email": config.InviteDeliveryEmail, "Email": config.InviteDeliveryEmail, "\temail\n": config.InviteDeliveryEmail,
	} {
		t.Setenv("TAPPA_INVITE_DELIVERY", spelling)
		c, err := config.Load()
		if err != nil {
			t.Fatalf("TAPPA_INVITE_DELIVERY=%q must load: %v", spelling, err)
		}
		if c.InviteDelivery != want {
			t.Errorf("TAPPA_INVITE_DELIVERY=%q gave %q, want %q", spelling, c.InviteDelivery, want)
		}
	}

	for _, bad := range []string{"none", "manager", "smtp", "ses", "mail", "e-mail", "true", "qq-delivery-sentinel"} {
		t.Setenv("TAPPA_INVITE_DELIVERY", bad)
		_, err := config.Load()
		if err == nil {
			t.Errorf("TAPPA_INVITE_DELIVERY=%q loaded; an unknown channel must stop the boot, not fall back to one", bad)
			continue
		}
		for _, must := range []string{"TAPPA_INVITE_DELIVERY", `"panel"`, `"email"`, "ADR 0022"} {
			if !strings.Contains(err.Error(), must) {
				t.Errorf("TAPPA_INVITE_DELIVERY=%q: the error does not say %s: %v", bad, must, err)
			}
		}
		if bad == "qq-delivery-sentinel" {
			noValue(t, err, bad)
		}
	}
}

// TestLoad_MailSettingsFailClosedWhenAFlowIsEmail is the fail-closed matrix (ADR 0022
// §5, the EM-3 acceptance): for each way of turning e-mail on — the reset flow, the
// invitation flow, both — and each REQUIRED setting, three ways of missing are each their
// own case: UNSET (really absent from the environment, as a missing `optional: true`
// Secret key leaves it), EMPTY (set to "") and BLANK (spaces). Each must stop the boot
// with an error that names that variable and the flow that requires it, and names no
// other required setting as missing.
//
// The port is not in the matrix because it has a default (587; its rules are
// TestLoad_SMTPPortRefusesImplicitTLSEverywhere), and Reply-To because it is optional.
//
// CONTROL: in every flow, the complete configuration loads and fills Mail.
func TestLoad_MailSettingsFailClosedWhenAFlowIsEmail(t *testing.T) {
	user, pass := credentialVariables(t)
	required := []string{"TAPPA_SMTP_HOST", user, pass, "TAPPA_MAIL_FROM"}
	flows := []struct {
		name, reset, invite string
		because             []string // the flow variables the refusal must name
	}{
		{"the reset flow is email", "email", "panel", []string{"TAPPA_RESET_DELIVERY"}},
		{"the invitation flow is email", "none", "email", []string{"TAPPA_INVITE_DELIVERY"}},
		{"both flows are email", "email", "email", []string{"TAPPA_RESET_DELIVERY", "TAPPA_INVITE_DELIVERY"}},
	}
	for _, fl := range flows {
		t.Run(fl.name+"/control: complete", func(t *testing.T) {
			setRequired(t)
			setMail(t)
			t.Setenv("TAPPA_RESET_DELIVERY", fl.reset)
			t.Setenv("TAPPA_INVITE_DELIVERY", fl.invite)
			c, err := config.Load()
			if err != nil {
				t.Fatalf("a complete transport configuration must load: %v", err)
			}
			if c.Mail.Host != shippedHost {
				t.Fatalf("Mail was not filled: host %q", c.Mail.Host)
			}
		})
		for _, key := range required {
			for _, missing := range []struct {
				name, value string
				unset       bool
			}{{"unset", "", true}, {"empty", "", false}, {"blank", "   ", false}} {
				t.Run(fl.name+"/"+key+" "+missing.name, func(t *testing.T) {
					setRequired(t)
					setMail(t)
					t.Setenv("TAPPA_RESET_DELIVERY", fl.reset)
					t.Setenv("TAPPA_INVITE_DELIVERY", fl.invite)
					t.Setenv(key, missing.value)
					if missing.unset {
						// Really absent, as a Secret key that is not there leaves it
						// (`optional: true`): t.Setenv above registered the restore.
						if err := os.Unsetenv(key); err != nil {
							t.Fatal(err)
						}
						if _, present := os.LookupEnv(key); present {
							t.Fatalf("PREMISE: %s is still in the environment", key)
						}
					}
					c, err := config.Load()
					if err == nil {
						t.Fatalf("%s %s while e-mail is on: the process started (Mail.Host %q) and would fail on its "+
							"first send instead", key, missing.name, c.Mail.Host)
					}
					if !strings.Contains(err.Error(), key+" is required") {
						t.Errorf("the refusal does not name %s as the missing setting: %v", key, err)
					}
					for _, flowVar := range fl.because {
						if !strings.Contains(err.Error(), flowVar) {
							t.Errorf("the refusal does not say which flow needs it (%s): %v", flowVar, err)
						}
					}
					for _, other := range required {
						if other != key && strings.Contains(err.Error(), other+" is required") {
							t.Errorf("%s is set, but the refusal names it as missing too: %v", other, err)
						}
					}
					noCredential(t, err, sentinelUser, sentinelPass)
				})
			}
		}
	}

	// All four at once: one pass, one error EACH (not a blanket "settings missing").
	t.Run("everything missing is reported in one pass", func(t *testing.T) {
		setRequired(t)
		t.Setenv("TAPPA_RESET_DELIVERY", "email")
		_, err := config.Load()
		if err == nil {
			t.Fatal("e-mail on with no transport settings at all loaded")
		}
		for _, key := range required {
			if !strings.Contains(err.Error(), key+" is required") {
				t.Errorf("the one-pass refusal does not name %s: %v", key, err)
			}
		}
	})
}

// TestLoad_MailSettingsAreNotReadWhileBothFlowsAreOff: with none + panel the transport's
// variables are neither read nor validated (ADR 0022 §5) — so the runbook's intermediate
// state (credentials in the Secret, the ConfigMap none/panel: before the switch, or after
// turning e-mail off again — the shipped ConfigMap says email/email since 2026-10-09)
// boots unchanged, and even values that would be refused with a flow on do not stop the
// boot. Mail stays the zero value: a field nothing should use holds nothing to use.
func TestLoad_MailSettingsAreNotReadWhileBothFlowsAreOff(t *testing.T) {
	user, pass := credentialVariables(t)
	for _, tc := range []struct {
		name string
		set  map[string]string
	}{
		{"nothing set (the default; development's .env)", nil},
		{"the runbook's intermediate state", map[string]string{
			"TAPPA_SMTP_HOST": shippedHost, "TAPPA_SMTP_PORT": "587", "TAPPA_MAIL_FROM": shippedFrom,
			user: sentinelUser, pass: sentinelPass}},
		{"credentials only", map[string]string{user: sentinelUser, pass: sentinelPass}},
		{"values a flow would refuse, in production", map[string]string{
			"TAPPA_ENV": "prod", "TAPPA_SMTP_HOST": "127.0.0.1", "TAPPA_SMTP_PORT": "465",
			"TAPPA_MAIL_FROM": "not an address", "TAPPA_MAIL_REPLY_TO": "Name <reply@taptime.mt>", user: "\t"}},
		{"explicitly off", map[string]string{"TAPPA_RESET_DELIVERY": "none", "TAPPA_INVITE_DELIVERY": "panel",
			"TAPPA_SMTP_PORT": "not-a-port"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			for k, v := range tc.set {
				t.Setenv(k, v)
			}
			c, err := config.Load()
			if err != nil {
				t.Fatalf("with both flows off the transport's settings must not stop the boot: %v", err)
			}
			if c.ResetDelivery != config.ResetDeliveryNone || c.InviteDelivery != config.InviteDeliveryPanel {
				t.Fatalf("PREMISE: flows are %q/%q", c.ResetDelivery, c.InviteDelivery)
			}
			if c.Mail != (mail.Config{}) {
				t.Errorf("with both flows off Mail must be the zero value; it holds host %q, port %d", c.Mail.Host, c.Mail.Port)
			}
		})
	}
}

// TestLoad_SMTPPortRefusesImplicitTLSEverywhere: 465 is refused in dev, staging and
// prod alike (ADR 0022 §5 — the refusal is about the protocol); unset is 587; anything
// that is not a port number 1-65535 is refused, without repeating the value.
func TestLoad_SMTPPortRefusesImplicitTLSEverywhere(t *testing.T) {
	for _, env := range []string{config.EnvDev, config.EnvStaging, config.EnvProd} {
		for _, tc := range []struct {
			port string
			want int // 0: refused
		}{
			{"", 587},
			{"587", 587},
			{"25", 25},
			{"2587", 2587},
			{"465", 0},
			{"0465", 0}, // the same number, another spelling
			{"0", 0},
			{"65536", 0},
			{"-587", 0},
			{" 587", 0},
			{"587.0", 0},
			{"qq-port", 0},
		} {
			t.Run(env+"/"+tc.port, func(t *testing.T) {
				setRequired(t)
				setMail(t)
				t.Setenv("TAPPA_ENV", env)
				t.Setenv("TAPPA_RESET_DELIVERY", "email")
				t.Setenv("TAPPA_SMTP_PORT", tc.port)
				c, err := config.Load()
				if tc.want != 0 {
					if err != nil {
						t.Fatalf("port %q must load: %v", tc.port, err)
					}
					if c.Mail.Port != tc.want {
						t.Fatalf("port %q gave %d, want %d", tc.port, c.Mail.Port, tc.want)
					}
					return
				}
				if err == nil {
					t.Fatalf("port %q loaded in %s", tc.port, env)
				}
				if !strings.Contains(err.Error(), "TAPPA_SMTP_PORT") {
					t.Errorf("the refusal does not name TAPPA_SMTP_PORT: %v", err)
				}
				if strings.TrimLeft(tc.port, "0") == "465" && !strings.Contains(err.Error(), "STARTTLS") {
					t.Errorf("the 465 refusal does not say why (implicit TLS against a STARTTLS transport): %v", err)
				}
				if tc.port == "qq-port" {
					noValue(t, err, tc.port)
				}
			})
		}
	}
}

// TestLoad_SMTPHostRefusesLoopbackAndAddressesInProduction: in production the relay must
// be a public DNS name — localhost, *.localhost and an IP address in any spelling are
// refused (ADR 0022 §5); outside production they are allowed (§12). In every
// environment the value has ONE spelling: a lower-case DNS name or an IP literal.
func TestLoad_SMTPHostRefusesLoopbackAndAddressesInProduction(t *testing.T) {
	for _, tc := range []struct {
		host          string
		devOK, prodOK bool
	}{
		{shippedHost, true, true},
		{"smtp.example.com", true, true},
		{"relay", true, true}, // a single label is a DNS name (counted: it resolves through the search list)
		{"localhost", true, false},
		{"relay.localhost", true, false},
		{"127.0.0.1", true, false},
		{"::1", true, false},
		{"192.0.2.25", true, false},
		{"2001:db8::25", true, false},
		{"::", true, false},               // the unspecified address
		{"::ffff:7f00:1", true, false},    // 127.0.0.1, v4-mapped
		{"::ffff:127.0.0.1", true, false}, // the same, dotted
		{"127.1", true, false},            // inet_aton's short form of 127.0.0.1
		{"0x7f000001", true, false},       // and its hex form
		{"mail.example.123", true, false},
		{"LOCALHOST", false, false},
		{"localhost.", false, false},
		{"smtp.example.com:587", false, false},
		{"smtps://smtp.example.com", false, false},
		{"[::1]", false, false},
		{"smtp example.com", false, false},
		{"-relay.example.com", false, false},
		{"Qq-Relay-Sentinel.example.com", false, false},
	} {
		for _, env := range []string{config.EnvDev, config.EnvStaging, config.EnvProd} {
			want := tc.devOK
			if env == config.EnvProd {
				want = tc.prodOK
			}
			t.Run(env+"/"+tc.host, func(t *testing.T) {
				setRequired(t)
				setMail(t)
				t.Setenv("TAPPA_ENV", env)
				t.Setenv("TAPPA_INVITE_DELIVERY", "email")
				t.Setenv("TAPPA_SMTP_HOST", tc.host)
				c, err := config.Load()
				if want {
					if err != nil {
						t.Fatalf("host %q must load in %s: %v", tc.host, env, err)
					}
					if c.Mail.Host != tc.host {
						t.Fatalf("host %q gave %q", tc.host, c.Mail.Host)
					}
					return
				}
				if err == nil {
					t.Fatalf("host %q loaded in %s", tc.host, env)
				}
				if !strings.Contains(err.Error(), "TAPPA_SMTP_HOST") {
					t.Errorf("the refusal does not name TAPPA_SMTP_HOST: %v", err)
				}
				// Only the sentinel row is searched for: "localhost" is in the production
				// refusal's own explanation, and saying what is refused is not a leak.
				if strings.Contains(tc.host, "Sentinel") {
					noValue(t, err, tc.host)
				}
			})
		}
	}
}

// TestLoad_SenderAddressesFollowTheTransportsOwnRule: TAPPA_MAIL_FROM and
// TAPPA_MAIL_REPLY_TO are judged by internal/mail's rule, not by a copy of it (ADR 0022
// §4: the sender rule for From, the RECIPIENT rule for Reply-To). Every row is asked
// twice — config.Load with the value in the environment, and mail.New with the value in
// a Config — and the two answers must agree with each other and with the row. A second
// rule in this package that drifted from mail.New's is red when it disagrees ON ONE OF
// THESE 21 ROWS, and only then: the third eye's X06 (the rule skipped for a From longer
// than any row here) stayed green. The table is a sample, not the rule's domain.
func TestLoad_SenderAddressesFollowTheTransportsOwnRule(t *testing.T) {
	type row struct {
		name, value string
		ok          bool
	}
	from := []row{
		{"the shipped sender", shippedFrom, true},
		{"a bare address", "no-reply@taptime.mt", true},
		{"a quoted display name", `"Taptime" <no-reply@taptime.mt>`, true},
		{"CR LF and a Bcc line", shippedFrom + "\r\nBcc: x@example.com", false},
		{"an encoded word", "=?utf-8?q?Taptime?= <no-reply@taptime.mt>", false},
		{"two addresses", "a@taptime.mt, b@taptime.mt", false},
		{"a name and no address", "Taptime", false},
		{"a non-ASCII address", "Taptime <ćali@taptime.mt>", false},
		{"a tab in the name", "Tap\ttime <no-reply@taptime.mt>", false},
	}
	replyTo := []row{
		{"a bare address", "support@taptime.mt", true},
		{"plus addressing", "support+mt@taptime.mt", true},
		{"upper case", "Support@Taptime.MT", true},
		{"a bracketed domain (ADR 0022 counted limit 7)", "support@[192.0.2.1]", true},
		{"254 bytes", strings.Repeat("a", 243) + "@taptime.mt", true},
		{"255 bytes", strings.Repeat("a", 244) + "@taptime.mt", false},
		{"a display name", "Support <support@taptime.mt>", false},
		{"angle brackets", "<support@taptime.mt>", false},
		{"a leading space", " support@taptime.mt", false},
		{"an encoded word", "=?utf-8?q?x?=@taptime.mt", false},
		{"a non-ASCII address", "ćali@taptime.mt", false},
		{"two addresses", "a@taptime.mt,b@taptime.mt", false},
	}
	transport := func(fromV, replyV string) bool {
		_, err := mail.New(mail.Config{Host: shippedHost, Port: 587, Username: mail.NewCredential("u"),
			Password: mail.NewCredential("p"), From: fromV, ReplyTo: replyV})
		return err == nil
	}
	check := func(t *testing.T, variable string, r row, transportOK bool) {
		t.Helper()
		if transportOK != r.ok {
			t.Fatalf("PREMISE: mail.New says %v for %q, the row says %v — the table is out of date with internal/mail",
				transportOK, r.value, r.ok)
		}
		setRequired(t)
		setMail(t)
		t.Setenv("TAPPA_RESET_DELIVERY", "email")
		t.Setenv(variable, r.value)
		c, err := config.Load()
		switch {
		case r.ok && err != nil:
			t.Fatalf("mail.New accepts %s=%q and config.Load refuses it: %v", variable, r.value, err)
		case !r.ok && err == nil:
			t.Fatalf("mail.New refuses %s=%q and config.Load accepted it (Mail.Host %q)", variable, r.value, c.Mail.Host)
		case !r.ok:
			if !strings.Contains(err.Error(), variable) {
				t.Errorf("the refusal does not name %s: %v", variable, err)
			}
			noValue(t, err, r.value)
		}
	}
	for _, r := range from {
		t.Run("From/"+r.name, func(t *testing.T) { check(t, "TAPPA_MAIL_FROM", r, transport(r.value, "")) })
	}
	for _, r := range replyTo {
		t.Run("Reply-To/"+r.name, func(t *testing.T) { check(t, "TAPPA_MAIL_REPLY_TO", r, transport(shippedFrom, r.value)) })
	}
}

// TestLoad_MailErrorsNameTheVariableNeverTheValue: with every setting wrong at once,
// the one error names all six variables. Of the two credential values it carries no
// 3-byte window at all (noCredential); of the other four, neither the value nor its
// first 8, 4 or 3 characters (noValue — a prefix search). The credential values are
// rejected for a control character — internal/mail's rule — so a refusal is the only
// place they could surface. The control character sits in the MIDDLE of each value, so
// that its first and its last 3 bytes are sentinel bytes: a refusal that leaked either
// end (the third eye's X07, the closing auditor's N12) or an inner run (N12b) is a
// window noCredential finds.
func TestLoad_MailErrorsNameTheVariableNeverTheValue(t *testing.T) {
	user, pass := credentialVariables(t)
	setRequired(t)
	t.Setenv("TAPPA_ENV", "prod")
	t.Setenv("TAPPA_RESET_DELIVERY", "email")
	values := map[string]string{
		"TAPPA_SMTP_HOST":     "Qq-Host-Sentinel.example",
		"TAPPA_SMTP_PORT":     "qq-port-sentinel",
		user:                  sentinelUser[:8] + "\t" + sentinelUser[8:],
		pass:                  sentinelPass[:8] + "\n" + sentinelPass[8:],
		"TAPPA_MAIL_FROM":     "qq-from-sentinel",
		"TAPPA_MAIL_REPLY_TO": "Qq Reply <qq-reply-sentinel@taptime.mt>",
	}
	for k, v := range values {
		t.Setenv(k, v)
	}
	_, err := config.Load()
	if err == nil {
		t.Fatal("six invalid settings loaded")
	}
	for k, v := range values {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("the error does not name %s: %v", k, err)
		}
		if k == user || k == pass {
			continue
		}
		noValue(t, err, v)
	}
	noCredential(t, err, values[user], values[pass])
	noValue(t, err, "qq-reply-sentinel")
}

// TestLoad_MailConfigCarriesTheEnvironmentsValues: what Load puts into Mail is the
// environment's value for each field — the credentials in the right slots (read back by
// deliberate reflection; a swap would pass mail.New), the port's default, no Reply-To
// when unset, NO root pool (ADR 0022 §2), and the zero timeouts that mean internal/mail's
// defaults. And mail.New accepts the result, which is what cmd/tappa calls for the
// reset flow (EM-5) and EM-7 will call for invitations.
func TestLoad_MailConfigCarriesTheEnvironmentsValues(t *testing.T) {
	setRequired(t)
	setMail(t)
	t.Setenv("TAPPA_INVITE_DELIVERY", "email")
	t.Setenv("TAPPA_SMTP_PORT", "")
	c, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := c.Mail
	if m.Host != shippedHost || m.Port != 587 || m.From != shippedFrom || m.ReplyTo != "" {
		t.Errorf("Mail = host %q, port %d, from %q, reply-to %q; want the environment's values and port 587",
			m.Host, m.Port, m.From, m.ReplyTo)
	}
	if got := revealForTest(t, m.Username); got != sentinelUser {
		t.Errorf("Mail.Username does not hold the username variable's value (it holds %d bytes)", len(got))
	}
	if got := revealForTest(t, m.Password); got != sentinelPass {
		t.Errorf("Mail.Password does not hold the password variable's value (it holds %d bytes)", len(got))
	}
	if m.RootCAs != nil {
		t.Error("Mail.RootCAs is set: a private root pool is forbidden in production (ADR 0022 §2)")
	}
	if m.Timeout != 0 || m.RetryDelay != 0 {
		t.Errorf("Mail's timeouts are %v/%v; no variable sets them, so they must be zero (internal/mail's defaults)",
			m.Timeout, m.RetryDelay)
	}
	if _, err := mail.New(m); err != nil {
		t.Errorf("mail.New refuses what Load produced: %v", err)
	}

	t.Setenv("TAPPA_MAIL_REPLY_TO", "support@taptime.mt")
	t.Setenv("TAPPA_SMTP_PORT", "2587")
	c, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Mail.ReplyTo != "support@taptime.mt" || c.Mail.Port != 2587 {
		t.Errorf("Reply-To %q, port %d: the set values did not reach Mail", c.Mail.ReplyTo, c.Mail.Port)
	}
}

// TestLoad_MailCredentialsAreRedactedInTheConfig: ADR 0022 İddia D's EM-3 case. A
// Config carrying the two credentials, rendered by every printer the test names —
// fmt's verbs on the Config and on its Mail, slog's text and JSON handlers, and
// encoding/json — shows neither value nor the first 8 characters of one. CONTROL: the
// renderings did print the struct (the host is in each), so their silence about the
// credentials is not an empty print. This is the struct's EXPORTED path; the rest of
// Config is not redacted (backlog T80).
func TestLoad_MailCredentialsAreRedactedInTheConfig(t *testing.T) {
	setRequired(t)
	setMail(t)
	t.Setenv("TAPPA_RESET_DELIVERY", "email")
	c, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	renderings := map[string]string{}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		renderings["Config "+verb] = fmt.Sprintf(verb, c)
		renderings["*Config "+verb] = fmt.Sprintf(verb, *c)
		renderings["Mail "+verb] = fmt.Sprintf(verb, c.Mail)
	}
	for name, h := range map[string]func(*bytes.Buffer) slog.Handler{
		"slog text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		"slog json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	} {
		var b bytes.Buffer
		slog.New(h(&b)).Info("config", "config", c, "mail", c.Mail)
		renderings[name] = b.String()
	}
	for name, v := range map[string]any{"json Config": c, "json Mail": c.Mail} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		renderings[name] = string(b)
	}
	for name, out := range renderings {
		if !strings.Contains(out, shippedHost) && !strings.Contains(out, fmt.Sprintf("%x", shippedHost)) {
			t.Errorf("CONTROL FAILED: %s does not show the host, so it did not print the struct", name)
		}
		for _, cred := range []string{sentinelUser, sentinelPass} {
			for _, frag := range []string{cred, cred[:8], hex.EncodeToString([]byte(cred[:8]))} {
				if strings.Contains(out, frag) {
					t.Errorf("%s shows a credential (or its first 8 characters)", name)
				}
			}
		}
	}
}
