// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/public/shared/request"
	"github.com/mattermost/mattermost/server/v8/channels/jobs"
	"github.com/mattermost/mattermost/server/v8/channels/store/sqlstore"
	"github.com/mattermost/mattermost/server/v8/channels/store/storetest"
	"github.com/mattermost/mattermost/server/v8/config"
)

const (
	bootstrapAdminUsername = "sysadmin"
	bootstrapAdminPassword = "Sys@dmin-sample1"
)

// bootstrapTestServer is a server started through runServer, as `mattermost server` starts it.
type bootstrapTestServer struct {
	URL       string
	interrupt chan os.Signal
	done      chan error
}

// emptyDatabase creates a database with no account in it.
func emptyDatabase(tb testing.TB) *model.SqlSettings {
	settings := storetest.MakeSqlSettings(model.DatabaseDriverPostgres)
	tb.Cleanup(func() { storetest.CleanupSqlSettings(settings) })
	return settings
}

// seedAccount saves an account straight to the database, so the server starts with an account already there.
func seedAccount(tb testing.TB, settings *model.SqlSettings, username string) {
	ss, err := sqlstore.New(*settings, mlog.CreateConsoleTestLogger(tb), nil, sqlstore.DisableMorphLogging())
	require.NoError(tb, err)
	defer ss.Close()
	_, err = ss.User().Save(request.TestContext(tb), &model.User{
		Username: username,
		Email:    username + "@seed.test",
		Password: "Seed-password-1",
		Roles:    model.SystemUserRoleId,
	})
	require.NoError(tb, err)
}

func freeListenAddress(tb testing.TB) string {
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(tb, err)
	addr := l.Addr().String()
	require.NoError(tb, l.Close())
	return addr
}

// setAdminEnv sets MM_ADMIN_* for the test; a missing key is cleared.
func setAdminEnv(tb testing.TB, env map[string]string) {
	for _, k := range []string{adminUsernameEnv, adminPasswordEnv, adminEmailEnv} {
		tb.(*testing.T).Setenv(k, env[k])
	}
}

// launchServer runs runServer against the database in the background and returns at once.
func launchServer(tb testing.TB, settings *model.SqlSettings, addr string) *bootstrapTestServer {
	originalInterval := jobs.DefaultWatcherPollingInterval
	jobs.DefaultWatcherPollingInterval = 200
	tb.Cleanup(func() { jobs.DefaultWatcherPollingInterval = originalInterval })

	configStore := config.NewTestMemoryStore()
	cfg := configStore.Get()
	*cfg.ServiceSettings.ListenAddress = addr
	*cfg.ServiceSettings.SiteURL = "http://" + addr
	cfg.SqlSettings = *settings
	_, _, err := configStore.Set(cfg)
	require.NoError(tb, err)

	s := &bootstrapTestServer{
		URL:       "http://" + addr,
		interrupt: make(chan os.Signal, 1),
		done:      make(chan error, 1),
	}
	go func() { s.done <- runServer(configStore, s.interrupt) }()
	return s
}

// waitReady waits for the server to answer ping, or returns the error runServer stopped with.
func (s *bootstrapTestServer) waitReady(tb testing.TB) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-s.done:
			s.done <- err
			if err == nil {
				return errors.New("server stopped before it was ready")
			}
			return err
		default:
		}
		if status, _, _ := doRequest(tb, http.MethodGet, s.URL+"/api/v4/system/ping", "", nil); status == http.StatusOK {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	tb.Fatal("server did not become ready")
	return nil
}

func (s *bootstrapTestServer) stop(tb testing.TB) {
	s.interrupt <- syscall.SIGINT
	select {
	case <-s.done:
	case <-time.After(60 * time.Second):
		tb.Error("server did not stop")
	}
}

// startServer starts the server and fails the test unless it becomes ready.
func startServer(tb testing.TB, settings *model.SqlSettings) *bootstrapTestServer {
	s := launchServer(tb, settings, freeListenAddress(tb))
	require.NoError(tb, s.waitReady(tb))
	tb.Cleanup(func() { s.stop(tb) })
	return s
}

// startBootstrappedServer starts the server on an empty database and waits for the env admin to be created:
// ping answers as soon as the listener is open, before bootstrapAdmin has run.
func startBootstrappedServer(tb testing.TB) *bootstrapTestServer {
	s := startServer(tb, emptyDatabase(tb))
	require.Eventually(tb, func() bool {
		status, _ := login(tb, s, bootstrapAdminUsername, bootstrapAdminPassword)
		return status == http.StatusOK
	}, 30*time.Second, 50*time.Millisecond, "the env admin was never created")
	return s
}

