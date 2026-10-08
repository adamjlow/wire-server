package gundeck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigurationRejectsMissingTLSFlagsAndInvalidAWSEndpoints(t *testing.T) {
	valid := func() map[string]any {
		return map[string]any{
			"gundeck":   map[string]any{"host": "127.0.0.1", "port": 8086},
			"brig":      map[string]any{"host": "127.0.0.1", "port": 8082},
			"redis":     map[string]any{"host": "127.0.0.1", "port": 6379, "connectionMode": "master", "enableTls": false, "insecureSkipVerifyTls": false},
			"cassandra": map[string]any{"endpoint": map[string]any{"host": "127.0.0.1", "port": 9042}, "keyspace": "synthetic"},
			"aws":       map[string]any{"account": "123456789012", "region": "eu-west-1", "arnEnv": "synthetic", "queueName": "synthetic", "snsEndpoint": "https://sns.example.invalid", "sqsEndpoint": "https://sqs.example.invalid"},
			"rabbitmq":  map[string]any{"host": "127.0.0.1", "port": 5672, "vHost": "synthetic"},
			"settings":  map[string]any{"httpPoolSize": 32, "notificationTTL": 60, "disabledAPIVersions": []string{}, "consumableNotifications": false},
			"logLevel":  "Warn",
		}
	}
	path := filepath.Join(t.TempDir(), "config.json")
	load := func(fields map[string]any) error {
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		_, err = LoadConfig(path)
		return err
	}
	if err := load(valid()); err != nil {
		t.Fatal("valid source-shaped config rejected", err)
	}
	for _, section := range []string{"redis", "redisAdditionalWrite"} {
		for _, key := range []string{"enableTls", "insecureSkipVerifyTls"} {
			fields := valid()
			if section == "redisAdditionalWrite" {
				fields[section] = valid()["redis"]
			}
			delete(fields[section].(map[string]any), key)
			if err := load(fields); err == nil {
				t.Fatalf("accepted missing %s.%s", section, key)
			}
		}
	}
	for _, key := range []string{"snsEndpoint", "sqsEndpoint"} {
		for _, value := range []string{"", "file:///synthetic", "https:///missing-host", "not-a-url"} {
			fields := valid()
			fields["aws"].(map[string]any)[key] = value
			if err := load(fields); err == nil {
				t.Fatalf("accepted invalid %s", key)
			}
		}
	}
}
