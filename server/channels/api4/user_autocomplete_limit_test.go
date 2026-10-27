// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package api4

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestAutocompleteUsersInvalidLimit(t *testing.T) {
	mainHelper.Parallel(t)
	th := Setup(t).InitBasic(t)
	th.LoginBasic(t)
	name := th.BasicUser.Username

	checkInvalidLimit := func(t *testing.T, query string) {
		t.Helper()
		r, err := th.Client.DoAPIGet(context.Background(), "/users/autocomplete?"+query, "")
		if r != nil {
			defer r.Body.Close()
		}
		CheckBadRequestStatus(t, model.BuildResponse(r))
		CheckErrorID(t, err, "api.context.invalid_url_param.app_error")
	}

	for _, limit := range []string{"abc", "0", "-1", "-5", "1.5", "10abc", "%2010", "-99999999999999999999"} {
		t.Run("rejects limit "+limit, func(t *testing.T) {
			checkInvalidLimit(t, "name="+name+"&limit="+limit)
		})
	}

	t.Run("rejects zero and negative limits from the client", func(t *testing.T) {
		_, resp, err := th.Client.AutocompleteUsers(context.Background(), name, 0, "")
		CheckErrorID(t, err, "api.context.invalid_url_param.app_error")
		CheckBadRequestStatus(t, resp)

		_, resp, err = th.Client.AutocompleteUsers(context.Background(), name, -1, "")
		CheckErrorID(t, err, "api.context.invalid_url_param.app_error")
		CheckBadRequestStatus(t, resp)
	})

	t.Run("rejects an invalid limit in team", func(t *testing.T) {
		_, resp, err := th.Client.AutocompleteUsersInTeam(context.Background(), th.BasicTeam.Id, name, 0, "")
		CheckErrorID(t, err, "api.context.invalid_url_param.app_error")
		CheckBadRequestStatus(t, resp)

		checkInvalidLimit(t, "in_team="+th.BasicTeam.Id+"&name="+name+"&limit=abc")
	})

	t.Run("rejects an invalid limit in channel", func(t *testing.T) {
		_, resp, err := th.Client.AutocompleteUsersInChannel(context.Background(), th.BasicTeam.Id, th.BasicChannel.Id, name, 0, "")
		CheckErrorID(t, err, "api.context.invalid_url_param.app_error")
		CheckBadRequestStatus(t, resp)

		checkInvalidLimit(t, "in_team="+th.BasicTeam.Id+"&in_channel="+th.BasicChannel.Id+"&name="+name+"&limit=abc")
	})
}

func TestAutocompleteUsersValidLimit(t *testing.T) {
	mainHelper.Parallel(t)
	th := Setup(t).InitBasic(t)

	// More matching users than the default limit, so the default and the cap are observable.
	const matching = model.UserSearchDefaultLimit + 1
	prefix := "aclim" + model.NewRandomString(8)
	for i := range matching {
		_, appErr := th.App.CreateUser(th.Context, &model.User{
			Email:    th.GenerateTestEmail(),
			Username: fmt.Sprintf("%s%03d", prefix, i),
			Password: "Pa$$word11",
		})
		require.Nil(t, appErr)
	}

	getRaw := func(t *testing.T, query string) []*model.User {
		t.Helper()
		r, err := th.SystemAdminClient.DoAPIGet(context.Background(), "/users/autocomplete?"+query, "")
		require.NoError(t, err)
		defer r.Body.Close()
		rusers, resp, err := model.DecodeJSONFromResponse[*model.UserAutocomplete](r)
		require.NoError(t, err)
		CheckOKStatus(t, resp)
		return rusers.Users
	}

	getWithLimit := func(t *testing.T, limit int) []*model.User {
		t.Helper()
		rusers, resp, err := th.SystemAdminClient.AutocompleteUsers(context.Background(), prefix, limit, "")
		require.NoError(t, err)
		CheckOKStatus(t, resp)
		return rusers.Users
	}

	t.Run("absent limit uses the default", func(t *testing.T) {
		assert.Len(t, getRaw(t, "name="+prefix), model.UserSearchDefaultLimit)
	})

	t.Run("empty limit uses the default", func(t *testing.T) {
		assert.Len(t, getRaw(t, "name="+prefix+"&limit="), model.UserSearchDefaultLimit)
	})

	t.Run("limit above the maximum is capped", func(t *testing.T) {
		assert.Len(t, getWithLimit(t, model.UserSearchMaxLimit+1), matching)
	})

	t.Run("limit at the maximum", func(t *testing.T) {
		assert.Len(t, getWithLimit(t, model.UserSearchMaxLimit), matching)
	})

	t.Run("limit too large for an int is capped", func(t *testing.T) {
		assert.Len(t, getRaw(t, "name="+prefix+"&limit=99999999999999999999"), matching)
	})

	for _, limit := range []int{1, 3, 25} {
		t.Run(fmt.Sprintf("limit %d is honoured", limit), func(t *testing.T) {
			assert.Len(t, getWithLimit(t, limit), limit)
		})
	}
}
