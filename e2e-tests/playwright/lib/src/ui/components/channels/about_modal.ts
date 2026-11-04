// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {Locator, expect} from '@playwright/test';

/**
 * The About dialog, opened from the product switch menu's "About Mattermost" item.
 */
export default class AboutModal {
    readonly container: Locator;

    readonly versionInfo: Locator;
    readonly clockSkewWarning: Locator;
    readonly closeButton: Locator;

    constructor(container: Locator) {
        this.container = container;
        this.versionInfo = container.getByTestId('aboutModalVersionInfo');
        this.clockSkewWarning = container.getByTestId('aboutModalClockSkew');
        this.closeButton = container.getByRole('button', {name: 'Close'});
    }

    async toBeVisible() {
        await expect(this.container).toBeVisible();
        await expect(this.versionInfo).toBeVisible();
    }

    async close() {
        await this.closeButton.click();
        await expect(this.versionInfo).toHaveCount(0);
    }
}
