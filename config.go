package main

import (
	"encoding/json"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

// ---------------------------------------------------------------------------
// Favourites persistence
// ---------------------------------------------------------------------------

// LoadFavourites reads the list of favourite chat IDs from favourites.json.
// Returns an empty map if the file does not exist or cannot be parsed.
func LoadFavourites() map[string]bool {
	dir, err := GetAppDir()
	if err != nil {
		return make(map[string]bool)
	}
	data, err := os.ReadFile(filepath.Join(dir, "favourites.json")) // #nosec G304 -- dir is the private OS config directory.
	if err != nil {
		return make(map[string]bool)
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return make(map[string]bool)
	}
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// SaveFavourites writes the current favourite chat IDs to favourites.json.
func SaveFavourites(favs map[string]bool) error {
	dir, err := GetAppDir()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(favs))
	for id := range favs {
		ids = append(ids, id)
	}
	// Sort for deterministic output.
	sort.Strings(ids)
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal favourites: %w", err)
	}
	return writePrivateFile(filepath.Join(dir, "favourites.json"), data)
}

// LoadChannelFavourites reads the locally favourited channel IDs.
func LoadChannelFavourites() map[string]bool {
	dir, err := GetAppDir()
	if err != nil {
		return make(map[string]bool)
	}
	data, err := os.ReadFile(filepath.Join(dir, "channel_favourites.json")) // #nosec G304 -- dir is the private OS config directory.
	if err != nil {
		return make(map[string]bool)
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return make(map[string]bool)
	}
	favourites := make(map[string]bool, len(ids))
	for _, id := range ids {
		favourites[id] = true
	}
	return favourites
}

// SaveChannelFavourites writes the locally favourited channel IDs.
func SaveChannelFavourites(favourites map[string]bool) error {
	dir, err := GetAppDir()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(favourites))
	for id := range favourites {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal channel favourites: %w", err)
	}
	return writePrivateFile(filepath.Join(dir, "channel_favourites.json"), data)
}

// LoadUnhiddenChannels reads the list of unhidden channel IDs from unhidden_channels.json.
// Returns an empty map if the file does not exist or cannot be parsed.
func LoadUnhiddenChannels() map[string]bool {
	dir, err := GetAppDir()
	if err != nil {
		return make(map[string]bool)
	}
	data, err := os.ReadFile(filepath.Join(dir, "unhidden_channels.json")) // #nosec G304 -- dir is the private OS config directory.
	if err != nil {
		return make(map[string]bool)
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return make(map[string]bool)
	}
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// SaveUnhiddenChannels writes the current unhidden channel IDs to unhidden_channels.json.
func SaveUnhiddenChannels(unhidden map[string]bool) error {
	dir, err := GetAppDir()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(unhidden))
	for id := range unhidden {
		ids = append(ids, id)
	}
	// Sort for deterministic output.
	sort.Strings(ids)
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal unhidden channels: %w", err)
	}
	return writePrivateFile(filepath.Join(dir, "unhidden_channels.json"), data)
}

// FilepickerSettings holds the filepicker sorting and directory persistence.
type FilepickerSettings struct {
	SortBy           string `json:"sort_by"`
	SortOrder        string `json:"sort_order"`
	CurrentDirectory string `json:"current_directory,omitempty"`
}

// LoadFilepickerSettings reads the filepicker sorting settings from filepicker_settings.json.
// Returns default values if the file does not exist or cannot be parsed.
func LoadFilepickerSettings() (string, string, string) {
	dir, err := GetAppDir()
	if err != nil {
		return "Name", "asc", ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "filepicker_settings.json")) // #nosec G304 -- dir is the private OS config directory.
	if err != nil {
		return "Name", "asc", ""
	}
	var settings FilepickerSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return "Name", "asc", ""
	}
	if settings.SortBy == "" {
		settings.SortBy = "Name"
	}
	if settings.SortOrder == "" {
		settings.SortOrder = "asc"
	}
	return settings.SortBy, settings.SortOrder, settings.CurrentDirectory
}

