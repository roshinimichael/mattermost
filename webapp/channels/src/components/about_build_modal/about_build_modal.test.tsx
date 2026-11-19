// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';

import type {ClientConfig, ClientLicense} from '@mattermost/types/config';

import {Client4} from 'mattermost-redux/client';

import AboutBuildModalConnected from 'components/about_build_modal';
import AboutBuildModal from 'components/about_build_modal/about_build_modal';

import {renderWithContext, screen, userEvent, waitFor} from 'tests/react_testing_utils';
import {AboutLinks} from 'utils/constants';

import AboutBuildModalCloud from './about_build_modal_cloud/about_build_modal_cloud';

describe('components/AboutBuildModal', () => {
    const RealDate: DateConstructor = Date;

    function mockDate(date: Date) {
        function mock() {
            return new RealDate(date);
        }
        mock.now = () => date.getTime();
        global.Date = mock as unknown as DateConstructor;
    }

    let config: Partial<ClientConfig> = {};
    let license: ClientLicense = {};
    let socketStatus = {
        connected: false,
        serverHostname: '',
    };

    afterEach(() => {
        global.Date = RealDate;
        config = {};
        license = {};
        socketStatus = {
            connected: false,
            serverHostname: '',
        };
        jest.restoreAllMocks();
    });

    beforeEach(() => {
        mockDate(new Date(2017, 6, 1));

        // Mock the license load metric API call for all tests to prevent errors
        jest.spyOn(Client4, 'getLicenseLoadMetric').mockResolvedValue({load: 0});

        config = {
            BuildEnterpriseReady: 'true',
            Version: '3.6.0',
            SchemaVersion: '77',
            BuildNumber: '123456',
            SQLDriverName: 'Postgres',
            BuildHash: 'abcdef1234567890',
            BuildHashEnterprise: '0123456789abcdef',
            BuildDate: '21 January 2017',
            TermsOfServiceLink: AboutLinks.TERMS_OF_SERVICE,
            PrivacyPolicyLink: AboutLinks.PRIVACY_POLICY,
        };
        license = {
            IsLicensed: 'true',
            Company: 'Mattermost Inc',
            SkuShortName: 'entry',
        };
        socketStatus = {
            connected: true,
            serverHostname: 'mock.localhost',
        };
    });

    test('should match snapshot for enterprise edition', () => {
        renderAboutBuildModal({config, license, socketStatus});
        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Server Version: 3.6.0');
        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Database Schema Version: 77');
        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Build Number: 123456');
        expect(screen.getByText('Mattermost Entry')).toBeInTheDocument();
        expect(screen.getByText('Modern communication from behind your firewall.')).toBeInTheDocument();
        expect(screen.getByRole('link', {name: 'mattermost.com'})).toHaveAttribute('href', 'https://mattermost.com/?utm_source=mattermost&utm_medium=in-product&utm_content=about_build_modal&uid=&sid=&edition=enterprise&server_version=3.6.0');
        expect(screen.getByText('EE Build Hash: 0123456789abcdef', {exact: false})).toBeInTheDocument();
        expect(screen.queryByText('Hostname: mock.localhost', {exact: false})).toBeInTheDocument();

        expect(screen.getByRole('link', {name: 'server'})).toHaveAttribute('href', 'https://github.com/mattermost/mattermost-server/blob/master/NOTICE.txt');
        expect(screen.getByRole('link', {name: 'desktop'})).toHaveAttribute('href', 'https://github.com/mattermost/desktop/blob/master/NOTICE.txt');
        expect(screen.getByRole('link', {name: 'mobile'})).toHaveAttribute('href', 'https://github.com/mattermost/mattermost-mobile/blob/master/NOTICE.txt');
    });

    test('should match snapshot for team edition', () => {
        const teamConfig = {
            ...config,
            BuildEnterpriseReady: 'false',
            BuildHashEnterprise: '',
        };

        renderAboutBuildModal({config: teamConfig, license: {}, socketStatus: {connected: false}});
        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Server Version: 3.6.0');
        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Database Schema Version: 77');
        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Build Number: 123456');
        expect(screen.getByText('Mattermost Team Edition')).toBeInTheDocument();
        expect(screen.getByText('All your team communication in one place, instantly searchable and accessible anywhere.')).toBeInTheDocument();
        expect(screen.getByRole('link', {name: 'mattermost.com/community/'})).toHaveAttribute('href', 'https://mattermost.com/community/?utm_source=mattermost&utm_medium=in-product&utm_content=about_build_modal&uid=&sid=&edition=team&server_version=3.6.0');
        expect(screen.queryByText('EE Build Hash: 0123456789abcdef')).not.toBeInTheDocument();
        expect(screen.queryByText('Hostname: disconnected', {exact: false})).toBeInTheDocument();

        expect(screen.getByRole('link', {name: 'server'})).toHaveAttribute('href', 'https://github.com/mattermost/mattermost-server/blob/master/NOTICE.txt');
        expect(screen.getByRole('link', {name: 'desktop'})).toHaveAttribute('href', 'https://github.com/mattermost/desktop/blob/master/NOTICE.txt');
        expect(screen.getByRole('link', {name: 'mobile'})).toHaveAttribute('href', 'https://github.com/mattermost/mattermost-mobile/blob/master/NOTICE.txt');
    });

    test('should match snapshot for cloud edition', () => {
        if (license !== null) {
            license.Cloud = 'true';
        }

        renderWithContext(
            <AboutBuildModalCloud
                config={config}
                license={license}
                show={true}
                onExited={jest.fn()}
                doHide={jest.fn()}
            />,
        );

        expect(screen.getByText('Mattermost Cloud')).toBeInTheDocument();
        expect(screen.getByText('High-trust messaging for the enterprise')).toBeInTheDocument();
        expect(screen.getByTestId('aboutModalVersion')).toHaveTextContent('Mattermost Version: 3.6.0');
        expect(screen.getByText('0123456789abcdef', {exact: false})).toBeInTheDocument();
        expect(screen.getByRole('link', {name: 'server'})).toHaveAttribute('href', 'https://github.com/mattermost/mattermost-server/blob/master/NOTICE.txt');
        expect(screen.getByRole('link', {name: 'desktop'})).toHaveAttribute('href', 'https://github.com/mattermost/desktop/blob/master/NOTICE.txt');
        expect(screen.getByRole('link', {name: 'mobile'})).toHaveAttribute('href', 'https://github.com/mattermost/mattermost-mobile/blob/master/NOTICE.txt');
    });

    test('should show n/a if this is a dev build', () => {
        const sameBuildConfig = {
            ...config,
            BuildEnterpriseReady: 'false',
            BuildHashEnterprise: '',
            Version: '3.6.0',
            SchemaVersion: '77',
            BuildNumber: 'dev',
        };

        renderAboutBuildModal({config: sameBuildConfig, license: {}, socketStatus: {connected: true}});

        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Server Version: dev');
        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Database Schema Version: 77');
        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Build Number: n/a');
        expect(screen.getByText('Mattermost Team Edition')).toBeInTheDocument();
        expect(screen.getByText('All your team communication in one place, instantly searchable and accessible anywhere.')).toBeInTheDocument();
        expect(screen.getByRole('link', {name: 'mattermost.com/community/'})).toHaveAttribute('href', 'https://mattermost.com/community/?utm_source=mattermost&utm_medium=in-product&utm_content=about_build_modal&uid=&sid=&edition=team&server_version=dev');
        expect(screen.queryByText('EE Build Hash: 0123456789abcdef')).not.toBeInTheDocument();
        expect(screen.queryByText('Hostname: server did not provide hostname', {exact: false})).toBeInTheDocument();

        expect(screen.getByRole('link', {name: 'server'})).toHaveAttribute('href', 'https://github.com/mattermost/mattermost-server/blob/master/NOTICE.txt');
        expect(screen.getByRole('link', {name: 'desktop'})).toHaveAttribute('href', 'https://github.com/mattermost/desktop/blob/master/NOTICE.txt');
        expect(screen.getByRole('link', {name: 'mobile'})).toHaveAttribute('href', 'https://github.com/mattermost/mattermost-mobile/blob/master/NOTICE.txt');
    });

    test('should call onExited callback when the modal is hidden', async () => {
        const onExited = jest.fn();
        const state = {
            entities: {
                general: {
                    config: {},
                    license: {
                        Cloud: 'false',
                    },
                },
                users: {
                    currentUserId: 'currentUserId',
                },
            },
        };

        renderWithContext(
            <AboutBuildModal
                config={config}
                license={license}
                socketStatus={socketStatus}
                onExited={onExited}
            />,
            state,
        );

        await userEvent.click(screen.getByText('Close'));
        expect(onExited).toHaveBeenCalledTimes(1);
    });

    test('should show default tos and privacy policy links and not the config links', () => {
        const state = {
            entities: {
                general: {
                    config,
                    license: {
                        Cloud: 'false',
                    },
                },
                users: {
                    currentUserId: 'currentUserId',
                },
            },
        };
        renderWithContext(
            <AboutBuildModal
                config={config}
                license={license}
                socketStatus={socketStatus}
                onExited={jest.fn()}
            />,
            state,
        );

        expect(screen.getByRole('link', {name: 'Terms of Use'})).toHaveAttribute('href', `${AboutLinks.TERMS_OF_SERVICE}?utm_source=mattermost&utm_medium=in-product&utm_content=about_build_modal&uid=currentUserId&sid=&edition=enterprise&server_version=3.6.0`);

        expect(screen.getByRole('link', {name: 'Privacy Policy'})).toHaveAttribute('href', `${AboutLinks.PRIVACY_POLICY}?utm_source=mattermost&utm_medium=in-product&utm_content=about_build_modal&uid=currentUserId&sid=&edition=enterprise&server_version=3.6.0`);

        expect(screen.getByRole('link', {name: 'Terms of Use'})).not.toHaveAttribute('href', config?.TermsOfServiceLink);
        expect(screen.getByRole('link', {name: 'Privacy Policy'})).not.toHaveAttribute('href', config?.PrivacyPolicyLink);
    });

    test('should show load metric when license is loaded and API returns data', async () => {
        // Override the global mock for this specific test
        jest.spyOn(Client4, 'getLicenseLoadMetric').mockResolvedValue({load: 75});

        renderAboutBuildModal({
            license: {
                IsLicensed: 'true',
                Company: 'Mattermost Inc',
            },
        });

        await waitFor(() => {
            expect(screen.getByTestId('aboutModalVersionInfo')).toBeInTheDocument();
            expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Load Metric: 75');
        });
    });

    test('should not show load metric when API returns zero', async () => {
        // This uses the mock set in beforeEach that returns load: 0
        renderAboutBuildModal();

        // Wait for any async operations to complete
        await waitFor(() => {
            expect(Client4.getLicenseLoadMetric).toHaveBeenCalled();
        });

        expect(screen.getByTestId('aboutModalVersionInfo')).not.toHaveTextContent('Load Metric:');
    });

    test('should handle API errors gracefully', async () => {
        // Temporarily suppress console.error for this test
        console.error = jest.fn();

        // Mock the API call to throw an error
        jest.spyOn(Client4, 'getLicenseLoadMetric').mockRejectedValue(new Error('API error'));

        renderAboutBuildModal();

        // Wait for the API call to be made
        await waitFor(() => {
            expect(Client4.getLicenseLoadMetric).toHaveBeenCalled();
        });

        // The error should be logged but not cause the component to crash
        expect(console.error).toHaveBeenCalled();
        expect(screen.getByTestId('aboutModalVersionInfo')).not.toHaveTextContent('Load Metric:');
    });

    test('should show FIPS indicator when IsFipsEnabled is true', () => {
        const fipsConfig = {
            ...config,
            IsFipsEnabled: 'true',
        };

        renderAboutBuildModal({config: fipsConfig});

        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Server Version: 3.6.0 (FIPS)');
    });

    test('should not show FIPS indicator when IsFipsEnabled is false', () => {
        const nonFipsConfig = {
            ...config,
            IsFipsEnabled: 'false',
        };

        renderAboutBuildModal({config: nonFipsConfig});

        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Server Version: 3.6.0');
        expect(screen.getByTestId('aboutModalVersionInfo')).not.toHaveTextContent('(FIPS)');
    });

    test('should not show FIPS indicator when IsFipsEnabled is not set', () => {
        const nonFipsConfig = {
            ...config,
        };

        renderAboutBuildModal({config: nonFipsConfig});

        expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Server Version: 3.6.0');
        expect(screen.getByTestId('aboutModalVersionInfo')).not.toHaveTextContent('(FIPS)');
    });

    describe('server start time', () => {
        const startedAt = 1699970400000;
        const expectedDate = new Intl.DateTimeFormat('en', {year: 'numeric', month: 'short', day: '2-digit'}).format(startedAt);
        const expectedTime = new Intl.DateTimeFormat('en', {hour: '2-digit', minute: '2-digit'}).format(startedAt);

        beforeEach(() => {
            global.Date = RealDate;
        });

        function connectedState(roles: string) {
            return {
                entities: {
                    general: {
                        config,
                        license,
                    },
                    users: {
                        currentUserId: 'u1',
                        profiles: {
                            u1: {id: 'u1', username: 'someone', roles},
                        },
                    },
                },
                websocket: {
                    connected: true,
                    serverHostname: 'mock.localhost',
                },
            };
        }

        test('shows the server start time under the hostname for a system admin', async () => {
            jest.spyOn(Client4, 'getServerStartTime').mockResolvedValue({started_at: startedAt});

            renderAboutBuildModal({isSystemAdmin: true});

            const line = await screen.findByTestId('aboutModalServerStarted');
            expect(line.textContent).toMatch(/^Server started:\s.+ - .+$/);
            expect(line).toHaveTextContent(`Server started: ${expectedDate} - ${expectedTime}`);

            const hostname = screen.getByText('Hostname: mock.localhost', {exact: false});
            expect(hostname.compareDocumentPosition(line) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        });

        test('still shows version, hostname and build info alongside the server start time', async () => {
            jest.spyOn(Client4, 'getServerStartTime').mockResolvedValue({started_at: startedAt});

            renderAboutBuildModal({isSystemAdmin: true});

            await screen.findByTestId('aboutModalServerStarted');
            expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Server Version: 3.6.0');
            expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Build Number: 123456');
            expect(screen.getByText('Hostname: mock.localhost', {exact: false})).toBeInTheDocument();
        });

        test('requests the server start time only once for a system admin, across rerenders', async () => {
            jest.spyOn(Client4, 'getServerStartTime').mockResolvedValue({started_at: startedAt});

            const props = {
                show: true,
                onExited: jest.fn(),
                config,
                license,
                socketStatus,
                isSystemAdmin: true,
            };
            const {rerender} = renderWithContext(<AboutBuildModal {...props}/>, {
                entities: {general: {config, license}, users: {currentUserId: ''}},
            });

            await screen.findByTestId('aboutModalServerStarted');
            rerender(
                <AboutBuildModal
                    {...props}
                    socketStatus={{connected: false, serverHostname: ''}}
                />,
            );
            rerender(<AboutBuildModal {...props}/>);

            expect(Client4.getServerStartTime).toHaveBeenCalledTimes(1);
        });

        test('still opens without the server start time when the request fails', async () => {
            console.error = jest.fn();
            jest.spyOn(Client4, 'getServerStartTime').mockRejectedValue(new Error('403'));

            renderAboutBuildModal({isSystemAdmin: true});

            await waitFor(() => {
                expect(Client4.getServerStartTime).toHaveBeenCalled();
            });
            await waitFor(() => {
                expect(console.error).toHaveBeenCalled();
            });

            expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Server Version: 3.6.0');
            expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Build Number: 123456');
            expect(screen.getByText('Hostname: mock.localhost', {exact: false})).toBeInTheDocument();
            expect(screen.queryByTestId('aboutModalServerStarted')).not.toBeInTheDocument();
            expect(screen.queryByText(/Server started:/)).not.toBeInTheDocument();
        });

        test('does not show or request the server start time for a user who is not a system admin', async () => {
            jest.spyOn(Client4, 'getServerStartTime').mockResolvedValue({started_at: startedAt});

            renderAboutBuildModal({isSystemAdmin: false});

            await waitFor(() => {
                expect(Client4.getLicenseLoadMetric).toHaveBeenCalled();
            });

            expect(Client4.getServerStartTime).not.toHaveBeenCalled();
            expect(screen.queryByTestId('aboutModalServerStarted')).not.toBeInTheDocument();
            expect(screen.queryByText(/Server started:/)).not.toBeInTheDocument();
            expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Server Version: 3.6.0');
            expect(screen.getByText('Hostname: mock.localhost', {exact: false})).toBeInTheDocument();
        });

        test('connected dialog requests and shows the server start time once for a system admin', async () => {
            jest.spyOn(Client4, 'getServerStartTime').mockResolvedValue({started_at: startedAt});

            const {rerender} = renderWithContext(
                <AboutBuildModalConnected onExited={jest.fn()}/>,
                connectedState('system_user system_admin'),
            );

            const line = await screen.findByTestId('aboutModalServerStarted');
            expect(line).toHaveTextContent('Server started:');
            expect(line).toHaveTextContent(expectedDate);

            rerender(<AboutBuildModalConnected onExited={jest.fn()}/>);
            expect(Client4.getServerStartTime).toHaveBeenCalledTimes(1);
        });

        test('connected dialog does not request the server start time for a regular user', async () => {
            jest.spyOn(Client4, 'getServerStartTime').mockResolvedValue({started_at: startedAt});

            renderWithContext(
                <AboutBuildModalConnected onExited={jest.fn()}/>,
                connectedState('system_user'),
            );

            await waitFor(() => {
                expect(Client4.getLicenseLoadMetric).toHaveBeenCalled();
            });

            expect(Client4.getServerStartTime).not.toHaveBeenCalled();
            expect(screen.queryByTestId('aboutModalServerStarted')).not.toBeInTheDocument();
            expect(screen.getByTestId('aboutModalVersionInfo')).toHaveTextContent('Server Version:');
        });
    });

    function renderAboutBuildModal(props = {}) {
        const onExited = jest.fn();
        const show = true;

        const allProps = {
            show,
            onExited,
            config,
            license,
            socketStatus,
            ...props,
        };

        // Create state with the config and license for useExternalLink hook to access
        const state = {
            entities: {
                general: {
                    config: allProps.config,
                    license: allProps.license,
                },
                users: {
                    currentUserId: '',
                },
            },
        };

        return renderWithContext(<AboutBuildModal {...allProps}/>, state);
    }
});
