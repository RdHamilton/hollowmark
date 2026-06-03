import { useState, useEffect, useRef } from 'react';
import { APIKeys } from '@clerk/react';
import './ApiKeys.css';

/**
 * API Keys page — lets authenticated users create, view, and revoke Clerk API keys.
 * Uses the Clerk built-in <APIKeys /> component which handles all key management UI.
 * Route: /api-keys (protected via ProtectedRoute in App.tsx)
 *
 * The Clerk <APIKeys /> component requires the API Keys feature to be enabled in
 * the Clerk Dashboard. If the feature is not enabled or the component takes too
 * long to initialize, a fallback message is shown after a short delay.
 */
const ApiKeysPage = () => {
  const [showFallback, setShowFallback] = useState(false);
  const contentRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Allow Clerk time to mount the API Keys component.
    // If the container is still empty after 3s, show the configuration fallback.
    const timer = setTimeout(() => {
      if (contentRef.current) {
        const hasRenderedContent = contentRef.current.children.length > 0 &&
          contentRef.current.children[0].children.length > 0;
        if (!hasRenderedContent) {
          setShowFallback(true);
        }
      }
    }, 3000);

    return () => clearTimeout(timer);
  }, []);

  return (
    <div className="page-container" data-testid="api-keys-page">
      <div className="api-keys-header">
        <h1 className="page-title">API Keys</h1>
        <p className="api-keys-description">
          Create and manage personal API keys for programmatic access to VaultMTG.
          Copy each key when it is created — the full key is only shown once.
        </p>
      </div>

      <div className="api-keys-content" data-testid="api-keys-content" ref={contentRef}>
        <APIKeys />
      </div>

      {showFallback && (
        <div className="api-keys-fallback" data-testid="api-keys-fallback">
          <p>
            API key management requires the API Keys feature to be enabled in the
            Clerk Dashboard. Contact your VaultMTG administrator if you need API
            access.
          </p>
        </div>
      )}
    </div>
  );
};

export default ApiKeysPage;
