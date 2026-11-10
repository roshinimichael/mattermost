// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import type {Page, Request} from '@playwright/test';

import {expect, test} from '@mattermost/playwright-lib';

const START_TIME_PATH = '/api/v4/system/start_time';

function recordStartTimeRequests(page: Page) {
    const requests: Request[] = [];
    page.on('request', (request) => {
        if (new URL(request.url()).pathname === START_TIME_PATH) {
            requests.push(request);
        }
    });
    return requests;
}

async function dismissOnboardingIfShown(page: Page) {
    const skip = page.getByText('No thanks, I’ll figure it out myself');
    if (await skip.isVisible()) {
        await skip.click();
    }
}

async function openAboutDialog(page: Page) {
    await page.getByRole('button', {name: 'Product switch menu'}).click();
    await page.getByRole('menuitem', {name: 'About Mattermost'}).click();
    const dialog = page.getByRole('dialog', {name: 'About Mattermost'});
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText('Hostname:');
    return dialog;
}

test.describe('About dialog server start time', () => {
    /**
     * @objective Verify a system admin sees when the server started, under Hostname, in the About dialog
     */
    test('system admin sees when the server started in About', async ({pw}) => {
        const {adminUser, team} = await pw.initSetup();
        if (!adminUser) {
            throw new Error('Admin user is required');
        }

        // # Log in as the system admin
        const {page, channelsPage} = await pw.testBrowser.login(adminUser);
        await test.step('sign in lands on town square', async () => {
            await channelsPage.goto(team.name, 'town-square');
            await channelsPage.toBeVisible();
            await expect(page.getByRole('button', {name: 'Product switch menu'})).toBeVisible();
        });

        await test.step('dismiss onboarding if shown', async () => {
            await dismissOnboardingIfShown(page);
            await expect(page.getByRole('button', {name: 'Product switch menu'})).toBeEnabled();
        });

        const requests = recordStartTimeRequests(page);
        const responsePromise = page.waitForResponse((r) => new URL(r.url()).pathname === START_TIME_PATH);

        // # Open About from the product menu
        const dialog = await test.step('open About Mattermost', async () => openAboutDialog(page));

        // * The Server started line shows today's date and a time
        await test.step('read the Server started line', async () => {
            const response = await responsePromise;
            expect.soft(response.request().method()).toBe('GET');
            expect.soft(response.status()).toBe(200);
            const body = await response.json();
            expect.soft(typeof body.started_at).toBe('number');

            const line = dialog.getByTestId('aboutModalServerStarted');
            await expect(line).toBeVisible();

            const today = new Date().toLocaleDateString('en-US', {year: 'numeric', month: 'short', day: '2-digit'});
            const startedDate = new Date(body.started_at).toLocaleDateString('en-US', {
                year: 'numeric',
                month: 'short',
                day: '2-digit',
            });
            const startedTime = new Date(body.started_at).toLocaleTimeString('en-US', {
                hour: '2-digit',
                minute: '2-digit',
            });
            await expect.soft(line).toHaveText(`Server started: ${startedDate} - ${startedTime}`);
            await expect.soft(line).toContainText(today);

            const hostname = dialog.getByText('Hostname:', {exact: false});
            const follows = await hostname.evaluate(
                (host, started) =>
                    Boolean(host.compareDocumentPosition(started as Node) & Node.DOCUMENT_POSITION_FOLLOWING),
                await line.elementHandle(),
            );
            expect.soft(follows).toBe(true);
            expect.soft(requests.filter((r) => r.method() === 'GET')).toHaveLength(1);
        });

        await test.step('close the dialog', async () => {
            await dialog.getByRole('button', {name: 'Close'}).click();
            await expect(dialog).not.toBeVisible();
        });
    });

    /**
     * @objective Verify a regular user sees no server start time in About and no request is made for it
     */
    test('regular user sees no server start time in About', async ({pw}) => {
        const {user, team} = await pw.initSetup();

        // # Log in as a regular user
        const {page, channelsPage} = await pw.testBrowser.login(user);
        await test.step('sign in lands on town square', async () => {
            await channelsPage.goto(team.name, 'town-square');
            await channelsPage.toBeVisible();
            await expect(page.getByRole('button', {name: 'Product switch menu'})).toBeVisible();
        });

        await test.step('dismiss onboarding if shown', async () => {
            await dismissOnboardingIfShown(page);
            await expect(page.getByRole('button', {name: 'Product switch menu'})).toBeEnabled();
        });

        const requests = recordStartTimeRequests(page);

        // # Open About from the product menu
        const dialog = await test.step('open About Mattermost', async () => openAboutDialog(page));

        // * No Server started line and no request for the start time
        await test.step('inspect the dialog', async () => {
            await page.waitForTimeout(3000);
            await expect.soft(dialog.getByTestId('aboutModalServerStarted')).toHaveCount(0);
            await expect.soft(dialog.getByText('Server started:')).toHaveCount(0);
            expect.soft(requests).toHaveLength(0);
        });

        await test.step('close the dialog', async () => {
            await dialog.getByRole('button', {name: 'Close'}).click();
            await expect(dialog).not.toBeVisible();
        });
    });
});