var testHTTPClient = &http.Client{Timeout: 10 * time.Second}

// doRequest sends a request with an optional bearer token and JSON body; status is 0 when the request did not get an answer.
func doRequest(tb testing.TB, method, url, token string, body any) (int, http.Header, []byte) {
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	default:
		data, err := json.Marshal(b)
		require.NoError(tb, err)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, reader)
	require.NoError(tb, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return 0, nil, nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, data
}

func login(tb testing.TB, s *bootstrapTestServer, loginID, password string) (int, string) {
	status, header, _ := doRequest(tb, http.MethodPost, s.URL+"/api/v4/users/login", "", map[string]string{"login_id": loginID, "password": password})
	if header == nil {
		return status, ""
	}
	return status, header.Get("Token")
}

func adminToken(tb testing.TB, s *bootstrapTestServer) string {
	status, token := login(tb, s, bootstrapAdminUsername, bootstrapAdminPassword)
	require.Equal(tb, http.StatusOK, status, "the env admin must be able to sign in")
	require.NotEmpty(tb, token)
	return token
}

func getMe(tb testing.TB, s *bootstrapTestServer, token string) *model.User {
	status, _, data := doRequest(tb, http.MethodGet, s.URL+"/api/v4/users/me", token, nil)
	require.Equal(tb, http.StatusOK, status)
	var me model.User
	require.NoError(tb, json.Unmarshal(data, &me))
	return &me
}

// listUsers lists every account (including bots) with the admin token.
func listUsers(tb testing.TB, s *bootstrapTestServer, token string) []*model.User {
	status, _, data := doRequest(tb, http.MethodGet, s.URL+"/api/v4/users?per_page=200", token, nil)
	require.Equal(tb, http.StatusOK, status)
	var users []*model.User
	require.NoError(tb, json.Unmarshal(data, &users))
	return users
}

func humans(users []*model.User) []*model.User {
	var out []*model.User
	for _, u := range users {
		if !u.IsBot {
			out = append(out, u)
		}
	}
	return out
}

func systemAdmins(users []*model.User) []string {
	var out []string
	for _, u := range users {
		if u.IsSystemAdmin() {
			out = append(out, u.Username)
		}
	}
	return out
}

func usernames(users []*model.User) []string {
	var out []string
	for _, u := range users {
		out = append(out, u.Username)
	}
	return out
}

