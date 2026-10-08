package gundeck

import (
	"github.com/redis/go-redis/v9"
	"reflect"
	"testing"
)

func TestEndpointOwnershipIsUUIDOnlyUniqueAndSorted(t *testing.T) {
	users := endpointUsers(map[string]string{"CustomUserData": "99999999-9999-4999-8999-999999999999:invalid:88888888-8888-4888-8888-888888888888:99999999-9999-4999-8999-999999999999"})
	expected := []string{"88888888-8888-4888-8888-888888888888", "99999999-9999-4999-8999-999999999999"}
	if !reflect.DeepEqual(users, expected) {
		t.Fatalf("owners=%v", users)
	}
}
func TestEndpointMustMatchStoredTransportAndApp(t *testing.T) {
	token := PushToken{Transport: "GCM", App: "test"}
	if !endpointMatches("arn:aws:sns:eu-west-1:123456789012:endpoint/GCM/integration-test/device", token) {
		t.Fatal("valid endpoint rejected")
	}
	for _, arn := range []string{"arn:aws:sns:eu-west-1:123456789012:endpoint/APNS/integration-test/device", "arn:aws:sns:eu-west-1:123456789012:endpoint/GCM/integration-other/device", "not-an-arn"} {
		if endpointMatches(arn, token) {
			t.Fatalf("accepted %s", arn)
		}
	}
}
func TestClusterTLSUsesRedirectedNodeHostname(t *testing.T) {
	config := RedisEndpoint{Endpoint: Endpoint{Host: "127.0.0.1", Port: 6379}, Mode: "cluster", EnableTLS: true}
	client, err := openRedis(config, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	options := client.(*redis.ClusterClient).Options()
	if options.TLSConfig.ServerName != "" || options.TLSConfig.InsecureSkipVerify {
		t.Fatal("cluster must infer and verify actual node hostname")
	}
	config.Mode = "master"
	client, err = openRedis(config, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.(*redis.Client).Options().TLSConfig.ServerName != "127.0.0.1" {
		t.Fatal("master hostname lost")
	}
}
