// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"errors"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/v8/channels/jobs"
	"github.com/mattermost/mattermost/server/v8/config"
)

const (
	unitTestListeningPort = "localhost:0"
)

//nolint:golint,unused
type ServerTestHelper struct {
	disableConfigWatch bool
	interruptChan      chan os.Signal
	originalInterval   int
}

//nolint:golint,unused
func SetupServerTest(tb testing.TB) *ServerTestHelper {
	if testing.Short() {
		tb.SkipNow()
	}
	// Build a channel that will be used by the server to receive system signals...
	interruptChan := make(chan os.Signal, 1)
	// ...and sent it immediately a SIGINT value.
	// This will make the server loop stop as soon as it started successfully.
	interruptChan <- syscall.SIGINT

	// Let jobs poll for termination every 0.2s (instead of every 15s by default)
	// Otherwise we would have to wait the whole polling duration before the test
	// terminates.
	originalInterval := jobs.DefaultWatcherPollingInterval
	jobs.DefaultWatcherPollingInterval = 200

	th := &ServerTestHelper{
		disableConfigWatch: true,
		interruptChan:      interruptChan,
		originalInterval:   originalInterval,
	}
	return th
}

//nolint:golint,unused
func (th *ServerTestHelper) TearDownServerTest() {
	jobs.DefaultWatcherPollingInterval = th.originalInterval
}

// backgroundServer is a server started by runServer, as `mattermost server` starts it, that keeps serving until Stop.
type backgroundServer struct {
	URL    string
	Client *model.Client4

	th       *ServerTestHelper
	ready    chan struct{}
	done     chan error
	stopOnce sync.Once
}

// startBackgroundServer runs runServer in the background on a free port against the given database. It is ready
// once it sends the systemd READY notification, which runServer only sends once it has finished starting.
func startBackgroundServer(t *testing.T, sqlSettings *model.SqlSettings) *backgroundServer {
	th := SetupServerTest(t)
	// SetupServerTest queues a SIGINT so that runServer stops as soon as it started; keep it serving until Stop.
	<-th.interruptChan

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	socketFile, err := os.CreateTemp("", "mattermost-systemd-mock-socket-")
	require.NoError(t, err)
	socketPath := socketFile.Name()
	socketFile.Close()
	os.Remove(socketPath)
	connection, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	require.NoError(t, err)
	t.Setenv("NOTIFY_SOCKET", socketPath)

	configStore := config.NewTestMemoryStore()
	cfg := configStore.Get()
	*cfg.ServiceSettings.ListenAddress = addr
	*cfg.ServiceSettings.SiteURL = "http://" + addr
	*cfg.PluginSettings.Enable = false
	cfg.SqlSettings = *sqlSettings
	configStore.Set(cfg)

	s := &backgroundServer{
		URL:    "http://" + addr,
		Client: model.NewAPIv4Client("http://" + addr),
		th:     th,
		ready:  make(chan struct{}),
		done:   make(chan error, 1),
	}
	go func() {
		buffer := make([]byte, 512)
		if count, readErr := connection.Read(buffer); readErr == nil && string(buffer[:count]) == "READY=1" {
			close(s.ready)
		}
	}()
	go func() { s.done <- runServer(configStore, th.interruptChan) }()
	t.Cleanup(func() {
		s.Stop()
		connection.Close()
		os.Remove(socketPath)
	})
	return s
}

// WaitReady waits until the server has started, returning runServer's error if it stops first.
func (s *backgroundServer) WaitReady(timeout time.Duration) error {
	select {
	case <-s.ready:
		return nil
	case err := <-s.done:
		s.done <- err
		if err == nil {
			err = errors.New("runServer returned before the server was ready")
		}
		return err
	case <-time.After(timeout):
		return errors.New("timed out waiting for the server to start")
	}
}

// WaitExit waits for runServer to return on its own, reporting whether it did and with which error.
func (s *backgroundServer) WaitExit(timeout time.Duration) (exited bool, err error) {
	select {
	case err = <-s.done:
		s.done <- err
		return true, err
	case <-time.After(timeout):
		return false, nil
	}
}

