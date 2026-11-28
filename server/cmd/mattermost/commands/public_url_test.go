// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicURL(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}

	t.Run("refuses to start without it", func(t *testing.T) {
		_, err := publicURL(env(map[string]string{}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "set MM_PUBLIC_URL")
	})

	t.Run("refuses one that is not an http URL", func(t *testing.T) {
		for _, value := range []string{"localhost:8065", "ftp://chat.acme.test", "http://"} {
			_, err := publicURL(env(map[string]string{publicURLEnv: value}))
			require.Error(t, err, value)
		}
	})

	t.Run("returns it without a trailing slash", func(t *testing.T) {
		value, err := publicURL(env(map[string]string{publicURLEnv: "http://localhost:8065/"}))
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:8065", value)
	})
}