// TestBootstrapAdminOnEmptyServer starts a server with no account and MM_ADMIN_USERNAME/MM_ADMIN_PASSWORD (no MM_ADMIN_EMAIL).
func TestBootstrapAdminOnEmptyServer(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	setAdminEnv(t, map[string]string{adminUsernameEnv: bootstrapAdminUsername, adminPasswordEnv: bootstrapAdminPassword})
	s := startBootstrappedServer(t)

	t.Run("R1a: signing in with the env credentials works", func(t *testing.T) {
		status, token := login(t, s, bootstrapAdminUsername, bootstrapAdminPassword)
		require.Equal(t, http.StatusOK, status)
		require.NotEmpty(t, token, "login returns a session token")
		me := getMe(t, s, token)
		assert.Equal(t, bootstrapAdminUsername, me.Username)
		pingStatus, _, _ := doRequest(t, http.MethodGet, s.URL+"/api/v4/system/ping", "", nil)
		assert.Equal(t, http.StatusOK, pingStatus)
	})

	t.Run("R1b: the env account is a system admin", func(t *testing.T) {
		token := adminToken(t, s)
		me := getMe(t, s, token)
		roles := strings.Fields(me.Roles)
		assert.Contains(t, roles, model.SystemAdminRoleId)
		assert.Contains(t, roles, model.SystemUserRoleId)
		status, _, _ := doRequest(t, http.MethodGet, s.URL+"/api/v4/config", token, nil)
		assert.Equal(t, http.StatusOK, status, "GET /api/v4/config is admin-only")
	})

	t.Run("R1c: without MM_ADMIN_EMAIL the email is <username>@example.com", func(t *testing.T) {
		me := getMe(t, s, adminToken(t, s))
		assert.Equal(t, bootstrapAdminUsername+"@example.com", me.Email)
	})

	t.Run("R1e: the admin's email is verified", func(t *testing.T) {
		me := getMe(t, s, adminToken(t, s))
		assert.True(t, me.EmailVerified)
	})

	t.Run("R1f: a wrong password for the env admin is rejected", func(t *testing.T) {
		status, token := login(t, s, bootstrapAdminUsername, bootstrapAdminPassword+"-wrong")
		assert.Equal(t, http.StatusUnauthorized, status)
		assert.Empty(t, token)
	})

	t.Run("R1g: exactly one human account exists after bootstrap", func(t *testing.T) {
		people := humans(listUsers(t, s, adminToken(t, s)))
		require.Len(t, people, 1, "accounts: %v", usernames(people))
		assert.Equal(t, bootstrapAdminUsername, people[0].Username)
	})

	t.Run("R4a: anonymous POST /api/v4/users is rejected and creates nothing", func(t *testing.T) {
		token := adminToken(t, s)
		before := len(listUsers(t, s, token))
		status, _, body := doRequest(t, http.MethodPost, s.URL+"/api/v4/users", "", map[string]string{
			"email": "anon@x.test", "username": "anon", "password": "Anon-pass-1",
		})
		assert.Contains(t, []int{http.StatusForbidden, http.StatusNotImplemented}, status, "body: %s", body)
		after := listUsers(t, s, token)
		assert.Len(t, after, before)
		assert.NotContains(t, usernames(after), "anon")
	})

	t.Run("anon-signup-rejected-count-unchanged: valid and empty anonymous sign-ups leave the total unchanged", func(t *testing.T) {
		token := adminToken(t, s)
		totalUsers := func() int64 {
			status, _, data := doRequest(t, http.MethodGet, s.URL+"/api/v4/users/stats", token, nil)
			require.Equal(t, http.StatusOK, status)
			var stats model.UsersStats
			require.NoError(t, json.Unmarshal(data, &stats))
			return stats.TotalUsersCount
		}
		before := totalUsers()

		status, _, body := doRequest(t, http.MethodPost, s.URL+"/api/v4/users", "", map[string]string{
			"email": "anon2@x.test", "username": "anon2", "password": "Anon-pass-1",
		})
		assert.True(t, (status >= 400 && status < 500) || status == http.StatusNotImplemented, "valid payload: got %d %s", status, body)

		status, _, body = doRequest(t, http.MethodPost, s.URL+"/api/v4/users", "", "")
		assert.True(t, (status >= 400 && status < 500) || status == http.StatusNotImplemented, "empty body: got %d %s", status, body)

		assert.Equal(t, before, totalUsers())
	})

	t.Run("R4b: open server stays off", func(t *testing.T) {
		status, _, data := doRequest(t, http.MethodGet, s.URL+"/api/v4/config", adminToken(t, s), nil)
		require.Equal(t, http.StatusOK, status)
		var cfg model.Config
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.NotNil(t, cfg.TeamSettings.EnableOpenServer)
		assert.False(t, *cfg.TeamSettings.EnableOpenServer)
	})

	t.Run("R5a: an admin creates a user, who can sign in as a plain user", func(t *testing.T) {
		token := adminToken(t, s)
		status, _, body := doRequest(t, http.MethodPost, s.URL+"/api/v4/users", token, map[string]string{
			"email": "member@x.test", "username": "member", "password": "Member-pass-1",
		})
		require.Equal(t, http.StatusCreated, status, "body: %s", body)

		status, memberToken := login(t, s, "member", "Member-pass-1")
		require.Equal(t, http.StatusOK, status)
		me := getMe(t, s, memberToken)
		assert.Equal(t, []string{model.SystemUserRoleId}, strings.Fields(me.Roles))
	})

	t.Run("R5b: an admin cannot create a second account with the env admin's username", func(t *testing.T) {
		token := adminToken(t, s)
		before := len(listUsers(t, s, token))
		status, _, body := doRequest(t, http.MethodPost, s.URL+"/api/v4/users", token, map[string]string{
			"email": "dup@x.test", "username": bootstrapAdminUsername, "password": "Dup-pass-1",
		})
		assert.Equal(t, http.StatusBadRequest, status, "body: %s", body)
		assert.Contains(t, string(body), "username")
		assert.Len(t, listUsers(t, s, token), before)
	})
}

// TestBootstrapAdminUsesEmailFromEnv: MM_ADMIN_EMAIL, when set, is the admin's email.
func TestBootstrapAdminUsesEmailFromEnv(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	setAdminEnv(t, map[string]string{adminUsernameEnv: bootstrapAdminUsername, adminPasswordEnv: bootstrapAdminPassword, adminEmailEnv: "ops@acme.test"})
	s := startBootstrappedServer(t)

	me := getMe(t, s, adminToken(t, s))
	assert.Equal(t, "ops@acme.test", me.Email)
}

