// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package server

// Separate-instance harness: each scenario starts its own `mattermost` process, built by .skyramp/sut/setup.sh,
// on its own empty database in the SUT's Postgres container, and controls exactly which MM_* variables it sees.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	pgContainer   = "skyramp-mm-postgres"
	startupWindow = 120 * time.Second
	bootPassword  = "Sys@dmin-sample1"
	noAdminEnvMsg = "set MM_ADMIN_USERNAME and MM_ADMIN_PASSWORD"
	adminCreated  = "Created the system admin from MM_ADMIN_USERNAME"
	listeningMsg  = "Server is listening on"
)

func mattermostBinary(t *testing.T) string {
	t.Helper()
	state := os.Getenv("SUT_STATE_DIR")
	if state == "" {
		tmp := os.Getenv("RUNNER_TEMP")
		if tmp == "" {
			tmp = "/tmp"
		}
		state = filepath.Join(tmp, "skyramp-mattermost-sut")
	}
	bin := filepath.Join(state, "bin", "mattermost")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no mattermost binary at %s (run .skyramp/sut/setup.sh): %v", bin, err)
	}
	return bin
}

func serverDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "server")
}

func psql(db, query string) (string, error) {
	out, err := exec.Command("docker", "exec", pgContainer, "psql", "-U", "mmuser", "-d", db, "-tAc", query).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// freshDB creates an empty database bs_<n>, dropped when the test ends.
func freshDB(t *testing.T, n int) string {
	t.Helper()
	db := fmt.Sprintf("bs_%d", n)
	psql("postgres", "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)")
	if out, err := psql("postgres", "CREATE DATABASE "+db); err != nil {
		t.Fatalf("create database %s: %v: %s", db, err, out)
	}
	t.Cleanup(func() { psql("postgres", "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)") })
	return db
}

func nonBotUserCount(t *testing.T, db string) int {
	t.Helper()
	out, err := psql(db, "select count(*) from users u where not exists (select 1 from bots b where b.userid = u.id)")
	if err != nil {
		t.Fatalf("count users in %s: %v: %s", db, err, out)
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		t.Fatalf("count users in %s: %q", db, out)
	}
	return n
}

type instance struct {
	t       *testing.T
	port    int
	cmd     *exec.Cmd
	logPath string
	done    chan struct{}
	exitErr error
}

func (i *instance) url() string { return fmt.Sprintf("http://localhost:%d", i.port) }

func (i *instance) log() string {
	b, _ := os.ReadFile(i.logPath)
	return string(b)
}

func (i *instance) exited() bool {
	select {
	case <-i.done:
		return true
	default:
		return false
	}
}

func (i *instance) exitCode() int {
	if i.exitErr == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(i.exitErr, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// launch starts the binary (args e.g. "server", or none) on db with only the harness's MM_* settings plus adminEnv.
func launch(t *testing.T, db string, port int, adminEnv map[string]string, args ...string) *instance {
	t.Helper()
	tmp := t.TempDir()
	dsn := fmt.Sprintf("postgres://mmuser:mostest@localhost:5432/%s?sslmode=disable&connect_timeout=10", db)
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"MM_CONFIG=" + dsn,
		"MM_SQLSETTINGS_DATASOURCE=" + dsn,
		"MM_SQLSETTINGS_DRIVERNAME=postgres",
		fmt.Sprintf("MM_SERVICESETTINGS_LISTENADDRESS=:%d", port),
		fmt.Sprintf("MM_SERVICESETTINGS_SITEURL=http://localhost:%d", port),
		"MM_SERVICESETTINGS_ENABLELOCALMODE=false",
		"MM_PLUGINSETTINGS_ENABLE=false",
		"MM_LOGSETTINGS_FILELOCATION=" + tmp,
		"MM_FILESETTINGS_DIRECTORY=" + tmp + "/data/",
	}
	for k, v := range adminEnv {
		env = append(env, k+"="+v)
	}
	logPath := filepath.Join(tmp, "console.log")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(mattermostBinary(t), args...)
	cmd.Dir = serverDir(t)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mattermost: %v", err)
	}
	inst := &instance{t: t, port: port, cmd: cmd, logPath: logPath, done: make(chan struct{})}
	go func() {
		inst.exitErr = cmd.Wait()
		f.Close()
		close(inst.done)
	}()
	t.Cleanup(inst.stop)
	return inst
}

// waitReady waits until ping answers 200 (true) or the process exits (false), within the start-up window.
func (i *instance) waitReady() bool {
	c := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(startupWindow)
	for time.Now().Before(deadline) {
		if i.exited() {
			return false
		}
		if resp, err := c.Get(i.url() + "/api/v4/system/ping"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	i.t.Fatalf("server on :%d neither answered nor exited within %s; log tail:\n%s", i.port, startupWindow, tail(i.log()))
	return false
}

func (i *instance) mustBeReady() {
	if !i.waitReady() {
		i.t.Fatalf("server on :%d exited (code %d) instead of starting; log tail:\n%s", i.port, i.exitCode(), tail(i.log()))
	}
}

func (i *instance) stop() {
	if i.exited() {
		return
	}
	i.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-i.done:
	case <-time.After(30 * time.Second):
		i.cmd.Process.Kill()
		<-i.done
	}
}

func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 25 {
		lines = lines[len(lines)-25:]
	}
	return strings.Join(lines, "\n")
}

func portClosed(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), time.Second)
	if err != nil {
		return true
	}
	c.Close()
	return false
}