// SaveFilepickerSettings writes the current filepicker settings to filepicker_settings.json.
func SaveFilepickerSettings(sortBy string, sortOrder string, currentDirectory string) error {
	dir, err := GetAppDir()
	if err != nil {
		return err
	}
	settings := FilepickerSettings{
		SortBy:           sortBy,
		SortOrder:        sortOrder,
		CurrentDirectory: currentDirectory,
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal filepicker settings: %w", err)
	}
	return writePrivateFile(filepath.Join(dir, "filepicker_settings.json"), data)
}

const (
	appDirName       = "ms-teams-tui"
	legacyAppDirName = "teams-tui-go"
)

// defaultClientID is the Microsoft Teams client ID fallback.
const defaultClientID = "d3590ed6-52b3-4102-aeff-aad2292ab01c"

// defaultTenantID keeps upstream behavior unless a tenant is configured.
const defaultTenantID = "common"

// Config holds persistent application settings.
type Config struct {
	ClientID                *string           `json:"client_id,omitempty"`
	TenantID                *string           `json:"tenant_id,omitempty"`
	NotificationMode        *NotificationMode `json:"notification_mode,omitempty"`
	NotificationShowPreview *bool             `json:"notification_show_preview,omitempty"`
	NotificationPreviewLen  *int              `json:"notification_preview_len,omitempty"`
	MessageLimit            *int              `json:"message_limit,omitempty"`
	SearchContextLimit      *int              `json:"search_context_limit,omitempty"`
	ChatLimit               *int              `json:"chat_limit,omitempty"`
	ChatIconTheme           *string           `json:"chat_icon_theme,omitempty"`
	CustomChatIcons         map[string]string `json:"custom_chat_icons,omitempty"`

	// Optional feature flags — each defaults to false (disabled).
	// When enabled, the corresponding Graph API permission must be granted
	// in the Azure app registration and the cached token refreshed.
	FilePreviewEnabled     *bool   `json:"file_preview_enabled,omitempty"`     // requires Files.Read.All
	FilePreviewInTerminal  *bool   `json:"file_preview_in_terminal,omitempty"` // show image in terminal if file_preview_enabled is true
	FileUploadEnabled      *bool   `json:"file_upload_enabled,omitempty"`      // requires Files.ReadWrite
	PresenceEnabled        *bool   `json:"presence_enabled,omitempty"`         // requires Presence.Read.All
	UserProfileEnabled     *bool   `json:"user_profile_enabled,omitempty"`     // requires User.ReadBasic.All
	UserProfileExtended    *bool   `json:"user_profile_extended,omitempty"`    // requires User.Read.All (admin consent)
	TeamsChannelsEnabled   *bool   `json:"teams_channels_enabled,omitempty"`   // requires Team.ReadBasic.All + Channel.ReadBasic.All + ChannelMessage.Read.All + ChannelMessage.Send + ChannelMessage.ReadWrite
	ChannelMentionsEnabled *bool   `json:"channel_mentions_enabled,omitempty"` // requires TeamMember.Read.All to load members for autocomplete in channels
	ChannelMsgRefreshMin   *int    `json:"channel_msg_refresh_min,omitempty"`
	SqliteEnabled          *bool   `json:"sqlite_enabled,omitempty"`
	ExternalEditor         *string `json:"external_editor,omitempty"`
	BrowserCommand         *string `json:"browser_command,omitempty"`
	YoutrackCommand        *string `json:"youtrack_command,omitempty"`
	GitlabCommand          *string `json:"gitlab_command,omitempty"`
}

