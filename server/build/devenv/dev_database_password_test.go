// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

// Package devenv holds functional tests for the local development environment:
// the server Makefile targets and the docker compose files that bring up the
// development database. They drive make and docker compose for real, and talk to a
// development server started with the same datasource `make run-server` exports.
package devenv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	testSecret     = "s3cretXYZ"
	oldPassword    = "mostest"
	missingMessage = "MM_DB_PASSWORD is not set"
	commandTimeout = 3 * time.Minute

	// isolatedProject runs a throwaway copy of the development postgres beside the
	// one the developer (and the running server) uses.
	isolatedProject = "mmdevdbtest"
	isolatedPort    = "15432"
)

var (
	serverDir = func() string {
		dir, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			panic(err)
		}
		return dir
	}()

	dsnRegexp = regexp.MustCompile(`DSN=\[([^]]*)\]`)
)

func skipUnlessDevEnv(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("development environment tests are skipped in short mode")
	}
	if os.Getenv("IS_CI") == "true" {
		t.Skip("development environment tests need a local docker-enabled environment")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
}

// cleanEnv returns the process environment without the variables these tests
// control, plus the given overrides. A value of "-" removes the variable.
func cleanEnv(overrides map[string]string) []string {
	controlled := map[string]bool{
		"MM_DB_PASSWORD":            true,
		"MM_SQLSETTINGS_DATASOURCE": true,
		"IS_CI":                     true,
		"MM_NO_DOCKER":              true,
		"COMPOSE_PROJECT_NAME":      true,
		"POSTGRES_PASSWORD":         true,
	}
	env := []string{}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if controlled[name] {
			continue
		}
		if _, ok := overrides[name]; ok {
			continue
		}
		env = append(env, kv)
	}
	for name, value := range overrides {
		if value == "-" {
			continue
		}
		env = append(env, name+"="+value)
	}
	return env
}

type cmdResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func (r cmdResult) output() string {
	return r.stdout + r.stderr
}

func run(t *testing.T, env map[string]string, name string, args ...string) cmdResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = serverDir
	cmd.Env = cleanEnv(env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, ctx.Err(), "%s %v timed out", name, args)

	res := cmdResult{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		res.exitCode = exitErr.ExitCode()
	} else {
		require.NoError(t, err, "%s %v could not run", name, args)
	}
	return res
}

func composeConfig(t *testing.T, file string, env map[string]string) map[string]any {
	t.Helper()
	res := run(t, env, "docker", "compose", "-f", file, "config", "--format", "json")
	require.Equal(t, 0, res.exitCode, res.output())
	var cfg map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &cfg))
	return cfg
}

func serviceEnv(t *testing.T, cfg map[string]any, service string) map[string]any {
	t.Helper()
	services, ok := cfg["services"].(map[string]any)
	require.True(t, ok, "rendered config has no services")
	svc, ok := services[service].(map[string]any)
	require.True(t, ok, "rendered config has no %s service", service)
	env, _ := svc["environment"].(map[string]any)
	return env
}

// exportedDSN prints the MM_SQLSETTINGS_DATASOURCE a Makefile target exports, without
// running the target: target-specific variables propagate to the dsnprobe prerequisite.
func exportedDSN(t *testing.T, target string, env map[string]string) string {
	t.Helper()
	res := run(t, env, "make", "-n",
		"--eval", target+": dsnprobe",
		"--eval", `dsnprobe: ; @echo "DSN=[$(MM_SQLSETTINGS_DATASOURCE)]"`,
		target)
	m := dsnRegexp.FindStringSubmatch(res.output())
	require.NotNil(t, m, "no DSN printed for %s: %s", target, res.output())
	return m[1]
}

func dsnPassword(t *testing.T, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	require.NoError(t, err, "datasource %q is not a valid URL", dsn)
	require.NotNil(t, u.User, "datasource %q has no credentials", dsn)
	password, _ := u.User.Password()
	return password
}

func psql(t *testing.T, port, password string) cmdResult {
	t.Helper()
	return run(t, map[string]string{"PGPASSWORD": password, "PGCONNECT_TIMEOUT": "10"},
		"psql", "-h", "127.0.0.1", "-p", port, "-U", "mmuser", "-d", "mattermost_test", "-tAc", "select 1")
}

