// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"time"
)

var errSAMLAssertion = errors.New("invalid SAML assertion")

const bearerMethod = "urn:oasis:names:tc:SAML:2.0:cm:bearer"

type samlConfirmation struct {
	Request      *string
	Recipient    string
	NotBefore    *time.Time
	NotOnOrAfter time.Time
}
type samlAuthnStatement struct {
	Instant    time.Time
	SessionEnd *time.Time
}
type samlConditions struct {
	NotBefore, NotOnOrAfter *time.Time
	Audiences               [][]string
}
type samlAssertion struct {
	ID, Issuer    string
	Instant       time.Time
	Subject       samlNameID
	Confirmations []*samlConfirmation
	Conditions    *samlConditions
	Statements    []samlAuthnStatement
}

func samlTime(n *xmlNode, attr string, required bool) (*time.Time, error) {
	text := optionalXMLAttr(n, attr)
	if text == nil {
		if required {
			return nil, errSAMLAssertion
		}
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339Nano, *text)
	if err != nil {
		return nil, errSAMLAssertion
	}
	return &value, nil
}
func singleSAMLChild(n *xmlNode, local string) (*xmlNode, error) {
	children := n.children(assertionNS, local)
	if len(children) != 1 {
		return nil, errSAMLAssertion
	}
	return children[0], nil
}
func samlIssuer(n *xmlNode) (string, error) {
	issuer, err := singleSAMLChild(n, "Issuer")
	if err != nil || len(issuer.Children) != 0 || !absoluteURI(issuer.Text) {
		return "", errSAMLAssertion
	}
	return issuer.Text, nil
}

func parseSAMLAssertion(n *xmlNode) (samlAssertion, error) {
	var a samlAssertion
	if n.Name.Space != assertionNS || n.Name.Local != "Assertion" || n.attr("Version") != "2.0" || optionalXMLAttr(n, "ID") == nil || len(n.children(assertionNS, "Advice")) > 0 {
		return a, errSAMLAssertion
	}
	a.ID = n.attr("ID")
	instant, err := samlTime(n, "IssueInstant", true)
	if err != nil {
		return a, err
	}
	a.Instant = *instant
	a.Issuer, err = samlIssuer(n)
	if err != nil {
		return a, err
	}
	subject, err := singleSAMLChild(n, "Subject")
	if err != nil {
		return a, err
	}
	name, err := singleSAMLChild(subject, "NameID")
	if err != nil {
		return a, err
	}
	a.Subject, err = parseNameIDNode(name)
	if err != nil {
		return a, err
	}
	if len(subject.children(assertionNS, "BaseID")) > 0 || len(subject.children(assertionNS, "EncryptedID")) > 0 {
		return a, errSAMLAssertion
	}
	for _, confirmation := range subject.children(assertionNS, "SubjectConfirmation") {
		if confirmation.attr("Method") != bearerMethod {
			return a, errSAMLAssertion
		}
		// Native importSubjectConfirmation rejects an embedded identifier.
		for _, kind := range []string{"NameID", "BaseID", "EncryptedID"} {
			if len(confirmation.children(assertionNS, kind)) > 0 {
				return a, errSAMLAssertion
			}
		}
		data := confirmation.children(assertionNS, "SubjectConfirmationData")
		if len(data) > 1 {
			return a, errSAMLAssertion
		}
		if len(data) == 0 {
			a.Confirmations = append(a.Confirmations, nil)
			continue
		}
		d := data[0]
		end, err := samlTime(d, "NotOnOrAfter", true)
		if err != nil {
			return a, err
		}
		before, err := samlTime(d, "NotBefore", false)
		if err != nil {
			return a, err
		}
		recipient := d.attr("Recipient")
		if !absoluteURI(recipient) {
			return a, errSAMLAssertion
		}
		if address := optionalXMLAttr(d, "Address"); address != nil && net.ParseIP(*address) == nil {
			return a, errSAMLAssertion
		}
		a.Confirmations = append(a.Confirmations, &samlConfirmation{Request: optionalXMLAttr(d, "InResponseTo"), Recipient: recipient, NotBefore: before, NotOnOrAfter: *end})
	}
	conditions := n.children(assertionNS, "Conditions")
	if len(conditions) > 1 {
		return a, errSAMLAssertion
	}
	if len(conditions) == 1 {
		c := conditions[0]
		a.Conditions = &samlConditions{Audiences: [][]string{}}
		a.Conditions.NotBefore, err = samlTime(c, "NotBefore", false)
		if err != nil {
			return a, err
		}
		a.Conditions.NotOnOrAfter, err = samlTime(c, "NotOnOrAfter", false)
		if err != nil {
			return a, err
		}
		for _, condition := range c.Children {
			if condition.Name.Space != assertionNS {
				return a, errSAMLAssertion
			}
			switch condition.Name.Local {
			case "OneTimeUse":
			case "AudienceRestriction":
				audiences := []string{}
				for _, audience := range condition.children(assertionNS, "Audience") {
					if len(audience.Children) != 0 || !absoluteURI(audience.Text) {
						return a, errSAMLAssertion
					}
					audiences = append(audiences, audience.Text)
				}
				if len(audiences) == 0 {
					return a, errSAMLAssertion
				}
				// Native importConditions prepends each parsed restriction.
				a.Conditions.Audiences = append([][]string{audiences}, a.Conditions.Audiences...)
			default:
				return a, errSAMLAssertion
			}
		}
	}
	for _, statement := range n.Children {
		if statement.Name.Space != assertionNS {
			continue
		}
		switch statement.Name.Local {
		case "AuthnStatement":
			instant, err := samlTime(statement, "AuthnInstant", true)
			if err != nil {
				return a, err
			}
			end, err := samlTime(statement, "SessionNotOnOrAfter", false)
			if err != nil {
				return a, err
			}
			localities := statement.children(assertionNS, "SubjectLocality")
			if len(localities) > 1 {
				return a, errSAMLAssertion
			}
			if len(localities) == 1 {
				if address := optionalXMLAttr(localities[0], "Address"); address != nil && net.ParseIP(*address) == nil {
					return a, errSAMLAssertion
				}
			}
			a.Statements = append(a.Statements, samlAuthnStatement{*instant, end})
		case "AuthzDecisionStatement", "Statement":
			return a, errSAMLAssertion
			// Attribute statements have no identification semantics in native Spar.
		}
	}
	if len(a.Statements) == 0 {
		return a, errSAMLAssertion
	}
	return a, nil
}