// TestBootstrapAdminRequiresCredentials: a server with no account refuses to start without both credentials (R2).
func TestBootstrapAdminRequiresCredentials(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	for name, env := range map[string]map[string]string{
		"neither":       {},
		"username only": {adminUsernameEnv: bootstrapAdminUsername},
		"password only": {adminPasswordEnv: bootstrapAdminPassword},
	} {
		t.Run(name, func(t *testing.T) {
			setAdminEnv(t, env)
			settings := emptyDatabase(t)
			s := launchServer(t, settings, freeListenAddress(t))
			err := s.waitReady(t)
			if err == nil {
				s.stop(t)
			}
			require.Error(t, err, "the server must not start")
			assert.Contains(t, err.Error(), adminUsernameEnv)
			assert.Contains(t, err.Error(), adminPasswordEnv)

			ss, sErr := sqlstore.New(*settings, mlog.CreateConsoleTestLogger(t), nil, sqlstore.DisableMorphLogging())
			require.NoError(t, sErr)
			defer ss.Close()
			count, cErr := ss.User().Count(model.UserCountOptions{IncludeDeleted: true, IncludeBotAccounts: true})
			require.NoError(t, cErr)
			assert.Zero(t, count, "no account is created")
		})
	}
}

// TestBootstrapAdminSkippedWhenAccountsExist: a server with accounts starts whether or not MM_ADMIN_* are set, and creates no admin (R3).
func TestBootstrapAdminSkippedWhenAccountsExist(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	for name, env := range map[string]map[string]string{
		"without MM_ADMIN_*": {},
		"with MM_ADMIN_*":    {adminUsernameEnv: "newadmin", adminPasswordEnv: "New@dmin-sample1"},
	} {
		t.Run(name, func(t *testing.T) {
			setAdminEnv(t, env)
			settings := emptyDatabase(t)
			seedAccount(t, settings, "existing")
			s := startServer(t, settings)

			status, token := login(t, s, "newadmin", "New@dmin-sample1")
			assert.Equal(t, http.StatusUnauthorized, status, "no account named by MM_ADMIN_USERNAME is created")
			assert.Empty(t, token)

			status, existingToken := login(t, s, "existing", "Seed-password-1")
			require.Equal(t, http.StatusOK, status)
			assert.Equal(t, "existing", getMe(t, s, existingToken).Username)

			ss, err := sqlstore.New(*settings, mlog.CreateConsoleTestLogger(t), nil, sqlstore.DisableMorphLogging())
			require.NoError(t, err)
			defer ss.Close()
			count, err := ss.User().Count(model.UserCountOptions{IncludeDeleted: true})
			require.NoError(t, err)
			assert.EqualValues(t, 1, count, "only the seeded account exists")
		})
	}
}

// TestBootstrapAdminSignupRaceAtStartup sends anonymous sign-ups from the moment the server is launched against an
// empty database until it is ready: none may be accepted, before or after bootstrapAdmin runs (R4c).
func TestBootstrapAdminSignupRaceAtStartup(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	setAdminEnv(t, map[string]string{adminUsernameEnv: bootstrapAdminUsername, adminPasswordEnv: bootstrapAdminPassword})
	settings := emptyDatabase(t)
	addr := freeListenAddress(t)
	url := "http://" + addr

	stop := make(chan struct{})
	var (
		mu       sync.Mutex
		accepted []int
		answered int
	)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				status, _, _ := doRequest(t, http.MethodPost, url+"/api/v4/users", "", map[string]string{
					"username": "intruder", "email": "intruder@x.test", "password": "Intruder-pass-1",
				})
				mu.Lock()
				if status != 0 {
					answered++
				}
				if status == http.StatusCreated || status >= 500 {
					accepted = append(accepted, status)
				}
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
			}
		})
	}

	s := launchServer(t, settings, addr)
	readyErr := s.waitReady(t)
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
	if readyErr == nil {
		t.Cleanup(func() { s.stop(t) })
	}
	require.NoError(t, readyErr, "the server must start")
	require.NotZero(t, answered, "some sign-ups reached the server")

	assert.Empty(t, accepted, "every anonymous POST /api/v4/users is rejected (403/501), never 201/5xx")

	ss, err := sqlstore.New(*settings, mlog.CreateConsoleTestLogger(t), nil, sqlstore.DisableMorphLogging())
	require.NoError(t, err)
	defer ss.Close()
	_, err = ss.User().GetByUsername("intruder")
	assert.Error(t, err, "no 'intruder' account exists")

	users, err := ss.User().GetAll()
	require.NoError(t, err)
	assert.Equal(t, []string{bootstrapAdminUsername}, systemAdmins(users), "the env admin is the only system admin")
}
