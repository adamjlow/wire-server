// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

func (p brigPeers) providerSecondFactorEnabled(ctx context.Context, requestID string) (bool, error) {
	raw, _, err := p.galleyRPC(ctx, http.MethodGet, "/i/feature-configs", requestID, nil, false, nil)
	if err != nil {
		return false, err
	}
	var features struct {
		SecondFactor struct {
			Status string `json:"status"`
		} `json:"sndFactorPasswordChallenge"`
	}
	if json.Unmarshal(raw, &features) != nil {
		return false, errors.New("invalid provider second factor feature")
	}
	switch features.SecondFactor.Status {
	case "enabled":
		return true, nil
	case "disabled":
		return false, nil
	default:
		return false, errors.New("missing provider second factor feature")
	}
}
