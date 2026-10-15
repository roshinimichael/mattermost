// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminFromEnv(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}

	t.Run("refuses to start without the credentials", func(t *testing.T) {
		for _, vars := range []map[string]string{
			{},
			{adminUsernameEnv: "sysadmin"},
			{adminPasswordEnv: "Sys-admin-1"},
		} {
			_, err := adminFromEnv(env(vars))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "set MM_ADMIN_USERNAME and MM_ADMIN_PASSWORD")
		}
	})

	t.Run("builds the admin, with a default email", func(t *testing.T) {
		admin, err := adminFromEnv(env(map[string]string{adminUsernameEnv: "sysadmin", adminPasswordEnv: "Sys-admin-1"}))
		require.NoError(t, err)
		assert.Equal(t, "sysadmin", admin.Username)
		assert.Equal(t, "Sys-admin-1", admin.Password)
		assert.Equal(t, "sysadmin@example.com", admin.Email)
		assert.True(t, admin.EmailVerified)
	})

	t.Run("uses MM_ADMIN_EMAIL when given", func(t *testing.T) {
		admin, err := adminFromEnv(env(map[string]string{adminUsernameEnv: "sysadmin", adminPasswordEnv: "Sys-admin-1", adminEmailEnv: "ops@acme.test"}))
		require.NoError(t, err)
		assert.Equal(t, "ops@acme.test", admin.Email)
	})
}
