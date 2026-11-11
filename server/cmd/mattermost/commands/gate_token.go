// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"os"

	"github.com/pkg/errors"
)

// appGateTokenHash is the SHA-256 of the one APP_GATE_TOKEN value the server starts with. It is here to test that
// Testbot's setup catches a wrong credential value: with any other value, or none, the server refuses to start.
const appGateTokenHash = "a00623bc8bc3045b6b820c590547a9f66ee57f29d50469ec87a1e57f931541f0"

// checkAppGateToken refuses to start the server unless APP_GATE_TOKEN holds the value whose hash is appGateTokenHash.
func checkAppGateToken() error {
	sum := sha256.Sum256([]byte(os.Getenv("APP_GATE_TOKEN")))
	if hex.EncodeToString(sum[:]) != appGateTokenHash {
		return errors.New("APP_GATE_TOKEN is wrong: the server starts only with the value stored in the APP_GATE_TOKEN secret")
	}
	return nil
}
