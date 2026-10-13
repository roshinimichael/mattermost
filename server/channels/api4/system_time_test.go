// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package api4

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
)

// serverTimeRequest sends a raw request to the server time route, so the tests see the exact status, headers and body.
func serverTimeRequest(t *testing.T, th *TestHelper, method, path string, headers map[string]string, body string) (*http.Response, map[string]any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, th.Client.APIURL+path, strings.NewReader(body))
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	return resp, parsed
}

func bearer(token string) map[string]string {
	return map[string]string{model.HeaderAuth: model.HeaderBearer + " " + token}
}

// requireMillisTime asserts the body's `time` is a JSON integer holding milliseconds since the epoch, and returns it.
func requireMillisTime(t *testing.T, body map[string]any) int64 {
	t.Helper()

	require.Contains(t, body, "time")
	value, ok := body["time"].(float64)
	require.True(t, ok, "time must be a JSON number, got %T", body["time"])
	require.Equal(t, float64(int64(value)), value, "time must be an integer")
	require.Greater(t, value, 1e12, "time must be in milliseconds")
	require.Less(t, value, 1e14, "time must be in milliseconds")
	return int64(value)
}

func loginNewUser(t *testing.T, th *TestHelper) (*model.User, *model.Client4) {
	t.Helper()

	user := th.CreateUser(t)
	client := th.CreateClient()
	_, _, err := client.Login(context.Background(), user.Email, user.Password)
	require.NoError(t, err)
	return user, client
}

func TestServerTimeAuthenticated(t *testing.T) {
	mainHelper.Parallel(t)
	th := Setup(t)

	t.Run("R1a system admin gets the time as integer milliseconds", func(t *testing.T) {
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(th.SystemAdminClient.AuthToken), "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json"), resp.Header.Get("Content-Type"))
		requireMillisTime(t, body)
	})

	t.Run("R1b the time is the server's current time", func(t *testing.T) {
		before := model.GetMillis()
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(th.SystemAdminClient.AuthToken), "")
		after := model.GetMillis()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		serverTime := requireMillisTime(t, body)
		assert.GreaterOrEqual(t, serverTime, before-2000)
		assert.LessOrEqual(t, serverTime, after+2000)
	})

	t.Run("R1c the time is live, a later call returns a later time", func(t *testing.T) {
		_, body1 := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(th.SystemAdminClient.AuthToken), "")
		t1 := requireMillisTime(t, body1)
		time.Sleep(1500 * time.Millisecond)
		_, body2 := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(th.SystemAdminClient.AuthToken), "")
		t2 := requireMillisTime(t, body2)
		assert.GreaterOrEqual(t, t2-t1, int64(1000))
		assert.LessOrEqual(t, t2-t1, int64(10000))
	})

	t.Run("R1d a regular user may read it", func(t *testing.T) {
		_, client := loginNewUser(t, th)
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(client.AuthToken), "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		requireMillisTime(t, body)
	})

	t.Run("server-time-two-users-consistent admin and regular user see the same time", func(t *testing.T) {
		_, client := loginNewUser(t, th)
		respAdmin, bodyAdmin := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(th.SystemAdminClient.AuthToken), "")
		respUser, bodyUser := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(client.AuthToken), "")
		require.Equal(t, http.StatusOK, respAdmin.StatusCode)
		require.Equal(t, http.StatusOK, respUser.StatusCode)
		diff := requireMillisTime(t, bodyAdmin) - requireMillisTime(t, bodyUser)
		if diff < 0 {
			diff = -diff
		}
		assert.Less(t, diff, int64(2000))
	})

	t.Run("R1e personal access token and bot token are accepted", func(t *testing.T) {
		th.App.UpdateConfig(func(cfg *model.Config) {
			*cfg.ServiceSettings.EnableUserAccessTokens = true
			*cfg.ServiceSettings.EnableBotAccountCreation = true
		})

		pat, _, err := th.SystemAdminClient.CreateUserAccessToken(context.Background(), th.SystemAdminUser.Id, "t")
		require.NoError(t, err)
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(pat.Token), "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		requireMillisTime(t, body)

		bot, _, err := th.SystemAdminClient.CreateBot(context.Background(), &model.Bot{Username: "timebot" + model.NewId()[:8]})
		require.NoError(t, err)
		botToken, _, err := th.SystemAdminClient.CreateUserAccessToken(context.Background(), bot.UserId, "t")
		require.NoError(t, err)
		resp, body = serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(botToken.Token), "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		requireMillisTime(t, body)
	})

	t.Run("R1f cookie session without an Authorization header is accepted", func(t *testing.T) {
		_, client := loginNewUser(t, th)
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", map[string]string{
			"Cookie":                  model.SessionCookieToken + "=" + client.AuthToken,
			model.HeaderRequestedWith: model.HeaderRequestedWithXML,
		}, "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		requireMillisTime(t, body)
	})

	t.Run("R1g query parameters are ignored", func(t *testing.T) {
		before := model.GetMillis()
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time?foo=bar&time=0", bearer(th.SystemAdminClient.AuthToken), "")
		after := model.GetMillis()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		serverTime := requireMillisTime(t, body)
		assert.GreaterOrEqual(t, serverTime, before-2000)
		assert.LessOrEqual(t, serverTime, after+2000)
	})
}