func resolvePrivateDataDir(base string) (string, error) {
	target := filepath.Join(base, appDirName)
	info, err := os.Lstat(target)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("application data path is not a real directory: %s", target)
		}
		if err := securePrivateDirectory(target); err != nil {
			return "", err
		}
		return target, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect application data directory: %w", err)
	}

	legacy := filepath.Join(base, legacyAppDirName)
	if legacyInfo, legacyErr := os.Lstat(legacy); legacyErr == nil {
		if legacyInfo.Mode()&os.ModeSymlink != 0 || !legacyInfo.IsDir() {
			return "", fmt.Errorf("legacy application data path is not a real directory: %s", legacy)
		}
		if err := securePrivateDataTree(legacy); err != nil {
			return "", err
		}
		if err := os.Rename(legacy, target); err != nil {
			return "", fmt.Errorf("migrate %s to %s: %w", legacy, target, err)
		}
	} else if !os.IsNotExist(legacyErr) {
		return "", fmt.Errorf("inspect legacy application data directory: %w", legacyErr)
	}

	if err := os.MkdirAll(target, 0o700); err != nil {
		return "", fmt.Errorf("create application data directory: %w", err)
	}
	if err := securePrivateDirectory(target); err != nil {
		return "", err
	}
	return target, nil
}

func securePrivateDataTree(rootPath string) error {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return fmt.Errorf("open application data root: %w", err)
	}
	defer root.Close()
	return securePrivateRootEntry(root, ".")
}

func securePrivateRootEntry(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("inspect application data entry %s: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("application data tree contains a symlink: %s", name)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return fmt.Errorf("application data tree contains a non-regular file: %s", name)
	}

	file, err := root.Open(name)
	if err != nil {
		return fmt.Errorf("open application data entry %s: %w", name, err)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("inspect opened application data entry %s: %w", name, err)
	}
	if !openedInfo.IsDir() && !openedInfo.Mode().IsRegular() {
		_ = file.Close()
		return fmt.Errorf("opened application data entry is not regular: %s", name)
	}
	mode := os.FileMode(0o600)
	if openedInfo.IsDir() {
		mode = 0o700
	}
	if err := file.Chmod(mode); err != nil { // #nosec G302 -- private app data has intentionally restrictive permissions.
		_ = file.Close()
		return fmt.Errorf("secure application data entry %s: %w", name, err)
	}

	if !openedInfo.IsDir() {
		if err := file.Close(); err != nil {
			return fmt.Errorf("close application data entry %s: %w", name, err)
		}
		return nil
	}
	entries, err := file.ReadDir(-1)
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("read application data directory %s: %w", name, err)
	}
	if closeErr != nil {
		return fmt.Errorf("close application data directory %s: %w", name, closeErr)
	}
	for _, entry := range entries {
		if err := securePrivateRootEntry(root, pathpkg.Join(name, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func securePrivateDirectory(path string) error {
	if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- private app data has intentionally restrictive permissions.
		return fmt.Errorf("secure application data directory: %w", err)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("inspect application data directory: %w", err)
	}
	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("application data directory contains a symlink: %s", entryPath)
		}
		mode := os.FileMode(0o600)
		if entry.IsDir() {
			mode = 0o700
		} else if !entry.Type().IsRegular() {
			return fmt.Errorf("application data directory contains a non-regular file: %s", entryPath)
		}
		if err := os.Chmod(entryPath, mode); err != nil { // #nosec G302 -- private app data has intentionally restrictive permissions.
			return fmt.Errorf("secure application data path %s: %w", entryPath, err)
		}
	}
	return nil
}

func writePrivateFile(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("private data path is not a regular file: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect private data path: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- callers provide an app-owned path.
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil { // #nosec G302 -- private app data has intentionally restrictive permissions.
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return nil
}

// GetAppDir returns the OS config directory for ms-teams-tui. Existing data
// from teams-tui-go is migrated on first use.
func GetAppDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not determine config dir: %w", err)
	}
	return resolvePrivateDataDir(base)
}

// GetCacheDir returns the OS cache directory for ms-teams-tui. Existing data
// from teams-tui-go is migrated on first use.
func GetCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("could not determine cache dir: %w", err)
	}
	return resolvePrivateDataDir(base)
}

