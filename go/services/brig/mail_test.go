// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestMailTemplateGrammarAndEscaping(t *testing.T) {
	template, err := compileMailTemplate(`<strong>$name</strong> ${url} $$ $unknown $_x $é2'`)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"name": `<&>"'`, "url": "$name", "_x": "ok", "é2'": "unicode"}
	var warnings []string
	got := template.render(values, true, func(s string) { warnings = append(warnings, s) })
	want := `<strong>&lt;&amp;&gt;&quot;&#39;</strong> $name $ unknown ok unicode`
	if got != want || !reflect.DeepEqual(warnings, []string{"unknown"}) {
		t.Fatalf("render/warnings: %q %v", got, warnings)
	}
	if !strings.Contains(template.render(values, false, nil), `<&>"'`) {
		t.Fatal("plain values unexpectedly escaped")
	}
	for _, bad := range []string{"$", "${}", "$Upper", "${name", "$3", "${name!}", string([]byte{255})} {
		if _, err := compileMailTemplate(bad); err == nil {
			t.Fatalf("invalid template accepted: %q", bad)
		}
	}
}

func TestMailNativeRepositoryTemplates(t *testing.T) {
	c, err := LoadConfig("../../../services/brig/brig.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	general := c.Other["emailSMS"].(map[string]any)["general"].(map[string]any)
	general["templateDir"] = "../../../libs/wire-subsystems/templates"
	// Native template locale defaults independently of default user locale.
	c.Settings.DefaultLocale = "de"
	var warnings []string
	renderer, err := newRegistrationMailRenderer(c, func(s string) { warnings = append(warnings, s) })
	if err != nil {
		t.Fatal(err)
	}
	if renderer.templates.defaultLocale != "en" {
		t.Fatal("wrong default template locale")
	}
	email := "synthetic@example.com"
	created := registrationCreation{profile: selfProfile{Email: &email, Name: "Synthetic <Owner>", Locale: "en"}, activation: &activationRecord{key: "synthetic-key", code: "123456"}}
	message, err := renderer.activation(created, registrationOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message.plain, "https://example.com/verify/?key=synthetic-key&code=123456") || !strings.Contains(message.html, "key=synthetic-key&amp;code=123456") {
		t.Fatal("activation URL replacement/HTML escaping missing")
	}
	if !reflect.DeepEqual(message.headers, [][2]string{{"X-Zeta-Purpose", "Activation"}, {"X-Zeta-Key", "synthetic-key"}, {"X-Zeta-Code", "123456"}}) {
		t.Fatal("activation processing headers changed")
	}
	team := registrationTeam{Name: "Synthetic <Team>"}
	message, err = renderer.activation(created, registrationOrigin{Team: &team})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message.plain, team.Name) || !strings.Contains(message.html, "Synthetic &lt;Team&gt;") {
		t.Fatal("team activation content escaping changed")
	}
	renderer.memberWelcomeURL = "https://example.com/$literal"
	teamID, err := gocql.ParseUUID("10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	welcome, err := renderer.welcome(created.profile, registrationCreatedTeam{ID: teamID, Name: team.Name})
	if err != nil {
		t.Fatal(err)
	}
	if welcome.toName != nil || !strings.Contains(welcome.plain, "https://example.com/$literal") || !strings.Contains(welcome.plain, "10000000-0000-0000-0000-000000000001") || welcome.headers[0][1] != "Welcome" {
		t.Fatal("welcome recipient/literal URL/team/purpose changed")
	}
	if len(warnings) != 0 {
		t.Fatalf("native templates emitted missing variables: %v", warnings)
	}
	idp, err := renderer.templates.selectTemplate("team", "idp-config-change", "en")
	if err != nil || len(idp.partials) != 4 {
		t.Fatal("certificate partials not retained")
	}
	for _, bad := range []any{int64(12), "not-a-domain@invalid!"} {
		general["emailSender"] = bad
		if _, err := newRegistrationMailRenderer(c, nil); err == nil {
			t.Fatal("invalid sender accepted")
		}
	}
}

func TestMailLocaleAndPerFileFallback(t *testing.T) {
	base := t.TempDir()
	write := func(name, value string) {
		t.Helper()
		path := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for group, names := range nativeMailFiles {
		write("en/"+group+"/email/sender.txt", "Default Sender\n")
		for _, name := range names {
			for _, suffix := range []string{"-subject.txt", ".txt", ".html"} {
				write("en/"+group+"/email/"+name+suffix, "default $name")
			}
		}
	}
	for _, name := range []string{"idp-certificate-added.html", "idp-certificate-added.txt", "idp-certificate-removed.html", "idp-certificate-removed.txt"} {
		write("en/partials/"+name, "certificate")
	}
	write("de/user/email/activation-subject.txt", "Deutsch $name")
	write("de-DE/user/email/activation.txt", "German country $name")
	templates, err := loadMailTemplates(base, "en")
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ locale, subject, plain string }{{"de-DE", "default N", "German country N"}, {"de-AT", "Deutsch N", "default N"}, {"fr", "default N", "default N"}, {"../../de", "default N", "default N"}} {
		selected, err := templates.selectTemplate("user", "activation", check.locale)
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]string{"name": "N"}
		if selected.subject.render(values, false, nil) != check.subject || selected.plain.render(values, false, nil) != check.plain || selected.sender != "Default Sender\n" {
			t.Fatalf("incorrect per-file/exact-language-default fallback: %s", check.locale)
		}
	}
	// No fallback after a file was found but has invalid content.
	write("de/user/email/activation.html", string([]byte{255}))
	if _, err := loadMailTemplates(base, "en"); err == nil {
		t.Fatal("invalid UTF-8 localized file accepted")
	}
	write("de/user/email/activation.html", "valid")
	write("not-a-locale/user/email/activation.html", "valid")
	if _, err := loadMailTemplates(base, "en"); err == nil {
		t.Fatal("invalid supported locale directory accepted")
	}
}

