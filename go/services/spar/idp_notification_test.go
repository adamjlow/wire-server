// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestIdPNotificationUsesNativeJSONVersion(t *testing.T) {
	version := "v2"
	info := idpConfig{Extra: idpExtra{Version: &version}}
	p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
		var value struct {
			Tag   string
			Value struct {
				IdP struct {
					Extra struct {
						Version string `json:"apiVersion"`
					} `json:"extraInfo"`
				} `json:"idp"`
			}
		}
		if r.Method != "POST" || r.URL.Path != "/i/idp/send-idp-changed-email" || json.NewDecoder(r.Body).Decode(&value) != nil || value.Tag != "created" || value.Value.IdP.Extra.Version != "WireIdPAPIV2" {
			t.Error("notification did not use native enum")
			w.WriteHeader(400)
		}
	})
	defer close()
	if err := p.notifyIdP(context.Background(), "created", "", "", nil, &info); err != nil {
		t.Fatal(err)
	}
}
