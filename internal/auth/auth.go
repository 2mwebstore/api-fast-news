// Package auth issues and validates JWTs and hashes passwords (§66).
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/cambodia-fast-news/backend/internal/config"
)

var (
	ErrTokenInvalid = errors.New("token invalid")
	ErrTokenExpired = errors.New("token expired")
	ErrWrongType    = errors.New("wrong token type")
)

// TokenType separates access from refresh so a refresh token can never be
// replayed as an access token against a protected endpoint.
type TokenType string

const (
	TokenAccess  TokenType = "access"
	TokenRefresh TokenType = "refresh"
)

// Claims is the JWT payload. RoleSlug is embedded so the auth middleware can
// short-circuit without a database round trip; full permissions are loaded
// only when a handler actually needs them.
type Claims struct {
	UserID       uint      `json:"uid"`
	Email        string    `json:"email"`
	RoleSlug     string    `json:"role"`
	TokenType    TokenType `json:"typ"`
	TokenVersion int       `json:"tv"`
	jwt.RegisteredClaims
}

type Service struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	issuer     string
}

func NewService(cfg *config.Config) *Service {
	return &Service{
		secret:     []byte(cfg.JWT.Secret),
		accessTTL:  cfg.JWT.AccessTTL,
		refreshTTL: cfg.JWT.RefreshTTL,
		issuer:     "cambodia-fast-news",
	}
}

// TokenPair is what the login and refresh endpoints return.
type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"` // access token lifetime, seconds
	TokenType    string `json:"tokenType"`
}

func (s *Service) Issue(userID uint, email, roleSlug string, tokenVersion int) (*TokenPair, error) {
	access, err := s.sign(userID, email, roleSlug, tokenVersion, TokenAccess, s.accessTTL)
	if err != nil {
		return nil, err
	}
	refresh, err := s.sign(userID, email, roleSlug, tokenVersion, TokenRefresh, s.refreshTTL)
	if err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(s.accessTTL.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

func (s *Service) sign(userID uint, email, roleSlug string, tokenVersion int, typ TokenType, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		UserID:       userID,
		Email:        email,
		RoleSlug:     roleSlug,
		TokenType:    typ,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   fmt.Sprint(userID),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// Parse validates a token's signature, expiry and type. Pinning the expected
// signing method is what blocks the "alg: none" substitution attack.
func (s *Service) Parse(tokenString string, expected TokenType) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return s.secret, nil
	}, jwt.WithIssuer(s.issuer), jwt.WithValidMethods([]string{"HS256"}))

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}
	if claims.TokenType != expected {
		return nil, ErrWrongType
	}
	return claims, nil
}

// HashPassword applies bcrypt at a cost tuned for interactive logins.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword reports whether the plaintext matches the stored hash.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
