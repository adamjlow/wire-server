// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type samlFinalizeStore interface {
	samlJudgeStore
	samlLoginStore
	idpByIssuer(context.Context, string, int, gocql.UUID) (*idpConfig, error)
	getVerdict(context.Context, string) (*verdictFormat, error)
}

func samlDecodeLenient(encoded string) []byte {
	var clean strings.Builder
	for _, ch := range []byte(encoded) {
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '+' || ch == '/' {
			clean.WriteByte(ch)
		}
	}
	decoded, _ := base64.RawStdEncoding.DecodeString(clean.String())
	return decoded
}

func samlResponse(ctx context.Context, s samlFinalizeStore, encoded string, team *gocql.UUID) ([]samlAssertion, *idpConfig, bool, error) {
	data := samlDecodeLenient(encoded)
	root, err := parseXMLNode(data)
	if err != nil || root.Name.Space != protocolNS || root.Name.Local != "Response" || root.attr("Version") != "2.0" {
		return nil, nil, false, &apiError{400, "bad-response-xml", "Bad response: XML parse error"}
	}
	if _, err = samlTime(root, "IssueInstant", true); err != nil || optionalXMLAttr(root, "ID") == nil {
		return nil, nil, false, &apiError{400, "bad-response-saml", "Bad response: invalid response attributes"}
	}
	issuer, err := samlIssuer(root)
	if err != nil {
		return nil, nil, false, &apiError{400, "bad-response-saml", "Bad response: no Issuer in AuthnResponse"}
	}
	unsigned := root.children(assertionNS, "Assertion")
	if len(unsigned) == 0 {
		return nil, nil, false, &apiError{400, "bad-response-saml", "Bad response: no assertions in AuthnResponse"}
	}
	for _, node := range unsigned {
		a, err := parseSAMLAssertion(node)
		if err != nil {
			if errors.Is(err, errSAMLXMLStructure) {
				return nil, nil, false, &apiError{400, "bad-response-xml", "Bad response: unexpected XML attribute"}
			}
			return nil, nil, false, &apiError{400, "bad-response-saml", "Bad response: SAML parse error"}
		}
		if a.Issuer != issuer {
			return nil, nil, false, &apiError{403, "bad-response-saml", "Bad response: IdP Issuer in AuthnResponse does not match AuthnRequest"}
		}
	}
	status := root.children(protocolNS, "Status")
	if len(status) != 1 {
		return nil, nil, false, &apiError{400, "bad-response-saml", "Bad response: SAML parse error"}
	}
	codes := status[0].children(protocolNS, "StatusCode")
	if len(codes) != 1 {
		return nil, nil, false, &apiError{400, "bad-response-saml", "Bad response: SAML parse error"}
	}
	success := codes[0].attr("Value") == "urn:oasis:names:tc:SAML:2.0:status:Success"
	version := 1
	var tid gocql.UUID
	if team != nil {
		version = 2
		tid = *team
	}
	idp, err := s.idpByIssuer(ctx, issuer, version, tid)
	if err != nil {
		return nil, nil, false, errors.Join(idpError(err), err)
	}
	if idp == nil {
		return nil, nil, false, errors.Join(idpError(errIdPNotFound), errIdPNotFound)
	}
	verified, err := verifiedAssertions(data, idp.Metadata.Certificates)
	if err != nil {
		return nil, nil, false, &apiError{400, "bad-response-signature", "Invalid SAML assertion signature"}
	}
	assertions := make([]samlAssertion, 0, len(verified))
	for _, node := range verified {
		a, err := parseSAMLAssertion(node)
		if err != nil {
			return nil, nil, false, &apiError{400, "bad-response-saml", "Bad response: SAML parse error"}
		}
		assertions = append(assertions, a)
	}
	return assertions, idp, success, nil
}

