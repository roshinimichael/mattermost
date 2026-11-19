// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

// These tests call a running Mattermost server (SKYRAMP_TEST_BASE_URL) with an admin
// token (SKYRAMP_TEST_TOKEN). The file name keeps the unit test runs from collecting it;
// run it with: npx jest --config jest.config.js --testMatch '**/system_start_time_api.live.ts'

// eslint-disable-next-line no-process-env
const baseUrl = (process.env.SKYRAMP_TEST_BASE_URL || 'http://localhost:8065').replace(/\/$/, '');
const apiUrl = `${baseUrl}/api/v4`;

// eslint-disable-next-line no-process-env
const adminToken = process.env.SKYRAMP_TEST_TOKEN;

type ApiResponse = {
    status: number;
    contentType: string;
    body: Record<string, unknown>;
    token: string | null;
};

async function call(method: string, path: string, options: {token?: string; body?: unknown; headers?: Record<string, string>} = {}): Promise<ApiResponse> {
    const headers: Record<string, string> = {'X-Requested-With': 'XMLHttpRequest', ...options.headers};
    if (options.token) {
        headers.Authorization = `Bearer ${options.token}`;
    }
    if (options.body !== undefined) {
        headers['Content-Type'] = 'application/json';
    }
    const response = await fetch(`${apiUrl}${path}`, {
        method,
        headers,
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
    const text = await response.text();
    let body: Record<string, unknown> = {raw: text};
    try {
        body = JSON.parse(text) as Record<string, unknown>;
    } catch {
        // keep the raw text
    }
    return {
        status: response.status,
        contentType: response.headers.get('content-type') || '',
        body,
        token: response.headers.get('token'),
    };
}

function uniqueName(prefix: string) {
    return `${prefix}${Date.now()}${Math.floor(Math.random() * 1e6)}`;
}

describe('GET /api/v4/system/start_time against a running server', () => {
    const createdUserIds: string[] = [];
    const createdTeamIds: string[] = [];

    async function createUserAndLogin() {
        const username = uniqueName('starttime');
        const password = `Pw-${uniqueName('x')}!`;
        const created = await call('POST', '/users', {
            token: adminToken,
            body: {username, email: `${username}@example.com`, password},
        });
        expect(created.status).toBe(201);
        createdUserIds.push(created.body.id as string);

        const login = await call('POST', '/users/login', {body: {login_id: username, password}});
        expect(login.status).toBe(200);
        expect(login.token).toBeTruthy();
        return {userId: created.body.id as string, token: login.token as string};
    }

    afterAll(async () => {
        for (const id of createdUserIds) {
            await call('DELETE', `/users/${id}`, {token: adminToken}); // eslint-disable-line no-await-in-loop
        }
        for (const id of createdTeamIds) {
            await call('DELETE', `/teams/${id}`, {token: adminToken}); // eslint-disable-line no-await-in-loop
        }
    });

    test('answers a system admin with the start time in milliseconds', async () => {
        const response = await call('GET', '/system/start_time', {token: adminToken});
        const receivedAt = Date.now();

        expect(response.status).toBe(200);
        expect(response.contentType).toContain('application/json');
        expect(Object.keys(response.body)).toEqual(['started_at']);
        expect(Number.isInteger(response.body.started_at)).toBe(true);
        expect(response.body.started_at).toBeGreaterThan(1e12);
        expect(response.body.started_at).toBeLessThanOrEqual(receivedAt);
    });

    test('returns the same start time on every call, earlier than the first request', async () => {
        const firstRequestAt = Date.now();
        const first = await call('GET', '/system/start_time', {token: adminToken});
        await new Promise((resolve) => setTimeout(resolve, 1100));

        let secondToken = adminToken;
        const username = process.env.SKYRAMP_UI_USERNAME; // eslint-disable-line no-process-env
        const password = process.env.SKYRAMP_UI_PASSWORD; // eslint-disable-line no-process-env
        if (username && password) {
            const login = await call('POST', '/users/login', {body: {login_id: username, password}});
            expect(login.status).toBe(200);
            secondToken = login.token as string;
        }
        const second = await call('GET', '/system/start_time', {token: secondToken});

        expect(first.status).toBe(200);
        expect(second.status).toBe(200);
        expect(second.body.started_at).toBe(first.body.started_at);
        expect(first.body.started_at).toBeLessThan(firstRequestAt);
    });

    test('refuses a request with no session with 401', async () => {
        const noAuth = await call('GET', '/system/start_time');
        expect(noAuth.status).toBe(401);
        expect(noAuth.body.id).toBe('api.context.session_expired.app_error');
        expect(noAuth.body).not.toHaveProperty('started_at');

        const badToken = await call('GET', '/system/start_time', {token: 'abcdefghijklmnopqrstuvwxyz'});
        expect(badToken.status).toBe(401);
        expect(badToken.body.id).toBe('api.context.session_expired.app_error');
        expect(badToken.body).not.toHaveProperty('started_at');
    });

    test('refuses a signed-in regular user with 403', async () => {
        const {token} = await createUserAndLogin();

        const withBearer = await call('GET', '/system/start_time', {token});
        expect(withBearer.status).toBe(403);
        expect(withBearer.body.id).toBe('api.context.permissions.app_error');
        expect(withBearer.body).not.toHaveProperty('started_at');

        const withCookie = await call('GET', '/system/start_time', {headers: {Cookie: `MMAUTHTOKEN=${token}`}});
        expect(withCookie.status).toBe(403);
        expect(withCookie.body.id).toBe('api.context.permissions.app_error');
        expect(withCookie.body).not.toHaveProperty('started_at');
    });

    test('refuses a team admin with 403', async () => {
        const teamName = uniqueName('starttimeteam');
        const team = await call('POST', '/teams', {
            token: adminToken,
            body: {name: teamName, display_name: teamName, type: 'O'},
        });
        expect(team.status).toBe(201);
        createdTeamIds.push(team.body.id as string);

        const {userId, token} = await createUserAndLogin();
        const member = await call('POST', `/teams/${team.body.id}/members`, {
            token: adminToken,
            body: {team_id: team.body.id, user_id: userId},
        });
        expect(member.status).toBe(201);
        const roles = await call('PUT', `/teams/${team.body.id}/members/${userId}/roles`, {
            token: adminToken,
            body: {roles: 'team_user team_admin'},
        });
        expect(roles.status).toBe(200);

        const response = await call('GET', '/system/start_time', {token});
        expect(response.status).toBe(403);
        expect(response.body.id).toBe('api.context.permissions.app_error');
        expect(response.body).not.toHaveProperty('started_at');
    });

    test('does not route POST to the start time handler', async () => {
        const startTime = await call('POST', '/system/start_time', {token: adminToken, body: {}});
        const timezones = await call('POST', '/system/timezones', {token: adminToken, body: {}});

        expect(timezones.status).toBe(404);
        expect(timezones.body.id).toBe('api.context.404.app_error');
        expect(startTime.status).toBe(404);
        expect(startTime.body.id).toBe('api.context.404.app_error');
        expect(startTime.body).not.toHaveProperty('started_at');
    });
});
