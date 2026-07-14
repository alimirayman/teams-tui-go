# Microsoft Entra App Registration Setup

This guide explains how to create an app registration so that `ms-teams-tui` can authenticate with Microsoft Graph on your behalf. The installed command is `teams`.

> [!IMPORTANT]
> Use your own app registration. The built-in upstream Microsoft client ID can fail with `AADSTS65002` because first-party Microsoft clients require preauthorization that tenant administrators cannot configure themselves.

---

## Steps

### 1. Go to Azure Portal

Navigate to [https://portal.azure.com](https://portal.azure.com) and sign in with your Microsoft account.

### 2. Create an App Registration

1. Search for **"App registrations"** in the top search bar and click it.
2. Click **"New registration"**.
3. Fill in:
   - **Name**: `ms-teams-tui` (or any name you like)
   - **Supported account types**: Select **"Accounts in this organizational directory only"** unless you intentionally want to support other tenants.
   - **Redirect URI**: Leave blank (not needed for device code flow)
4. Click **Register**.

### 3. Enable Public Client Flow

1. In your app registration, go to **Authentication** (left sidebar).
2. Scroll down to **Advanced settings**.
3. Set **"Allow public client flows"** to **Yes**.
4. Click **Save**.

### 4. Add API Permissions

1. Go to **API permissions** (left sidebar).
2. Click **"Add a permission"** → **Microsoft Graph** → **Delegated permissions**.
3. Add the **Basic Permissions** (always required), plus any **Optional Feature Permissions** you plan to enable.
4. Click **Add permissions**.
5. (Optional) Click **"Grant admin consent"** if you have admin rights.

### 5. Copy Your Client ID and Tenant ID

1. Go to **Overview** (left sidebar).
2. Copy the **Application (client) ID** — this is your `CLIENT_ID`.
3. Copy the **Directory (tenant) ID** — this is your `TENANT_ID`.

### 6. Configure ms-teams-tui

Set your client ID and tenant ID using either method:

**Method A — `.env` file** (in the project directory):
```bash
cp .env.example .env
# Edit .env:
CLIENT_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
TENANT_ID=yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy
```

**Method B — config file** (`~/Library/Application Support/ms-teams-tui/config.json` on macOS, `~/.config/ms-teams-tui/config.json` on Linux):
```json
{
  "client_id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "tenant_id": "yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy"
}
```

---

## Permissions Reference

### Basic Permissions (Always Required)

These permissions are needed for all core functionality. Every user must grant them.

| Permission | Type | Purpose |
|------------|------|---------|
| `User.Read` | Delegated | Read your profile (display name, ID) |
| `Chat.Read` | Delegated | Read your Teams chats and messages |
| `Chat.ReadWrite` | Delegated | Send messages and mark chats as read |
| `offline_access` | Delegated | Get refresh tokens for silent sign-in |

---

### Optional Feature Permissions

The following permissions unlock additional features. Each feature can be **enabled or disabled independently** in `~/.config/ms-teams-tui/config.json`. If a feature is disabled, its permission is never requested and users without access are completely unaffected.

---

#### Feature: File Preview & Download (`file_preview_enabled`)

Enable in config:
```json
{ "file_preview_enabled": true }
```

In the message view popup (`v`), press **Tab** to enter attachment cursor mode. Press **Space** for a cached quick preview or **Enter** to download and open from `~/Downloads/`.

| Permission | Type | Admin Consent | Purpose |
|------------|------|---------------|---------|
| `Files.Read.All` | Delegated | Not required | Download Teams attachments the signed-in user can access, including files owned by other participants |

> **Note**: Inline images embedded in message HTML may display without this permission. Reference attachments from another participant's OneDrive or SharePoint require `Files.Read.All`; it remains limited to files the signed-in user can already access.

---

#### Feature: File Upload & Attachment (`file_upload_enabled`)

Enable in config:
```json
{ "file_upload_enabled": true }
```

In compose mode (`i`), press **Ctrl+f** to open the offline file browser overlay. Type to fuzzy-filter the current directory, including dotfiles and dot-directories; use arrow keys to navigate and press **Enter** to attach. Pasting or dragging a local filepath into compose also attaches it directly.

| Permission        | Type      | Admin Consent | Purpose                                                                                  |
| ----------------- | --------- | ------------- | ---------------------------------------------------------------------------------------- |
| `Files.ReadWrite` | Delegated | Not required  | Upload file attachments from the local computer to OneDrive/SharePoint via the Graph API |

> **Note**: Files are uploaded to your OneDrive `Microsoft Teams Chat Files` folder (for chats) or the channel's SharePoint document library folder (for channels).

---

#### Feature: User Presence Status (`presence_enabled`)

Enable in config:
```json
{ "presence_enabled": true }
```

Key binding: In message selection mode (`m`), press **p** to view the presence status of the message sender.

| Permission | Type | Admin Consent | Purpose |
|------------|------|---------------|---------|
| `Presence.Read.All` | Delegated | Not required | Read real-time presence (Available, Busy, Away, DoNotDisturb, etc.) for users in your organisation |

> **Note**: Presence is only available for users in the same Azure AD organisation. Personal Microsoft accounts do not support presence.

---

#### Feature: User Profile Info (`user_profile_enabled`)

Enable in config:
```json
{ "user_profile_enabled": true }
```

Enable extended profile (job title, department, office — requires admin consent):
```json
{ "user_profile_enabled": true, "user_profile_extended": true }
```

Key binding: In message selection mode (`m`), press **i** to view the profile of the message sender.

| Permission | Type | Admin Consent | Purpose |
|------------|------|---------------|---------|
| `User.ReadBasic.All` | Delegated | **Not required** | Read basic profile for any user in the organisation: display name, email, photo |
| `User.Read.All` | Delegated | **Required** | Read full profile including job title, department, office location, and manager (only needed when `user_profile_extended: true`) |

> **Recommendation**: Start with `User.ReadBasic.All` (no admin needed). Add `User.Read.All` and set `user_profile_extended: true` only if you need job title / department data.

---

#### Feature: Teams Channels (`teams_channels_enabled`)

Enable in config:
```json
{ "teams_channels_enabled": true }
```

Effect: Teams channels appear in the **main chat list sidebar** below your chats, under a `── Teams ──` divider. Navigate into them with `j`/`k` just like chats; their messages load in the right panel. Press `f` to favourite a channel locally and keep it at the top of the channel list. This does not require another Graph permission.

| Permission                 | Type      | Admin Consent   | Purpose                                      |
| -------------------------- | --------- | --------------- | -------------------------------------------- |
| `Team.ReadBasic.All`       | Delegated | May be required | List all Teams the signed-in user belongs to |
| `Channel.ReadBasic.All`    | Delegated | May be required | List channels within each team               |
| `ChannelMessage.Read.All`  | Delegated | **Required**    | Read messages from Teams channels            |
| `ChannelMessage.Send`      | Delegated | Not required    | Post new messages to Teams channels          |
| `ChannelMessage.ReadWrite` | Delegated | Not required    | Edit and delete your own channel messages    |

> **Note**: `ChannelMessage.Read.All` requires **admin consent** in most enterprise tenants — your IT administrator must grant it in the Azure portal. Personal Microsoft accounts cannot join Teams and this feature will return no results.
> **Note**: **this feature is under development still**

---

#### Feature: Channel Mentions & Autocomplete (`channel_mentions_enabled`)

Enable in config:
```json
{ "channel_mentions_enabled": true }
```

Effect: Enables mentioning people inside Teams Channels. Type `@` in a channel message input to show a dropdown list of team members, search/select one, and post the message with a native Teams mention notification.

| Permission            | Type      | Admin Consent   | Purpose                                                                |
| --------------------- | --------- | --------------- | ---------------------------------------------------------------------- |
| `TeamMember.Read.All` | Delegated | May be required | Retrieve the list of team/channel members for autocomplete suggestions |

---

## Re-authentication After Enabling Features

When you enable a new feature in `config.json`, the existing token does not automatically gain the new permission. Current builds detect missing enabled-feature scopes and start a fresh device-code sign-in. If that does not happen, force it manually:

```bash
# Delete the cached token
rm ~/.cache/ms-teams-tui/token.json

# Restart the app — it will prompt you to authenticate again
teams
```

The new device code login will request all permissions for your currently enabled features in one go.

> **Tip**: You can check which scopes your current token includes by inspecting the JWT at [jwt.ms](https://jwt.ms).

---

## Troubleshooting

**"AADSTS50020: User account from identity provider does not exist in tenant"**
→ Make sure `tenant_id` is set to the Directory (tenant) ID of the organization where your Teams account lives. If you intentionally omit `tenant_id`, create the app as multitenant.

**"AADSTS7000218: The request body must contain the following parameter: 'client_assertion' or 'client_secret'"**
→ Make sure **"Allow public client flows"** is set to **Yes** in the Authentication settings (step 3).

**"Presence unavailable" / "Profile unavailable" errors**
→ The required permission may not be granted. Check the API permissions in your Azure app registration and re-authenticate after granting the permission.

**"Teams unavailable" error when using `teams_channels_enabled`**
→ `Team.ReadBasic.All` and `Channel.ReadBasic.All` may require admin consent in your tenant. Ask your IT administrator to grant consent in the Azure portal.

**Permissions not working after changing config**
→ Delete `~/.cache/ms-teams-tui/token.json` and re-authenticate so that the new permissions are included in the device code flow.
