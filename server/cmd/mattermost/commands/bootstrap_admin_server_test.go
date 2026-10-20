// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/v8/channels/store/storetest"
)

const serverStartTimeout = 2 * time.Minute

// emptyDatabase creates a database with no account, dropped once the test is done.
func emptyDatabase(t *testing.T) *model.SqlSettings {
	settings := storetest.MakeSqlSettings(model.DatabaseDriverPostgres)
	t.Cleanup(func() { storetest.CleanupSqlSettings(settings) })
	return settings
}

// setAdminEnv sets the MM_ADMIN_* variables given and unsets the others for the duration of the test.
func setAdminEnv(t *testing.T, vars map[string]string) {
	for _, key := range []string{adminUsernameEnv, adminPasswordEnv, adminEmailEnv} {
		value, ok := vars[key]
		t.Setenv(key, value)
		if !ok {
			os.Unsetenv(key)
		}
	}
}

// countUsers counts the accounts, bots included, stored in the database.
func countUsers(t *testing.T, settings *model.SqlSettings) int {
	db, err := sql.Open(model.DatabaseDriverPostgres, *settings.DataSource)
	require.NoError(t, err)
	defer db.Close()
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM Users").Scan(&count))
	return count
}

// humanUsers lists the accounts that are not bots.
func humanUsers(t *testing.T, client *model.Client4) []*model.User {
	users, _, err := client.GetUsers(context.Background(), 0, 200, "")
	require.NoError(t, err)
	var humans []*model.User
	for _, user := range users {
		if !user.IsBot {
			humans = append(humans, user)
		}
	}
	return humans
}

func TestBootstrapAdminOnFirstStart(t *testing.T) {
	ctx := context.Background()
	settings := emptyDatabase(t)
	setAdminEnv(t, map[string]string{adminUsernameEnv: "sysadmin", adminPasswordEnv: "Sys-admin-1"})

	server := startBackgroundServer(t, settings)
	require.NoError(t, server.WaitReady(serverStartTimeout))

	admin := server.Client
	t.Run("R1a signs in with MM_ADMIN_USERNAME and MM_ADMIN_PASSWORD", func(t *testing.T) {
		user, resp, err := admin.Login(ctx, "sysadmin", "Sys-admin-1")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.NotEmpty(t, resp.Header.Get(model.HeaderToken))
		assert.Equal(t, "sysadmin", user.Username)
	})
	require.NotEmpty(t, admin.AuthToken, "the bootstrap admin could not sign in")

	t.Run("R1b the account is a system admin", func(t *testing.T) {
		me, _, err := admin.GetMe(ctx, "")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{model.SystemAdminRoleId, model.SystemUserRoleId}, strings.Fields(me.Roles))
		_, resp, err := admin.GetConfig(ctx)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("R1c without MM_ADMIN_EMAIL the email is <username>@example.com", func(t *testing.T) {
		me, _, err := admin.GetMe(ctx, "")
		require.NoError(t, err)
		assert.Equal(t, "sysadmin@example.com", me.Email)
	})

	t.Run("R1e the email is verified", func(t *testing.T) {
		me, _, err := admin.GetMe(ctx, "")
		require.NoError(t, err)
		assert.True(t, me.EmailVerified)
	})

	t.Run("R1f a wrong password is rejected", func(t *testing.T) {
		client := model.NewAPIv4Client(server.URL)
		_, resp, err := client.Login(ctx, "sysadmin", "wrong-Pass-1")
		require.Error(t, err)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.Empty(t, client.AuthToken)
	})

	t.Run("R1g exactly one account exists, the bootstrap admin", func(t *testing.T) {
		humans := humanUsers(t, admin)
		require.Len(t, humans, 1)
		assert.Equal(t, "sysadmin", humans[0].Username)
	})

	t.Run("R4a anonymous sign-up is rejected", func(t *testing.T) {
		anonymous := model.NewAPIv4Client(server.URL)
		_, resp, err := anonymous.CreateUser(ctx, &model.User{Email: "x@ex.com", Username: "intruder", Password: "Intruder-1"})
		require.Error(t, err)
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden}, resp.StatusCode)
		_, resp, err = admin.GetUserByUsername(ctx, "intruder", "")
		require.Error(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("R4b the client config no longer reports NoAccounts", func(t *testing.T) {
		resp, err := model.NewAPIv4Client(server.URL).DoAPIGet(ctx, "/config/client?format=old", "")
		require.NoError(t, err)
		defer resp.Body.Close()
		var clientConfig map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&clientConfig))
		assert.Equal(t, "false", clientConfig["NoAccounts"])
	})

	t.Run("R4d the admin can still create users", func(t *testing.T) {
		user, resp, err := admin.CreateUser(ctx, &model.User{Email: "u1@ex.com", Username: "user1", Password: "User-pass-1"})
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.Equal(t, model.SystemUserRoleId, user.Roles)
	})

	server.Stop()
	usersBefore := countUsers(t, settings)

	t.Run("R3a restarts without MM_ADMIN_* on a database with an account", func(t *testing.T) {
		setAdminEnv(t, nil)
		restarted := startBackgroundServer(t, settings)
		require.NoError(t, restarted.WaitReady(serverStartTimeout))
		_, resp, err := restarted.Client.Login(ctx, "sysadmin", "Sys-admin-1")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		restarted.Stop()
		assert.Equal(t, usersBefore, countUsers(t, settings))
	})

	t.Run("R3b restarts with other MM_ADMIN_* values and creates nobody", func(t *testing.T) {
		setAdminEnv(t, map[string]string{adminUsernameEnv: "other", adminPasswordEnv: "Other-pass-1"})
		restarted := startBackgroundServer(t, settings)
		require.NoError(t, restarted.WaitReady(serverStartTimeout))
		_, resp, err := model.NewAPIv4Client(restarted.URL).Login(ctx, "other", "Other-pass-1")
		require.Error(t, err)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		user, _, err := restarted.Client.Login(ctx, "sysadmin", "Sys-admin-1")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{model.SystemAdminRoleId, model.SystemUserRoleId}, strings.Fields(user.Roles))
		restarted.Stop()
		assert.Equal(t, usersBefore, countUsers(t, settings))
	})
}

