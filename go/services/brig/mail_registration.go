// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"errors"
	"maps"
	"strings"
)

// Rendering is independent of delivery. A rendered message is never evidence
// of successful SES/SMTP delivery; public registration must await the transport.
type registrationMailRenderer struct {
	templates                        mailTemplates
	sender                           string
	branding                         map[string]string
	activationURL, teamActivationURL mailTemplate
	passwordResetURL                 mailTemplate
	memberWelcomeURL                 string
	warn                             func(string)
}

func (registrationMailRenderer) String() string { return "<RegistrationMailRenderer>" }

func newRegistrationMailRenderer(c Config, warn func(string)) (registrationMailRenderer, error) {
	var result registrationMailRenderer
	failure := errors.New("invalid native mail configuration")
	object := func(value any) (map[string]any, error) {
		fields, ok := value.(map[string]any)
		if !ok {
			return nil, failure
		}
		return fields, nil
	}
	text := func(fields map[string]any, key string) (string, error) {
		value, ok := fields[key].(string)
		if !ok {
			return "", failure
		}
		return value, nil
	}
	options, err := object(c.Other["emailSMS"])
	if err != nil {
		return result, err
	}
	general, err := object(options["general"])
	if err != nil {
		return result, err
	}
	base, err := text(general, "templateDir")
	if err != nil {
		return result, err
	}
	sender, err := text(general, "emailSender")
	if err != nil {
		return result, err
	}
	sender, err = canonicalRegistrationEmail(sender)
	if err != nil {
		return result, failure
	}
	domain := sender[strings.LastIndexByte(sender, '@')+1:]
	if _, err = parseEmailDomain(domain); err != nil {
		return result, failure
	}
	branding, err := object(general["templateBranding"])
	if err != nil {
		return result, err
	}
	result.branding = map[string]string{}
	for field, key := range map[string]string{"brand": "brand", "brandUrl": "brand_url", "brandLabelUrl": "brand_label_url", "brandLogoUrl": "brand_logo", "brandService": "brand_service", "copyright": "copyright", "misuse": "misuse", "legal": "legal", "forgot": "forgot", "support": "support"} {
		value, err := text(branding, field)
		if err != nil {
			return result, err
		}
		result.branding[key] = value
	}
	user, err := object(options["user"])
	if err != nil {
		return result, err
	}
	team, err := object(options["team"])
	if err != nil {
		return result, err
	}
	for _, url := range []struct {
		fields      map[string]any
		key         string
		destination *mailTemplate
	}{
		{user, "activationUrl", &result.activationURL}, {team, "tActivationUrl", &result.teamActivationURL}, {user, "passwordResetUrl", &result.passwordResetURL},
	} {
		value, err := text(url.fields, url.key)
		if err != nil {
			return result, err
		}
		*url.destination, err = compileMailTemplate(value)
		if err != nil {
			return result, err
		}
	}
	result.memberWelcomeURL, err = text(team, "tMemberWelcomeUrl")
	if err != nil {
		return result, err
	}
	locale := "en"
	if value, present := c.Settings.Other["setDefaultTemplateLocale"]; present && value != nil {
		var ok bool
		locale, ok = value.(string)
		if !ok {
			return result, failure
		}
	}
	result.templates, err = loadMailTemplates(base, locale)
	if err != nil {
		return result, err
	}
	result.sender, result.warn = sender, warn
	return result, nil
}

func (r registrationMailRenderer) activation(created registrationCreation, origin registrationOrigin) (brigMail, error) {
	var result brigMail
	if created.profile.Email == nil || created.activation == nil {
		return result, errors.New("activation mail identity unavailable")
	}
	templateName, urlTemplate := "activation", r.activationURL
	if origin.Team != nil {
		templateName, urlTemplate = "team-activation", r.teamActivationURL
	}
	template, err := r.templates.selectTemplate("user", templateName, created.profile.Locale)
	if err != nil {
		return result, err
	}
	urlValues := maps.Clone(r.branding)
	urlValues["key"], urlValues["code"] = created.activation.key, created.activation.code
	values := maps.Clone(r.branding)
	values["url"] = urlTemplate.render(urlValues, false, r.warn)
	values["email"], values["name"] = *created.profile.Email, created.profile.Name
	if origin.Team != nil {
		values["team"] = origin.Team.Name
	}
	result = r.render(template, *created.profile.Email, &created.profile.Name, values)
	result.headers = [][2]string{{"X-Zeta-Purpose", "Activation"}, {"X-Zeta-Key", created.activation.key}, {"X-Zeta-Code", created.activation.code}}
	return result, nil
}
func (r registrationMailRenderer) welcome(profile selfProfile, team registrationCreatedTeam) (brigMail, error) {
	if profile.Email == nil {
		return brigMail{}, errors.New("welcome mail identity unavailable")
	}
	template, err := r.templates.selectTemplate("team", "new-member-welcome", profile.Locale)
	if err != nil {
		return brigMail{}, err
	}
	values := maps.Clone(r.branding)
	// Native memberWelcomeEmailUrl is literal text, not a URL template.
	values["url"], values["email"], values["team_id"], values["team_name"] = r.memberWelcomeURL, *profile.Email, team.ID.String(), team.Name
	result := r.render(template, *profile.Email, nil, values)
	result.headers = [][2]string{{"X-Zeta-Purpose", "Welcome"}}
	return result, nil
}
func (r registrationMailRenderer) render(template localizedMailTemplate, email string, name *string, values map[string]string) brigMail {
	return brigMail{from: r.sender, fromName: template.sender, to: email, toName: name,
		subject: template.subject.render(values, false, r.warn), plain: template.plain.render(values, false, r.warn), html: template.html.render(values, true, r.warn)}
}

func (r registrationMailRenderer) passwordReset(email, key, code, locale string) (brigMail, error) {
	template, err := r.templates.selectTemplate("user", "password-reset", locale)
	if err != nil {
		return brigMail{}, err
	}
	urlValues := maps.Clone(r.branding)
	urlValues["key"], urlValues["code"] = key, code
	values := maps.Clone(r.branding)
	values["url"] = r.passwordResetURL.render(urlValues, false, r.warn)
	result := r.render(template, email, nil, values)
	result.headers = [][2]string{{"X-Zeta-Purpose", "PasswordReset"}, {"X-Zeta-Key", key}, {"X-Zeta-Code", code}}
	return result, nil
}
