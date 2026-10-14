// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"bytes"
	"database/sql"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serverDir is the directory of the server Makefile and dev compose files (tests run from a resources directory).
func serverDir(tb testing.TB) string {
	_, file, _, ok := runtime.Caller(0)
	require.True(tb, ok)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// devEnv is the test's environment without the variables the development database setup reads, plus extra.
func devEnv(extra ...string) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		for _, k := range []string{"MM_DB_PASSWORD=", "MM_SQLSETTINGS_DATASOURCE=", "IS_CI=", "MM_NO_DOCKER="} {
			if strings.HasPrefix(kv, k) {
				return true
			}
		}
		return false
	})
	return append(env, extra...)
}

func runIn(tb testing.TB, dir string, env []string, name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		tb.Skipf("%s is not available", name)
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// runServerDatasource runs `make run-server` with its prerequisites and recipe replaced, and returns the
// MM_SQLSETTINGS_DATASOURCE the server would be started with.
func runServerDatasource(tb testing.TB, env []string) (string, error) {
	override := filepath.Join(tb.TempDir(), "override.mk")
	require.NoError(tb, os.WriteFile(override, []byte(
		"setup-go-work prepackaged-binaries validate-go-version start-docker client:\n\t@:\n"+
			"run-server:\n\t@printf 'DATASOURCE=%s\\n' \"$$MM_SQLSETTINGS_DATASOURCE\"\n"), 0600))
	out, err := runIn(tb, serverDir(tb), env, "make", "-s", "-f", "Makefile", "-f", override, "run-server")
	for line := range strings.SplitSeq(out, "\n") {
		if ds, ok := strings.CutPrefix(line, "DATASOURCE="); ok {
			return ds, err
		}
	}
	return "", err
}

func TestDevDatabasePassword(t *testing.T) {
	t.Run("R6: both dev compose files require MM_DB_PASSWORD", func(t *testing.T) {
		for _, file := range []string{"docker-compose.yaml", "docker-compose.makefile.yml"} {
			t.Run(file, func(t *testing.T) {
				out, err := runIn(t, serverDir(t), devEnv(), "docker", "compose", "-f", file, "config")
				require.Error(t, err, "compose must refuse to run without MM_DB_PASSWORD: %s", out)
				assert.Contains(t, out, "MM_DB_PASSWORD")

				out, err = runIn(t, serverDir(t), devEnv("MM_DB_PASSWORD=dev-secret-1"), "docker", "compose", "-f", file, "config", "postgres")
				require.NoError(t, err, out)
				assert.Contains(t, out, "POSTGRES_PASSWORD: dev-secret-1")
			})
		}
	})

	t.Run("R6: CI's build/docker-compose.yml keeps its default password", func(t *testing.T) {
		out, err := runIn(t, serverDir(t), devEnv(), "docker", "compose", "-f", "build/docker-compose.yml", "config", "postgres")
		require.NoError(t, err, out)
		assert.Contains(t, out, "POSTGRES_PASSWORD: mostest")
	})

	t.Run("R6: start-docker stops with a message naming MM_DB_PASSWORD", func(t *testing.T) {
		out, err := runIn(t, serverDir(t), devEnv("IS_CI=false"), "make", "-n", "start-docker")
		require.Error(t, err)
		assert.Contains(t, out, "MM_DB_PASSWORD is not set")
	})

	t.Run("R6: run-server connects to the dev database with MM_DB_PASSWORD", func(t *testing.T) {
		password := os.Getenv("MM_DB_PASSWORD")
		if password == "" {
			t.Skip("MM_DB_PASSWORD (the running dev database's password) is not set")
		}
		ds, err := runServerDatasource(t, devEnv("MM_DB_PASSWORD="+password))
		require.NoError(t, err)
		db, err := sql.Open("postgres", ds)
		require.NoError(t, err)
		defer db.Close()
		assert.NoError(t, db.Ping(), "datasource %s", ds)
	})

	t.Run("R6: an explicit MM_SQLSETTINGS_DATASOURCE wins", func(t *testing.T) {
		explicit := "postgres://someone:else@db.example:5432/other?sslmode=disable"
		ds, err := runServerDatasource(t, devEnv("MM_DB_PASSWORD=dev-secret-1", "MM_SQLSETTINGS_DATASOURCE="+explicit))
		require.NoError(t, err)
		assert.Equal(t, explicit, ds)
	})

	t.Run("db-password-reserved-chars: a password with URL-reserved characters reaches the dev database", func(t *testing.T) {
		const password = "p@ss/w?rd#1"
		ds, err := runServerDatasource(t, devEnv("MM_DB_PASSWORD="+password))
		if err != nil {
			// Rejecting such a password up front, with a clear message, is acceptable.
			require.Empty(t, ds)
			return
		}
		// lib/pq reads a postgres:// datasource with net/url, as here.
		u, parseErr := url.Parse(ds)
		require.NoError(t, parseErr, "the datasource must be a valid URL: %s", ds)
		require.NotNil(t, u.User)
		got, _ := u.User.Password()
		assert.Equal(t, "mmuser", u.User.Username())
		assert.Equal(t, password, got, "datasource %s", ds)
		assert.Equal(t, "localhost", u.Host)
		assert.Equal(t, "/mattermost_test", u.Path)
	})
}