func TestServerTimeRequiresSession(t *testing.T) {
	mainHelper.Parallel(t)
	th := Setup(t)

	t.Run("R2a no credentials", func(t *testing.T) {
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", nil, "")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.EqualValues(t, http.StatusUnauthorized, body["status_code"])
		assert.NotContains(t, body, "time")
	})

	t.Run("server-time-no-store-on-401 unauthenticated response is an AppError without the time", func(t *testing.T) {
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", nil, "")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.NotEmpty(t, body["id"])
		assert.NotEmpty(t, body["message"])
		assert.EqualValues(t, http.StatusUnauthorized, body["status_code"])
		assert.NotContains(t, body, "time")
	})

	t.Run("R2b garbage bearer token", func(t *testing.T) {
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer("invalidtoken123"), "")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.NotContains(t, body, "time")
	})

	t.Run("R2c token of a logged out session", func(t *testing.T) {
		_, client := loginNewUser(t, th)
		token := client.AuthToken
		resp, _ := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(token), "")
		require.Equal(t, http.StatusOK, resp.StatusCode)

		_, err := client.Logout(context.Background())
		require.NoError(t, err)

		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(token), "")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.NotContains(t, body, "time")
	})

	t.Run("R2d token of a deactivated user", func(t *testing.T) {
		user, client := loginNewUser(t, th)
		token := client.AuthToken

		_, err := th.SystemAdminClient.DeleteUser(context.Background(), user.Id)
		require.NoError(t, err)

		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(token), "")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.NotContains(t, body, "time")
	})
}

func TestServerTimeIsNeverCached(t *testing.T) {
	mainHelper.Parallel(t)
	th := Setup(t)

	t.Run("R3a Cache-Control forbids storing", func(t *testing.T) {
		resp, _ := serverTimeRequest(t, th, http.MethodGet, "/system/time", bearer(th.SystemAdminClient.AuthToken), "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		cacheControl := resp.Header.Get("Cache-Control")
		assert.Contains(t, cacheControl, "no-store")
		assert.Contains(t, cacheControl, "no-cache")
		assert.NotContains(t, cacheControl, "public")
		assert.NotRegexp(t, `max-age=[1-9]`, cacheControl)
	})

	t.Run("R3b conditional headers do not produce a 304", func(t *testing.T) {
		headers := bearer(th.SystemAdminClient.AuthToken)
		headers["If-None-Match"] = "*"
		headers["If-Modified-Since"] = time.Now().UTC().Format(http.TimeFormat)

		before := model.GetMillis()
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/time", headers, "")
		after := model.GetMillis()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		serverTime := requireMillisTime(t, body)
		assert.GreaterOrEqual(t, serverTime, before-2000)
		assert.LessOrEqual(t, serverTime, after+2000)
		assert.Empty(t, resp.Header.Get("ETag"))
		assert.Empty(t, resp.Header.Get("Last-Modified"))
	})
}

func TestServerTimeRouting(t *testing.T) {
	mainHelper.Parallel(t)
	th := Setup(t)

	t.Run("R4a only GET is routed", func(t *testing.T) {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			headers := bearer(th.SystemAdminClient.AuthToken)
			headers[model.HeaderRequestedWith] = model.HeaderRequestedWithXML
			headers["Content-Type"] = "application/json"
			resp, body := serverTimeRequest(t, th, method, "/system/time", headers, `{"time":0}`)
			assert.GreaterOrEqual(t, resp.StatusCode, 400, method)
			assert.Less(t, resp.StatusCode, 500, method)
			assert.NotContains(t, body, "time", method)
		}
	})

	t.Run("R4b neighbouring paths", func(t *testing.T) {
		resp, _ := serverTimeRequest(t, th, http.MethodGet, "/system/time/", bearer(th.SystemAdminClient.AuthToken), "")
		assert.Less(t, resp.StatusCode, 500)

		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/times", bearer(th.SystemAdminClient.AuthToken), "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		assert.NotContains(t, body, "time")
	})
}

func TestServerTimeAdjacentSystemRoutes(t *testing.T) {
	mainHelper.Parallel(t)
	th := Setup(t)

	t.Run("R6a timezones still requires a session and returns the list", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		timezones, resp, err := th.Client.GetSupportedTimezone(ctx)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.NotEmpty(t, timezones)

		noAuth, _ := serverTimeRequest(t, th, http.MethodGet, "/system/timezones", nil, "")
		assert.Equal(t, http.StatusUnauthorized, noAuth.StatusCode)
	})

	t.Run("R6b ping stays unauthenticated", func(t *testing.T) {
		resp, body := serverTimeRequest(t, th, http.MethodGet, "/system/ping", nil, "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, model.StatusOk, body["status"])
	})
}

func TestServerTimeOpenAPIDocumented(t *testing.T) {
	// openapi-documents-system-time / R6d: every /system route is documented in the API reference.
	// The test helper changes the working directory, so locate the API reference from this file.
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "api", "v4", "source", "system.yaml"))
	require.NoError(t, err)

	var paths map[string]map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &paths))

	get, ok := paths["/api/v4/system/time"]["get"].(map[string]any)
	require.True(t, ok, "api/v4/source/system.yaml has no GET /api/v4/system/time entry")

	responses, _ := get["responses"].(map[string]any)
	assert.Contains(t, responses, "200")
	assert.Contains(t, responses, "401")

	ok200, _ := responses["200"].(map[string]any)
	content, _ := ok200["content"].(map[string]any)
	jsonContent, _ := content["application/json"].(map[string]any)
	schema, _ := jsonContent["schema"].(map[string]any)
	if ref, isRef := schema["$ref"].(string); isRef {
		// A shared ServerTime component in definitions.yaml is documented elsewhere.
		assert.NotEmpty(t, ref)
		return
	}
	properties, _ := schema["properties"].(map[string]any)
	timeProp, _ := properties["time"].(map[string]any)
	assert.Equal(t, "integer", timeProp["type"], "200 schema must have an integer `time` property")
}