// Native web verdicts communicate through the title and postMessage payload;
// mobile verdicts replace literal variables and redirect without a success body.
func samlRenderVerdict(w http.ResponseWriter, format verdictFormat, uid gocql.UUID, cookie, label string, reasons []string) error {
	granted := label == ""
	if format.Mobile {
		if format.Success == nil || format.Error == nil {
			return errors.New("invalid mobile verdict")
		}
		location := *format.Error
		if granted {
			location = *format.Success
			location = samlSubstitute(location, "userid", uid.String())
			location = samlSubstitute(location, "cookie", cookie)
		} else {
			location = samlSubstitute(location, "label", label)
		}
		location, err := samlVerdictURI(location)
		if err != nil {
			errorLabel := "bad-failure-redirect"
			if granted {
				errorLabel = "bad-success-redirect"
			}
			return &apiError{400, errorLabel, "re-parsing the substituted URI failed"}
		}
		w.Header().Set("Location", location)
		if granted {
			w.Header().Set("Set-Cookie", cookie)
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(303)
		if !granted {
			body, _ := json.Marshal(reasons)
			_, _ = w.Write(body)
		}
		return nil
	}
	w.Header().Set("Content-Type", "text/html;charset=utf-8")
	body := `<head>  <title>wire:sso:success</title>   <script type="text/javascript">       const receiverOrigin = '*';       window.opener.postMessage({type: 'AUTH_SUCCESS'}, receiverOrigin);   </script></head>`
	if granted {
		w.Header().Set("Set-Cookie", cookie)
	} else {
		payload, _ := json.Marshal(map[string]any{"type": "AUTH_ERROR", "payload": map[string]any{"label": "forbidden", "errors": reasons}})
		body = `<head>  <title>wire:sso:error:` + nativeXMLEscape(label, false) + `</title>   <script type="text/javascript">       const receiverOrigin = '*';       window.opener.postMessage(` + string(payload) + `, receiverOrigin);   </script></head>`
	}
	w.WriteHeader(200)
	_, _ = w.Write([]byte(samlHTML(body)))
	return nil
}
func samlSubstitute(uri, name, value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(uri, "%24"+name, value), "$"+name, value)
}

// Spar pins uri-bytestring 0.3.3.1: paths reject unescaped whitespace,
// while lax query values accept Set-Cookie and serialize it percent-encoded.
// net/url alone accepts raw path spaces and leaves query spaces unescaped.
func samlVerdictURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() {
		return "", errors.New("invalid substituted URI")
	}
	path := raw[len(u.Scheme)+1:]
	path, _, _ = strings.Cut(path, "?")
	path, _, _ = strings.Cut(path, "#")
	if strings.HasPrefix(path, "//") {
		_, path, _ = strings.Cut(path[2:], "/")
	}
	for index := 0; index < len(path); index++ {
		ch := path[index]
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("/~._-%!$&'()*+,;=:@", rune(ch))) {
			return "", errors.New("invalid substituted URI path")
		}
	}
	query := []string{}
	encode := func(value string) string {
		return strings.ReplaceAll(url.QueryEscape(samlFormDecode(value)), "+", "%20")
	}
	for _, pair := range strings.Split(u.RawQuery, "&") {
		key, value, _ := strings.Cut(pair, "=")
		if key != "" {
			query = append(query, encode(key)+"="+encode(value))
		}
	}
	u.RawQuery, u.ForceQuery = strings.Join(query, "&"), false
	return u.String(), nil
}

func samlFailurePage(w http.ResponseWriter, err error, inputs url.Values) {
	var api *apiError
	var ttl *ttlError
	if errors.As(err, &ttl) {
		err = &apiError{400, "ttl-error", ttl.Error()}
	}
	if !errors.As(err, &api) {
		api = &apiError{500, "server-error", "Server Error"}
	}
	// Native error pages include client-supplied form inputs in a base64 debug
	// block. Preserve that response contract without logging the assertion.
	// Retain the actual lookup cause: the public HTTP error deliberately hides it.
	diagnostic := api.Message
	if errors.Is(err, errIdPNotFound) {
		diagnostic = "CustomError (IdpDbError IdpNotFound)"
	}
	fields := make([]string, 0, len(inputs))
	for name := range inputs {
		fields = append(fields, name)
	}
	sort.Strings(fields)
	var values []string
	for _, name := range fields {
		for _, value := range inputs[name] {
			values = append(values, "Input {iName = "+strconv.Quote(name)+", iValue = "+strconv.Quote(value)+"}")
		}
	}
	debug := base64.StdEncoding.EncodeToString([]byte("(" + diagnostic + ",[" + strings.Join(values, ",") + "])"))
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(api.Code)
	_, _ = w.Write([]byte(samlHTML("<head>\n  <title>wire:sso:error:" + nativeXMLEscape(api.Label, false) + "</title>\n</head>\n</body>\n  sorry, something went wrong :(<br>\n  please copy the following debug information to your clipboard and provide it when opening an issue in our customer support.<br><br>\n  <pre>" + debug + "</pre>\n</body>")))
}