func adminEnv(username, password string) map[string]string {
	return map[string]string{"MM_ADMIN_USERNAME": username, "MM_ADMIN_PASSWORD": password}
}

// assertRefusesToStart checks the process exits non-zero with logMsg, stops listening and creates no user.
func assertRefusesToStart(t *testing.T, inst *instance, db, logMsg string) {
	t.Helper()
	if inst.waitReady() {
		t.Fatalf("server started; want it to refuse to start")
	}
	if code := inst.exitCode(); code == 0 {
		t.Errorf("exit code = 0, want non-zero")
	}
	if !strings.Contains(inst.log(), logMsg) {
		t.Errorf("log does not contain %q; tail:\n%s", logMsg, tail(inst.log()))
	}
	if !portClosed(inst.port) {
		t.Errorf("something still listens on :%d after exit", inst.port)
	}
	if n := nonBotUserCount(t, db); n != 0 {
		t.Errorf("database holds %d users, want 0", n)
	}
}

// R2a
func TestEmptyDBWithoutAdminEnvRefusesToStart(t *testing.T) {
	db := freshDB(t, 1)
	inst := launch(t, db, 8071, nil, "server")
	assertRefusesToStart(t, inst, db, noAdminEnvMsg)
}

// R5a: the bare `mattermost` entrypoint runs the same server and is gated the same way.
func TestEmptyDBWithoutAdminEnvRefusesToStartNoSubcommand(t *testing.T) {
	db := freshDB(t, 10)
	inst := launch(t, db, 8080, nil)
	assertRefusesToStart(t, inst, db, noAdminEnvMsg)
}

// R2d
func TestEmptyDBWithUnusableAdminEnvRefusesToStart(t *testing.T) {
	t.Run("password below policy", func(t *testing.T) {
		db := freshDB(t, 2)
		inst := launch(t, db, 8072, adminEnv("sysadmin", "abc"), "server")
		assertRefusesToStart(t, inst, db, "failed to create the system admin")
	})
	t.Run("invalid username", func(t *testing.T) {
		db := freshDB(t, 3)
		inst := launch(t, db, 8073, adminEnv("Bad User!", bootPassword), "server")
		assertRefusesToStart(t, inst, db, "failed to create the system admin")
	})
}