// Stop interrupts the server, if it is still running, and waits for runServer to return.
func (s *backgroundServer) Stop() {
	s.stopOnce.Do(func() {
		select {
		case s.th.interruptChan <- syscall.SIGINT:
		default:
		}
		select {
		case <-s.done:
		case <-time.After(time.Minute):
		}
		s.th.TearDownServerTest()
	})
}

func TestRunServerSuccess(t *testing.T) {
	th := SetupServerTest(t)
	defer th.TearDownServerTest()

	// A server whose database has no account yet only starts with the credentials of the system admin it creates.
	t.Setenv(adminUsernameEnv, "sysadmin")
	t.Setenv(adminPasswordEnv, "Sys-admin-1")

	configStore := config.NewTestMemoryStore()

	// Use non-default listening port in case another server instance is already running.
	cfg := configStore.Get()
	*cfg.ServiceSettings.ListenAddress = unitTestListeningPort
	cfg.SqlSettings = *mainHelper.GetSQLSettings()
	configStore.Set(cfg)

	err := runServer(configStore, th.interruptChan)
	require.NoError(t, err)
}

func TestRunServerSystemdNotification(t *testing.T) {
	th := SetupServerTest(t)
	defer th.TearDownServerTest()

	// A server whose database has no account yet only starts with the credentials of the system admin it creates.
	t.Setenv(adminUsernameEnv, "sysadmin")
	t.Setenv(adminPasswordEnv, "Sys-admin-1")

	// Get a random temporary filename for using as a mock systemd socket
	socketFile, err := os.CreateTemp("", "mattermost-systemd-mock-socket-")
	require.NoError(t, err)

	socketPath := socketFile.Name()
	os.Remove(socketPath)

	// t.Setenv prevents t.Parallel — env var has no config equivalent
	t.Setenv("NOTIFY_SOCKET", socketPath)

	// Open the socket connection
	addr := &net.UnixAddr{
		Name: socketPath,
		Net:  "unixgram",
	}
	connection, err := net.ListenUnixgram("unixgram", addr)
	require.NoError(t, err)

	defer connection.Close()
	defer os.Remove(socketPath)

	// Listen for socket data
	socketReader := make(chan string)
	go func(ch chan string) {
		buffer := make([]byte, 512)
		count, readErr := connection.Read(buffer)
		require.NoError(t, readErr)
		data := buffer[0:count]
		ch <- string(data)
	}(socketReader)

	configStore := config.NewTestMemoryStore()

	// Use non-default listening port in case another server instance is already running.
	cfg := configStore.Get()
	*cfg.ServiceSettings.ListenAddress = unitTestListeningPort
	cfg.SqlSettings = *mainHelper.GetSQLSettings()
	configStore.Set(cfg)

	// Start and stop the server
	err = runServer(configStore, th.interruptChan)
	require.NoError(t, err)

	// Ensure the notification has been sent on the socket and is correct
	notification := <-socketReader
	require.Equal(t, notification, "READY=1")
}

func TestRunServerNoSystemd(t *testing.T) {
	th := SetupServerTest(t)
	defer th.TearDownServerTest()

	// A server whose database has no account yet only starts with the credentials of the system admin it creates.
	t.Setenv(adminUsernameEnv, "sysadmin")
	t.Setenv(adminPasswordEnv, "Sys-admin-1")

	// Temporarily remove any Systemd socket defined in the environment
	// t.Setenv prevents t.Parallel — env var has no config equivalent
	t.Setenv("NOTIFY_SOCKET", "")

	configStore := config.NewTestMemoryStore()

	// Use non-default listening port in case another server instance is already running.
	cfg := configStore.Get()
	*cfg.ServiceSettings.ListenAddress = unitTestListeningPort
	cfg.SqlSettings = *mainHelper.GetSQLSettings()
	configStore.Set(cfg)

	err := runServer(configStore, th.interruptChan)
	require.NoError(t, err)
}