func (a samlAssertion) requestID() (*string, error) {
	ids := map[string]bool{}
	for _, confirmation := range a.Confirmations {
		if confirmation != nil && confirmation.Request != nil {
			ids[*confirmation.Request] = true
		}
	}
	if len(ids) != 1 {
		return nil, errors.New("missing or incoherent request ID")
	}
	for id := range ids {
		return &id, nil
	}
	return nil, errors.New("missing request ID")
}
func (a samlAssertion) endOfLife() time.Time {
	if a.Conditions != nil && a.Conditions.NotOnOrAfter != nil {
		return *a.Conditions.NotOnOrAfter
	}
	return a.Instant.Add(30 * 24 * time.Hour)
}

type samlJudgeStore interface {
	authRequestIssuer(context.Context, string) (*string, error)
	assertionAlive(context.Context, string) (bool, error)
	storeAssertion(context.Context, string, time.Time, time.Time, TTL) error
	deleteAuthRequest(context.Context, string) error
}
type samlDenied struct{ Kind, Message string }
type samlVerdict struct {
	User    *samlUserRef
	Reasons []samlDenied
}

func (v *samlVerdict) deny(kind, message string) {
	v.Reasons = append(v.Reasons, samlDenied{kind, message})
}
func (v *samlVerdict) giveup(kind, message string) {
	v.Reasons = append([]samlDenied{{kind, message}}, v.Reasons...)
	v.User = nil
}

func samlEarlier(early, late time.Time) bool { return early.Before(late.Add(time.Minute)) }
func samlNoLater(early, late time.Time) bool { return !early.After(late.Add(time.Minute)) }
func samlRenderTime(value time.Time) string  { return value.UTC().Format(time.RFC3339Nano) }

