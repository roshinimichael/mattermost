// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {Locator, expect} from '@playwright/test';

/**
 * The "About Mattermost" dialog, opened from the product switch menu.
 */
export default class AboutModal {
    readonly container: Locator;

    readonly versionInfo: Locator;
    readonly hostname: Locator;
    readonly serverStarted: Locator;
    readonly closeButton: Locator;

    constructor(container: Locator) {
        this.container = container;

        this.versionInfo = container.getByTestId('aboutModalVersionInfo');
        this.hostname = container.getByText(/^Hostname:/);
        this.serverStarted = container.getByTestId('aboutModalServerStarted');
        this.closeButton = container.getByRole('button', {name: 'Close'});
    }

    async toBeVisible() {
        await expect(this.container).toBeVisible();
    }

    async close() {
        await this.closeButton.click();
        await expect(this.container).toHaveCount(0);
    }
}
