// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package api4

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
)

const invalidURLParamErrorID = "api.context.invalid_url_param.app_error"

// getUsersAutocomplete sends GET /api/v4/users/autocomplete with the query string
// exactly as given, so that limits Client4 cannot express (empty, non-numeric,
// repeated) reach the server untouched.
func getUsersAutocomplete(t *testing.T, client *model.Client4, rawQuery string) (int, []byte) {
	t.Helper()

	rq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, client.APIURL+"/users/autocomplete?"+rawQuery, nil)
	require.NoError(t, err)
	if client.AuthToken != "" {
		rq.Header.Set(model.HeaderAuth, client.AuthType+" "+client.AuthToken)
	}

	rp, err := client.HTTPClient.Do(rq)
	require.NoError(t, err)
	defer rp.Body.Close()

	body, err := io.ReadAll(rp.Body)
	require.NoError(t, err)
	return rp.StatusCode, body
}

func requireInvalidLimit(t *testing.T, client *model.Client4, rawQuery string) {
	t.Helper()

	status, body := getUsersAutocomplete(t, client, rawQuery)
	require.Equal(t, http.StatusBadRequest, status, "query %q: %s", rawQuery, body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	assert.Equal(t, invalidURLParamErrorID, parsed["id"], "query %q", rawQuery)
	assert.NotContains(t, parsed, "users", "query %q", rawQuery)
}

func requireAutocompleteOK(t *testing.T, client *model.Client4, rawQuery string) *model.UserAutocomplete {
	t.Helper()

	status, body := getUsersAutocomplete(t, client, rawQuery)
	require.Equal(t, http.StatusOK, status, "query %q: %s", rawQuery, body)

	var autocomplete model.UserAutocomplete
	require.NoError(t, json.Unmarshal(body, &autocomplete))
	return &autocomplete
}

// createAutocompleteUsers creates count users through POST /api/v4/users whose
// usernames share prefix, optionally joining them to team.
func createAutocompleteUsers(t *testing.T, th *TestHelper, prefix string, count int, team *model.Team) []*model.User {
	t.Helper()

	users := make([]*model.User, 0, count)
	for i := range count {
		user, _, err := th.SystemAdminClient.CreateUser(context.Background(), &model.User{
			Email:    th.GenerateTestEmail(),
			Username: fmt.Sprintf("%s%03d", prefix, i),
			Password: model.NewTestPassword(),
		})
		require.NoError(t, err)
		if team != nil {
			th.LinkUserToTeam(t, user, team)
		}
		users = append(users, user)
	}
	return users
}

func newAutocompletePrefix() string {
	return "acp" + strings.ToLower(model.NewId()[:8])
}

func TestAutocompleteUsersInvalidLimit(t *testing.T) {
	mainHelper.Parallel(t)
	th := Setup(t).InitBasic(t)
	admin := th.SystemAdminClient

	t.Run("R1a", func(t *testing.T) {
		requireInvalidLimit(t, admin, "name=&limit=0")
	})

	t.Run("R1b", func(t *testing.T) {
		requireInvalidLimit(t, admin, "name=&limit=-1")
		requireInvalidLimit(t, admin, "name=&limit=-100")
	})

	t.Run("R1c", func(t *testing.T) {
		requireInvalidLimit(t, admin, "name=&limit=abc")
		requireInvalidLimit(t, admin, "name=&limit=10abc")
	})

	t.Run("R1d", func(t *testing.T) {
		requireInvalidLimit(t, admin, "name=&limit=1.5")
		requireInvalidLimit(t, admin, "name=&limit=%205")
	})

	t.Run("R1e", func(t *testing.T) {
		team := th.CreateTeamWithClient(t, admin)
		channel, _, err := admin.CreateChannel(context.Background(), &model.Channel{
			TeamId:      team.Id,
			Name:        GenerateTestChannelName(),
			DisplayName: "R1e channel",
			Type:        model.ChannelTypeOpen,
		})
		require.NoError(t, err)

		requireInvalidLimit(t, admin, "in_team="+team.Id+"&name=&limit=0")
		requireInvalidLimit(t, admin, "in_team="+team.Id+"&in_channel="+channel.Id+"&name=&limit=-5")
		requireInvalidLimit(t, admin, "name=&limit=abc")
	})

	t.Run("R1f", func(t *testing.T) {
		status, body := getUsersAutocomplete(t, admin, "name=&limit=99999999999999999999")
		require.Less(t, status, http.StatusInternalServerError, "%s", body)
		if status == http.StatusBadRequest {
			var parsed map[string]any
			require.NoError(t, json.Unmarshal(body, &parsed))
			assert.Equal(t, invalidURLParamErrorID, parsed["id"])
			return
		}
		require.Equal(t, http.StatusOK, status, "%s", body)
		var autocomplete model.UserAutocomplete
		require.NoError(t, json.Unmarshal(body, &autocomplete))
		assert.LessOrEqual(t, len(autocomplete.Users), model.UserSearchMaxLimit)
	})

	t.Run("limit-overflow-no-5xx", func(t *testing.T) {
		status, body := getUsersAutocomplete(t, admin, "in_team="+th.BasicTeam.Id+"&name=&limit=9223372036854775808")
		require.Less(t, status, http.StatusInternalServerError, "%s", body)
		if status == http.StatusBadRequest {
			var parsed map[string]any
			require.NoError(t, json.Unmarshal(body, &parsed))
			assert.Equal(t, invalidURLParamErrorID, parsed["id"])
			return
		}
		require.Equal(t, http.StatusOK, status, "%s", body)
		var autocomplete model.UserAutocomplete
		require.NoError(t, json.Unmarshal(body, &autocomplete))
		assert.LessOrEqual(t, len(autocomplete.Users), model.UserSearchMaxLimit)
	})

	t.Run("R1g", func(t *testing.T) {
		user := th.CreateUserWithClient(t, admin)
		th.LinkUserToTeam(t, user, th.BasicTeam)
		client := th.CreateClient()
		_, _, err := client.Login(context.Background(), user.Email, user.Password)
		require.NoError(t, err)

		requireInvalidLimit(t, client, "in_team="+th.BasicTeam.Id+"&name=&limit=0")
	})

	t.Run("invalid-limit-before-permission", func(t *testing.T) {
		th.LoginBasic(t)
		requireInvalidLimit(t, th.Client, "in_team="+th.BasicTeam.Id+"&name=&limit=-1")
	})

	t.Run("repeated-limit-first-wins", func(t *testing.T) {
		requireInvalidLimit(t, admin, "name=&limit=0&limit=5")
	})
}

func TestAutocompleteUsersValidLimit(t *testing.T) {
	mainHelper.Parallel(t)
	th := Setup(t).InitBasic(t)
	admin := th.SystemAdminClient

	// MaxUsersPerTeam is 50 in tests, so the count boundaries around the
	// default limit of 100 are checked in global mode with a per-run prefix.
	globalPrefix := newAutocompletePrefix()
	createAutocompleteUsers(t, th, globalPrefix, model.UserSearchDefaultLimit+1, nil)

	t.Run("R2a", func(t *testing.T) {
		autocomplete := requireAutocompleteOK(t, admin, "name="+globalPrefix)
		assert.Len(t, autocomplete.Users, model.UserSearchDefaultLimit)
	})

	t.Run("R2b", func(t *testing.T) {
		autocomplete := requireAutocompleteOK(t, admin, "name="+globalPrefix+"&limit=")
		assert.Len(t, autocomplete.Users, model.UserSearchDefaultLimit)
	})

	t.Run("R2e", func(t *testing.T) {
		autocomplete := requireAutocompleteOK(t, admin, "name="+globalPrefix+"&limit=1000")
		assert.Len(t, autocomplete.Users, model.UserSearchDefaultLimit+1)
	})

	t.Run("R2f", func(t *testing.T) {
		for _, limit := range []string{"1001", "5000"} {
			autocomplete := requireAutocompleteOK(t, admin, "name="+globalPrefix+"&limit="+limit)
			assert.Len(t, autocomplete.Users, model.UserSearchDefaultLimit+1, "limit=%s", limit)
		}
	})

	team := th.CreateTeamWithClient(t, admin)
	teamPrefix := newAutocompletePrefix()
	teamUsers := createAutocompleteUsers(t, th, teamPrefix, 30, team)

	t.Run("R2c", func(t *testing.T) {
		autocomplete := requireAutocompleteOK(t, admin, "in_team="+team.Id+"&name="+teamPrefix+"&limit=1")
		assert.Len(t, autocomplete.Users, 1)

		autocomplete = requireAutocompleteOK(t, admin, "name="+teamPrefix+"&limit=1")
		assert.Len(t, autocomplete.Users, 1)
	})

	t.Run("R2d", func(t *testing.T) {
		for _, limit := range []int{5, 25} {
			autocomplete := requireAutocompleteOK(t, admin, fmt.Sprintf("in_team=%s&name=%s&limit=%d", team.Id, teamPrefix, limit))
			assert.Len(t, autocomplete.Users, limit, "limit=%d", limit)
		}
	})

	t.Run("R2g", func(t *testing.T) {
		channel, _, err := admin.CreateChannel(context.Background(), &model.Channel{
			TeamId:      team.Id,
			Name:        GenerateTestChannelName(),
			DisplayName: "R2g channel",
			Type:        model.ChannelTypeOpen,
		})
		require.NoError(t, err)
		for _, user := range teamUsers[:3] {
			_, _, err = admin.AddChannelMember(context.Background(), channel.Id, user.Id)
			require.NoError(t, err)
		}

		autocomplete := requireAutocompleteOK(t, admin, "in_team="+team.Id+"&in_channel="+channel.Id+"&name="+teamPrefix+"&limit=2")
		assert.Len(t, autocomplete.Users, 2)
		assert.Len(t, autocomplete.OutOfChannel, 2)
	})
}

func TestAutocompleteUsersLimitSurroundingBehaviour(t *testing.T) {
	mainHelper.Parallel(t)
	th := Setup(t).InitBasic(t)
	admin := th.SystemAdminClient

	t.Run("R3a", func(t *testing.T) {
		anonymous := th.CreateClient()
		for _, query := range []string{"name=&limit=5", "name=&limit=0"} {
			status, body := getUsersAutocomplete(t, anonymous, query)
			assert.Equal(t, http.StatusUnauthorized, status, "query %q: %s", query, body)
		}
	})

	t.Run("R3b", func(t *testing.T) {
		th.LoginBasic(t)

		otherTeam := th.CreateTeamWithClient(t, admin)
		status, body := getUsersAutocomplete(t, th.Client, "in_team="+otherTeam.Id+"&name=&limit=10")
		assert.Equal(t, http.StatusForbidden, status, "%s", body)

		privateChannel, _, err := admin.CreateChannel(context.Background(), &model.Channel{
			TeamId:      th.BasicTeam.Id,
			Name:        GenerateTestChannelName(),
			DisplayName: "R3b private channel",
			Type:        model.ChannelTypePrivate,
		})
		require.NoError(t, err)
		status, body = getUsersAutocomplete(t, th.Client, "in_team="+th.BasicTeam.Id+"&in_channel="+privateChannel.Id+"&name=&limit=10")
		assert.Equal(t, http.StatusForbidden, status, "%s", body)
	})

	t.Run("R3c", func(t *testing.T) {
		status, body := getUsersAutocomplete(t, admin, "in_channel="+th.BasicChannel.Id+"&name=&limit=10")
		assert.Equal(t, http.StatusInternalServerError, status, "%s", body)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal(body, &parsed))
		assert.Equal(t, "api.user.autocomplete_users.missing_team_id.app_error", parsed["id"])
	})

	t.Run("R3d", func(t *testing.T) {
		prefix := newAutocompletePrefix()
		createAutocompleteUsers(t, th, prefix, 4, th.BasicTeam)
		query := "in_team=" + th.BasicTeam.Id + "&name=" + prefix + "&limit=3"

		status, body := getUsersAutocomplete(t, admin, query)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal(body, &parsed))
		users, ok := parsed["users"].([]any)
		require.True(t, ok, "users should be an array: %s", body)
		require.Len(t, users, 3)
		for _, raw := range users {
			user, ok := raw.(map[string]any)
			require.True(t, ok)
			assert.NotEmpty(t, user["id"])
			assert.True(t, strings.HasPrefix(user["username"].(string), prefix))
		}

		th.App.UpdateConfig(func(cfg *model.Config) { *cfg.PrivacySettings.ShowEmailAddress = false })
		th.LoginBasic(t)
		autocomplete := requireAutocompleteOK(t, th.Client, query)
		require.Len(t, autocomplete.Users, 3)
		for _, user := range autocomplete.Users {
			assert.Empty(t, user.Email, "non-admin autocomplete should not expose email")
		}
	})
}
