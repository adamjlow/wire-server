// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestStompNativeScalarAdmission(t *testing.T) {
	for _, test := range []struct {
		host, port, tls string
		valid           bool
		want            int
	}{
		{"localhost", "61613", "false", true, 61613},
		{"localhost", "61613.0", "yes", true, 61613},
		{"localhost", "6.1613e4", "OFF", true, 61613},
		{"localhost", "061613", "false", true, 61613},
		{"localhost", "0xF0AD", "false", true, 61613},
		{"localhost", "9223372036854775807.0", "false", true, int(^uint(0) >> 1)},
		{"localhost", "-9223372036854775808.0", "false", true, -int(^uint(0)>>1) - 1},
		{"\"123\"", "61613", "false", true, 61613},
		{"!!str on", "61613", "false", true, 61613},
		{"123", "61613", "false", false, 0},
		{"on", "61613", "false", false, 0},
		{"localhost", "61613.5", "false", false, 0},
		{"localhost", "\"61613\"", "false", false, 0},
		{"localhost", "61613", "\"false\"", false, 0},
		{"localhost", "61613", "1", false, 0},
		{"localhost", "61613", "null", false, 0},
		{"localhost", "9223372036854775808.0", "false", false, 0},
		{"localhost", "1e1000000", "false", false, 0},
		{"localhost", "0b111", "false", false, 0},
		{"localhost", "61_613", "false", false, 0},
	} {
		t.Run(test.host+"/"+test.port+"/"+test.tls, func(t *testing.T) {
			var c internalStompConfig
			err := yaml.Unmarshal([]byte("stomHost: "+test.host+"\nstomPort: "+test.port+"\nstomTls: "+test.tls+"\n"), &c)
			if (err == nil) != test.valid || err == nil && c.Port != test.want {
				t.Fatalf("native admission=%v port=%d; want admission=%v port=%d", err == nil, c.Port, test.valid, test.want)
			}
		})
	}
}

func TestStompNativeExactIntegerNoRounding(t *testing.T) {
	for _, text := range []string{"61613." + strings.Repeat("0", 1000) + "1", "616130000000000000001e-13", "1e-1000000"} {
		if _, err := stompNativeInteger(text); err == nil {
			t.Fatal("fraction rounded or truncated")
		}
	}
	for _, text := range []string{"61613." + strings.Repeat("0", 1000), "61613" + strings.Repeat("0", 1000) + "e-1000"} {
		if n, err := stompNativeInteger(text); err != nil || n != 61613 {
			t.Fatal("exact integral decimal rejected")
		}
	}
}

func TestStompNativeCredentialsAdmission(t *testing.T) {
	c := Config{Stomp: &internalStompConfig{Host: "localhost", Port: 61613}}
	path := filepath.Join(t.TempDir(), "credentials.yaml")
	c.Settings.Other = map[string]any{"setStomp": path}
	for i, test := range []struct {
		raw   string
		valid bool
		user  string
	}{
		{"user: synthetic\npass: synthetic\n", true, "synthetic"},
		{"user: 123\npass: synthetic\n", false, ""},
		{"user: synthetic\npass: false\n", false, ""},
		{"user: on\npass: synthetic\n", false, ""},
		{"user: \"on\"\npass: \"123\"\n", true, "on"},
		{"user: !!str true\npass: synthetic\n", true, "true"},
		{"user: synthetic\npass: null\n", false, ""},
		{"user: &name synthetic\npass: *name\n", true, "synthetic"},
		{"user: first\nuser: second\npass: synthetic\n", true, "second"},
		{"defaults: &defaults {user: synthetic, pass: synthetic}\n<<: *defaults\nuser: override\n", true, "override"},
		{"&key user: first\n*key: second\npass: synthetic\n", true, "second"},
		{"user: &cycle {user: *cycle}\npass: synthetic\n", false, ""},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.raw), 0600); err != nil {
				t.Fatal(err)
			}
			q, err := newInternalStompQueue(c, "/queue/brig")
			if (err == nil) != test.valid || err == nil && q.user != test.user {
				t.Fatal("native credential type admission differs")
			}
		})
	}
}
