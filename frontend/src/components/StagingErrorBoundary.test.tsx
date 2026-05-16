import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import StagingErrorBoundary from './StagingErrorBoundary'

/** A child component that always throws during render. */
const ThrowingChild = ({ message }: { message: string }): never => {
  throw new Error(message)
}

describe('StagingErrorBoundary', () => {
  // Suppress the React error boundary console.error output so tests stay clean.
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('when no error is thrown', () => {
    it('renders children normally', () => {
      render(
        <StagingErrorBoundary>
          <p>Hello, world</p>
        </StagingErrorBoundary>,
      )
      expect(screen.getByText('Hello, world')).toBeInTheDocument()
    })
  })

  describe('in non-production environment (MODE = test)', () => {
    it('renders the error panel when a child throws', () => {
      render(
        <StagingErrorBoundary>
          <ThrowingChild message="Clerk publishable key is invalid" />
        </StagingErrorBoundary>,
      )

      expect(screen.getByTestId('staging-error-boundary')).toBeInTheDocument()
    })

    it('displays the thrown error message in the panel', () => {
      render(
        <StagingErrorBoundary>
          <ThrowingChild message="Clerk publishable key is invalid" />
        </StagingErrorBoundary>,
      )

      expect(
        screen.getByText('Clerk publishable key is invalid'),
      ).toBeInTheDocument()
    })

    it('does not render the plain production fallback in non-production', () => {
      render(
        <StagingErrorBoundary>
          <ThrowingChild message="any error" />
        </StagingErrorBoundary>,
      )

      // The production fallback <p>Something went wrong</p> should NOT appear
      expect(screen.queryByText('Something went wrong')).not.toBeInTheDocument()
    })

    it('suppresses unhandled React error boundary warning via console.error spy', () => {
      render(
        <StagingErrorBoundary>
          <ThrowingChild message="test error" />
        </StagingErrorBoundary>,
      )

      // React calls console.error with the error boundary uncaught-error warning.
      // Our spy swallows it — verify no real console.error leaks through.
      expect(console.error).toHaveBeenCalled()
    })
  })
})
