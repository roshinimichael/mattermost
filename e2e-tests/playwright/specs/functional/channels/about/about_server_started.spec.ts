// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import type {Page} from '@playwright/test';

import {expect, test} from '@mattermost/playwright-lib';

async function loginThroughLoginPage(page: Page, baseURL: string, username: string, password: string) {
    await page.goto(`${baseURL}/login`);
    const viewInBrowser = page.getByRole('link', {name: 'View in Browser'});
    if (await viewInBrowser.isVisible().catch(() => false)) {
        await viewInBrowser.click();
    }
    await page.getByRole('textbox', {name: 'Email or Username'}).fill(username);
    await page.getByRole('textbox', {name: 'Password'}).fill(password);
    await page.getByRole('button', {name: 'Log in'}).click();
    await expect(page).toHaveURL(/\/[^/]+\/channels\/[^/]+/);
}

async function openAboutFromProductMenu(page: Page) {
    const noThanks = page.getByText('No thanks, I’ll figure it out myself');
    if (await noThanks.isVisible().catch(() => false)) {
        await noThanks.click();
    }
    await page.getByRole('button', {name: 'Product switch menu'}).click();
    const about = page.getByRole('menuitem', {name: 'About Mattermost'});
    await expect(about).toBeVisible();
    return about;
}

test.describe('About Mattermost server start time', () => {
    /**
     * @objective Verify that a system admin sees when the server started, under the Hostname line
     */
    test('system admin sees the server start time under the hostname in About', async ({pw, page}) => {
        const {adminUser} = await pw.initSetup();
        const baseURL = pw.url;
        const requests: string[] = [];
        const responses: number[] = [];

        await test.step('log in as the system admin on the login page', async () => {
            await loginThroughLoginPage(page, baseURL, adminUser.username, adminUser.password);
        });

        let about = page.getByRole('menuitem', {name: 'About Mattermost'});
        await test.step('open the product menu', async () => {
            about = await openAboutFromProductMenu(page);
        });

        const dialog = page.getByRole('dialog', {name: 'About Mattermost'});
        await test.step('open About Mattermost', async () => {
            page.on('request', (r) => {
                if (r.url().includes('/api/v4/system/start_time')) {
                    requests.push(r.method());
                }
            });
            page.on('response', (r) => {
                if (r.url().includes('/api/v4/system/start_time')) {
                    responses.push(r.status());
                }
            });
            await about.click();
            await expect(dialog).toBeVisible();
            await expect.soft(dialog.getByTestId('aboutModalVersionInfo')).toContainText('Server Version:');
            await expect.soft(dialog).toContainText('Hostname:');

            const today = await page.evaluate(() =>
                new Date().toLocaleDateString('en-US', {year: 'numeric', month: 'short', day: '2-digit'}),
            );
            const line = dialog.getByTestId('aboutModalServerStarted');
            await expect.soft(line).toContainText('Server started:');
            await expect.soft(line).toContainText(today);
            const previousText = await line.evaluate((el) => el.previousElementSibling?.textContent || '');
            expect.soft(previousText.startsWith('Hostname:')).toBe(true);

            await page.waitForTimeout(2000);
            expect.soft(requests).toEqual(['GET']);
            expect.soft(responses).toEqual([200]);
        });

        await test.step('close the dialog', async () => {
            await dialog.getByRole('button', {name: 'Close'}).click();
            await expect(dialog).toHaveCount(0);
        });
    });

    /**
     * @objective Verify that a regular user sees no server start time and the browser does not ask for it
     */
    test('regular user sees no server start time in About and sends no request for it', async ({pw, page}) => {
        const {user} = await pw.initSetup();
        const baseURL = pw.url;
        const requests: string[] = [];
        page.on('request', (r) => {
            if (r.url().includes('/api/v4/system/start_time')) {
                requests.push(r.url());
            }
        });

        await test.step('log in as a regular user on the login page', async () => {
            await loginThroughLoginPage(page, baseURL, user.username, user.password);
        });

        let about = page.getByRole('menuitem', {name: 'About Mattermost'});
        await test.step('open the product menu', async () => {
            about = await openAboutFromProductMenu(page);
        });

        const dialog = page.getByRole('dialog', {name: 'About Mattermost'});
        await test.step('open About Mattermost', async () => {
            await about.click();
            await expect(dialog.getByTestId('aboutModalVersionInfo')).toContainText('Server Version:');
            await page.waitForTimeout(2500);
            await expect.soft(dialog).toContainText('Hostname:');
            await expect.soft(dialog.getByTestId('aboutModalServerStarted')).toHaveCount(0);
            await expect.soft(dialog.getByText('Server started:')).toHaveCount(0);
            expect.soft(requests).toHaveLength(0);
        });

        await test.step('close the dialog', async () => {
            await dialog.getByRole('button', {name: 'Close'}).click();
            await expect(dialog).toHaveCount(0);
            await expect(page.getByRole('heading', {name: 'Town Square'})).toBeVisible();
            expect(requests).toHaveLength(0);
        });
    });
});