// R3a, R3b, R3c: one database, restarted with different MM_ADMIN_* settings.
func TestExistingAccountServerIsUnaffectedByAdminEnv(t *testing.T) {
	db := freshDB(t, 4)
	const port = 8074
	first := launch(t, db, port, adminEnv("boot1", bootPassword), "server")
	first.mustBeReady()
	first.stop()

	t.Run("R3a restart without MM_ADMIN_*", func(t *testing.T) {
		inst := launch(t, db, port, nil, "server")
		inst.mustBeReady()
		if _, r := login(t, inst.url(), "boot1", bootPassword); r.Status != http.StatusOK {
			t.Errorf("login boot1: status %d, want 200: %s", r.Status, r.Body)
		}
		inst.stop()
	})

	t.Run("R3b restart with the same MM_ADMIN_*", func(t *testing.T) {
		inst := launch(t, db, port, adminEnv("boot1", bootPassword), "server")
		inst.mustBeReady()
		if n := nonBotUserCount(t, db); n != 1 {
			t.Errorf("non-bot users = %d, want 1", n)
		}
		inst.stop()
	})

	t.Run("R3c restart with different MM_ADMIN_*", func(t *testing.T) {
		inst := launch(t, db, port, adminEnv("other", "Other@pass-123"), "server")
		inst.mustBeReady()
		token, r := login(t, inst.url(), "boot1", bootPassword)
		if r.Status != http.StatusOK {
			t.Fatalf("login boot1: status %d: %s", r.Status, r.Body)
		}
		if l := doRequest(t, http.MethodGet, inst.url()+"/api/v4/users/username/other", token, nil); l.Status != http.StatusNotFound {
			t.Errorf("GET /users/username/other: status %d, want 404", l.Status)
		}
		if _, r := login(t, inst.url(), "other", "Other@pass-123"); r.Status != http.StatusUnauthorized {
			t.Errorf("login other: status %d, want 401", r.Status)
		}
		inst.stop()
	})
}

// R4b: EnableOpenServer left at its default (false).
func TestClosedServerRejectsSignUpAfterBootstrap(t *testing.T) {
	db := freshDB(t, 5)
	inst := launch(t, db, 8075, adminEnv("boot2", bootPassword), "server")
	inst.mustBeReady()

	r := doRequest(t, http.MethodPost, inst.url()+"/api/v4/users", "", map[string]string{
		"username": "intruder", "email": "intruder@x.test", "password": "Passw0rd!x",
	})
	if r.Status != http.StatusForbidden {
		t.Errorf("unauthenticated POST /api/v4/users: status %d, want 403: %s", r.Status, r.Body)
	}
	if !strings.Contains(string(r.Body), "api.user.create_user.no_open_server") {
		t.Errorf("error id is not api.user.create_user.no_open_server: %s", r.Body)
	}
	token, lr := login(t, inst.url(), "boot2", bootPassword)
	if lr.Status != http.StatusOK {
		t.Fatalf("login boot2: status %d: %s", lr.Status, lr.Body)
	}
	if l := doRequest(t, http.MethodGet, inst.url()+"/api/v4/users/username/intruder", token, nil); l.Status != http.StatusNotFound {
		t.Errorf("GET /users/username/intruder: status %d, want 404", l.Status)
	}
}

type signupResult struct {
	status int
	roles  string
	user   string
}

// signupStorm runs workers that post unauthenticated sign-ups to port from before launch until stop is closed.
func signupStorm(port, workers int, prefix string, stop <-chan struct{}) func() []signupResult {
	var (
		mu      sync.Mutex
		results []signupResult
		wg      sync.WaitGroup
	)
	c := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://localhost:%d/api/v4/users", port)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				name := fmt.Sprintf("%s%d_%d", prefix, w, i)
				body := fmt.Sprintf(`{"username":%q,"email":"%s@x.test","password":"Passw0rd!x"}`, name, name)
				resp, err := c.Post(url, "application/json", strings.NewReader(body))
				if err != nil {
					time.Sleep(time.Millisecond) // not listening yet
					continue
				}
				var res struct {
					Roles string `json:"roles"`
				}
				json.NewDecoder(resp.Body).Decode(&res)
				resp.Body.Close()
				mu.Lock()
				results = append(results, signupResult{status: resp.StatusCode, roles: res.Roles, user: name})
				mu.Unlock()
			}
		}(w)
	}
	return func() []signupResult {
		wg.Wait()
		return results
	}
}

// raceStartup launches a server with adminUser while an 8-worker sign-up storm hits its port, stopping 5s after ping.
func raceStartup(t *testing.T, n int, adminUser, prefix string) (*instance, string, []signupResult) {
	t.Helper()
	db := freshDB(t, n)
	port := 8070 + n
	stop := make(chan struct{})
	collect := signupStorm(port, 8, prefix, stop)
	time.Sleep(time.Second)
	inst := launch(t, db, port, adminEnv(adminUser, bootPassword), "server")
	ready := inst.waitReady()
	if ready {
		time.Sleep(5 * time.Second)
	}
	close(stop)
	results := collect()
	if !ready {
		t.Fatalf("server exited (code %d) instead of starting; log tail:\n%s", inst.exitCode(), tail(inst.log()))
	}
	return inst, db, results
}