func dockerContainers(t *testing.T) string {
	t.Helper()
	res := run(t, nil, "docker", "ps", "-a", "--format", "{{.ID}} {{.Names}} {{.Status}}")
	require.Equal(t, 0, res.exitCode, res.output())
	return res.stdout
}

// --- R1: the compose files take the postgres password from MM_DB_PASSWORD ---

func TestComposePostgresPasswordFromSecret(t *testing.T) {
	skipUnlessDevEnv(t)

	for _, file := range []string{"docker-compose.makefile.yml", "docker-compose.yaml"} {
		t.Run(file, func(t *testing.T) {
			env := serviceEnv(t, composeConfig(t, file, map[string]string{"MM_DB_PASSWORD": testSecret}), "postgres")
			assert.Equal(t, testSecret, env["POSTGRES_PASSWORD"])
			assert.Equal(t, "mmuser", env["POSTGRES_USER"], "POSTGRES_USER is still inherited from build/docker-compose.common.yml")
			assert.Equal(t, "mattermost_test", env["POSTGRES_DB"], "POSTGRES_DB is still inherited from build/docker-compose.common.yml")
		})
	}
}

func TestComposeRefusesWithoutSecret(t *testing.T) {
	skipUnlessDevEnv(t)

	for _, file := range []string{"docker-compose.makefile.yml", "docker-compose.yaml"} {
		for name, value := range map[string]string{"unset": "-", "empty": ""} {
			t.Run(file+"/"+name, func(t *testing.T) {
				res := run(t, map[string]string{"MM_DB_PASSWORD": value}, "docker", "compose", "-f", file, "config")
				assert.NotEqual(t, 0, res.exitCode, "compose must refuse to render without MM_DB_PASSWORD")
				assert.Contains(t, res.stderr, missingMessage)
			})
		}
	}
}

func TestComposeOtherServicesUnchanged(t *testing.T) {
	skipUnlessDevEnv(t)

	for _, file := range []string{"docker-compose.makefile.yml", "docker-compose.yaml"} {
		t.Run(file, func(t *testing.T) {
			master := run(t, nil, "git", "show", "origin/master:server/"+file)
			require.Equal(t, 0, master.exitCode, master.output())
			// Same directory, so relative paths (extends, build contexts, volumes) resolve the same way.
			masterFile := filepath.Join(serverDir, ".devenv-master-"+file)
			require.NoError(t, os.WriteFile(masterFile, []byte(master.stdout), 0600))
			defer os.Remove(masterFile)

			env := map[string]string{"MM_DB_PASSWORD": testSecret}
			before := composeConfig(t, filepath.Base(masterFile), env)["services"].(map[string]any)
			after := composeConfig(t, file, env)["services"].(map[string]any)

			require.Equal(t, len(before), len(after), "set of services changed")
			for name, svc := range before {
				if name == "postgres" {
					continue
				}
				assert.Equal(t, svc, after[name], "service %s changed", name)
			}
		})
	}
}

// isolatedComposeArgs returns the compose arguments that bring up the development postgres service of
// docker-compose.makefile.yml under its own project, container, port and network.
func isolatedComposeArgs(t *testing.T) []string {
	t.Helper()
	override := filepath.Join(t.TempDir(), "isolated.yml")
	require.NoError(t, os.WriteFile(override, []byte(fmt.Sprintf(`services:
  postgres:
    container_name: %[1]s-postgres
    ports: !override
      - "%[2]s:5432"
    networks: !override
      - isolated
networks:
  isolated: {}
`, isolatedProject, isolatedPort)), 0600))
	return []string{"compose", "-p", isolatedProject, "-f", "docker-compose.makefile.yml", "-f", override}
}

func isolatedDown(t *testing.T, args []string) {
	t.Helper()
	res := run(t, map[string]string{"MM_DB_PASSWORD": testSecret}, "docker", append(args, "down", "-v")...)
	require.Equal(t, 0, res.exitCode, res.output())
}

func isolatedUp(t *testing.T, args []string, password string) cmdResult {
	t.Helper()
	return run(t, map[string]string{"MM_DB_PASSWORD": password}, "docker", append(args, "up", "-d", "--wait", "postgres")...)
}

