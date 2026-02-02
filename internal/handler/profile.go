package handler

import (
	"net/http"

	"github.com/qwc/asiakirjat/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

func (h *Handler) handleProfilePage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	h.render(w, "profile", map[string]any{
		"User": user,
	})
}

func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	if user.AuthSource != "builtin" {
		h.render(w, "profile", map[string]any{
			"User":  user,
			"Error": "Password is managed by an external provider",
		})
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	if currentPassword == "" || newPassword == "" || confirmPassword == "" {
		h.render(w, "profile", map[string]any{
			"User":  user,
			"Error": "All password fields are required",
		})
		return
	}

	if newPassword != confirmPassword {
		h.render(w, "profile", map[string]any{
			"User":  user,
			"Error": "New passwords do not match",
		})
		return
	}

	if user.Password == nil {
		h.render(w, "profile", map[string]any{
			"User":  user,
			"Error": "Account has no password set",
		})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*user.Password), []byte(currentPassword)); err != nil {
		h.render(w, "profile", map[string]any{
			"User":  user,
			"Error": "Current password is incorrect",
		})
		return
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		h.logger.Error("hashing password", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	user.Password = &hash
	if err := h.users.Update(ctx, user); err != nil {
		h.logger.Error("updating password", "error", err)
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	h.render(w, "profile", map[string]any{
		"User":    user,
		"Success": "Password changed successfully",
	})
}
