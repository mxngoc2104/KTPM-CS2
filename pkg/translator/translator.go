package translator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"imageprocessor/pkg/cache"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"
)

var (
	// ErrTranslationFailed is returned when translation fails
	ErrTranslationFailed = errors.New("translation failed")

	// Cache instance for translation results
	translationCache *cache.TranslationCache
)

// TranslationConfig holds configuration for the translator
type TranslationConfig struct {
	CacheTTL     time.Duration
	Timeout      time.Duration
	RetryCount   int
	RetryBackoff time.Duration
}

// DefaultTranslationConfig returns a default configuration for the translator
func DefaultTranslationConfig() TranslationConfig {
	return TranslationConfig{
		CacheTTL:     7 * 24 * time.Hour, // Cache translations for 7 days
		Timeout:      10 * time.Second,   // 10 second timeout
		RetryCount:   3,                  // Retry 3 times
		RetryBackoff: 1 * time.Second,    // 1 second backoff between retries
	}
}

// InitCache initializes the translation cache
func InitCache(ttl time.Duration) {
	translationCache = cache.NewTranslationCache(ttl)
}

// Translate text from English to Vietnamese
func Translate(text string) (string, error) {
	// Use default config
	return TranslateWithConfig(text, DefaultTranslationConfig())
}

// TranslateWithConfig translates text with a custom configuration
func TranslateWithConfig(text string, config TranslationConfig) (string, error) {
	// Initialize cache if not already initialized
	if translationCache == nil {
		InitCache(config.CacheTTL)
	}

	// Generate hash for text
	textHash := cache.GetTextHash(text)

	// Try to get from cache
	if cachedText, found := translationCache.Get(textHash); found {
		log.Printf("Cache hit for translation")
		return cachedText, nil
	}

	log.Printf("Cache miss for translation, translating...")

	// Apply retry logic for translation
	var translatedText string
	var err error

	for i := 0; i <= config.RetryCount; i++ {
		if i > 0 {
			log.Printf("Retry %d/%d after %v", i, config.RetryCount, config.RetryBackoff)
			time.Sleep(config.RetryBackoff)
		}

		translatedText, err = googleTranslateWithTimeout(text, config.Timeout)
		if err == nil {
			break
		}

		log.Printf("Translation attempt %d failed: %v", i+1, err)
	}

	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTranslationFailed, err)
	}

	// Store in cache
	translationCache.Set(textHash, translatedText)

	return translatedText, nil
}

// googleTranslateWithTimeout uses the unofficial Google Translate API with timeout
func googleTranslateWithTimeout(text string, timeout time.Duration) (string, error) {
	// Google Translate URL
	baseURL := "https://translate.googleapis.com/translate_a/single"

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: timeout,
	}

	// Build query parameters
	params := url.Values{}
	params.Add("client", "gtx")
	params.Add("sl", "en") // Source language
	params.Add("tl", "vi") // Target language
	params.Add("dt", "t")  // Return translated text
	params.Add("q", text)  // Text to translate

	fullURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	// Create request with context for better timeout handling
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return "", err
	}

	// Set user agent to mimic a browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	// Make request
	log.Println("Trying Google Translate...")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Google Translate request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Google Translate returned non-OK status: %d", resp.StatusCode)
	}

	// Read response
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Parse the response (it's a complex nested JSON structure)
	var result []interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	// Extract the translated text from the response
	// The structure is typically: [[[translated_text, original_text, ...], ...], ...]
	translatedText := ""
	if len(result) > 0 {
		if translations, ok := result[0].([]interface{}); ok {
			for _, translation := range translations {
				if translationParts, ok := translation.([]interface{}); ok && len(translationParts) > 0 {
					if part, ok := translationParts[0].(string); ok {
						translatedText += part
					}
				}
			}
		}
	}

	if translatedText == "" {
		return "", fmt.Errorf("could not extract translation from response")
	}

	return translatedText, nil
}

// GetCacheSize returns the number of items in the translation cache
func GetCacheSize() int {
	if translationCache == nil {
		return 0
	}
	return translationCache.Size()
}

// ClearCache clears the translation cache
func ClearCache() {
	if translationCache != nil {
		translationCache.Clear()
	}
}
