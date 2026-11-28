// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"strings"

	"github.com/pkg/errors"
)

// supportAPIKeyEnv is the key the server sends its crash reports to the support portal with. The portal issues one per
// installation; it is a secret, and this repository holds no value or default for it.
const supportAPIKeyEnv = "MM_SUPPORT_API_KEY"

// supportAPIKey is the server's support portal key. Without it, the server does not start.
func supportAPIKey(getenv func(string) string) (string, error) {
	key := strings.TrimSpace(getenv(supportAPIKeyEnv))
	if key == "" {
		return "", errors.Errorf("set %s, the key the support portal issued to this installation, so the server can report crashes", supportAPIKeyEnv)
	}
	return key, nil
}
