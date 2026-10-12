// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"maps"
)

func (r registrationMailRenderer) providerPasswordReset(email, key, code string) (brigMail, error) {
	// Provider.API sends with providerTemplatesWithLocale Nothing, independently
	// of the account or default user locale.
	template, err := r.templates.selectTemplate("provider", "password-reset", r.templates.defaultLocale)
	if err != nil {
		return brigMail{}, err
	}
	urlValues := maps.Clone(r.branding)
	urlValues["key"], urlValues["code"] = key, code
	values := maps.Clone(r.branding)
	values["url"] = r.providerResetURL.render(urlValues, false, r.warn)
	result := r.render(template, email, nil, values)
	result.headers = [][2]string{{"X-Zeta-Purpose", "ProviderPasswordReset"}, {"X-Zeta-Key", key}, {"X-Zeta-Code", code}}
	return result, nil
}

func (d *registrationMailDelivery) providerPasswordReset(ctx context.Context, email, key, code string) error {
	if d == nil || d.sender == nil {
		return errors.New("provider reset delivery unavailable")
	}
	message, err := d.renderer.providerPasswordReset(email, key, code)
	if err != nil {
		return err
	}
	return d.sender.send(ctx, message)
}

func (r registrationMailRenderer) providerActivation(name, email, key, code string, update bool) (brigMail, error) {
	templateName := "activation"
	if update {
		templateName = "update"
	}
	template, err := r.templates.selectTemplate("provider", templateName, r.templates.defaultLocale)
	if err != nil {
		return brigMail{}, err
	}
	urlValues := maps.Clone(r.branding)
	urlValues["key"], urlValues["code"] = key, code
	values := maps.Clone(r.branding)
	values["name"], values["email"], values["url"] = name, email, r.providerActivationURL.render(urlValues, false, r.warn)
	result := r.render(template, email, &name, values)
	result.headers = [][2]string{{"X-Zeta-Purpose", "ProviderActivation"}, {"X-Zeta-Key", key}, {"X-Zeta-Code", code}}
	return result, nil
}

func (r registrationMailRenderer) providerApproval(name, email string) (brigMail, error) {
	template, err := r.templates.selectTemplate("provider", "approval-confirm", r.templates.defaultLocale)
	if err != nil {
		return brigMail{}, err
	}
	values := maps.Clone(r.branding)
	values["name"], values["email"], values["homeUrl"] = name, email, r.providerHomeURL
	result := r.render(template, email, &name, values)
	result.headers = [][2]string{{"X-Zeta-Purpose", "ProviderApprovalConfirm"}}
	return result, nil
}

func (d *registrationMailDelivery) providerActivation(ctx context.Context, name, email, key, code string, update bool) error {
	if d == nil || d.sender == nil {
		return errors.New("provider activation delivery unavailable")
	}
	message, err := d.renderer.providerActivation(name, email, key, code, update)
	if err != nil {
		return err
	}
	return d.sender.send(ctx, message)
}

func (d *registrationMailDelivery) providerApproval(ctx context.Context, name, email string) error {
	if d == nil || d.sender == nil {
		return errors.New("provider approval delivery unavailable")
	}
	message, err := d.renderer.providerApproval(name, email)
	if err != nil {
		return err
	}
	return d.sender.send(ctx, message)
}
