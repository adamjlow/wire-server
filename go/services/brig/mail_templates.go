// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type mailFragment struct {
	text     string
	variable bool
}
type mailTemplate []mailFragment

func compileMailTemplate(text string) (mailTemplate, error) {
	failure := errors.New("invalid mail template")
	if !utf8.ValidString(text) {
		return nil, failure
	}
	parts := mailTemplate{}
	for len(text) > 0 {
		i := strings.IndexByte(text, '$')
		if i < 0 {
			parts = append(parts, mailFragment{text: text})
			break
		}
		if i > 0 {
			parts = append(parts, mailFragment{text: text[:i]})
		}
		text = text[i+1:]
		if strings.HasPrefix(text, "$") {
			parts = append(parts, mailFragment{text: "$"})
			text = text[1:]
			continue
		}
		bracket := strings.HasPrefix(text, "{")
		if bracket {
			text = text[1:]
		}
		first, _ := utf8.DecodeRuneInString(text)
		if !unicode.IsLower(first) && first != '_' {
			return nil, failure
		}
		n := 0
		for _, r := range text {
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '\'' {
				break
			}
			n += utf8.RuneLen(r)
		}
		parts = append(parts, mailFragment{text: text[:n], variable: true})
		text = text[n:]
		if bracket {
			if !strings.HasPrefix(text, "}") {
				return nil, failure
			}
			text = text[1:]
		}
	}
	return parts, nil
}

var mailHTMLEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

func (t mailTemplate) render(values map[string]string, html bool, warn func(string)) string {
	var out strings.Builder
	for _, part := range t {
		value := part.text
		if part.variable {
			replacement, found := values[value]
			if found {
				value = replacement
			} else if warn != nil {
				warn(value)
			}
			if html {
				value = mailHTMLEscape.Replace(value)
			}
		}
		out.WriteString(value)
	}
	return out.String()
}

type localizedMailTemplate struct {
	sender               string
	subject, plain, html mailTemplate
	partials             map[string]mailTemplate
}
type mailTemplates struct {
	defaultLocale string
	groups        map[string]map[string]map[string]localizedMailTemplate
}

var nativeMailFiles = map[string][]string{
	"provider": {"password-reset"},
	"user":     {"verification", "activation", "update", "team-activation", "password-reset", "deletion", "new-client", "verification-login", "verification-scim-token", "verification-delete-team"},
	"team":     {"invitation", "migration", "new-member-welcome", "new-team-owner-welcome", "idp-config-change"},
}

func loadMailTemplates(base, defaultLocale string) (mailTemplates, error) {
	parsedDefault, err := nativeMailLocale(defaultLocale)
	result := mailTemplates{defaultLocale: parsedDefault, groups: map[string]map[string]map[string]localizedMailTemplate{}}
	if err != nil {
		return result, errors.New("invalid default mail locale")
	}
	defaultLocale = parsedDefault
	entries, err := os.ReadDir(base)
	if err != nil {
		return result, errors.New("mail templates unavailable")
	}
	read := func(locale, group, name string) (string, error) {
		path := filepath.Join(base, locale, group, name)
		info, err := os.Stat(path)
		if os.IsNotExist(err) || err == nil && !info.Mode().IsRegular() {
			path = filepath.Join(base, defaultLocale, group, name)
		} else if err != nil {
			return "", errors.New("mail template unavailable")
		}
		raw, err := os.ReadFile(path)
		if err != nil || !utf8.Valid(raw) {
			return "", errors.New("mail template unavailable")
		}
		return string(raw), nil
	}
	for group, names := range nativeMailFiles {
		locales := []string{defaultLocale}
		for _, entry := range entries {
			if entry.Name() == defaultLocale {
				continue
			}
			baseInfo, err := os.Stat(filepath.Join(base, entry.Name()))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return result, errors.New("mail locale unavailable")
			}
			if !baseInfo.IsDir() {
				continue
			}
			info, err := os.Stat(filepath.Join(base, entry.Name(), group))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return result, errors.New("mail locale unavailable")
			}
			if !info.IsDir() {
				continue
			}
			if _, err := parseRegistrationLocale(entry.Name()); err != nil {
				return result, errors.New("invalid mail locale directory")
			}
			locales = append(locales, entry.Name())
		}
		result.groups[group] = map[string]map[string]localizedMailTemplate{}
		for _, locale := range locales {
			templates := map[string]localizedMailTemplate{}
			sender, err := read(locale, group, "email/sender.txt")
			if err != nil {
				return result, err
			}
			for _, name := range names {
				parts := make([]mailTemplate, 3)
				for i, suffix := range []string{"-subject.txt", ".txt", ".html"} {
					raw, err := read(locale, group, "email/"+name+suffix)
					if err != nil {
						return result, err
					}
					parts[i], err = compileMailTemplate(raw)
					if err != nil {
						return result, err
					}
				}
				templates[name] = localizedMailTemplate{sender: sender, subject: parts[0], plain: parts[1], html: parts[2]}
			}
			// Native IdP templates also load the four certificate partials.
			if group == "team" {
				partials := map[string]mailTemplate{}
				for _, name := range []string{"idp-certificate-added.html", "idp-certificate-added.txt", "idp-certificate-removed.html", "idp-certificate-removed.txt"} {
					raw, err := read(locale, group, "../partials/"+name)
					if err != nil {
						return result, err
					}
					partials[name], err = compileMailTemplate(raw)
					if err != nil {
						return result, err
					}
				}
				idp := templates["idp-config-change"]
				idp.partials = partials
				templates["idp-config-change"] = idp
			}
			parsed, err := nativeMailLocale(locale)
			if err != nil {
				return result, err
			}
			result.groups[group][parsed] = templates
		}
	}
	return result, nil
}

func (t mailTemplates) selectTemplate(group, name, preferred string) (localizedMailTemplate, error) {
	// Native parseOnly deliberately accepts a valid locale prefix. Catalogue
	// keys are parsed Locale values, while filesystem paths use directory names.
	if parsed, err := nativeMailLocale(preferred); err == nil {
		preferred = parsed
	} else {
		preferred = ""
	}
	locales := t.groups[group]
	selected := t.defaultLocale
	if _, exists := locales[preferred]; exists {
		selected = preferred
	} else if language, _, _ := strings.Cut(preferred, "-"); language != "" {
		if _, exists := locales[language]; exists {
			selected = language
		}
	}
	template, exists := locales[selected][name]
	if !exists {
		return localizedMailTemplate{}, errors.New("mail template unavailable")
	}
	return template, nil
}

func nativeMailLocale(value string) (string, error) {
	locale, err := parseRegistrationLocale(value)
	if err != nil {
		return "", errors.New("invalid mail locale")
	}
	canonical := locale.Language
	if locale.Country != nil {
		canonical += "-" + *locale.Country
	}
	return canonical, nil
}