func adminsAmongSignups(results []signupResult) (created int, admins []string) {
	for _, r := range results {
		if r.status == http.StatusCreated {
			created++
			if roleSet(r.roles)["system_admin"] {
				admins = append(admins, r.user)
			}
		}
	}
	return created, admins
}

func assertIsSystemAdmin(t *testing.T, base, username string) {
	t.Helper()
	token, r := login(t, base, username, bootPassword)
	if r.Status != http.StatusOK {
		t.Errorf("login %s: status %d, want 200: %s", username, r.Status, r.Body)
		return
	}
	me := doRequest(t, http.MethodGet, base+"/api/v4/users/me", token, nil)
	if !roleSet(me.json(t)["roles"])["system_admin"] {
		t.Errorf("%s is not a system admin: %s", username, me.Body)
	}
}

// R4c (defect F1)
func TestSignUpCannotClaimFirstAccountDuringStartup(t *testing.T) {
	inst, _, results := raceStartup(t, 6, "boot3", "racer")
	created, admins := adminsAmongSignups(results)
	t.Logf("%d sign-up responses, %d created", len(results), created)
	if len(admins) > 0 {
		t.Errorf("%d accounts created by unauthenticated POST /api/v4/users hold system_admin: %v", len(admins), admins)
	}
	assertIsSystemAdmin(t, inst.url(), "boot3")
	log := inst.log()
	ia, il := strings.Index(log, adminCreated), strings.Index(log, listeningMsg)
	if ia < 0 || il < 0 || ia > il {
		t.Errorf("want %q logged before %q (admin index %d, listening index %d)", adminCreated, listeningMsg, ia, il)
	}
}

// signup-race-at-startup-no-admin-takeover (defect F1)
func TestEnvAdminIsFirstAccountDespiteSignUpRace(t *testing.T) {
	inst, db, _ := raceStartup(t, 7, "boot4", "rc")
	first, err := psql(db, "select username from users u where not exists (select 1 from bots b where b.userid = u.id) order by createat limit 1")
	if err != nil {
		t.Fatalf("query earliest user: %v: %s", err, first)
	}
	if first != "boot4" {
		t.Errorf("earliest-created user = %q, want boot4", first)
	}
	admins, err := psql(db, "select username from users where roles like '%system_admin%' and username <> 'boot4'")
	if err != nil {
		t.Fatalf("query admins: %v: %s", err, admins)
	}
	if admins != "" {
		t.Errorf("sign-up accounts holding system_admin: %s", strings.ReplaceAll(admins, "\n", ", "))
	}
	assertIsSystemAdmin(t, inst.url(), "boot4")
}

// startup-log-admin-before-listen (defect F1)
func TestAdminCreatedBeforeServerListens(t *testing.T) {
	db := freshDB(t, 8)
	inst := launch(t, db, 8078, adminEnv("boot5", bootPassword), "server")
	inst.mustBeReady()
	log := inst.log()
	ia, il := strings.Index(log, adminCreated), strings.Index(log, listeningMsg)
	if ia < 0 {
		t.Fatalf("log has no %q line", adminCreated)
	}
	if il < 0 {
		t.Fatalf("log has no %q line", listeningMsg)
	}
	if ia > il {
		t.Errorf("%q is logged after %q: HTTP is accepted before the admin exists", adminCreated, listeningMsg)
	}
}

// startup-signup-closed-server-rejected (defect F1): EnableOpenServer left at its default (false).
func TestClosedServerRejectsSignUpDuringStartup(t *testing.T) {
	_, db, results := raceStartup(t, 9, "boot6", "cl")
	statuses := map[int]int{}
	for _, r := range results {
		statuses[r.status]++
	}
	t.Logf("sign-up statuses: %v", statuses)
	if statuses[http.StatusCreated] > 0 {
		t.Errorf("%d unauthenticated sign-ups returned 201 on a server with EnableOpenServer=false", statuses[http.StatusCreated])
	}
	users, err := psql(db, "select username || ':' || roles from users u where not exists (select 1 from bots b where b.userid = u.id)")
	if err != nil {
		t.Fatalf("query users: %v: %s", err, users)
	}
	if !strings.HasPrefix(users, "boot6:") || strings.Contains(users, "\n") || !strings.Contains(users, "system_admin") {
		t.Errorf("non-bot users = %q, want only boot6 with system_admin", strings.ReplaceAll(users, "\n", ", "))
	}
}