func judgeSAML(ctx context.Context, store samlJudgeStore, assertions []samlAssertion, statusSuccess bool, audience, recipient string, maximum TTL, now func() time.Time) (samlVerdict, error) {
	v := samlVerdict{Reasons: []samlDenied{}}
	if !statusSuccess {
		v.deny("status-failure", "status: failure")
	}
	refs := []samlUserRef{}
	for _, a := range assertions {
		request, err := a.requestID()
		if err != nil {
			v.giveup("no-in-response-to", "authentication response without authentication request ID")
			return v, nil
		}
		issuer, err := store.authRequestIssuer(ctx, *request)
		if err != nil {
			return v, err
		}
		if issuer == nil {
			v.giveup("bad-in-response-to", fmt.Sprintf("bad InResponseTo attribute(s): response: ID {fromID = %q}", *request))
			return v, nil
		}
		if *issuer != a.Issuer {
			v.giveup("issuer-mismatch", "mismatching Issuers: in header: "+encodedIssuer(*issuer)+", in Assertion: "+encodedIssuer(a.Issuer))
			return v, nil
		}
		t := now()
		if !samlEarlier(a.Instant, t) {
			v.deny("assertion-issue-instant", "IssueInstant in Assertion must be older than "+samlRenderTime(t)+", but is "+samlRenderTime(a.Instant))
		}
		alive, err := store.assertionAlive(ctx, a.ID)
		if err != nil {
			return v, err
		}
		if alive {
			v.deny("status-failure", "status: failure")
		} else {
			if err = store.storeAssertion(ctx, a.ID, now(), a.endOfLife(), maximum); err != nil {
				return v, err
			}
		}
		if c := a.Conditions; c != nil {
			t = now()
			if c.NotBefore != nil && !samlNoLater(*c.NotBefore, t) {
				v.deny("condition-not-before", "Condition only valid starting "+samlRenderTime(*c.NotBefore))
			}
			if c.NotOnOrAfter != nil && !samlEarlier(t, *c.NotOnOrAfter) {
				v.deny("condition-expired", "Condition expired at "+samlRenderTime(*c.NotOnOrAfter))
			}
			for _, restriction := range c.Audiences {
				matched := false
				for _, target := range restriction {
					if target == audience {
						matched = true
					}
				}
				if !matched {
					v.deny("audience-mismatch", "Audience mismatch: we are "+audience+", they expect one of ["+strings.Join(restriction, ", ")+"]")
				}
			}
		}
		for _, confirmation := range a.Confirmations {
			if a.Conditions == nil || len(a.Conditions.Audiences) == 0 {
				v.deny("missing-audience", "AudienceRestriction required")
			}
			if confirmation == nil {
				continue
			}
			if confirmation.Recipient != recipient {
				v.deny("bad-recipient", "bad Recipient: we are "+recipient+", they expected "+confirmation.Recipient)
			}
			t = now()
			if !samlEarlier(t, confirmation.NotOnOrAfter) {
				v.deny("confirmation-expired", "SubjectConfirmation expired at "+samlRenderTime(confirmation.NotOnOrAfter))
			}
			if confirmation.NotBefore != nil && !samlNoLater(*confirmation.NotBefore, t) {
				v.deny("confirmation-not-before", "SubjectConfirmation only valid starting "+samlRenderTime(*confirmation.NotBefore))
			}
		}
		if len(a.Confirmations) == 0 {
			v.deny("no-bearer-confirmation", "No Bearer SubjectConfirmation")
		}
		if len(a.Statements) == 0 {
			v.deny("no-authn-statement", "no AuthnStatement")
		}
		for _, statement := range a.Statements {
			t = now()
			if !samlEarlier(statement.Instant, t) {
				v.deny("authn-issue-instant", "IssueInstant in AuthnStatement must be older than "+samlRenderTime(t)+", but is "+samlRenderTime(statement.Instant))
			}
			if statement.SessionEnd != nil && !samlEarlier(now(), *statement.SessionEnd) {
				v.deny("authn-expired", "AuthnStatement expired at "+samlRenderTime(*statement.SessionEnd))
			}
		}
		if err = store.deleteAuthRequest(ctx, *request); err != nil {
			return v, err
		}
		ref := samlUserRef{a.Issuer, a.Subject}
		exists := false
		for _, other := range refs {
			if reflect.DeepEqual(ref, other) {
				exists = true
			}
		}
		if !exists {
			refs = append(refs, ref)
		}
	}
	// Native runJudgeT returns accumulated ordinary denials before the folded
	// verdict. Conflicting identities must not replace those error messages.
	if len(refs) != 1 && len(v.Reasons) == 0 {
		v.giveup("bad-user-refs", "bad user refs: conflicting or missing assertions")
		return v, nil
	}
	if len(v.Reasons) == 0 {
		v.User = &refs[0]
	}
	return v, nil
}

func encodedIssuer(issuer string) string {
	return `<Issuer` + nativeNamespaces + ` xmlns="` + assertionNS + `">` + nativeXMLEscape(issuer, false) + `</Issuer>`
}
