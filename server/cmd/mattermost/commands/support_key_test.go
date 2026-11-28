// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupportAPIKey(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}

	t.Run("refuses to start without the key", func(t *testing.T) {
		for _, vars := range []map[string]string{{}, {supportAPIKeyEnv: "  "}} {
			_, err := supportAPIKey(env(vars))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "set MM_SUPPORT_API_KEY")
		}
	})

	t.Run("returns the key", func(t *testing.T) {
		key, err := supportAPIKey(env(map[string]string{supportAPIKeyEnv: "sk-portal-1"}))
		require.NoError(t, err)
		assert.Equal(t, "sk-portal-1", key)
	})
}
