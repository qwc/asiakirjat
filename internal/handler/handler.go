package handler

import (
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/qwc/asiakirjat/internal/access"
	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/docs"
	"github.com/qwc/asiakirjat/internal/projects"
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
	accessLists    store.AccessListStore
	uploadLogs     store.UploadLogStore
	authenticators []auth.Authenticator
	oauth2Auth     *auth.OAuth2Authenticator
	tokenAuth      *auth.TokenAuthenticator
	sessionMgr     *auth.SessionManager
	loginLimiter   *RateLimiter
	trustedProxies []*net.IPNet
	searchIndex    *docs.SearchIndex
	projectService *projects.Service
	checker        *access.Checker
	logger         *slog.Logger

	// Serializes storage-mutating work (archive extraction vs. rename) per
	// project — see internal/handler/locks.go.
	projectLocks *keyedMutex

	// Synchronized state — see internal/handler/state.go.
	latestTags latestTagsCache
	reindex    reindexState

	// Background job lifecycle — see internal/handler/jobs.go.
	jobs *jobs
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
	AccessLists    store.AccessListStore
	UploadLogs     store.UploadLogStore
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
		accessLists:    deps.AccessLists,
		uploadLogs:     deps.UploadLogs,
		authenticators: deps.Authenticators,
		oauth2Auth:     deps.OAuth2Auth,
		tokenAuth:      auth.NewTokenAuthenticator(deps.Tokens, deps.Users),
		sessionMgr:     deps.SessionMgr,
		loginLimiter:   NewRateLimiter(10, 60*time.Second),
		trustedProxies: parseTrustedProxies(deps.Config.Server.TrustedProxies),
		searchIndex:    deps.SearchIndex,
		projectService: projects.NewService(deps.Projects, deps.Versions, deps.Access, deps.Storage, deps.Logger),
		checker:        access.NewChecker(deps.Access, deps.GlobalAccess, deps.AccessLists, deps.Logger),
		jobs:           newJobs(),
		projectLocks:   newKeyedMutex(),
		logger:         deps.Logger,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Use RoutePrefix for route registration (empty when proxy strips path)
	bp := h.config.RoutePrefix()

	// Static files
	mux.Handle("GET "+bp+"/static/", http.StripPrefix(bp+"/static/", http.FileServerFS(h.staticFS)))

	// Public pages
	mux.HandleFunc("GET "+bp+"/{$}", h.withSession(h.handleFrontpage))
	mux.HandleFunc("GET "+bp+"/login", h.withSession(h.handleLoginPage))
	mux.HandleFunc("POST "+bp+"/login", h.withRateLimit(h.loginLimiter, h.withSession(h.handleLoginSubmit)))
	mux.HandleFunc("GET "+bp+"/logout", h.withSession(h.handleLogout))
	mux.HandleFunc("GET "+bp+"/licenses", h.withSession(h.handleLicenses))
	mux.HandleFunc("GET "+bp+"/auth/oauth2", h.handleOAuth2Login)
	mux.HandleFunc("GET "+bp+"/auth/callback", h.withSession(h.handleOAuth2Callback))

	// Project pages
	mux.HandleFunc("GET "+bp+"/project/{slug}", h.withSession(h.handleProjectDetail))
	// Stable "latest" permalink: redirects to the current newest version's
	// docs so the URL can be shared/linked without pinning a version. The
	// literal "latest" segment is more specific than {version}, so these win
	// over the generic doc route below for that path.
	mux.HandleFunc("GET "+bp+"/project/{slug}/latest", h.withSession(h.handleLatestSlashRedirect))
	mux.HandleFunc("GET "+bp+"/project/{slug}/latest/{path...}", h.withSession(h.handleServeLatest))
	mux.HandleFunc("GET "+bp+"/project/{slug}/{version}/{path...}", h.withSession(h.handleServeDoc))
	mux.HandleFunc("GET "+bp+"/project/{slug}/upload", h.withSession(h.requireAuth(h.handleUploadForm)))
	mux.HandleFunc("POST "+bp+"/project/{slug}/upload", h.withSession(h.requireAuth(h.handleUploadSubmit)))
	mux.HandleFunc("POST "+bp+"/project/{slug}/version/{tag}/delete", h.withSession(h.requireAuth(h.requireCSRF(h.handleDeleteVersion))))
	mux.HandleFunc("POST "+bp+"/project/{slug}/version/{tag}/pin", h.withSession(h.requireAuth(h.requireCSRF(h.handlePinVersion))))
	mux.HandleFunc("POST "+bp+"/project/{slug}/unpin", h.withSession(h.requireAuth(h.requireCSRF(h.handleUnpinVersion))))
	mux.HandleFunc("GET "+bp+"/project/{slug}/version/{tag}/download", h.withSession(h.handleDownloadVersion))

	// Project token management (for editors)
	mux.HandleFunc("GET "+bp+"/project/{slug}/tokens", h.withSession(h.requireAuth(h.handleProjectTokens)))
	mux.HandleFunc("POST "+bp+"/project/{slug}/tokens", h.withSession(h.requireAuth(h.requireCSRF(h.handleProjectCreateToken))))
	mux.HandleFunc("POST "+bp+"/project/{slug}/tokens/{id}/revoke", h.withSession(h.requireAuth(h.requireCSRF(h.handleProjectRevokeToken))))

	// Search
	mux.HandleFunc("GET "+bp+"/search", h.withSession(h.handleSearchPage))
	mux.HandleFunc("GET "+bp+"/api/search", h.withSession(h.handleAPISearch))

	// API endpoints
	mux.HandleFunc("GET "+bp+"/api/projects", h.withSession(h.handleAPIProjects))
	mux.HandleFunc("POST "+bp+"/api/projects", h.handleAPICreateProject)
	mux.HandleFunc("GET "+bp+"/api/project/{slug}/versions", h.withSession(h.handleAPIVersions))
	mux.HandleFunc("POST "+bp+"/api/project/{slug}/upload", h.handleAPIUpload)
	mux.HandleFunc("POST "+bp+"/api/upload", h.handleAPIUploadGeneral)

	// Profile routes
	mux.HandleFunc("GET "+bp+"/profile", h.withSession(h.requireAuth(h.handleProfilePage)))
	mux.HandleFunc("POST "+bp+"/profile/password", h.withSession(h.requireAuth(h.requireCSRF(h.handleChangePassword))))

	// Admin routes (project list + create accessible to editors)
	mux.HandleFunc("GET "+bp+"/admin/projects", h.withSession(h.requireEditorOrAdmin(h.handleAdminProjects)))
	mux.HandleFunc("POST "+bp+"/admin/projects", h.withSession(h.requireEditorOrAdmin(h.requireCSRF(h.handleAdminCreateProject))))
	// Edit / delete / access management are open to editors but each handler
	// gates on access.Checker.CanManage so a non-admin can only touch projects
	// they created (not every project they can merely see or upload to).
	mux.HandleFunc("GET "+bp+"/admin/projects/{slug}/edit", h.withSession(h.requireEditorOrAdmin(h.handleAdminEditProject)))
	mux.HandleFunc("POST "+bp+"/admin/projects/{slug}/edit", h.withSession(h.requireEditorOrAdmin(h.requireCSRF(h.handleAdminUpdateProject))))
	mux.HandleFunc("POST "+bp+"/admin/projects/{slug}/delete", h.withSession(h.requireEditorOrAdmin(h.requireCSRF(h.handleAdminDeleteProject))))
	mux.HandleFunc("POST "+bp+"/admin/projects/{slug}/access/grant", h.withSession(h.requireEditorOrAdmin(h.requireCSRF(h.handleAdminGrantAccess))))
	mux.HandleFunc("POST "+bp+"/admin/projects/{slug}/access/revoke", h.withSession(h.requireEditorOrAdmin(h.requireCSRF(h.handleAdminRevokeAccess))))
	mux.HandleFunc("GET "+bp+"/admin/users", h.withSession(h.requireAdmin(h.handleAdminUsers)))
	mux.HandleFunc("POST "+bp+"/admin/users", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminCreateUser))))
	mux.HandleFunc("POST "+bp+"/admin/users/{id}/delete", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminDeleteUser))))
	mux.HandleFunc("POST "+bp+"/admin/users/{id}/role", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminUpdateUserRole))))
	mux.HandleFunc("POST "+bp+"/admin/users/{id}/password", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminResetPassword))))
	mux.HandleFunc("GET "+bp+"/admin/robots", h.withSession(h.requireAdmin(h.handleAdminRobots)))
	mux.HandleFunc("POST "+bp+"/admin/robots", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminCreateRobot))))
	mux.HandleFunc("POST "+bp+"/admin/robots/{id}/tokens", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminGenerateToken))))
	mux.HandleFunc("POST "+bp+"/admin/robots/{id}/tokens/{tid}/revoke", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminRevokeToken))))
	mux.HandleFunc("POST "+bp+"/admin/robots/{id}/delete", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminDeleteRobot))))
	mux.HandleFunc("POST "+bp+"/admin/reindex", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminReindex))))
	mux.HandleFunc("GET "+bp+"/admin/groups", h.withSession(h.requireAdmin(h.handleAdminGroups)))
	mux.HandleFunc("POST "+bp+"/admin/groups", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminCreateGroupMapping))))
	mux.HandleFunc("POST "+bp+"/admin/groups/{id}/delete", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminDeleteGroupMapping))))
	mux.HandleFunc("GET "+bp+"/admin/global-access", h.withSession(h.requireAdmin(h.handleAdminGlobalAccess)))
	mux.HandleFunc("POST "+bp+"/admin/global-access", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminCreateGlobalAccessRule))))
	mux.HandleFunc("POST "+bp+"/admin/global-access/{id}/delete", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminDeleteGlobalAccessRule))))
	mux.HandleFunc("POST "+bp+"/admin/deploy-docs", h.withSession(h.requireAdmin(h.requireCSRF(h.handleAdminDeployBuiltinDocs))))

	// Health check (keep at root for load balancer compatibility, but also at base path)
	mux.HandleFunc("GET "+bp+"/healthz", h.handleHealthz)
	if bp != "" {
		mux.HandleFunc("GET /healthz", h.handleHealthz)
		// Redirect root to base path for convenience when routes are prefixed
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, h.config.Server.BasePath+"/", http.StatusFound)
		})
	}
}

// render writes a template to w. CSRFToken is injected into the data map
// so every form template can render its hidden field without each handler
// remembering to pass it. data may be nil.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	if _, present := data["CSRFToken"]; !present {
		data["CSRFToken"] = h.sessionMgr.CSRFToken(r)
	}
	if err := h.templates.Render(w, name, data); err != nil {
		h.logger.Error("template render error", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// redirect performs an HTTP redirect with the base path prepended to the path.
func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, path string, code int) {
	http.Redirect(w, r, h.config.Server.BasePath+path, code)
}