func TestFreshDatabaseAcceptsSecret(t *testing.T) {
	skipUnlessDevEnv(t)

	args := isolatedComposeArgs(t)
	isolatedDown(t, args)
	t.Cleanup(func() { isolatedDown(t, args) })

	res := isolatedUp(t, args, testSecret)
	require.Equal(t, 0, res.exitCode, res.output())

	ok := psql(t, isolatedPort, testSecret)
	assert.Equal(t, 0, ok.exitCode, ok.output())
	assert.Equal(t, "1", strings.TrimSpace(ok.stdout))

	old := psql(t, isolatedPort, oldPassword)
	assert.NotEqual(t, 0, old.exitCode, "the old default password must no longer be accepted")
	assert.Contains(t, old.stderr, "password authentication failed")
}

// R6a / F7: a postgres-14-data volume initialised before the change (with 'mostest')
// must still be reachable after the developer sets MM_DB_PASSWORD.
func TestExistingVolumeReachableWithSecret(t *testing.T) {
	skipUnlessDevEnv(t)

	args := isolatedComposeArgs(t)
	isolatedDown(t, args)
	t.Cleanup(func() { isolatedDown(t, args) })

	// Initialise the volume the way origin/master did.
	res := isolatedUp(t, args, oldPassword)
	require.Equal(t, 0, res.exitCode, res.output())
	require.Equal(t, 0, psql(t, isolatedPort, oldPassword).exitCode)
	res = run(t, map[string]string{"MM_DB_PASSWORD": oldPassword}, "docker", append(args, "stop")...)
	require.Equal(t, 0, res.exitCode, res.output())

	// Restart on the existing volume with the secret.
	res = isolatedUp(t, args, testSecret)
	require.Equal(t, 0, res.exitCode, res.output())

	conn := psql(t, isolatedPort, testSecret)
	assert.Equal(t, 0, conn.exitCode,
		"an existing postgres-14-data volume is not reachable with MM_DB_PASSWORD and nothing tells the developer how to migrate: %s", conn.output())
}

// --- R2: make start-docker requires MM_DB_PASSWORD (local docker-enabled dev only) ---

func TestStartDockerRefusesWithoutSecret(t *testing.T) {
	skipUnlessDevEnv(t)

	for name, value := range map[string]string{"unset": "-", "empty": ""} {
		t.Run(name, func(t *testing.T) {
			before := dockerContainers(t)
			res := run(t, map[string]string{"MM_DB_PASSWORD": value, "IS_CI": "false"}, "make", "start-docker")
			assert.NotEqual(t, 0, res.exitCode)
			assert.Contains(t, res.output(), "MM_DB_PASSWORD is not set: the development database takes its password from that secret")
			assert.NotContains(t, res.output(), "Starting docker containers", "no docker command may run")
			assert.Equal(t, before, dockerContainers(t), "no container may be created or changed")
		})
	}
}

func TestStartDockerSkippedWithoutDocker(t *testing.T) {
	skipUnlessDevEnv(t)

	cases := map[string]struct {
		env     map[string]string
		message string
	}{
		"IS_CI=true":        {map[string]string{"IS_CI": "true"}, "CI Build: skipping docker start"},
		"MM_NO_DOCKER=true": {map[string]string{"IS_CI": "false", "MM_NO_DOCKER": "true"}, "No Docker Enabled: skipping docker start"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tc.env["MM_DB_PASSWORD"] = "-"
			res := run(t, tc.env, "make", "-n", "start-docker")
			assert.Equal(t, 0, res.exitCode, res.output())
			assert.Contains(t, res.output(), tc.message)
			assert.NotContains(t, res.output(), missingMessage)
		})
	}
}

func TestStartDockerWithSecret(t *testing.T) {
	skipUnlessDevEnv(t)

	// Postgres only: the other development images are not needed to check the database.
	res := run(t, map[string]string{"MM_DB_PASSWORD": testSecret, "IS_CI": "false"},
		"make", "start-docker", "ENABLED_DOCKER_SERVICES=postgres", "DOCKER_SERVICES_OVERRIDE=true")
	require.Equal(t, 0, res.exitCode, res.output())

	status := run(t, nil, "docker", "inspect", "-f", "{{.State.Health.Status}}", "mattermost-postgres")
	assert.Equal(t, "healthy", strings.TrimSpace(status.stdout), status.output())

	conn := psql(t, "5432", testSecret)
	assert.Equal(t, 0, conn.exitCode, conn.output())
}