func TestBootstrapAdminWithEmail(t *testing.T) {
	ctx := context.Background()
	setAdminEnv(t, map[string]string{adminUsernameEnv: "ProbeAdmin", adminPasswordEnv: "Probe-pass-1", adminEmailEnv: "Ops@Acme.test"})

	server := startBackgroundServer(t, emptyDatabase(t))
	require.NoError(t, server.WaitReady(serverStartTimeout))

	for _, loginID := range []string{"ProbeAdmin", "probeadmin"} {
		_, resp, err := server.Client.Login(ctx, loginID, "Probe-pass-1")
		require.NoError(t, err, loginID)
		assert.Equal(t, http.StatusOK, resp.StatusCode, loginID)
	}
	me, _, err := server.Client.GetMe(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, "ops@acme.test", me.Email)
	assert.Contains(t, strings.Fields(me.Roles), model.SystemAdminRoleId)
}

func TestBootstrapAdminRefusesToStart(t *testing.T) {
	for _, tc := range []struct {
		name    string
		vars    map[string]string
		message string
	}{
		{"R2a neither variable set", nil, "set MM_ADMIN_USERNAME and MM_ADMIN_PASSWORD"},
		{"R2b only MM_ADMIN_USERNAME set", map[string]string{adminUsernameEnv: "sysadmin"}, "set MM_ADMIN_USERNAME and MM_ADMIN_PASSWORD"},
		{"R2c only MM_ADMIN_PASSWORD set", map[string]string{adminPasswordEnv: "Sys-admin-1"}, "set MM_ADMIN_USERNAME and MM_ADMIN_PASSWORD"},
		{"R2d password violates the password policy", map[string]string{adminUsernameEnv: "sysadmin", adminPasswordEnv: "a"}, "failed to create the system admin"},
		{"R2e invalid username", map[string]string{adminUsernameEnv: "bad name!", adminPasswordEnv: "Sys-admin-1"}, "failed to create the system admin"},
		{"R2f invalid MM_ADMIN_EMAIL", map[string]string{adminUsernameEnv: "probeadmin", adminPasswordEnv: "Probe-pass-1", adminEmailEnv: "notanemail"}, "failed to create the system admin"},
		{"R2g both variables empty", map[string]string{adminUsernameEnv: "", adminPasswordEnv: ""}, "set MM_ADMIN_USERNAME and MM_ADMIN_PASSWORD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := emptyDatabase(t)
			setAdminEnv(t, tc.vars)

			server := startBackgroundServer(t, settings)
			exited, err := server.WaitExit(serverStartTimeout)
			require.True(t, exited, "the server started")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.message)
			assert.Equal(t, 0, countUsers(t, settings))
		})
	}
}

// Nobody signing up while the server starts may take the place of the admin it creates (defect F1).
func TestBootstrapAdminStartupSignUpRace(t *testing.T) {
	ctx := context.Background()
	setAdminEnv(t, map[string]string{adminUsernameEnv: "sysadmin", adminPasswordEnv: "Sys-admin-1"})

	server := startBackgroundServer(t, emptyDatabase(t))

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var created []string
	wg.Go(func() {
		anonymous := model.NewAPIv4Client(server.URL)
		for {
			select {
			case <-stop:
				return
			case <-time.After(3 * time.Millisecond):
			}
			username := "racer" + model.NewId()[:8]
			if _, resp, _ := anonymous.CreateUser(ctx, &model.User{Email: username + "@ex.com", Username: username, Password: "Racer-pass-1"}); resp != nil && resp.StatusCode == http.StatusCreated {
				created = append(created, username)
			}
		}
	})

	readyErr := server.WaitReady(serverStartTimeout)
	if readyErr == nil {
		time.Sleep(2 * time.Second)
	}
	close(stop)
	wg.Wait()
	require.NoError(t, readyErr)

	assert.Empty(t, created, "anonymous sign-ups accepted while the server started")

	admin := server.Client
	_, _, err := admin.Login(ctx, "sysadmin", "Sys-admin-1")
	require.NoError(t, err, "the bootstrap admin could not sign in")
	var admins []string
	for _, user := range humanUsers(t, admin) {
		assert.False(t, strings.HasPrefix(user.Username, "racer"), "%s was created", user.Username)
		if user.IsSystemAdmin() {
			admins = append(admins, user.Username)
		}
	}
	assert.Equal(t, []string{"sysadmin"}, admins)
}
