// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"maps"
)

func (r registrationMailRenderer) deletion(profile selfProfile, key, code string) (brigMail, error) {
	if profile.Email == nil {
		return brigMail{}, errors.New("deletion mail identity unavailable")
	}
	template, err := r.templates.selectTemplate("user", "deletion", profile.Locale)
	if err != nil {
		return brigMail{}, err
	}
	urlValues := maps.Clone(r.branding)
	urlValues["key"], urlValues["code"] = key, code
	values := maps.Clone(r.branding)
	values["url"] = r.deletionURL.render(urlValues, false, r.warn)
	values["email"], values["name"] = *profile.Email, profile.Name
	message := r.render(template, *profile.Email, &profile.Name, values)
	message.headers = [][2]string{{"X-Zeta-Purpose", "Delete"}, {"X-Zeta-Key", key}, {"X-Zeta-Code", code}}
	return message, nil
}

func (d *registrationMailDelivery) deletion(ctx context.Context, profile selfProfile, key, code string) error {
	if d == nil || d.sender == nil {
		return errors.New("mail delivery unavailable")
	}
	message, err := d.renderer.deletion(profile, key, code)
	if err != nil {
		return err
	}
	return d.sender.send(ctx, message)
}
