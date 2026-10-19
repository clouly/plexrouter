// Package oauth implements "Sign in with Google / GitHub": the OAuth 2.0
// authorization-code flow (with PKCE) and fetching the signed-in profile.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"
)

const (
	Google = "google"
	GitHub = "github"
)

// Profile is what PlexRouter needs to know about the signed-in account.
type Profile struct {
	Subject       string // stable account ID at the provider
	Email         string
	EmailVerified bool
	Name          string
}

// Provider is one configured identity provider. The URL fields are exported
// so tests (or self-hosted GitHub Enterprise) can point them elsewhere.
type Provider struct {
	Name         string
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	Scopes       []string
	// UserInfoURL is Google's OpenID userinfo endpoint, or GitHub's API base.
	UserInfoURL string
	HTTPClient  *http.Client
}

func NewGoogle(clientID, clientSecret string) *Provider {
	return &Provider{
		Name: Google, ClientID: clientID, ClientSecret: clientSecret,
		AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:    "https://oauth2.googleapis.com/token",
		UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:      []string{"openid", "email", "profile"},
	}
}

func NewGitHub(clientID, clientSecret string) *Provider {
	return &Provider{
		Name: GitHub, ClientID: clientID, ClientSecret: clientSecret,
		AuthURL:     "https://github.com/login/oauth/authorize",
		TokenURL:    "https://github.com/login/oauth/access_token",
		UserInfoURL: "https://api.github.com",
		Scopes:      []string{"read:user", "user:email"},
	}
}

func (p *Provider) config(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: p.ClientID, ClientSecret: p.ClientSecret, RedirectURL: redirectURL, Scopes: p.Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: p.AuthURL, TokenURL: p.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
}

func (p *Provider) client() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// NewVerifier returns a PKCE code verifier for one sign-in attempt.
func NewVerifier() string { return oauth2.GenerateVerifier() }

// AuthCodeURL is where to send the browser to start signing in.
func (p *Provider) AuthCodeURL(redirectURL, state, verifier string) string {
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier)}
	if p.Name == Google {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "select_account"))
	}
	return p.config(redirectURL).AuthCodeURL(state, opts...)
}

// Exchange trades the callback code for a token and fetches the profile.
func (p *Provider) Exchange(ctx context.Context, redirectURL, code, verifier string) (*Profile, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.client())
	tok, err := p.config(redirectURL).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	if p.Name == GitHub {
		return p.githubProfile(ctx, tok.AccessToken)
	}
	return p.googleProfile(ctx, tok.AccessToken)
}

func (p *Provider) getJSON(ctx context.Context, url, token string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := p.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("GET %s: %d %s", url, resp.StatusCode, b)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (p *Provider) googleProfile(ctx context.Context, token string) (*Profile, error) {
	var u struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := p.getJSON(ctx, p.UserInfoURL, token, &u); err != nil {
		return nil, err
	}
	if u.Sub == "" {
		return nil, errors.New("google userinfo has no subject")
	}
	return &Profile{Subject: u.Sub, Email: u.Email, EmailVerified: u.EmailVerified, Name: u.Name}, nil
}

func (p *Provider) githubProfile(ctx context.Context, token string) (*Profile, error) {
	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := p.getJSON(ctx, p.UserInfoURL+"/user", token, &u); err != nil {
		return nil, err
	}
	if u.ID == 0 {
		return nil, errors.New("github user has no id")
	}
	// The public profile email may be empty or unverified; ask for the
	// account's emails and prefer the verified primary one.
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := p.getJSON(ctx, p.UserInfoURL+"/user/emails", token, &emails); err != nil {
		return nil, err
	}
	prof := &Profile{Subject: strconv.FormatInt(u.ID, 10), Name: u.Name}
	if prof.Name == "" {
		prof.Name = u.Login
	}
	for _, e := range emails {
		if e.Verified && (e.Primary || prof.Email == "") {
			prof.Email, prof.EmailVerified = e.Email, true
		}
	}
	return prof, nil
}