// LoadConfig reads config.json from the app dir.
// Returns nil if the file does not exist or cannot be parsed.
func LoadConfig() *Config {
	dir, err := GetAppDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json")) // #nosec G304 -- dir is the private OS config directory.
	if err != nil {
		return nil
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// InitConfig loads the configuration, populates any missing options with their
// default values, and saves the updated configuration back to config.json.
func InitConfig() {
	dir, err := GetAppDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "config.json")

	var cfg Config
	exists := true
	data, err := os.ReadFile(path) // #nosec G304 -- path is generated inside the private OS config directory.
	if err != nil {
		exists = false
	} else {
		if err := json.Unmarshal(data, &cfg); err != nil {
			exists = false
		}
	}

	modified := false

	if cfg.ClientID == nil {
		id := defaultClientID
		cfg.ClientID = &id
		modified = true
	}
	if cfg.TenantID == nil {
		id := defaultTenantID
		cfg.TenantID = &id
		modified = true
	}
	if cfg.NotificationMode == nil {
		mode := NotificationNone
		cfg.NotificationMode = &mode
		modified = true
	}
	if cfg.NotificationShowPreview == nil {
		show := false
		cfg.NotificationShowPreview = &show
		modified = true
	}
	if cfg.NotificationPreviewLen == nil {
		length := 50
		cfg.NotificationPreviewLen = &length
		modified = true
	}
	if cfg.MessageLimit == nil {
		limit := 50
		cfg.MessageLimit = &limit
		modified = true
	}
	if cfg.SearchContextLimit == nil {
		limit := 3
		cfg.SearchContextLimit = &limit
		modified = true
	}
	if cfg.ChatLimit == nil {
		limit := 50
		cfg.ChatLimit = &limit
		modified = true
	}
	if cfg.ChatIconTheme == nil {
		theme := "unicode"
		cfg.ChatIconTheme = &theme
		modified = true
	}
	// Feature flags default to false (disabled) — written so users can see them in config.json.
	if cfg.FilePreviewEnabled == nil {
		v := false
		cfg.FilePreviewEnabled = &v
		modified = true
	}
	if cfg.FilePreviewInTerminal == nil {
		v := false
		cfg.FilePreviewInTerminal = &v
		modified = true
	}
	if cfg.FileUploadEnabled == nil {
		v := false
		cfg.FileUploadEnabled = &v
		modified = true
	}
	if cfg.PresenceEnabled == nil {
		v := false
		cfg.PresenceEnabled = &v
		modified = true
	}
	if cfg.UserProfileEnabled == nil {
		v := false
		cfg.UserProfileEnabled = &v
		modified = true
	}
	if cfg.UserProfileExtended == nil {
		v := false
		cfg.UserProfileExtended = &v
		modified = true
	}
	if cfg.TeamsChannelsEnabled == nil {
		v := false
		cfg.TeamsChannelsEnabled = &v
		modified = true
	}
	if cfg.ChannelMentionsEnabled == nil {
		v := false
		cfg.ChannelMentionsEnabled = &v
		modified = true
	}
	if cfg.ChannelMsgRefreshMin == nil {
		val := 2
		cfg.ChannelMsgRefreshMin = &val
		modified = true
	}
	if cfg.SqliteEnabled == nil {
		v := false
		cfg.SqliteEnabled = &v
		modified = true
	}
	if cfg.ExternalEditor == nil {
		editor := ""
		cfg.ExternalEditor = &editor
		modified = true
	}
	if cfg.BrowserCommand == nil {
		cmd := "xdg-open"
		cfg.BrowserCommand = &cmd
		modified = true
	}

	if !exists || modified {
		_ = SaveConfig(&cfg)
	}
}

// SaveConfig writes config.json (pretty-printed) to the app dir.
func SaveConfig(cfg *Config) error {
	dir, err := GetAppDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal config: %w", err)
	}
	return writePrivateFile(filepath.Join(dir, "config.json"), data)
}

// ResolveClientID returns the client ID using the precedence:
//  1. CLIENT_ID environment variable (loads .env first)
//  2. config.json → client_id
//  3. Built-in default
func ResolveClientID() string {
	// Load .env file if present; ignore errors (file may not exist).
	_ = godotenv.Load()

	if id := os.Getenv("CLIENT_ID"); id != "" {
		return id
	}
	cfg := LoadConfig()
	if cfg != nil && cfg.ClientID != nil && *cfg.ClientID != "" {
		return *cfg.ClientID
	}
	return defaultClientID
}