// --- R3: make run-server connects with MM_DB_PASSWORD; a caller's datasource wins ---

func TestRunServerExportsSecretDatasource(t *testing.T) {
	skipUnlessDevEnv(t)

	dsn := exportedDSN(t, "run-server", map[string]string{"MM_DB_PASSWORD": testSecret, "IS_CI": "false"})
	assert.Equal(t, "postgres://mmuser:"+testSecret+"@localhost/mattermost_test?sslmode=disable&connect_timeout=10&binary_parameters=yes", dsn)
}

func TestRunServerKeepsCallerDatasource(t *testing.T) {
	skipUnlessDevEnv(t)

	caller := "postgres://someone:theirs@db.example/their_db?sslmode=disable"
	dsn := exportedDSN(t, "run-server", map[string]string{
		"MM_DB_PASSWORD":            testSecret,
		"IS_CI":                     "false",
		"MM_SQLSETTINGS_DATASOURCE": caller,
	})
	assert.Equal(t, caller, dsn)
}

// R3e / F4: without docker and without the secret, run-server must leave the
// developer's config.json DataSource alone instead of exporting an empty password.
func TestRunServerWithoutDockerKeepsConfigDatasource(t *testing.T) {
	skipUnlessDevEnv(t)

	for name, env := range map[string]map[string]string{
		"MM_NO_DOCKER=true": {"IS_CI": "false", "MM_NO_DOCKER": "true", "MM_DB_PASSWORD": "-"},
		"IS_CI=true":        {"IS_CI": "true", "MM_DB_PASSWORD": "-"},
	} {
		t.Run(name, func(t *testing.T) {
			dsn := exportedDSN(t, "run-server", env)
			assert.NotContains(t, dsn, "postgres://mmuser:@", "run-server exports a password-less datasource that overrides config.json")
		})
	}
}

// --- R4: the other development-database consumers next to the change ---

// F1: run-node still hardcodes the old password.
func TestRunNodeUsesSecret(t *testing.T) {
	skipUnlessDevEnv(t)

	dsn := exportedDSN(t, "run-node", map[string]string{"MM_DB_PASSWORD": testSecret, "IS_CI": "false"})
	assert.Equal(t, testSecret, dsnPassword(t, dsn), "run-node datasource: %s", dsn)
	assert.NotContains(t, dsn, oldPassword)
}

// F3: the other targets that start the server or CLI against the development database.
func TestOtherServerTargetsUseSecret(t *testing.T) {
	skipUnlessDevEnv(t)

	for _, target := range []string{"debug-server", "debug-server-headless", "run-server-faketime", "run-cli"} {
		t.Run(target, func(t *testing.T) {
			dsn := exportedDSN(t, target, map[string]string{"MM_DB_PASSWORD": testSecret, "IS_CI": "false"})
			assert.Contains(t, dsn, ":"+testSecret+"@", "%s does not export a datasource with MM_DB_PASSWORD (got %q), so the server falls back to the built-in 'mostest' default", target, dsn)
		})
	}
}

// F2: the HA nodes in docker-compose.yaml talk to that file's postgres service.
func TestHANodesUseSecret(t *testing.T) {
	skipUnlessDevEnv(t)

	cfg := composeConfig(t, "docker-compose.yaml", map[string]string{"MM_DB_PASSWORD": testSecret})
	for _, node := range []string{"leader", "follower", "follower2"} {
		t.Run(node, func(t *testing.T) {
			dsn, _ := serviceEnv(t, cfg, node)["MM_SQLSETTINGS_DATASOURCE"].(string)
			require.NotEmpty(t, dsn)
			assert.Equal(t, testSecret, dsnPassword(t, dsn), "%s datasource: %s", node, dsn)
			assert.NotContains(t, dsn, oldPassword)
		})
	}
}

// F5: targets that only stop or remove containers must not need the secret. They run
// against an unused compose project so the developer's containers are left alone.
func TestStopTargetsWorkWithoutSecret(t *testing.T) {
	skipUnlessDevEnv(t)

	for _, target := range []string{"stop-docker", "clean-docker", "stop-haserver"} {
		t.Run(target, func(t *testing.T) {
			// The Makefile exports its own COMPOSE_PROJECT_NAME, so override it on the command line.
			res := run(t, map[string]string{"MM_DB_PASSWORD": "-", "IS_CI": "false"},
				"make", target, "COMPOSE_PROJECT_NAME="+isolatedProject+"-stop")
			assert.Equal(t, 0, res.exitCode, "make %s fails without MM_DB_PASSWORD: %s", target, res.output())
		})
	}
}

