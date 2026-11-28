// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

// publicURLEnv is the address people reach this server at, such as http://localhost:8065 for a local server. It is no
// secret, but the repository sets no default: each deployment names its own.
const publicURLEnv = "MM_PUBLIC_URL"

// publicURL is the server's public address. Without one, or with one that is not an http(s) URL, the server does not
// start.
func publicURL(getenv func(string) string) (string, error) {
	raw := strings.TrimSpace(getenv(publicURLEnv))
	if raw == "" {
		return "", errors.Errorf("set %s to the address people reach this server at, such as http://localhost:8065", publicURLEnv)
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.Errorf("%s must be an http or https URL, such as http://localhost:8065", publicURLEnv)
	}
	return strings.TrimRight(raw, "/"), nil
}
