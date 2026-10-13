// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import type {Page, Route} from '@playwright/test';

import {expect, test} from '@mattermost/playwright-lib';

const SERVER_TIME_ROUTE = '**/api/v4/system/time';

async function openAboutModal(page: Page) {
    await page.getByRole('button', {name: 'Product switch menu'}).click();
    await page.getByRole('button', {name: 'About Mattermost'}).click();
    const modal = page.getByRole('dialog', {name: 'About Mattermost'});
    await expect(modal).toBeVisible();
    return modal;
}

async function closeAboutModal(page: Page) {
    const modal = page.getByRole('dialog', {name: 'About Mattermost'});
    await modal.getByRole('button', {name: 'Close'}).click();
    await expect(modal).toBeHidden();
}

/**
 * Freezes the device clock at `now` and answers GET /api/v4/system/time with `serverTime`.
 * Returns a getter for how many times the route was hit.
 */
async function stubSkew(page: Page, now: number, serverTime: number) {
    await page.clock.setFixedTime(now);
    let hits = 0;
    await page.route(SERVER_TIME_ROUTE, async (route: Route) => {
        hits++;
        await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({time: serverTime})});
    });
    return () => hits;
}

async function waitForSettledSkew(page: Page, hits: () => number) {
    await expect.poll(hits).toBeGreaterThan(0);

    // # Give the modal a moment to render the result of the request
    await page.waitForTimeout(500);
}