func TestMailMIMEHeadersAndDecodedBodies(t *testing.T) {
	name := "Synthetic ü\r\nBcc: injected@example.com"
	message := brigMail{from: "sender@example.com\r\n", fromName: "Sender", to: "recipient@example.com", toName: &name, subject: "Synthetic\r\nBcc: harmless", plain: "plain ü\nsecond\r\n", html: "<p>ü &amp;</p>\n", headers: [][2]string{{"X-Zeta-Purpose", "Activation"}}}
	raw, err := message.mime()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Get("Bcc") != "" || parsed.Header.Get("Subject") != "SyntheticBcc: harmless" || parsed.Header.Get("X-Zeta-Purpose") != "Activation" {
		t.Fatal("injected or changed headers")
	}
	if got := mailEncodedWord("a _?=\"(),.:;<>@[]\\ü"); got != "=?utf-8?Q?a_=5F=3F=3D=22=28=29=2C=2E=3A=3B=3C=3E=40=5B=5D=5C=C3=BC?=" {
		t.Fatalf("native address Q encoding differs: %s", got)
	}
	media, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || media != "multipart/alternative" {
		t.Fatal("missing alternative MIME")
	}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	for i, want := range []string{"plain ü\r\nsecond\r\n", "<p>ü &amp;</p>\r\n"} {
		part, err := reader.NextRawPart()
		if err != nil {
			t.Fatal(err)
		}
		expected := []string{"text/plain", "text/html"}[i]
		partMedia, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil || partMedia != expected || part.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
			t.Fatal("incorrect MIME part")
		}
		body, err := io.ReadAll(quotedprintable.NewReader(part))
		if err != nil || string(body) != want {
			t.Fatalf("decoded body: %q %v", body, err)
		}
	}
	if _, err := reader.NextRawPart(); err != io.EOF {
		t.Fatal("unexpected MIME part")
	}
	// The native recipient limit is based on the rendered address, including Q encoding.
	longName := strings.Repeat("ü", 60)
	if got := mailAddress("recipient@example.com", &longName, true); got != "<recipient@example.com>" {
		t.Fatal("long encoded recipient name retained")
	}
	if got := mailAddress("recipient@example.com", nil, true); got != "<recipient@example.com>" {
		t.Fatal("bare recipient changed")
	}
}
