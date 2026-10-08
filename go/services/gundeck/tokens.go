package gundeck

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/smithy-go"
)

var transports = []string{"GCM", "APNS", "APNS_SANDBOX", "APNS_VOIP", "APNS_VOIP_SANDBOX"}

type PushToken struct {
	Transport string `json:"transport"`
	App       string `json:"app"`
	Token     string `json:"token"`
	Client    string `json:"client"`
}
type pushAddress struct {
	User string
	PushToken
	ARN        string
	Connection []byte
}
type tokenError struct {
	status         int
	label, message string
}

func (e *tokenError) Error() string { return e.label }
func transportCode(t string) int {
	for i, v := range transports {
		if v == t {
			return i
		}
	}
	return -1
}
func openAWS(ctx context.Context, c Config, clientHTTP *http.Client) (aws.Config, *sns.Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(c.AWS.Region), awsconfig.WithHTTPClient(clientHTTP))
	if err != nil {
		return cfg, nil, fmt.Errorf("AWS initialization failed")
	}
	client := sns.NewFromConfig(cfg, func(o *sns.Options) {
		if c.AWS.SNS != "" {
			o.BaseEndpoint = aws.String(c.AWS.SNS)
		}
		o.RetryMaxAttempts = 3
	})
	return cfg, client, nil
}
func serviceError(err error) (string, string) {
	var api smithy.APIError
	if errors.As(err, &api) {
		return api.ErrorCode(), api.ErrorMessage()
	}
	return "", ""
}
func endpointNotFound(err error) bool {
	code, _ := serviceError(err)
	return code == "NotFound" || code == "NotFoundException"
}
func (s *Server) lookupTokens(ctx context.Context, user string, all bool) ([]pushAddress, error) {
	uid, _ := gocql.ParseUUID(user)
	query := s.query(ctx, "SELECT transport, app, ptoken, arn, connection, client FROM user_push WHERE usr = ?", uid)
	if all {
		query.Consistency(gocql.All)
	}
	iter := query.Iter()
	addresses := []pushAddress{}
	legacy := []pushAddress{}
	for {
		var transport int
		var app, token string
		var arn, client *string
		var connection []byte
		if !iter.Scan(&transport, &app, &token, &arn, &connection, &client) {
			break
		}
		if transport < 0 || transport >= len(transports) {
			_ = iter.Close()
			return nil, fmt.Errorf("invalid stored transport")
		}
		address := pushAddress{User: user, PushToken: PushToken{Transport: transports[transport], App: app, Token: token}, Connection: connection}
		if arn == nil || client == nil {
			legacy = append(legacy, address)
			continue
		}
		address.ARN = *arn
		if !clientValid(*client) {
			_ = iter.Close()
			return nil, fmt.Errorf("invalid stored token client")
		}
		address.Client = canonicalClient(*client)
		addresses = append(addresses, address)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	for _, address := range legacy {
		if err := s.deleteTokenRow(ctx, address); err != nil {
			return nil, err
		}
	}
	// Cassandra's ascending clustering order is reversed by Haskell's fold.
	for i, j := 0, len(addresses)-1; i < j; i, j = i+1, j-1 {
		addresses[i], addresses[j] = addresses[j], addresses[i]
	}
	return addresses, nil
}
func (s *Server) insertToken(ctx context.Context, address pushAddress) error {
	uid, _ := gocql.ParseUUID(address.User)
	return s.query(ctx, "INSERT INTO user_push (usr, transport, app, ptoken, arn, connection, client) VALUES (?, ?, ?, ?, ?, ?, ?)", uid, transportCode(address.Transport), address.App, address.Token, address.ARN, address.Connection, address.Client).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
}
func (s *Server) deleteTokenRow(ctx context.Context, address pushAddress) error {
	uid, _ := gocql.ParseUUID(address.User)
	return s.query(ctx, "DELETE FROM user_push WHERE usr = ? AND transport = ? AND app = ? AND ptoken = ?", uid, transportCode(address.Transport), address.App, address.Token).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
}
func endpointUsers(attrs map[string]string) []string {
	users := map[string]bool{}
	for _, user := range strings.Split(attrs["CustomUserData"], ":") {
		if uuidValid(user) {
			users[strings.ToLower(user)] = true
		}
	}
	result := []string{}
	for user := range users {
		result = append(result, user)
	}
	sort.Strings(result)
	return result
}
func (s *Server) endpoint(ctx context.Context, arn string) (map[string]string, error) {
	response, err := s.sns.GetEndpointAttributes(ctx, &sns.GetEndpointAttributesInput{EndpointArn: aws.String(arn)})
	if endpointNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, ok := response.Attributes["Token"]; !ok {
		return nil, fmt.Errorf("endpoint missing token")
	}
	return response.Attributes, nil
}
func (s *Server) updateEndpoint(ctx context.Context, address pushAddress, users []string) error {
	unique := map[string]bool{}
	for _, user := range users {
		unique[user] = true
	}
	users = users[:0]
	for user := range unique {
		users = append(users, user)
	}
	sort.Strings(users)
	_, err := s.sns.SetEndpointAttributes(ctx, &sns.SetEndpointAttributesInput{EndpointArn: aws.String(address.ARN), Attributes: map[string]string{"Token": address.Token, "Enabled": "true", "CustomUserData": strings.Join(users, ":")}})
	code, message := serviceError(err)
	if code == "InvalidParameter" && strings.HasPrefix(message, "Invalid parameter: Attributes Reason: Invalid value for attribute: CustomUserData: must be at most 2048 bytes long in UTF-8 encoding") {
		return &tokenError{413, "metadata-too-long", "Tried to add token to endpoint resulting in metadata length > 2048"}
	}
	return err
}
func (s *Server) createEndpoint(ctx context.Context, address pushAddress) (string, bool, error) {
	arn := "arn:aws:sns:" + s.Config.AWS.Region + ":" + s.Config.AWS.Account + ":app/" + address.Transport + "/" + s.Config.AWS.ArnEnv + "-" + address.App
	response, err := s.sns.CreatePlatformEndpoint(ctx, &sns.CreatePlatformEndpointInput{PlatformApplicationArn: aws.String(arn), Token: aws.String(address.Token), CustomUserData: aws.String(address.User), Attributes: map[string]string{"Enabled": "true"}})
	if err == nil {
		if response.EndpointArn == nil {
			return "", false, fmt.Errorf("endpoint missing ARN")
		}
		return *response.EndpointArn, true, nil
	}
	code, message := serviceError(err)
	prefix := "Invalid parameter: Token Reason: Endpoint "
	suffix := " already exists with the same Token, but different attributes."
	if code == "InvalidParameter" && strings.HasPrefix(message, prefix) && strings.HasSuffix(message, suffix) {
		existing := strings.TrimSuffix(strings.TrimPrefix(message, prefix), suffix)
		if !strings.HasPrefix(existing, "arn:aws:sns:") {
			return "", false, fmt.Errorf("invalid endpoint ARN")
		}
		return existing, false, nil
	}
	if code == "InvalidParameter" && (strings.HasPrefix(message, "Invalid parameter: Token Reason: must be at most 8192 bytes long in UTF-8 encoding") || strings.HasPrefix(message, "Invalid parameter: Token Reason: iOS device tokens must be no more than 400 hexadecimal characters")) {
		return "", false, &tokenError{413, "token-too-long", "Push token length must be < 8192 for GCM or 400 for APNS"}
	}
	if code == "InvalidParameter" && strings.HasPrefix(message, "Invalid parameter: Token") {
		return "", false, &tokenError{404, "invalid-token", "Invalid push token"}
	}
	if endpointNotFound(err) || code == "AuthorizationError" || code == "AuthorizationErrorException" {
		return "", false, &tokenError{404, "app-not-found", "App does not exist"}
	}
	return "", false, err
}
func endpointMatches(arn string, token PushToken) bool {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[2] != "sns" {
		return false
	}
	topic := strings.Split(parts[5], "/")
	if len(topic) != 4 || topic[0] != "endpoint" || topic[1] != token.Transport {
		return false
	}
	// Upstream ARN parser treats the final hyphen-separated component as app name.
	app := strings.Split(topic[2], "-")
	return app[len(app)-1] == token.App
}
func (s *Server) registerToken(ctx context.Context, user, connection string, token PushToken) error {
	if !s.acquireBudget(1) {
		return &tokenError{413, "sns-thread-budget-reached", "Too many concurrent calls to SNS; is SNS down?"}
	}
	defer s.releaseBudget(1)
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	addresses, err := s.lookupTokens(ctx, user, false)
	if err != nil {
		return err
	}
	old := []pushAddress{}
	current := ""
	for _, address := range addresses {
		if address.Transport == token.Transport && address.App == token.App && address.Client == token.Client {
			if address.Token == token.Token {
				current = address.ARN
			} else {
				old = append(old, address)
			}
		}
	}
	address := pushAddress{User: user, PushToken: token, Connection: []byte(connection), ARN: current}
	for attempt := 0; attempt < 3; attempt++ {
		if address.ARN == "" {
			var fresh bool
			address.ARN, fresh, err = s.createEndpoint(ctx, address)
			if err != nil {
				return err
			}
			if fresh {
				if err = s.insertToken(ctx, address); err != nil {
					return err
				}
				return s.deleteTokens(ctx, old, &address)
			}
		}
		attrs, err := s.endpoint(ctx, address.ARN)
		if err != nil {
			return err
		}
		if attrs == nil {
			address.ARN = ""
			continue
		}
		if !endpointMatches(address.ARN, token) {
			return fmt.Errorf("endpoint does not match token")
		}
		users := append(endpointUsers(attrs), user)
		if err = s.updateEndpoint(ctx, address, users); err != nil {
			if endpointNotFound(err) {
				address.ARN = ""
				continue
			}
			return err
		}
		if err = s.insertToken(ctx, address); err != nil {
			return err
		}
		return s.deleteTokens(ctx, old, &address)
	}
	return fmt.Errorf("endpoint remained inconsistent")
}
func (s *Server) deleteTokens(ctx context.Context, addresses []pushAddress, replacement *pushAddress) error {
	for _, address := range addresses {
		if err := s.deleteTokenRow(ctx, address); err != nil {
			return err
		}
		attrs, err := s.endpoint(ctx, address.ARN)
		if err != nil {
			return err
		}
		if attrs == nil {
			continue
		}
		owners := endpointUsers(attrs)
		remaining := []string{}
		for _, user := range owners {
			if user != address.User {
				remaining = append(remaining, user)
			}
		}
		if len(remaining) > 0 && replacement == nil {
			if err = s.updateEndpoint(ctx, address, remaining); err != nil {
				return err
			}
			continue
		}
		if len(remaining) > 0 && replacement != nil {
			for _, user := range remaining {
				shared, err := s.lookupTokens(ctx, user, false)
				if err != nil {
					return err
				}
				for _, other := range shared {
					if other.ARN != address.ARN {
						continue
					}
					// Preserve the same source connection/client used by Haskell's migration.
					migrated := address
					migrated.User = user
					migrated.Token = replacement.Token
					migrated.ARN = replacement.ARN
					if err = s.insertToken(ctx, migrated); err != nil {
						return err
					}
					if err = s.deleteTokenRow(ctx, other); err != nil {
						return err
					}
				}
			}
			if err = s.updateEndpoint(ctx, *replacement, owners); err != nil {
				return err
			}
		}
		if _, err = s.sns.DeleteEndpoint(ctx, &sns.DeleteEndpointInput{EndpointArn: aws.String(address.ARN)}); err != nil {
			return err
		}
	}
	return nil
}
func tokenFailure(w http.ResponseWriter, err error) {
	var mapped *tokenError
	if errors.As(err, &mapped) {
		failure(w, mapped.status, mapped.label, mapped.message)
		return
	}
	failure(w, 500, "server-error", "Server Error")
}
func (s *Server) tokensHTTP(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method == http.MethodPost && !jsonMediaType(r) {
		plain(w, 415, "")
		return
	}
	user := ""
	if strings.HasPrefix(path, "/i/push-tokens/") {
		user = strings.TrimPrefix(path, "/i/push-tokens/")
		if !uuidValid(user) {
			plain(w, 400, "invalid UUID")
			return
		}
		user = strings.ToLower(user)
	} else {
		var ok bool
		user, ok = principal(w, r)
		if !ok {
			return
		}
	}
	if r.Method == http.MethodPost && path == "/push/tokens" {
		connection, exists := r.Header["Z-Connection"]
		if !exists || len(connection) == 0 {
			plain(w, 400, "Header Z-Connection is required")
			return
		}
		var token PushToken
		if err := decodeJSON(r.Body, &token); err != nil {
			plain(w, 400, "Invalid push token JSON")
			return
		}
		if transportCode(token.Transport) < 0 || !clientValid(token.Client) || token.App == "" {
			plain(w, 400, "Invalid push token")
			return
		}
		token.Client = canonicalClient(token.Client)
		if token.Transport == "APNS_VOIP" || token.Transport == "APNS_VOIP_SANDBOX" {
			failure(w, 400, "apns-voip-not-supported", "Adding APNS_VOIP tokens is not supported")
			return
		}
		if err := s.registerToken(r.Context(), user, connection[0], token); err != nil {
			tokenFailure(w, err)
			return
		}
		w.Header().Set("Location", "/push/tokens/"+url.PathEscape(token.Token))
		jsonResponse(w, 201, token)
		return
	}
	all := strings.HasPrefix(path, "/i/push-tokens/")
	if all {
		user = strings.TrimPrefix(path, "/i/push-tokens/")
		if !uuidValid(user) {
			plain(w, 400, "invalid UUID")
			return
		}
	}
	addresses, err := s.lookupTokens(r.Context(), user, all)
	if err != nil {
		tokenFailure(w, err)
		return
	}
	if r.Method == http.MethodGet {
		tokens := []PushToken{}
		for _, address := range addresses {
			tokens = append(tokens, address.PushToken)
		}
		jsonResponse(w, 200, map[string]any{"tokens": tokens})
		return
	}
	token := strings.TrimPrefix(path, "/push/tokens/")
	selected := []pushAddress{}
	for _, address := range addresses {
		if address.Token == token {
			selected = append(selected, address)
		}
	}
	if len(selected) == 0 {
		failure(w, 404, "not-found", "Push token not found")
		return
	}
	s.tokenMu.Lock()
	err = s.deleteTokens(r.Context(), selected, nil)
	s.tokenMu.Unlock()
	if err != nil {
		tokenFailure(w, err)
		return
	}
	emptyJSON(w, 200)
}
