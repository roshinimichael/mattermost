// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {SystemConsolePage, expect, test, testConfig} from '@mattermost/playwright-lib';

/**
 * @objective Verify the system admin a fresh server creates from MM_ADMIN_USERNAME/MM_ADMIN_PASSWORD can sign in
 * through the web UI and reach the System Console
 * @precondition The server was started on an empty database with MM_ADMIN_USERNAME/MM_ADMIN_PASSWORD equal to
 * PW_ADMIN_USERNAME/PW_ADMIN_PASSWORD
 */
test('MM-T_BOOTSTRAP_ADMIN signs in with the start-up credentials and opens the System Console', async ({pw}) => {
    await pw.hasSeenLandingPage();

    // # Sign in with the credentials the server was started with
    await pw.loginPage.goto();
    await pw.loginPage.toBeVisible();
    await pw.loginPage.loginInput.fill(testConfig.adminUsername);
    await pw.loginPage.passwordInput.fill(testConfig.adminPassword);
    await pw.loginPage.signInButton.click();

    // * The user leaves the login page and lands in the app
    await expect(pw.loginPage.page).not.toHaveURL(/\/login/);

    // # Open the System Console
    const systemConsolePage = new SystemConsolePage(pw.loginPage.page);
    await systemConsolePage.goto();

    // * The System Console renders for this admin, without a redirect to login
    await systemConsolePage.toBeVisible();
    await expect(systemConsolePage.page).toHaveURL(/\/admin_console/);
});
