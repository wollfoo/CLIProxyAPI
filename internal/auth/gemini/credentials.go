package gemini

import "os"

const (
	defaultGeminiOAuthClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	defaultGeminiOAuthClientSecret = "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"
)

var (
	GeminiOAuthScopes = []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	}
)

func GetGeminiOAuthClientID() string {
	if v := os.Getenv("GEMINI_OAUTH_CLIENT_ID"); v != "" {
		return v
	}
	return defaultGeminiOAuthClientID
}

func GetGeminiOAuthClientSecret() string {
	if v := os.Getenv("GEMINI_OAUTH_CLIENT_SECRET"); v != "" {
		return v
	}
	return defaultGeminiOAuthClientSecret
}