// --- R5: any valid secret works as the password (F6) ---

func TestRunServerDatasourceWithURLSpecialPassword(t *testing.T) {
	skipUnlessDevEnv(t)

	secret := "p@ss/w#rd?:%"
	dsn := exportedDSN(t, "run-server", map[string]string{"MM_DB_PASSWORD": secret, "IS_CI": "false"})
	u, err := url.Parse(dsn)
	require.NoError(t, err, "run-server exports an unparsable datasource for a password with URL-special characters: %q", dsn)
	require.NotNil(t, u.User)
	password, _ := u.User.Password()
	assert.Equal(t, secret, password, "datasource %q", dsn)
	assert.Equal(t, "localhost", u.Host, "datasource %q", dsn)
}

func TestRunServerDatasourceWithDollarPassword(t *testing.T) {
	skipUnlessDevEnv(t)

	secret := "pa$word"
	dsn := exportedDSN(t, "run-server", map[string]string{"MM_DB_PASSWORD": secret, "IS_CI": "false"})
	assert.Equal(t, secret, dsnPassword(t, dsn), "make re-expanded the password in %q", dsn)
}

// --- The running development server, connected with the secret datasource ---

func serverURL() string {
	if u := os.Getenv("MM_DEVENV_SERVER_URL"); u != "" {
		return u
	}
	return "http://localhost:8065"
}

func requireServer(t *testing.T) *model.Client4 {
	t.Helper()
	skipUnlessDevEnv(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Get(serverURL() + "/api/v4/system/ping")
	if err != nil {
		t.Skipf("no development server at %s: %v", serverURL(), err)
	}
	resp.Body.Close()
	client := model.NewAPIv4Client(serverURL())
	client.HTTPClient.Timeout = 15 * time.Second
	return client
}

func newSysadmin(t *testing.T, client *model.Client4) (*model.Client4, *model.User) {
	t.Helper()
	ctx := context.Background()
	id := model.NewId()
	password := "Pa$$w0rd-" + id
	user, resp, err := client.CreateUser(ctx, &model.User{
		Email:    "devenv-" + id + "@example.com",
		Username: "devenv" + id[:10],
		Password: password,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	if !strings.Contains(user.Roles, model.SystemAdminRoleId) {
		socket := os.Getenv("MM_DEVENV_LOCAL_SOCKET")
		if socket == "" {
			socket = "/tmp/mm_local.socket"
		}
		local := model.NewAPIv4SocketClient(socket)
		_, err = local.UpdateUserRoles(ctx, user.Id, model.SystemUserRoleId+" "+model.SystemAdminRoleId)
		require.NoError(t, err, "promote user through local mode")
	}

	_, _, err = client.Login(ctx, user.Email, password)
	require.NoError(t, err)
	return client, user
}

func TestServerPingWithDatabase(t *testing.T) {
	client := requireServer(t)

	status, resp, err := client.GetPingWithFullServerStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, model.StatusOk, status["status"])
	assert.Equal(t, model.StatusOk, status["database_status"])
}

func TestServerWritesAndReadsThroughDatabase(t *testing.T) {
	client, _ := newSysadmin(t, requireServer(t))
	ctx := context.Background()

	name := "devenv-" + model.NewId()[:10]
	team, resp, err := client.CreateTeam(ctx, &model.Team{Name: name, DisplayName: "Dev env " + name, Type: model.TeamOpen})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	got, resp, err := client.GetTeam(ctx, team.Id, "")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, team.Id, got.Id)
	assert.Equal(t, name, got.Name)
	assert.Equal(t, "Dev env "+name, got.DisplayName)
}

func TestServerConfigMasksDatabasePassword(t *testing.T) {
	client, _ := newSysadmin(t, requireServer(t))

	resp, err := client.DoAPIGet(context.Background(), "/config", "")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var cfg model.Config
	require.NoError(t, json.Unmarshal(body, &cfg))
	require.NotNil(t, cfg.SqlSettings.DataSource)
	assert.NotContains(t, string(body), testSecret, "GET /api/v4/config leaks the database password")
}
