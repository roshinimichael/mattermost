// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import type {Page} from '@playwright/test';

import {ChannelsPage, expect, test} from '@mattermost/playwright-lib';

const FIVE_MINUTES_MS = 5 * 60 * 1000;

/**
 * Shift only the browser's clock by offsetMs, before any page script runs.
 */
async function shiftDeviceClock(page: Page, offsetMs: number) {
    await page.addInitScript((off: number) => {
        const RealDate = Date;
        class ShiftedDate extends RealDate {
            constructor(...args: any[]) {
                if (args.length === 0) {
                    super(RealDate.now() + off);
                } else {
                    // @ts-expect-error forwarding constructor arguments
                    super(...args);
                }
            }
            static now() {
                return RealDate.now() + off;
            }
        }
        (globalThis as any).Date = ShiftedDate;
    }, offsetMs);
}

const SKEW_WARNING = /^Your device's clock differs from the server's by (299|300|301) seconds\. Times shown in messages may be off\.$/;

/**
 * Log in through the login page and land in the team's town-square.
 */
async function loginToTownSquare(pw: any, page: Page, user: {username: string; password: string}, teamName: string) {
    const channelsPage = new ChannelsPage(page);
    await pw.loginPage.goto();
    if (await pw.landingLoginPage.viewInBrowserButton.isVisible().catch(() => false)) {
        await pw.landingLoginPage.viewInBrowserButton.click();
    }
    await pw.loginPage.toBeVisible();
    await pw.loginPage.login(user);
    await channelsPage.goto(teamName, 'town-square');
    await channelsPage.toBeVisible();
    await expect(page).toHaveURL(new RegExp(`/${teamName}/channels/town-square`));
    await expect(page.getByRole('textbox', {name: 'Write to Town Square'})).toBeVisible();
    return channelsPage;
}

/**
 * Open About Mattermost from the product switch menu and check the server-time request it sends.
 */
async function openAboutAndCheckServerTime(page: Page, channelsPage: ChannelsPage) {
    const serverTime = page.waitForResponse(
        (response) => response.url().endsWith('/api/v4/system/time') && response.request().method() === 'GET',
    );
    await channelsPage.globalHeader.openAboutModal();

    const response = await serverTime;
    expect(response.status()).toBe(200);
    const body = await response.json();
    expect(typeof body.time).toBe('number');

    await channelsPage.aboutModal.toBeVisible();
    await expect(channelsPage.aboutModal.versionInfo).toContainText('Server Version:');
}

test.describe('About dialog clock skew warning', () => {
    test('warns a user whose device clock is 5 minutes fast', async ({pw, page}) => {
        const {team, user} = await pw.initSetup();
        await shiftDeviceClock(page, FIVE_MINUTES_MS);
        let channelsPage!: ChannelsPage;

        await test.step('log in and land in town-square', async () => {
            channelsPage = await loginToTownSquare(pw, page, user, team.name);
        });

        await test.step('open About Mattermost from the product switch menu', async () => {
            await openAboutAndCheckServerTime(page, channelsPage);
        });

        await test.step('read the clock skew warning in the About dialog', async () => {
            const warning = channelsPage.aboutModal.clockSkewWarning;
            await expect(warning).toBeVisible();
            await expect(warning).toHaveText(SKEW_WARNING);
        });

        await test.step('close the About dialog', async () => {
            await channelsPage.aboutModal.closeButton.click();
            await expect(channelsPage.aboutModal.versionInfo).toHaveCount(0);
            await expect(channelsPage.aboutModal.container).toHaveCount(0);
        });
    });

    test('shows no clock warning to a user whose device clock is correct', async ({pw, page}) => {
        const {team, user} = await pw.initSetup();
        let channelsPage!: ChannelsPage;

        await test.step('log in and land in town-square', async () => {
            channelsPage = await loginToTownSquare(pw, page, user, team.name);
        });

        await test.step('open About Mattermost and see version info without a clock warning', async () => {
            await openAboutAndCheckServerTime(page, channelsPage);
            await page.waitForTimeout(1500);
            await expect(channelsPage.aboutModal.clockSkewWarning).toHaveCount(0);
        });

        await test.step('close the About dialog', async () => {
            await channelsPage.aboutModal.closeButton.click();
            await expect(channelsPage.aboutModal.versionInfo).toHaveCount(0);
            await expect(channelsPage.aboutModal.container).toHaveCount(0);
        });
    });

    test('warns with a positive number a user whose device clock is 5 minutes slow', async ({pw, page}) => {
        const {team, user} = await pw.initSetup();
        await shiftDeviceClock(page, -FIVE_MINUTES_MS);
        let channelsPage!: ChannelsPage;

        await test.step('log in and land in town-square', async () => {
            channelsPage = await loginToTownSquare(pw, page, user, team.name);
        });

        await test.step('open About Mattermost and read a positive clock skew warning', async () => {
            await openAboutAndCheckServerTime(page, channelsPage);
            const warning = channelsPage.aboutModal.clockSkewWarning;
            await expect(warning).toBeVisible();
            await expect(warning).toHaveText(SKEW_WARNING);
            await expect(warning).not.toContainText('-');
        });

        await test.step('close the About dialog', async () => {
            await channelsPage.aboutModal.closeButton.click();
            await expect(channelsPage.aboutModal.versionInfo).toHaveCount(0);
            await expect(channelsPage.aboutModal.container).toHaveCount(0);
        });
    });
});
