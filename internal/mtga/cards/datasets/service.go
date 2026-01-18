package datasets

import (
	"context"
	"fmt"
	"log"

	"github.com/ramonehamilton/MTGA-Companion/internal/mtga/cards/seventeenlands"
)

// Service provides unified access to 17Lands card ratings from multiple sources.
type Service struct {
	downloader *Downloader
	parser     *CSVParser
	webScraper *WebScraper
}

// ServiceOptions configures the dataset service.
type ServiceOptions struct {
	DownloaderOptions DownloaderOptions
	WebScraperOptions WebScraperOptions
}

// DefaultServiceOptions returns default service options.
func DefaultServiceOptions() ServiceOptions {
	return ServiceOptions{
		DownloaderOptions: DefaultDownloaderOptions(),
		WebScraperOptions: DefaultWebScraperOptions(),
	}
}

// NewService creates a new dataset service.
func NewService(options ServiceOptions) (*Service, error) {
	// Create downloader
	downloader, err := NewDownloader(options.DownloaderOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create downloader: %w", err)
	}

	// Create web scraper
	webScraper := NewWebScraper(options.WebScraperOptions)

	// Create CSV parser
	parser := NewCSVParser()

	return &Service{
		downloader: downloader,
		parser:     parser,
		webScraper: webScraper,
	}, nil
}

// GetCardRatings fetches card ratings for a set and format.
// Strategy:
// 1. Try S3 public datasets first (recommended approach)
// 2. If S3 data is found, merge Arena IDs and Scryfall URLs from web API
// 3. If S3 fails, dataset doesn't exist, or merge fails, fall back to web API
//
// This ensures we use the recommended datasets when available while
// still supporting newer sets like TLA that only have web API data.
// The web API provides Arena IDs (mtga_id) and Scryfall URLs that CSV doesn't have.
//
// IMPORTANT: CSV ratings without Arena IDs are NOT returned, since they can't
// be matched to cards and would be skipped during save. If the merge fails,
// we fall back to the web API which includes Arena IDs.
func (s *Service) GetCardRatings(ctx context.Context, setCode, format string) ([]seventeenlands.CardRating, error) {
	log.Printf("[DatasetService] Fetching card ratings for %s / %s", setCode, format)

	// Strategy 1: Try S3 datasets first
	ratings, err := s.tryS3Dataset(ctx, setCode, format)
	if err == nil && len(ratings) > 0 {
		log.Printf("[DatasetService] Successfully fetched %d ratings from S3 datasets", len(ratings))

		// Merge Arena IDs and Scryfall URLs from web API
		// This is needed because CSV files don't contain card identifiers
		mergedRatings, mergeErr := s.mergeWebAPIMetadata(ctx, ratings, setCode, format)
		if mergeErr != nil {
			log.Printf("[DatasetService] Warning: failed to merge web API metadata: %v, falling back to web API", mergeErr)
			// Don't return CSV ratings without Arena IDs - they can't be matched to cards
			// Fall through to web API fallback below
		} else {
			// Check that we actually got Arena IDs for most cards
			cardsWithArenaID := 0
			for _, r := range mergedRatings {
				if r.MTGAID > 0 {
					cardsWithArenaID++
				}
			}

			if cardsWithArenaID > 0 {
				log.Printf("[DatasetService] Merged %d/%d ratings with Arena IDs from web API", cardsWithArenaID, len(mergedRatings))
				return mergedRatings, nil
			}

			log.Printf("[DatasetService] Warning: merge succeeded but no Arena IDs found, falling back to web API")
		}
	} else {
		// Log S3 failure but continue to fallback
		if err != nil {
			log.Printf("[DatasetService] S3 dataset unavailable: %v, falling back to web API", err)
		} else {
			log.Printf("[DatasetService] S3 dataset returned no data, falling back to web API")
		}
	}

	// Strategy 2: Fall back to web API (for sets like TLA, or when CSV merge failed)
	ratings, err = s.webScraper.FetchCardRatings(ctx, setCode, format)
	if err != nil {
		return nil, fmt.Errorf("both S3 and web API failed: web API error: %w", err)
	}

	if len(ratings) == 0 {
		return nil, fmt.Errorf("no card ratings available for %s / %s", setCode, format)
	}

	log.Printf("[DatasetService] Successfully fetched %d ratings from web API (fallback)", len(ratings))
	return ratings, nil
}

// mergeWebAPIMetadata fetches card metadata (Arena IDs, Scryfall URLs) from the web API
// and merges them into CSV-parsed ratings based on card name.
func (s *Service) mergeWebAPIMetadata(ctx context.Context, csvRatings []seventeenlands.CardRating, setCode, format string) ([]seventeenlands.CardRating, error) {
	// Fetch web API data for card identifiers
	webRatings, err := s.webScraper.FetchCardRatings(ctx, setCode, format)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch web API metadata: %w", err)
	}

	// Build name-to-metadata lookup map from web API data
	webMetadataByName := make(map[string]*seventeenlands.CardRating)
	for i := range webRatings {
		webMetadataByName[webRatings[i].Name] = &webRatings[i]
	}

	// Merge metadata into CSV ratings
	mergedCount := 0
	for i := range csvRatings {
		if webData, ok := webMetadataByName[csvRatings[i].Name]; ok {
			// Copy identifiers from web API
			csvRatings[i].MTGAID = webData.MTGAID // Arena ID
			csvRatings[i].URL = webData.URL       // Scryfall image URL (contains Scryfall ID)
			csvRatings[i].URLBack = webData.URLBack
			csvRatings[i].Color = webData.Color   // Color from web API is more reliable
			csvRatings[i].Rarity = webData.Rarity // Rarity from web API is more reliable
			mergedCount++
		}
	}

	log.Printf("[DatasetService] Merged metadata for %d/%d cards", mergedCount, len(csvRatings))
	return csvRatings, nil
}

// tryS3Dataset attempts to download and parse a dataset from S3.
func (s *Service) tryS3Dataset(ctx context.Context, setCode, format string) ([]seventeenlands.CardRating, error) {
	// Download dataset (or use cached)
	csvPath, err := s.downloader.DownloadDataset(ctx, setCode, format)
	if err != nil {
		return nil, fmt.Errorf("failed to download dataset: %w", err)
	}

	// Parse CSV to calculate card ratings
	// Note: Create new parser for each parse to avoid state conflicts
	parser := NewCSVParser()
	ratings, err := parser.ParseCSV(csvPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV: %w", err)
	}

	return ratings, nil
}

// CheckDatasetAvailability checks if a dataset is available in S3 without downloading it.
func (s *Service) CheckDatasetAvailability(ctx context.Context, setCode, format string) (bool, error) {
	return s.webScraper.IsDatasetAvailable(ctx, setCode, format), nil
}

// ClearCache removes all cached datasets.
func (s *Service) ClearCache() error {
	return s.downloader.ClearCache()
}

// GetDataSource returns the data source used for a given set/format.
// Returns "s3" if S3 dataset is available, "web_api" otherwise.
func (s *Service) GetDataSource(ctx context.Context, setCode, format string) string {
	available := s.webScraper.IsDatasetAvailable(ctx, setCode, format)
	if available {
		return "s3"
	}
	return "web_api"
}
