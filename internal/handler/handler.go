package handler

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/docs"
	"github.com/qwc/asiakirjat/internal/store"
	"github.com/qwc/asiakirjat/internal/templates"
)

type Handler struct {
	config         *config.Config
	templates      *templates.Engine
	storage        docs.Storage
	staticFS       fs.FS
	projects       store.ProjectStore
	versions       store.VersionStore
	users          store.UserStore
	sessions       store.SessionStore
	access         store.ProjectAccessStore
	tokens         store.TokenStore
	authenticators []auth.Authenticator
	sessionMgr     *auth.SessionManager
	logger         *slog.Logger
}

type Deps struct {
	Config         *config.Config
	Templates      *templates.Engine
	Storage        docs.Storage
	StaticFS       fs.FS
	Projects       store.ProjectStore
	Versions       store.VersionStore
	Users          store.UserStore
	Sessions       store.SessionStore
	Access         store.ProjectAccessStore
	Tokens         store.TokenStore
	Authenticators []auth.Authenticator
	SessionMgr     *auth.SessionManager
	Logger         *slog.Logger
}

func New(deps Deps) *Handler {
	return &Handler{
		config:         deps.Config,
		templates:      deps.Templates,
		storage:        deps.Storage,
		staticFS:       deps.StaticFS,
		projects:       deps.Projects,
		versions:       deps.Versions,
		users:          deps.Users,
		sessions:       deps.Sessions,
		access:         deps.Access,
		tokens:         deps.Tokens,
		authenticators: deps.Authenticators,
		sessionMgr:     deps.SessionMgr,
		logger:         deps.Logger,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Static files
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(h.staticFS)))

	// Public pages
	mux.HandleFunc("GET /{$}", h.withSession(h.handleFrontpage))
	mux.HandleFunc("GET /login", h.withSession(h.handleLoginPage))
	mux.HandleFunc("POST /login", h.withSession(h.handleLoginSubmit))
	mux.HandleFunc("GET /logout", h.withSession(h.handleLogout))
	mux.HandleFunc("GET /auth/callback", h.withSession(h.handleOAuth2Callback))

	// Project pages
	mux.HandleFunc("GET /project/{slug}", h.withSession(h.handleProjectDetail))
	mux.HandleFunc("GET /project/{slug}/{version}/{path...}", h.withSession(h.handleServeDoc))
	mux.HandleFunc("GET /project/{slug}/upload", h.withSession(h.requireAuth(h.handleUploadForm)))
	mux.HandleFunc("POST /project/{slug}/upload", h.withSession(h.requireAuth(h.handleUploadSubmit)))

	// API endpoints
	mux.HandleFunc("GET /api/projects", h.withSession(h.handleAPIProjects))
	mux.HandleFunc("GET /api/project/{slug}/versions", h.withSession(h.handleAPIVersions))
	mux.HandleFunc("POST /api/project/{slug}/upload", h.handleAPIUpload)

	// Admin routes
	mux.HandleFunc("GET /admin/projects", h.withSession(h.requireAdmin(h.handleAdminProjects)))
	mux.HandleFunc("POST /admin/projects", h.withSession(h.requireAdmin(h.handleAdminCreateProject)))
	mux.HandleFunc("GET /admin/projects/{slug}/edit", h.withSession(h.requireAdmin(h.handleAdminEditProject)))
	mux.HandleFunc("POST /admin/projects/{slug}/edit", h.withSession(h.requireAdmin(h.handleAdminUpdateProject)))
	mux.HandleFunc("POST /admin/projects/{slug}/delete", h.withSession(h.requireAdmin(h.handleAdminDeleteProject)))
	mux.HandleFunc("POST /admin/projects/{slug}/access/grant", h.withSession(h.requireAdmin(h.handleAdminGrantAccess)))
	mux.HandleFunc("POST /admin/projects/{slug}/access/revoke", h.withSession(h.requireAdmin(h.handleAdminRevokeAccess)))
	mux.HandleFunc("GET /admin/users", h.withSession(h.requireAdmin(h.handleAdminUsers)))
	mux.HandleFunc("POST /admin/users", h.withSession(h.requireAdmin(h.handleAdminCreateUser)))
	mux.HandleFunc("POST /admin/users/{id}/delete", h.withSession(h.requireAdmin(h.handleAdminDeleteUser)))
	mux.HandleFunc("GET /admin/robots", h.withSession(h.requireAdmin(h.handleAdminRobots)))
	mux.HandleFunc("POST /admin/robots", h.withSession(h.requireAdmin(h.handleAdminCreateRobot)))
	mux.HandleFunc("POST /admin/robots/{id}/tokens", h.withSession(h.requireAdmin(h.handleAdminGenerateToken)))
	mux.HandleFunc("POST /admin/robots/{id}/tokens/{tid}/revoke", h.withSession(h.requireAdmin(h.handleAdminRevokeToken)))
	mux.HandleFunc("POST /admin/robots/{id}/delete", h.withSession(h.requireAdmin(h.handleAdminDeleteRobot)))

	// Health check
	mux.HandleFunc("GET /healthz", h.handleHealthz)
}

func (h *Handler) render(w http.ResponseWriter, name string, data map[string]any) {
	if err := h.templates.Render(w, name, data); err != nil {
		h.logger.Error("template render error", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
