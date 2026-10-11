// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
)

func nativeNewClientDate(at time.Time, locale string) string {
	at = at.UTC()
	days := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	months := []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	switch strings.Split(locale, "-")[0] {
	case "de":
		days = []string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
		months = []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
	case "fr":
		days = []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
		months = []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	}
	return fmt.Sprintf("%s %2d %s %04d, %02d:%02d - UTC", days[at.Weekday()], at.Day(), months[at.Month()-1], at.Year(), at.Hour(), at.Minute())
}

func (r registrationMailRenderer) newClient(profile selfProfile, client storedClient) (brigMail, error) {
	if profile.Email == nil {
		return brigMail{}, errors.New("new client mail identity unavailable")
	}
	template, err := r.templates.selectTemplate("user", "new-client", profile.Locale)
	if err != nil {
		return brigMail{}, err
	}
	values := maps.Clone(r.branding)
	values["name"], values["label"], values["model"], values["date"] = profile.Name, "N/A", "N/A", nativeNewClientDate(client.Created, profile.Locale)
	if client.Label != nil {
		values["label"] = *client.Label
	}
	if client.Model != nil {
		values["model"] = *client.Model
	}
	mail := r.render(template, *profile.Email, &profile.Name, values)
	mail.headers = [][2]string{{"X-Zeta-Purpose", "NewDevice"}}
	return mail, nil
}

func (d *registrationMailDelivery) newClient(ctx context.Context, profile selfProfile, client storedClient) error {
	if d == nil || d.sender == nil {
		return errors.New("mail delivery unavailable")
	}
	message, err := d.renderer.newClient(profile, client)
	if err != nil {
		return err
	}
	return d.sender.send(ctx, message)
}
