// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"os"

	"github.com/pkg/errors"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/public/shared/request"
	"github.com/mattermost/mattermost/server/v8/channels/app"
)

const (
	adminUsernameEnv = "MM_ADMIN_USERNAME"
	adminPasswordEnv = "MM_ADMIN_PASSWORD"
	adminEmailEnv    = "MM_ADMIN_EMAIL"
)

// adminFromEnv is the system admin a server with no account yet creates, from MM_ADMIN_USERNAME and
// MM_ADMIN_PASSWORD (and MM_ADMIN_EMAIL, else <username>@example.com). All three are optional: with neither, it is nil
// and the server opens first-user sign-up, as Mattermost does; with only one, the server does not start.
func adminFromEnv(getenv func(string) string) (*model.User, error) {
	username, password := getenv(adminUsernameEnv), getenv(adminPasswordEnv)
	if username == "" && password == "" {
		return nil, nil
	}
	if username == "" || password == "" {
		return nil, errors.Errorf("set both %s and %s, or neither to open first-user sign-up", adminUsernameEnv, adminPasswordEnv)
	}
	email := getenv(adminEmailEnv)
	if email == "" {
		email = username + "@example.com"
	}
	return &model.User{Username: username, Password: password, Email: email, EmailVerified: true}, nil
}

// bootstrapAdmin gives a server with no account its system admin when MM_ADMIN_USERNAME and MM_ADMIN_PASSWORD are set;
// without them, the first person to sign up becomes the admin.
func bootstrapAdmin(server *app.Server) error {
	a := app.New(app.ServerConnector(server.Channels()))
	if !a.IsFirstUserAccount() {
		return nil
	}
	admin, err := adminFromEnv(os.Getenv)
	if err != nil || admin == nil {
		return err
	}
	rctx := request.EmptyContext(a.Log())
	user, appErr := a.CreateUser(rctx, admin)
	if appErr != nil {
		return errors.Wrap(appErr, "failed to create the system admin")
	}
	if _, appErr = a.UpdateUserRoles(rctx, user.Id, model.SystemAdminRoleId+" "+model.SystemUserRoleId, false); appErr != nil {
		return errors.Wrap(appErr, "failed to make the new account a system admin")
	}
	mlog.Info("Created the system admin from "+adminUsernameEnv, mlog.String("username", user.Username))
	return nil
}