test.describe('About modal clock skew warning', () => {
    let page: Page;

    test.beforeEach(async ({pw}) => {
        const {user, team} = await pw.initSetup();
        const login = await pw.testBrowser.login(user);
        page = login.page;
        await login.channelsPage.goto(team.name);
        await login.channelsPage.toBeVisible();
    });

    /**
     * @objective R5a: warns when the device clock is 5 minutes ahead of the server
     */
    test('R5a device clock ahead by 300 seconds shows the warning', {tag: '@about'}, async () => {
        const now = Date.now();
        await stubSkew(page, now, now - 300000);

        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);

        // * Verify the dialog shows the expected clock skew state
        await expect(modal.getByTestId('aboutModalClockSkew')).toBeVisible();
        await expect(modal.getByTestId('aboutModalClockSkew')).toHaveText(
            "Your device's clock differs from the server's by 300 seconds. Times shown in messages may be off.",
        );
    });

    /**
     * @objective R5b: warns with the absolute value when the device clock is behind the server
     */
    test('R5b device clock behind by 300 seconds shows the absolute skew', {tag: '@about'}, async () => {
        const now = Date.now();
        await stubSkew(page, now, now + 300000);

        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);

        // * Verify the dialog shows the expected clock skew state
        await expect(modal.getByTestId('aboutModalClockSkew')).toContainText('by 300 seconds');
        await expect(modal.getByTestId('aboutModalClockSkew')).not.toContainText('-300');
    });

    /**
     * @objective R5c: no warning against the real server when both clocks agree; version info unchanged
     */
    test('R5c clocks in sync shows no warning', {tag: '@about'}, async () => {
        const responsePromise = page.waitForResponse((r) => r.url().endsWith('/api/v4/system/time'));
        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);
        const response = await responsePromise;

        // * Verify the dialog shows the expected clock skew state
        expect(response.status()).toBe(200);
        await page.waitForTimeout(500);
        await expect(modal.getByTestId('aboutModalClockSkew')).toHaveCount(0);
        await expect(modal.getByTestId('aboutModalVersionInfo')).toContainText('Server Version:');
    });

    /**
     * @objective R5d: a skew of exactly a minute warns
     */
    test('R5d exactly 60 seconds shows the warning', {tag: '@about'}, async () => {
        const now = Date.now();
        await stubSkew(page, now, now - 60000);

        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);

        // * Verify the dialog shows the expected clock skew state
        await expect(modal.getByTestId('aboutModalClockSkew')).toContainText('by 60 seconds');
    });

    /**
     * @objective R5e / F1: a skew of 59.6 s (device ahead) is under a minute and must not warn
     */
    test('clock-skew-59-6s-ahead-no-warning (R5e)', {tag: '@about'}, async () => {
        const now = Date.now();
        const hits = await stubSkew(page, now, now - 59600);

        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);
        await waitForSettledSkew(page, hits);

        // * Verify the dialog shows the expected clock skew state
        await expect(modal.getByTestId('aboutModalClockSkew')).toHaveCount(0);
    });

    /**
     * @objective R5e: a skew of 59.6 s (device behind) is under a minute and must not warn
     */
    test('clock-skew-59-6s-behind-no-warning (R5e)', {tag: '@about'}, async () => {
        const now = Date.now();
        const hits = await stubSkew(page, now, now + 59600);

        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);
        await waitForSettledSkew(page, hits);

        // * Verify the dialog shows the expected clock skew state
        await expect(modal.getByTestId('aboutModalClockSkew')).toHaveCount(0);
    });

    /**
     * @objective R5f: 30 s skew (device ahead) is well below the threshold
     */
    test('R5f 30 seconds ahead shows no warning', {tag: '@about'}, async () => {
        const now = Date.now();
        const hits = await stubSkew(page, now, now - 30000);

        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);
        await waitForSettledSkew(page, hits);

        // * Verify the dialog shows the expected clock skew state
        await expect(modal.getByTestId('aboutModalClockSkew')).toHaveCount(0);
    });

    /**
     * @objective R5f: 30 s skew (device behind) is well below the threshold
     */
    test('clock-skew-30s-no-warning (R5f)', {tag: '@about'}, async () => {
        const now = Date.now();
        const hits = await stubSkew(page, now, now + 30000);

        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);
        await waitForSettledSkew(page, hits);

        // * Verify the dialog shows the expected clock skew state
        await expect(modal.getByTestId('aboutModalClockSkew')).toHaveCount(0);
    });

    /**
     * @objective R5g: a large skew is formatted as a localized number
     */
    test('R5g one day of skew is formatted as 86,400 seconds', {tag: '@about'}, async () => {
        const now = Date.now();
        await stubSkew(page, now, now - 86400000);

        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);

        // * Verify the dialog shows the expected clock skew state
        await expect(modal.getByTestId('aboutModalClockSkew')).toContainText('by 86,400 seconds');
    });

    /**
     * @objective R5h: skew is measured at the midpoint of the round-trip, so a slow response alone does not warn
     */
    test('R5h slow round-trip with server time at the midpoint shows no warning', {tag: '@about'}, async () => {
        await page.clock.install();
        let hits = 0;
        await page.route(SERVER_TIME_ROUTE, async (route: Route) => {
            hits++;

            // # The request leaves at `sentAt`; the server answers with the time half-way through a 180 s round-trip
            const sentAt = await page.evaluate(() => Date.now());
            await page.clock.fastForward(180000);
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({time: sentAt + 90000}),
            });
        });

        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);
        await waitForSettledSkew(page, () => hits);

        // * Verify the dialog shows the expected clock skew state
        await expect(modal.getByTestId('aboutModalClockSkew')).toHaveCount(0);
    });

    /**
     * @objective R5i: a failing server time request leaves the modal intact and shows no warning
     */
    test('R5i server time request failing with 500 or a network error', {tag: '@about'}, async () => {
        await page.route(SERVER_TIME_ROUTE, (route) =>
            route.fulfill({
                status: 500,
                contentType: 'application/json',
                body: JSON.stringify({id: 'x', message: 'boom', status_code: 500}),
            }),
        );

        // # Open the About Mattermost dialog
        let modal = await openAboutModal(page);
        await page.waitForTimeout(500);
        // * Verify the dialog shows the expected clock skew state
        await expect(modal.getByTestId('aboutModalVersionInfo')).toContainText('Server Version:');
        await expect(modal.getByTestId('aboutModalClockSkew')).toHaveCount(0);
        await closeAboutModal(page);

        await page.unroute(SERVER_TIME_ROUTE);
        await page.route(SERVER_TIME_ROUTE, (route) => route.abort());

        modal = await openAboutModal(page);
        await page.waitForTimeout(500);
        await expect(modal.getByTestId('aboutModalVersionInfo')).toContainText('Server Version:');
        await expect(modal.getByTestId('aboutModalClockSkew')).toHaveCount(0);
    });

    /**
     * @objective R5j: each open of the About dialog issues exactly one GET /api/v4/system/time
     */
    test('R5j one server time request per modal open', {tag: '@about'}, async () => {
        const statuses: number[] = [];
        const methods: string[] = [];
        page.on('response', (response) => {
            if (new URL(response.url()).pathname === '/api/v4/system/time') {
                statuses.push(response.status());
                methods.push(response.request().method());
            }
        });

        // # Open the About Mattermost dialog
        await openAboutModal(page);
        await expect.poll(() => statuses.length).toBe(1);
        await closeAboutModal(page);

        await openAboutModal(page);
        await expect.poll(() => statuses.length).toBe(2);
        await page.waitForTimeout(500);

        // * Verify the dialog shows the expected clock skew state
        expect(statuses).toEqual([200, 200]);
        expect(methods).toEqual(['GET', 'GET']);
    });

    /**
     * @objective R6c: the About modal's existing content is unchanged, and the copied version text has no clock text
     */
    test('R6c existing About modal content and copied version info are unchanged', {tag: '@about'}, async () => {
        // # Open the About Mattermost dialog
        const modal = await openAboutModal(page);
        const versionInfo = modal.getByTestId('aboutModalVersionInfo');

        // * Verify the existing version info is unchanged
        await expect(versionInfo).toContainText('Server Version:');
        await expect(versionInfo).toContainText('Database Schema Version:');
        await expect(versionInfo).toContainText('Build Number:');
        await expect(versionInfo).toContainText('Database:');

        // # The copy button only shows while hovering the version info
        await versionInfo.hover();
        await versionInfo.getByLabel('Copy text').click();
        const copied = await page.evaluate(() => navigator.clipboard.readText());
        expect(copied).toContain('Server Version:');
        expect(copied.toLowerCase()).not.toContain('clock');
    });
});
