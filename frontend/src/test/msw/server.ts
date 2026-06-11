/**
 * MSW server setup for integration tests.
 *
 * ADR-077 / C1: server is created with NO default handlers. DAEMON_BASE is
 * derived from runtimeConfig at factory-invocation time, which means calling
 * setupServer(...createHandlers()) at module scope would throw because
 * loadConfig() has not yet run when this module is imported.
 *
 * Instead, test suites wire handlers in beforeEach:
 *
 *   import { server } from '@/test/msw/server';
 *   import { createHandlers } from '@/test/msw/handlers';
 *   import { setRuntimeConfig } from '@/config/runtimeConfig';
 *
 *   beforeEach(() => {
 *     setRuntimeConfig(testDefaults);
 *     server.use(...createHandlers());
 *   });
 */
import { setupServer } from 'msw/node';

// No handlers at construction time — see above.
export const server = setupServer();
