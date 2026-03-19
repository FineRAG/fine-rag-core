package managers

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	util "enterprise-go-rag/backend/util/apiutil"
)

type AuthManager struct {
	DB       *sql.DB
	Secret   string
	TokenTTL time.Duration
}

type dbUser struct {
	ID           int64
	Username     string
	PasswordHash string
	APIKeyHash   string
	Active       bool
}

func (m *AuthManager) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		APIKey   string `json:"apiKey"`
	}
	if err := util.DecodeJSON(r.Body, &req); err != nil {
		util.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		util.WriteError(w, http.StatusBadRequest, "username_required", "username is required")
		return
	}
	user, err := m.fetchUserByUsername(r.Context(), req.Username)
	if err != nil || !user.Active {
		util.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return
	}
	ok := false
	if strings.TrimSpace(req.Password) != "" {
		ok = util.VerifySecret(req.Password, user.PasswordHash)
	}
	if !ok && strings.TrimSpace(req.APIKey) != "" {
		ok = util.VerifySecret(req.APIKey, user.APIKeyHash)
	}
	if !ok {
		util.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return
	}
	now := time.Now().UTC()
	token, err := util.SignJWT(util.AuthClaims{
		Sub: user.Username,
		UID: user.ID,
		Iat: now.Unix(),
		Exp: now.Add(m.TokenTTL).Unix(),
	}, m.Secret)
	if err != nil {
		util.WriteError(w, http.StatusInternalServerError, "token_failed", "failed to issue token")
		return
	}
	util.WriteJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (m *AuthManager) fetchUserByUsername(ctx context.Context, username string) (dbUser, error) {
	var u dbUser
	err := m.DB.QueryRowContext(ctx, `SELECT id, username, password_hash, api_key_hash, active FROM app_users WHERE username = $1`, username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.APIKeyHash, &u.Active)
	return u, err
}
