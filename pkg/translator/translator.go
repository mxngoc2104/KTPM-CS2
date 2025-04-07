package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"
)

// Translate text from English to Vietnamese
func Translate(text string) (string, error) {
	// First try Google Translate (unofficial API)
	translatedText, err := googleTranslate(text)
	if err == nil {
		fmt.Println("Translation successful using Google Translate")
		return translatedText, nil
	}
	
	fmt.Printf("Google Translate failed: %v. Trying alternative services...\n", err)

	// If Google Translate fails, return error
	return "", fmt.Errorf("Translation failed")
}

// googleTranslate uses the unofficial Google Translate API
func googleTranslate(text string) (string, error) {
	// Google Translate URL
	baseURL := "https://translate.googleapis.com/translate_a/single"
	
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	
	// Build query parameters
	params := url.Values{}
	params.Add("client", "gtx")
	params.Add("sl", "en")     // Source language
	params.Add("tl", "vi")     // Target language
	params.Add("dt", "t")      // Return translated text
	params.Add("q", text)      // Text to translate
	
	fullURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())
	
	// Create request with context for better timeout handling
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return "", err
	}
	
	// Set user agent to mimic a browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	
	// Make request
	fmt.Println("Trying Google Translate...")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Google Translate request failed: %v", err)
	}
	defer resp.Body.Close()
	
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