// ResolveTenantID returns the authority tenant using the precedence:
//  1. TENANT_ID environment variable (loads .env first)
//  2. config.json → tenant_id
//  3. Built-in default ("common")
func ResolveTenantID() string {
	// Load .env file if present; ignore errors (file may not exist).
	_ = godotenv.Load()

	if id := strings.TrimSpace(os.Getenv("TENANT_ID")); id != "" {
		return id
	}
	cfg := LoadConfig()
	if cfg != nil && cfg.TenantID != nil {
		if id := strings.TrimSpace(*cfg.TenantID); id != "" {
			return id
		}
	}
	return defaultTenantID
}

// ResolveMessageLimit returns the number of messages to fetch, using precedence:
//  1. config.json -> message_limit
//  2. Default (50)
//     Note: Capped at 200 to prevent excessive API requests.
func ResolveMessageLimit() int {
	cfg := LoadConfig()
	if cfg != nil && cfg.MessageLimit != nil && *cfg.MessageLimit > 0 {
		limit := *cfg.MessageLimit
		if limit > 200 {
			return 200
		}
		return limit
	}
	return 50
}

// ResolveSearchContextLimit returns the number of context messages to show before/after matching message:
//  1. config.json -> search_context_limit
//  2. Default (3)
func ResolveSearchContextLimit() int {
	cfg := LoadConfig()
	if cfg != nil && cfg.SearchContextLimit != nil && *cfg.SearchContextLimit >= 0 {
		return *cfg.SearchContextLimit
	}
	return 3
}

// ResolveChatLimit returns the number of chats to fetch, using precedence:
//  1. config.json -> chat_limit
//  2. Default (50)
//     Note: Capped at 100 to prevent API throttling during concurrent member fetching.
func ResolveChatLimit() int {
	cfg := LoadConfig()
	if cfg != nil && cfg.ChatLimit != nil && *cfg.ChatLimit > 0 {
		limit := *cfg.ChatLimit
		if limit > 100 {
			return 100
		}
		return limit
	}
	return 50
}

// ResolveChannelMsgRefreshMin returns the channel messages refresh interval in minutes.
// Precedence:
//  1. config.json -> channel_msg_refresh_min
//  2. Default (2)
func ResolveChannelMsgRefreshMin() int {
	cfg := LoadConfig()
	if cfg != nil && cfg.ChannelMsgRefreshMin != nil && *cfg.ChannelMsgRefreshMin > 0 {
		return *cfg.ChannelMsgRefreshMin
	}
	return 2
}

// ---------------------------------------------------------------------------
// Feature flag resolvers
// ---------------------------------------------------------------------------

// ResolveFeatureFilePreview returns true when file preview/download is enabled.
func ResolveFeatureFilePreview() bool {
	cfg := LoadConfig()
	return cfg != nil && cfg.FilePreviewEnabled != nil && *cfg.FilePreviewEnabled
}

// ResolveFeatureFilePreviewInTerminal returns true when file preview in terminal is enabled.
func ResolveFeatureFilePreviewInTerminal() bool {
	cfg := LoadConfig()
	return cfg != nil && cfg.FilePreviewInTerminal != nil && *cfg.FilePreviewInTerminal
}

// ResolveFeatureFileUpload returns true when file upload to OneDrive is enabled.
func ResolveFeatureFileUpload() bool {
	cfg := LoadConfig()
	return cfg != nil && cfg.FileUploadEnabled != nil && *cfg.FileUploadEnabled
}

// ResolveFeaturePresence returns true when user presence status is enabled.
func ResolveFeaturePresence() bool {
	cfg := LoadConfig()
	return cfg != nil && cfg.PresenceEnabled != nil && *cfg.PresenceEnabled
}

// ResolveFeatureUserProfile returns true when user profile info is enabled.
func ResolveFeatureUserProfile() bool {
	cfg := LoadConfig()
	return cfg != nil && cfg.UserProfileEnabled != nil && *cfg.UserProfileEnabled
}