// Native missing/incoherent verdict exceptions cross the outer WAI error
// boundary, which replaces inner SAML cache headers with generic JSON.
func samlFinalizeServerError(w http.ResponseWriter) {
	w.Header().Del("Cache-Control")
	w.Header().Del("Pragma")
	wireError(w, 500, "server-error", "Internal Server Error")
}

func serveSAMLFinalize(w http.ResponseWriter, r *http.Request, c Config, s samlFinalizeStore, p samlLoginPeers) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "sso" || parts[1] != "finalize-login" || len(parts) > 3 {
		return false
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	var team *gocql.UUID
	if len(parts) == 3 {
		parsed, err := gocql.ParseUUID(parts[2])
		if err != nil {
			domainError(w, &apiError{400, "bad-request", "Invalid team ID"})
			return true
		}
		team = &parsed
	}
	host := r.Header.Get("Z-Host")
	uri, err := responseURI(c, host, team)
	if err != nil {
		samlFailurePage(w, err, r.PostForm)
		return true
	}
	r.PostForm, err = parseSAMLForm(r)
	if err != nil {
		switch {
		case errors.Is(err, errSAMLFormType):
			w.Header()["Content-Type"] = nil
			w.WriteHeader(400)
			_, _ = w.Write([]byte("The content type of the request body is not in application/x-www-form-urlencoded or multipart/form-data"))
		case errors.Is(err, errSAMLFormWire):
			w.Header().Del("Cache-Control")
			w.Header().Del("Pragma")
			wireError(w, 400, "client-error", "Invalid Request")
		default:
			samlFinalizeServerError(w)
		}
		return true
	}
	values, ok := r.PostForm["SAMLResponse"]
	if !ok || len(values) == 0 {
		samlFailurePage(w, &apiError{400, "bad-response-saml", "Bad response: SAMLResponse form field missing from HTTP body"}, r.PostForm)
		return true
	}
	assertions, idp, success, err := samlResponse(r.Context(), s, values[0], team)
	if err != nil {
		samlFailurePage(w, err, r.PostForm)
		return true
	}
	verdict, err := judgeSAML(r.Context(), s, assertions, success, uri, uri, *c.MaxAssertionTTL, time.Now)
	if err == nil {
		err = checkIdPCertificates(c, idp.Metadata)
	}
	if err != nil {
		samlFailurePage(w, err, r.PostForm)
		return true
	}
	for _, reason := range verdict.Reasons {
		if reason.Kind == "no-in-response-to" {
			w.Header().Set("Location", "/sso/initiate-login/"+idp.ID.String())
			w.WriteHeader(303)
			return true
		}
	}
	request := ""
	for _, assertion := range assertions {
		id, err := assertion.requestID()
		if err != nil {
			samlFailurePage(w, &apiError{400, "server-error-unsupported-saml", "The IdP needs to provide an InResponseTo attribute in the assertion: missing or incoherent requestIDs"}, r.PostForm)
			return true
		}
		if request != "" && request != *id {
			samlFinalizeServerError(w)
			return true
		}
		request = *id
	}
	format, err := s.getVerdict(r.Context(), request)
	if err != nil {
		domainError(w, err)
		return true
	}
	if format == nil {
		samlFinalizeServerError(w)
		return true
	}
	var uid gocql.UUID
	cookie, label := "", ""
	reasons := []string{}
	if verdict.User == nil {
		label = "forbidden"
		for _, reason := range verdict.Reasons {
			reasons = append(reasons, reason.Message)
		}
	} else {
		var domain *string
		if host != "" {
			domain = &host
		}
		uid, cookie, err = samlLoginUser(r.Context(), c, s, p, *idp, *verdict.User, domain, format.Label, r.Header.Get("Request-Id"))
		if err != nil {
			var api *apiError
			if !errors.As(err, &api) {
				api = &apiError{500, "server-error", "Server Error"}
			}
			label = api.Label
			reasons = []string{api.Message}
		}
	}
	if err = samlRenderVerdict(w, *format, uid, cookie, label, reasons); err != nil {
		samlFailurePage(w, err, r.PostForm)
	}
	return true
}
