package handler

import (
	"net/http"
	"strconv"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Admin handlers for user accounts.

func (h *Handler) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	users, err := h.users.List(ctx)
	if err != nil {
		h.logger.Error("listing users", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "admin_users", map[string]any{
		"User":  user,
		"Users": users,
	})
}

func (h *Handler) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	username := r.FormValue("username")
	password := r.FormValue("password")
	email := r.FormValue("email")
	role := r.FormValue("role")

	if username == "" || password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	if role != "admin" && role != "editor" && role != "viewer" {
		role = "viewer"
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		h.logger.Error("hashing password", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	user := &database.User{
		Username:   username,
		Email:      email,
		Password:   &hash,
		AuthSource: "builtin",
		Role:       role,
	}

	if err := h.users.Create(ctx, user); err != nil {
		h.userError(w, http.StatusBadRequest,
			"Failed to create user — the username or email may already be taken",
			err, "username", username)
		return
	}

	h.redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	if err := h.users.Delete(ctx, id); err != nil {
		h.logger.Error("deleting user", "error", err)
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) handleAdminUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	role := r.FormValue("role")
	if role != "admin" && role != "editor" && role != "viewer" {
		http.Error(w, "Invalid role", http.StatusBadRequest)
		return
	}

	user, err := h.users.GetByID(ctx, id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	previousRole := user.Role
	user.Role = role
	if err := h.users.Update(ctx, user); err != nil {
		h.logger.Error("updating user role", "error", err)
		http.Error(w, "Failed to update role", http.StatusInternalServerError)
		return
	}

	// Demotion cleanup (audit M-4). When an editor or admin is demoted to
	// viewer, the rationale for the access they accumulated as an editor
	// no longer holds: revoke manual editor-role ProjectAccess rows and
	// delete any API tokens they own.
	if role == "viewer" && previousRole != "viewer" {
		if err := h.access.RevokeManualEditorByUser(ctx, user.ID); err != nil {
			h.logger.Error("revoking manual editor grants on demotion", "user_id", user.ID, "error", err)
		}
		tokens, err := h.tokens.ListByUser(ctx, user.ID)
		if err != nil {
			h.logger.Error("listing user tokens on demotion", "user_id", user.ID, "error", err)
		} else {
			for _, t := range tokens {
				if err := h.tokens.Delete(ctx, t.ID); err != nil {
					h.logger.Error("revoking token on demotion", "token_id", t.ID, "user_id", user.ID, "error", err)
				}
			}
		}
	}

	h.redirect(w, r, "/admin/users", http.StatusSeeOther)
}