// ResolveFeatureUserProfileExtended returns true when extended profile (job title etc.) is enabled.
// Requires User.Read.All which needs admin consent.
func ResolveFeatureUserProfileExtended() bool {
	cfg := LoadConfig()
	return cfg != nil && cfg.UserProfileExtended != nil && *cfg.UserProfileExtended
}

// ResolveFeatureTeamsChannels returns true when Teams channels browsing is enabled.
func ResolveFeatureTeamsChannels() bool {
	cfg := LoadConfig()
	return cfg != nil && cfg.TeamsChannelsEnabled != nil && *cfg.TeamsChannelsEnabled
}

// ResolveFeatureChannelMentions returns true when channel mentions (and member autocomplete) are enabled.
func ResolveFeatureChannelMentions() bool {
	cfg := LoadConfig()
	return cfg != nil && cfg.ChannelMentionsEnabled != nil && *cfg.ChannelMentionsEnabled
}

// ResolveFeatureSqlite returns true when SQLite caching is enabled.
func ResolveFeatureSqlite() bool {
	cfg := LoadConfig()
	return cfg != nil && cfg.SqliteEnabled != nil && *cfg.SqliteEnabled
}

// BuildScopes constructs the OAuth2 scope string from config feature flags.
// Basic scopes are always included; additional scopes are appended for enabled features.
func BuildScopes() string {
	base := "User.Read Chat.ReadWrite offline_access"
	if ResolveFeatureFilePreview() {
		// Teams reference attachments often live in another participant's
		// OneDrive. Files.Read.All is read-only and covers every file the signed-in
		// user can already access; Files.Read only covers that user's own files.
		base += " Files.Read.All"
	}
	if ResolveFeatureFileUpload() {
		base += " Files.ReadWrite"
	}
	if ResolveFeaturePresence() {
		base += " Presence.Read.All"
	}
	if ResolveFeatureUserProfile() {
		if ResolveFeatureUserProfileExtended() {
			base += " User.Read.All"
		} else {
			base += " User.ReadBasic.All"
		}
	}
	if ResolveFeatureTeamsChannels() {
		base += " Team.ReadBasic.All Channel.ReadBasic.All ChannelMessage.Read.All ChannelMessage.Send ChannelMessage.ReadWrite"
	}
	if ResolveFeatureChannelMentions() {
		base += " TeamMember.Read.All"
	}
	return base
}

// ResolveExternalEditor returns the command for the external editor, using precedence:
//  1. config.json -> external_editor
//  2. EDITOR environment variable
//  3. VISUAL environment variable
//  4. Default ("vim")
func ResolveExternalEditor() string {
	cfg := LoadConfig()
	if cfg != nil && cfg.ExternalEditor != nil && *cfg.ExternalEditor != "" {
		return *cfg.ExternalEditor
	}
	if ed := os.Getenv("EDITOR"); ed != "" {
		return ed
	}
	if vis := os.Getenv("VISUAL"); vis != "" {
		return vis
	}
	return "vim"
}

// ResolveBrowserCommand returns the browser command, using precedence:
//  1. config.json -> browser_command
//  2. Default ("xdg-open")
func ResolveBrowserCommand() string {
	cfg := LoadConfig()
	if cfg != nil && cfg.BrowserCommand != nil && *cfg.BrowserCommand != "" {
		return *cfg.BrowserCommand
	}
	return "xdg-open"
}

// ResolveYoutrackCommand returns the youtrack command, using precedence:
//  1. config.json -> youtrack_command
//  2. Default ("") - meaning fall back to browser command if not set
func ResolveYoutrackCommand() string {
	cfg := LoadConfig()
	if cfg != nil && cfg.YoutrackCommand != nil && *cfg.YoutrackCommand != "" {
		return *cfg.YoutrackCommand
	}
	return ""
}

// ResolveGitlabCommand returns the gitlab command, using precedence:
//  1. config.json -> gitlab_command
//  2. Default ("") - meaning fall back to browser command if not set
func ResolveGitlabCommand() string {
	cfg := LoadConfig()
	if cfg != nil && cfg.GitlabCommand != nil && *cfg.GitlabCommand != "" {
		return *cfg.GitlabCommand
	}
	return ""
}
