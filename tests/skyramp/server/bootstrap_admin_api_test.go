// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package server

// Against the running SUT, which setup.sh starts on a fresh database with MM_ADMIN_USERNAME, MM_ADMIN_PASSWORD
// and MM_ADMIN_EMAIL set.

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

func adminSession(t *testing.T) string {
	t.Helper()
	token, r := login(t, baseURL(), sutAdminUsername, sutAdminPassword)
	if r.Status != http.StatusOK || token == "" {
		t.Fatalf("login as %s: status %d, token present %v: %s", sutAdminUsername, r.Status, token != "", r.Body)
	}
	return token
}

// R1a
func TestBootstrappedServerAnswersPing(t *testing.T) {
	r := doRequest(t, http.MethodGet, baseURL()+"/api/v4/system/ping?get_server_status=true", "", nil)
	if r.Status != http.StatusOK {
		t.Fatalf("ping: status %d: %s", r.Status, r.Body)
	}
	if got := r.json(t)["status"]; got != "OK" {
		t.Errorf("ping status = %v, want OK", got)
	}
}

// R1b
func TestLoginWithEnvAdminCredentials(t *testing.T) {
	token, r := login(t, baseURL(), sutAdminUsername, sutAdminPassword)
	if r.Status != http.StatusOK {
		t.Fatalf("login: status %d: %s", r.Status, r.Body)
	}
	if token == "" {
		t.Error("login response has no Token header")
	}
	if got := r.json(t)["username"]; got != sutAdminUsername {
		t.Errorf("username = %v, want %s", got, sutAdminUsername)
	}
}

// R1c
func TestEnvAdminHoldsSystemAdminRole(t *testing.T) {
	token := adminSession(t)
	me := doRequest(t, http.MethodGet, baseURL()+"/api/v4/users/me", token, nil)
	if me.Status != http.StatusOK {
		t.Fatalf("users/me: status %d: %s", me.Status, me.Body)
	}
	roles := roleSet(me.json(t)["roles"])
	if !roles["system_admin"] || !roles["system_user"] {
		t.Errorf("roles = %v, want system_admin and system_user", roles)
	}
	if cfg := doRequest(t, http.MethodGet, baseURL()+"/api/v4/config", token, nil); cfg.Status != http.StatusOK {
		t.Errorf("GET /api/v4/config as the env admin: status %d, want 200", cfg.Status)
	}
}

// R1d
func TestEnvAdminEmailIsMMAdminEmail(t *testing.T) {
	me := doRequest(t, http.MethodGet, baseURL()+"/api/v4/users/me", adminSession(t), nil)
	if me.Status != http.StatusOK {
		t.Fatalf("users/me: status %d: %s", me.Status, me.Body)
	}
	if got := me.json(t)["email"]; got != sutAdminEmail {
		t.Errorf("email = %v, want %s", got, sutAdminEmail)
	}
}

// R1f
func TestEnvAdminEmailIsVerified(t *testing.T) {
	me := doRequest(t, http.MethodGet, baseURL()+"/api/v4/users/me", adminSession(t), nil)
	if me.Status != http.StatusOK {
		t.Fatalf("users/me: status %d: %s", me.Status, me.Body)
	}
	if got := me.json(t)["email_verified"]; got != true {
		t.Errorf("email_verified = %v, want true", got)
	}
}

// R4a: the SUT runs with EnableOpenServer=true, so sign-up works, but never yields a system admin.
func TestOpenSignUpCreatesPlainUserOnly(t *testing.T) {
	name := fmt.Sprintf("signup%d", time.Now().UnixNano())
	r := doRequest(t, http.MethodPost, baseURL()+"/api/v4/users", "", map[string]string{
		"username": name, "email": name + "@sample.mattermost.com", "password": "Passw0rd!x",
	})
	if r.Status != http.StatusCreated {
		t.Fatalf("POST /api/v4/users: status %d, want 201: %s", r.Status, r.Body)
	}
	if roles := r.json(t)["roles"]; roles != "system_user" {
		t.Errorf("new user's roles = %v, want system_user", roles)
	}

	token := os.Getenv("SKYRAMP_TEST_TOKEN")
	if token == "" {
		token = adminSession(t)
	}
	admin := doRequest(t, http.MethodGet, baseURL()+"/api/v4/users/username/"+sutAdminUsername, token, nil)
	if admin.Status != http.StatusOK {
		t.Fatalf("lookup %s: status %d: %s", sutAdminUsername, admin.Status, admin.Body)
	}
	if !roleSet(admin.json(t)["roles"])["system_admin"] {
		t.Errorf("%s lost system_admin: %s", sutAdminUsername, admin.Body)
	}
}
