import { Component, type ErrorInfo, type ReactNode } from 'react'

interface Props {
  children: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
}

/**
 * StagingErrorBoundary — catches render-time errors and surfaces them visibly
 * in non-production environments (development, test, staging).
 *
 * In production the fallback is a plain <p> tag — Sentry's outer ErrorBoundary
 * will capture and report the error before this fallback is ever seen.
 *
 * Placement: wrap <ClerkProvider> so that Clerk initialization failures
 * (wrong publishable key, unauthorized domain) are immediately visible rather
 * than showing a blank dark screen.
 */
class StagingErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    if (import.meta.env.MODE !== 'production') {
      // Intentional: surface full error context in non-production environments.
      // This block is stripped in production builds (tree-shaken by Vite).
      console.error('[StagingErrorBoundary]', error, info.componentStack)
    }
  }

  render(): ReactNode {
    if (!this.state.hasError) {
      return this.props.children
    }

    if (import.meta.env.MODE !== 'production') {
      const { error } = this.state
      return (
        <div
          data-testid="staging-error-boundary"
          style={{
            position: 'fixed',
            inset: 0,
            backgroundColor: '#7f1d1d',
            color: '#ffffff',
            fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
            fontSize: '14px',
            lineHeight: '1.6',
            padding: '2rem',
            overflow: 'auto',
            zIndex: 9999,
          }}
        >
          <h1 style={{ fontSize: '1.5rem', fontWeight: 'bold', marginBottom: '1rem' }}>
            App Error (non-production)
          </h1>
          <p style={{ marginBottom: '1rem', fontWeight: 'bold' }}>
            {error?.message ?? 'Unknown error'}
          </p>
          {error?.stack && (
            <pre
              style={{
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                backgroundColor: 'rgba(0,0,0,0.3)',
                padding: '1rem',
                borderRadius: '4px',
              }}
            >
              {error.stack}
            </pre>
          )}
        </div>
      )
    }

    // Production fallback — Sentry's outer boundary handles reporting.
    return <p>Something went wrong</p>
  }
}

export default StagingErrorBoundary
