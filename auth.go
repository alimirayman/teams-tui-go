package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func accessTokenIncludesScopes(accessToken, requestedScopes string) (bool, bool) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return false, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false, false
	}
	var claims struct {
		Scopes string `json:"scp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Scopes == "" {
		return false, false
	}

	granted := make(map[string]bool)
	for _, scope := range strings.Fields(claims.Scopes) {
		granted[scope] = true
	}
	for _, scope := range strings.Fields(requestedScopes) {
		if scope == "offline_access" {
			continue
		}
		if !granted[scope] {
			return false, true
		}
	}
	return true, true
}

func validateEnabledFeatureScopes(accessToken string) error {
	if includes, known := accessTokenIncludesScopes(accessToken, BuildScopes()); known && !includes {
		return fmt.Errorf("token is missing permissions required by enabled features; sign in again")
	}
	return nil
}

// DeviceCodeResponse is the response from the device code endpoint.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

// TokenResponse holds the OAuth2 token data.
type TokenResponse struct {
	AccessToken  string  `json:"access_token"`
	TokenType    string  `json:"token_type"`
	ExpiresIn    int     `json:"expires_in"`
	RefreshToken *string `json:"refresh_token,omitempty"`
	ExpiresAt    int64   `json:"expires_at"`
}

var activeSessionToken struct {
	sync.RWMutex
	accessToken string
	expiresAt   int64
}

func rememberSessionToken(token *TokenResponse) {
	if token == nil || token.AccessToken == "" {
		return
	}
	activeSessionToken.Lock()
	activeSessionToken.accessToken = token.AccessToken
	activeSessionToken.expiresAt = token.ExpiresAt
	activeSessionToken.Unlock()
}

func validSessionAccessToken() (string, bool) {
	activeSessionToken.RLock()
	defer activeSessionToken.RUnlock()
	const bufferSeconds = 5 * 60
	if activeSessionToken.accessToken == "" || time.Now().Unix()+bufferSeconds >= activeSessionToken.expiresAt {
		return "", false
	}
	return activeSessionToken.accessToken, true
}

func clearSessionToken() {
	activeSessionToken.Lock()
	activeSessionToken.accessToken = ""
	activeSessionToken.expiresAt = 0
	activeSessionToken.Unlock()
}

// tokenErrorResponse is returned when token polling is not yet complete or fails.
type tokenErrorResponse struct {
	Error string `json:"error"`
}

func oauthEndpoint(action string) string {
	tenantID := strings.TrimSpace(ResolveTenantID())
	if tenantID == "" {
		tenantID = defaultTenantID
	}
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/%s", url.PathEscape(tenantID), action)
}

// tokenFilePath returns the path to the cached token file.
func tokenFilePath() (string, error) {
	dir, err := GetCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token.json"), nil
}

// saveToken persists the token to disk.
func saveToken(token *TokenResponse) error {
	path, err := tokenFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ") // #nosec G117 -- OAuth refresh token persistence is required and written with mode 0600.
	if err != nil {
		return fmt.Errorf("could not marshal token: %w", err)
	}
	return writePrivateFile(path, data)
}

// loadToken reads the token from disk.
// Returns nil, nil if the file does not exist.
func loadToken() (*TokenResponse, error) {
	path, err := tokenFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is generated inside the private application cache directory.
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read token file: %w", err)
	}
	var token TokenResponse
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("could not parse token file: %w", err)
	}
	// Back-fill ExpiresAt if it was not stored.
	if token.ExpiresAt == 0 && token.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Unix() + int64(token.ExpiresIn)
	}
	return &token, nil
}

// StartDeviceFlow initiates the OAuth2 device code flow.
func StartDeviceFlow(clientID, scopes string) (*DeviceCodeResponse, error) {
	form := url.Values{
		"client_id": {clientID},
		"scope":     {scopes},
	}
	resp, err := http.PostForm(oauthEndpoint("devicecode"), form)
	if err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code endpoint returned %d: %s", resp.StatusCode, body)
	}

	var dc DeviceCodeResponse
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, fmt.Errorf("could not parse device code response: %w", err)
	}
	return &dc, nil
}

// PollForToken polls the token endpoint until the user authenticates or the code expires.
func PollForToken(clientID, deviceCode string, interval int) (*TokenResponse, error) {
	if interval <= 0 {
		interval = 5
	}
	for {
		time.Sleep(time.Duration(interval) * time.Second)

		form := url.Values{
			"client_id":   {clientID},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
		}
		resp, err := http.PostForm(oauthEndpoint("token"), form)
		if err != nil {
			return nil, fmt.Errorf("token poll request failed: %w", err)
		}

		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("could not read token response: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("could not close token response: %w", closeErr)
		}

		if resp.StatusCode == http.StatusOK {
			var token TokenResponse
			if err := json.Unmarshal(body, &token); err != nil {
				return nil, fmt.Errorf("could not parse token response: %w", err)
			}
			token.ExpiresAt = time.Now().Unix() + int64(token.ExpiresIn)
			if err := saveToken(&token); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not save token: %v\n", err)
			}
			return &token, nil
		}

		// Parse error response.
		var errResp tokenErrorResponse
		_ = json.Unmarshal(body, &errResp)
		switch errResp.Error {
		case "authorization_pending":
			// User hasn't completed auth yet — keep polling.
			continue
		case "authorization_declined":
			return nil, fmt.Errorf("authorization was declined by the user")
		case "expired_token":
			return nil, fmt.Errorf("device code expired; please restart and try again")
		default:
			return nil, fmt.Errorf("unexpected token error (%s): %s", errResp.Error, body)
		}
	}
}

// RefreshAccessToken uses the refresh token to obtain a new access token.
func RefreshAccessToken(clientID, refreshToken, scopes string) (*TokenResponse, error) {
	form := url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {scopes},
	}
	resp, err := http.PostForm(oauthEndpoint("token"), form)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh endpoint returned %d: %s", resp.StatusCode, body)
	}

	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("could not parse refresh response: %w", err)
	}
	token.ExpiresAt = time.Now().Unix() + int64(token.ExpiresIn)
	// Preserve the old refresh token if none was returned.
	if token.RefreshToken == nil {
		token.RefreshToken = &refreshToken
	}
	if err := saveToken(&token); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save refreshed token: %v\n", err)
	}
	return &token, nil
}

// GetValidTokenSilent returns a valid access token from the cache, refreshing if necessary.
// Returns an error if the token is expired and cannot be refreshed.
func GetValidTokenSilent(clientID string) (string, error) {
	token, err := loadToken()
	if err != nil {
		if accessToken, ok := validSessionAccessToken(); ok {
			if scopeErr := validateEnabledFeatureScopes(accessToken); scopeErr != nil {
				return "", scopeErr
			}
			return accessToken, nil
		}
		return "", fmt.Errorf("could not load token: %w", err)
	}
	if token == nil {
		if accessToken, ok := validSessionAccessToken(); ok {
			if scopeErr := validateEnabledFeatureScopes(accessToken); scopeErr != nil {
				return "", scopeErr
			}
			return accessToken, nil
		}
		return "", fmt.Errorf("no cached token found")
	}

	// Check if token is still valid (with a 5-minute buffer).
	const bufferSeconds = 5 * 60
	if time.Now().Unix()+bufferSeconds < token.ExpiresAt {
		if scopeErr := validateEnabledFeatureScopes(token.AccessToken); scopeErr != nil {
			return "", scopeErr
		}
		rememberSessionToken(token)
		return token.AccessToken, nil
	}

	// Token expired — try to refresh.
	if token.RefreshToken == nil || *token.RefreshToken == "" {
		return "", fmt.Errorf("token expired and no refresh token available")
	}
	newToken, err := RefreshAccessToken(clientID, *token.RefreshToken, BuildScopes())
	if err != nil {
		return "", fmt.Errorf("token refresh failed: %w", err)
	}
	if scopeErr := validateEnabledFeatureScopes(newToken.AccessToken); scopeErr != nil {
		return "", scopeErr
	}
	rememberSessionToken(newToken)
	return newToken.AccessToken, nil
}

// GetAccessToken returns a valid access token, running the full device code flow if needed.
func GetAccessToken(clientID string) (string, error) {
	// Try silent first.
	if token, err := GetValidTokenSilent(clientID); err == nil {
		return token, nil
	}

	// Full device code flow.
	dc, err := StartDeviceFlow(clientID, BuildScopes())
	if err != nil {
		return "", fmt.Errorf("could not start device flow: %w", err)
	}

	// Print the user-facing instructions.
	fmt.Println(dc.Message)
	fmt.Printf("\nOpen: %s\nCode: %s\n\n", dc.VerificationURI, dc.UserCode)
	fmt.Println("Waiting for authentication...")

	token, err := PollForToken(clientID, dc.DeviceCode, dc.Interval)
	if err != nil {
		return "", fmt.Errorf("authentication failed: %w", err)
	}
	if includes, known := accessTokenIncludesScopes(token.AccessToken, BuildScopes()); known && !includes {
		return "", fmt.Errorf("authentication succeeded but required permissions were not granted")
	}

	// Mask the token in output.
	masked := strings.Repeat("*", 8)
	_ = masked
	rememberSessionToken(token)
	return token.AccessToken, nil
}
