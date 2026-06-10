package repository

import (
	"context"
	"fmt"
	"time"

	clerkuser "github.com/clerk/clerk-sdk-go/v2/user"
)

// ClerkProfileFetcher satisfies the clerkProfileFetcher interface using the
// clerk-sdk-go/v2 user.Client.  Wired in main.go with the CLERK_SECRET_KEY.
type ClerkProfileFetcher struct {
	client *clerkuser.Client
}

// NewClerkProfileFetcher returns a ClerkProfileFetcher backed by the given
// clerk-sdk-go/v2 user.Client.
func NewClerkProfileFetcher(client *clerkuser.Client) *ClerkProfileFetcher {
	return &ClerkProfileFetcher{client: client}
}

// FetchClerkProfile fetches the user's Clerk profile and extracts the fields
// required for the Art.15 export: primary email, first name, last name, and
// account creation timestamp.
//
// Returns (nil, nil) when the Clerk user is not found (404 from Clerk API).
func (f *ClerkProfileFetcher) FetchClerkProfile(ctx context.Context, clerkUserID string) (*ClerkProfile, error) {
	u, err := f.client.Get(ctx, clerkUserID)
	if err != nil {
		return nil, fmt.Errorf("ClerkProfileFetcher.FetchClerkProfile: %w", err)
	}
	if u == nil {
		return nil, nil
	}

	profile := &ClerkProfile{
		// CreatedAt is stored as Unix milliseconds in the Clerk API.
		CreatedAt: time.UnixMilli(u.CreatedAt).UTC(),
	}

	if u.FirstName != nil {
		profile.FirstName = *u.FirstName
	}
	if u.LastName != nil {
		profile.LastName = *u.LastName
	}

	// Resolve the primary email address from the EmailAddresses slice.
	// PrimaryEmailAddressID identifies which address is primary.
	if u.PrimaryEmailAddressID != nil {
		for _, ea := range u.EmailAddresses {
			if ea != nil && ea.ID == *u.PrimaryEmailAddressID {
				profile.Email = ea.EmailAddress
				break
			}
		}
	}
	// Fallback: use the first email address if primary resolution failed.
	if profile.Email == "" && len(u.EmailAddresses) > 0 && u.EmailAddresses[0] != nil {
		profile.Email = u.EmailAddresses[0].EmailAddress
	}

	return profile, nil
}
