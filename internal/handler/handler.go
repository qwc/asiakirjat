package handler

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

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
	groupMappings  store.AuthGroupMappingStore
	globalAccess   store.GlobalAccessStore
	authenticators []auth.Authenticator
	oauth2Auth     *auth.OAuth2Authenticator
	sessionMgr     *auth.SessionManager
	loginLimiter   *RateLimiter
	searchIndex    *docs.SearchIndex
	logger         *slog.Logger

	// Cache for latest version tags (invalidated on upload/delete)
	latestTagsCache     map[string]string
	latestTagsCacheTime time.Time

	// Reindex state tracking
	reindexRunning  bool
	reindexProgress string
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
	GroupMappings  store.AuthGroupMappingStore
	GlobalAccess   store.GlobalAccessStore
	Authenticators []auth.Authenticator
	OAuth2Auth     *auth.OAuth2Authenticator
	SessionMgr     *auth.SessionManager
	SearchIndex    *docs.SearchIndex
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
		groupMappings:  deps.GroupMappings,
		globalAccess:   deps.GlobalAccess,
		authenticators: deps.Authenticators,
		oauth2Auth:     deps.OAuth2Auth,
		sessionMgr:     deps.SessionMgr,
		loginLimiter:   NewRateLimiter(10, 60*time.Second),
		searchIndex:    deps.SearchIndex,
		logger:         deps.Logger,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	bp := h.config.Server.BasePath

	// Static files
	mux.Handle("GET "+bp+"/static/", http.StripPrefix(bp+"/static/", http.FileServerFS(h.staticFS)))

	// Public pages
	mux.HandleFunc("GET "+bp+"/{$}", h.withSession(h.handleFrontpage))
	mux.HandleFunc("GET "+bp+"/login", h.withSession(h.handleLoginPage))
	mux.HandleFunc("POST "+bp+"/login", withRateLimit(h.loginLimiter, h.withSession(h.handleLoginSubmit)))
	mux.HandleFunc("GET "+bp+"/logout", h.withSession(h.handleLogout))
	mux.HandleFunc("GET "+bp+"/licenses", h.withSession(h.handleLicenses))
	mux.HandleFunc("GET "+bp+"/auth/oauth2", h.handleOAuth2Login)
	mux.HandleFunc("GET "+bp+"/auth/callback", h.withSession(h.handleOAuth2Callback))

	// Project pages
	mux.HandleFunc("GET "+bp+"/project/{slug}", h.withSession(h.handleProjectDetail))
	mux.HandleFunc("GET "+bp+"/project/{slug}/{version}/{path...}", h.withSession(h.handleServeDoc))
	mux.HandleFunc("GET "+bp+"/project/{slug}/upload", h.withSession(h.requireAuth(h.handleUploadForm)))
	mux.HandleFunc("POST "+bp+"/project/{slug}/upload", h.withSession(h.requireAuth(h.handleUploadSubmit)))
	mux.HandleFunc("POST "+bp+"/project/{slug}/version/{tag}/delete", h.withSession(h.requireAuth(h.handleDeleteVersion)))

	// Project token management (for editors)
	mux.HandleFunc("GET "+bp+"/project/{slug}/tokens", h.withSession(h.requireAuth(h.handleProjectTokens)))
	mux.HandleFunc("POST "+bp+"/project/{slug}/tokens", h.withSession(h.requireAuth(h.handleProjectCreateToken)))
	mux.HandleFunc("POST "+bp+"/project/{slug}/tokens/{id}/revoke", h.withSession(h.requireAuth(h.handleProjectRevokeToken)))

	// Search
	mux.HandleFunc("GET "+bp+"/search", h.withSession(h.handleSearchPage))
	mux.HandleFunc("GET "+bp+"/api/search", h.withSession(h.handleAPISearch))

	// API endpoints
	mux.HandleFunc("GET "+bp+"/api/projects", h.withSession(h.handleAPIProjects))
	mux.HandleFunc("GET "+bp+"/api/project/{slug}/versions", h.withSession(h.handleAPIVersions))
	mux.HandleFunc("POST "+bp+"/api/project/{slug}/upload", h.handleAPIUpload)

	// Profile routes
	mux.HandleFunc("GET "+bp+"/profile", h.withSession(h.requireAuth(h.handleProfilePage)))
	mux.HandleFunc("POST "+bp+"/profile/password", h.withSession(h.requireAuth(h.handleChangePassword)))

	// Admin routes
	mux.HandleFunc("GET "+bp+"/admin/projects", h.withSession(h.requireAdmin(h.handleAdminProjects)))
	mux.HandleFunc("POST "+bp+"/admin/projects", h.withSession(h.requireAdmin(h.handleAdminCreateProject)))
	mux.HandleFunc("GET "+bp+"/admin/projects/{slug}/edit", h.withSession(h.requireAdmin(h.handleAdminEditProject)))
	mux.HandleFunc("POST "+bp+"/admin/projects/{slug}/edit", h.withSession(h.requireAdmin(h.handleAdminUpdateProject)))
	mux.HandleFunc("POST "+bp+"/admin/projects/{slug}/delete", h.withSession(h.requireAdmin(h.handleAdminDeleteProject)))
	mux.HandleFunc("POST "+bp+"/admin/projects/{slug}/access/grant", h.withSession(h.requireAdmin(h.handleAdminGrantAccess)))
	mux.HandleFunc("POST "+bp+"/admin/projects/{slug}/access/revoke", h.withSession(h.requireAdmin(h.handleAdminRevokeAccess)))
	mux.HandleFunc("GET "+bp+"/admin/users", h.withSession(h.requireAdmin(h.handleAdminUsers)))
	mux.HandleFunc("POST "+bp+"/admin/users", h.withSession(h.requireAdmin(h.handleAdminCreateUser)))
	mux.HandleFunc("POST "+bp+"/admin/users/{id}/delete", h.withSession(h.requireAdmin(h.handleAdminDeleteUser)))
	mux.HandleFunc("POST "+bp+"/admin/users/{id}/role", h.withSession(h.requireAdmin(h.handleAdminUpdateUserRole)))
	mux.HandleFunc("POST "+bp+"/admin/users/{id}/password", h.withSession(h.requireAdmin(h.handleAdminResetPassword)))
	mux.HandleFunc("GET "+bp+"/admin/robots", h.withSession(h.requireAdmin(h.handleAdminRobots)))
	mux.HandleFunc("POST "+bp+"/admin/robots", h.withSession(h.requireAdmin(h.handleAdminCreateRobot)))
	mux.HandleFunc("POST "+bp+"/admin/robots/{id}/tokens", h.withSession(h.requireAdmin(h.handleAdminGenerateToken)))
	mux.HandleFunc("POST "+bp+"/admin/robots/{id}/tokens/{tid}/revoke", h.withSession(h.requireAdmin(h.handleAdminRevokeToken)))
	mux.HandleFunc("POST "+bp+"/admin/robots/{id}/delete", h.withSession(h.requireAdmin(h.handleAdminDeleteRobot)))
	mux.HandleFunc("POST "+bp+"/admin/reindex", h.withSession(h.requireAdmin(h.handleAdminReindex)))
	mux.HandleFunc("GET "+bp+"/admin/groups", h.withSession(h.requireAdmin(h.handleAdminGroups)))
	mux.HandleFunc("POST "+bp+"/admin/groups", h.withSession(h.requireAdmin(h.handleAdminCreateGroupMapping)))
	mux.HandleFunc("POST "+bp+"/admin/groups/{id}/delete", h.withSession(h.requireAdmin(h.handleAdminDeleteGroupMapping)))
	mux.HandleFunc("GET "+bp+"/admin/global-access", h.withSession(h.requireAdmin(h.handleAdminGlobalAccess)))
	mux.HandleFunc("POST "+bp+"/admin/global-access", h.withSession(h.requireAdmin(h.handleAdminCreateGlobalAccessRule)))
	mux.HandleFunc("POST "+bp+"/admin/global-access/{id}/delete", h.withSession(h.requireAdmin(h.handleAdminDeleteGlobalAccessRule)))
	mux.HandleFunc("POST "+bp+"/admin/deploy-docs", h.withSession(h.requireAdmin(h.handleAdminDeployBuiltinDocs)))

	// Health check (keep at root for load balancer compatibility, but also at base path)
	mux.HandleFunc("GET "+bp+"/healthz", h.handleHealthz)
	if bp != "" {
		mux.HandleFunc("GET /healthz", h.handleHealthz)
	}
}

func (h *Handler) render(w http.ResponseWriter, name string, data map[string]any) {
	if err := h.templates.Render(w, name, data); err != nil {
		h.logger.Error("template render error", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// redirect performs an HTTP redirect with the base path prepended to the path.
func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, path string, code int) {
	http.Redirect(w, r, h.config.Server.BasePath+path, code)
}
