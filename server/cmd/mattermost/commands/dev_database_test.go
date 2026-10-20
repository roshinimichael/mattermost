// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
)

// The development database started by `make start-docker` / docker compose takes its password from MM_DB_PASSWORD.
func devDatabasePassword(t *testing.T) string {
	password := os.Getenv("MM_DB_PASSWORD")
	if password == "" {
		t.Skip("MM_DB_PASSWORD is not set: no development database to reach")
	}
	return password
}

// runServerDataSource is the datasource `make run-server` builds from MM_DB_PASSWORD (server/Makefile run-server).
func runServerDataSource(password string) string {
	return "postgres://mmuser:" + password + "@localhost/mattermost_test?sslmode=disable&connect_timeout=10&binary_parameters=yes"
}

func pingDatabase(dataSource string) error {
	db, err := sql.Open(model.DatabaseDriverPostgres, dataSource)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return db.PingContext(ctx)
}

func TestDevDatabaseRunServerDataSource(t *testing.T) {
	password := devDatabasePassword(t)
	setAdminEnv(t, nil)

	settings := *mainHelper.GetSQLSettings()
	settings.DataSource = model.NewPointer(runServerDataSource(password))

	server := startBackgroundServer(t, &settings)
	require.NoError(t, server.WaitReady(serverStartTimeout))

	resp, err := server.Client.DoAPIGet(context.Background(), "/system/ping?get_server_status=true", "")
	require.NoError(t, err)
	defer resp.Body.Close()
	var status map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))
	assert.Equal(t, model.StatusOk, status["database_status"])
}

// The other datasources the repo points at the development database (run-node, the default config and the test
// server) must reach it once its password comes from MM_DB_PASSWORD (defect F2).
func TestDevDatabaseDefaultDataSource(t *testing.T) {
	devDatabasePassword(t)

	assert.NoError(t, pingDatabase(model.SqlSettingsDefaultDataSource))
}

// A secret with URL-reserved characters must still give `make run-server` a working datasource (defect F4).
func TestDevDatabaseRunServerDataSourceSpecialCharacters(t *testing.T) {
	password := devDatabasePassword(t)
	const special = "p@ss/w?rd#1"

	db, err := sql.Open(model.DatabaseDriverPostgres, runServerDataSource(password))
	require.NoError(t, err)
	defer db.Close()
	// One connection, opened before the password changes, so that it can put the password back.
	admin, err := db.Conn(context.Background())
	require.NoError(t, err)
	defer admin.Close()
	_, err = admin.ExecContext(context.Background(), "ALTER ROLE mmuser PASSWORD "+pq.QuoteLiteral(special))
	require.NoError(t, err)
	pingErr := pingDatabase(runServerDataSource(special))
	_, err = admin.ExecContext(context.Background(), "ALTER ROLE mmuser PASSWORD "+pq.QuoteLiteral(password))
	require.NoError(t, err)

	assert.NoError(t, pingErr)
}
