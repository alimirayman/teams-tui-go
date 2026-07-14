package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

func scopedTestToken(scopes string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"scp":%q}`, scopes)))
	return header + "." + payload + ".signature"
}

func TestAccessTokenIncludesScopes(t *testing.T) {
	token := scopedTestToken("User.Read Chat.ReadWrite Files.Read.All")
	if includes, known := accessTokenIncludesScopes(token, "User.Read Chat.ReadWrite Files.Read.All offline_access"); !known || !includes {
		t.Fatalf("expected token scopes to satisfy request: includes=%v known=%v", includes, known)
	}
	if includes, known := accessTokenIncludesScopes(token, "User.Read Files.ReadWrite"); !known || includes {
		t.Fatalf("expected missing scope to require sign-in: includes=%v known=%v", includes, known)
	}
	if includes, known := accessTokenIncludesScopes("opaque-token", "User.Read"); known || includes {
		t.Fatalf("opaque token should have unknown scopes: includes=%v known=%v", includes, known)
	}
}

func TestGetValidTokenSilentFallsBackToAuthenticatedSession(t *testing.T) {
	withTempConfigHome(t)
	clearSessionToken()
	t.Cleanup(clearSessionToken)

	rememberSessionToken(&TokenResponse{
		AccessToken: "session-access-token",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	})
	got, err := GetValidTokenSilent("client-id")
	if err != nil {
		t.Fatal(err)
	}
	if got != "session-access-token" {
		t.Fatalf("token = %q", got)
	}
}

func TestGetValidTokenSilentRejectsExpiredSessionFallback(t *testing.T) {
	withTempConfigHome(t)
	clearSessionToken()
	t.Cleanup(clearSessionToken)

	rememberSessionToken(&TokenResponse{
		AccessToken: "expired-session-token",
		ExpiresAt:   time.Now().Add(-time.Minute).Unix(),
	})
	_, err := GetValidTokenSilent("client-id")
	if err == nil || !strings.Contains(err.Error(), "no cached token") {
		t.Fatalf("error = %v, want no cached token", err)
	}
}
