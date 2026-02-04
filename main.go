package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
	"github.com/qwc/asiakirjat/internal/docs/builtin"
	"github.com/qwc/asiakirjat/internal/handler"
	"github.com/qwc/asiakirjat/internal/store"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/templates"
)

// version is set via ldflags at build time.
var version = "dev"

//go:embed static
var staticFiles embed.FS

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	// Set the version for built-in docs
	builtin.Version = version

	// Load config first so we can use log_level
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	logLevel := slog.LevelInfo
	if cfg.Server.LogLevel != "" {
		switch strings.ToLower(cfg.Server.LogLevel) {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn", "warning":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		default:
			slog.Warn("unknown log_level, defaulting to info", "log_level", cfg.Server.LogLevel)
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Ensure database directory exists (SQLite needs it before opening)
	if dbDir := filepath.Dir(cfg.Database.DSN); dbDir != "" && dbDir != "." {
		os.MkdirAll(dbDir, 0755)
	}

	// Open database
	db, dialect, err := database.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		logger.Error("opening database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Run migrations
	if err := database.RunMigrations(db, dialect); err != nil {
		logger.Error("running migrations", "error", err)
		os.Exit(1)
	}

	// Initialize stores
	projectStore := sqlstore.NewProjectStore(db)
	versionStore := sqlstore.NewVersionStore(db)
	userStore := sqlstore.NewUserStore(db)
	sessionStore := sqlstore.NewSessionStore(db)
	accessStore := sqlstore.NewProjectAccessStore(db)
	tokenStore := sqlstore.NewTokenStore(db)
	groupMappingStore := sqlstore.NewAuthGroupMappingStore(db)
	globalAccessStore := sqlstore.NewGlobalAccessStore(db)

	// Initialize storage
	storage := docs.NewFilesystemStorage(cfg.Storage.BasePath)

	// Ensure storage directory exists
	os.MkdirAll(cfg.Storage.BasePath, 0755)

	// Initialize search index
	searchIndex, err := docs.NewSearchIndex(cfg.Storage.BasePath)
	if err != nil {
		logger.Error("opening search index", "error", err)
		os.Exit(1)
	}
	defer searchIndex.Close()

	// Initialize auth
	sessionMgr := auth.NewSessionManager(
		sessionStore, userStore,
		cfg.Auth.Session.CookieName,
		cfg.Auth.Session.MaxAge,
		cfg.Auth.Session.Secure,
	)

	builtinAuth := auth.NewBuiltinAuthenticator(userStore)
	authenticators := []auth.Authenticator{builtinAuth}

	// Add LDAP authenticator if enabled
	var ldapAuth *auth.LDAPAuthenticator
	if cfg.Auth.LDAP.Enabled {
		if err := auth.ValidateLDAPConfig(cfg.Auth.LDAP); err != nil {
			logger.Error("invalid LDAP config", "error", err)
			os.Exit(1)
		}
		ldapAuth = auth.NewLDAPAuthenticator(cfg.Auth.LDAP, userStore, logger)
		ldapAuth.SetStores(accessStore, groupMappingStore, globalAccessStore)
		authenticators = append(authenticators, ldapAuth)
		logger.Info("LDAP authentication enabled", "url", cfg.Auth.LDAP.URL)

		// Sync LDAP project_groups from config to database
		if len(cfg.Auth.LDAP.ProjectGroups) > 0 {
			if err := syncConfigGroupMappings(context.Background(), logger, projectStore, groupMappingStore, "ldap", cfg.Auth.LDAP.ProjectGroups); err != nil {
				logger.Error("syncing LDAP project groups from config", "error", err)
			}
		}
	}

	// Add OAuth2 authenticator if enabled
	var oauth2Auth *auth.OAuth2Authenticator
	if cfg.Auth.OAuth2.Enabled {
		if err := auth.ValidateOAuth2Config(cfg.Auth.OAuth2); err != nil {
			logger.Error("invalid OAuth2 config", "error", err)
			os.Exit(1)
		}
		oauth2Auth = auth.NewOAuth2Authenticator(cfg.Auth.OAuth2, userStore, logger)
		oauth2Auth.SetStores(accessStore, groupMappingStore, globalAccessStore)
		authenticators = append(authenticators, oauth2Auth)
		logger.Info("OAuth2 authentication enabled")

		// Sync OAuth2 project_groups from config to database
		if len(cfg.Auth.OAuth2.ProjectGroups) > 0 {
			if err := syncConfigGroupMappings(context.Background(), logger, projectStore, groupMappingStore, "oauth2", cfg.Auth.OAuth2.ProjectGroups); err != nil {
				logger.Error("syncing OAuth2 project groups from config", "error", err)
			}
		}
	}

	// Sync global access config (access.private section)
	syncGlobalAccessConfig(context.Background(), logger, globalAccessStore, cfg)

	// Create initial admin user if no users exist
	ensureInitialAdmin(logger, userStore, cfg)

	// Initialize templates
	templates.SetVersion(version)
	templates.SetBasePath(cfg.Server.BasePath)
	templates.SetBranding(templates.Branding{
		AppName:   cfg.Branding.AppName,
		LogoURL:   cfg.Branding.LogoURL,
		CustomCSS: cfg.Branding.CustomCSS,
	})
	tmpl, err := templates.New()
	if err != nil {
		logger.Error("loading templates", "error", err)
		os.Exit(1)
	}

	// Extract static sub-filesystem
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		logger.Error("creating static sub-fs", "error", err)
		os.Exit(1)
	}

	// Initialize handler
	h := handler.New(handler.Deps{
		Config:         cfg,
		Templates:      tmpl,
		Storage:        storage,
		StaticFS:       staticFS,
		Projects:       projectStore,
		Versions:       versionStore,
		Users:          userStore,
		Sessions:       sessionStore,
		Access:         accessStore,
		Tokens:         tokenStore,
		GroupMappings:  groupMappingStore,
		GlobalAccess:   globalAccessStore,
		Authenticators: authenticators,
		OAuth2Auth:     oauth2Auth,
		SessionMgr:     sessionMgr,
		SearchIndex:    searchIndex,
		Logger:         logger,
	})

	// Register routes
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Wrap with middleware
	var httpHandler http.Handler = mux
	httpHandler = handler.LoggingMiddleware(logger, httpHandler)
	httpHandler = handler.RecoveryMiddleware(logger, httpHandler)

	// Start server
	server := &http.Server{
		Addr:    cfg.ListenAddr(),
		Handler: httpHandler,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down server")
		server.Shutdown(context.Background())
	}()

	logger.Info("starting server", "address", cfg.ListenAddr())
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

// syncConfigGroupMappings converts config file group mappings to database records.
func syncConfigGroupMappings(ctx context.Context, logger *slog.Logger, projects store.ProjectStore, groupMappings store.AuthGroupMappingStore, source string, configMappings []config.AuthGroupMapping) error {
	var dbMappings []database.AuthGroupMapping

	for _, cm := range configMappings {
		// Look up project by slug
		project, err := projects.GetBySlug(ctx, cm.Project)
		if err != nil {
			logger.Warn("project not found for group mapping", "source", source, "group", cm.Group, "project", cm.Project, "error", err)
			continue
		}

		role := cm.Role
		if role == "" {
			role = "viewer"
		}
		if role != "viewer" && role != "editor" {
			logger.Warn("invalid role in group mapping, defaulting to viewer", "source", source, "group", cm.Group, "role", cm.Role)
			role = "viewer"
		}

		dbMappings = append(dbMappings, database.AuthGroupMapping{
			GroupIdentifier: cm.Group,
			ProjectID:       project.ID,
			Role:            role,
		})
	}

	if len(dbMappings) > 0 {
		if err := groupMappings.SyncFromConfig(ctx, source, dbMappings); err != nil {
			return err
		}
		logger.Info("synced group mappings from config", "source", source, "count", len(dbMappings))
	}

	return nil
}

// syncGlobalAccessConfig converts access.private config rules to database records
// and resolves user-type rules into direct grants.
func syncGlobalAccessConfig(ctx context.Context, logger *slog.Logger, globalAccess store.GlobalAccessStore, cfg *config.Config) {
	var rules []database.GlobalAccess

	// Viewers
	for _, u := range cfg.Access.Private.Viewers.Users {
		rules = append(rules, database.GlobalAccess{
			SubjectType: "user", SubjectIdentifier: u, Role: "viewer",
		})
	}
	for _, g := range cfg.Access.Private.Viewers.LDAPGroups {
		rules = append(rules, database.GlobalAccess{
			SubjectType: "ldap_group", SubjectIdentifier: g, Role: "viewer",
		})
	}
	for _, g := range cfg.Access.Private.Viewers.OAuth2Groups {
		rules = append(rules, database.GlobalAccess{
			SubjectType: "oauth2_group", SubjectIdentifier: g, Role: "viewer",
		})
	}

	// Editors
	for _, u := range cfg.Access.Private.Editors.Users {
		rules = append(rules, database.GlobalAccess{
			SubjectType: "user", SubjectIdentifier: u, Role: "editor",
		})
	}
	for _, g := range cfg.Access.Private.Editors.LDAPGroups {
		rules = append(rules, database.GlobalAccess{
			SubjectType: "ldap_group", SubjectIdentifier: g, Role: "editor",
		})
	}
	for _, g := range cfg.Access.Private.Editors.OAuth2Groups {
		rules = append(rules, database.GlobalAccess{
			SubjectType: "oauth2_group", SubjectIdentifier: g, Role: "editor",
		})
	}

	if len(rules) > 0 {
		if err := globalAccess.SyncFromConfig(ctx, rules); err != nil {
			logger.Error("syncing global access config", "error", err)
			return
		}
		logger.Info("synced global access config", "rules", len(rules))
	}

	// Resolve user-type rules into direct grants
	for _, rule := range rules {
		if rule.SubjectType == "user" {
			// Direct user rules create manual grants at startup
			// (LDAP/OAuth2 group rules are resolved at login time)
			// We need the user store for this, but we'll handle it via
			// the admin UI and auth sync instead to keep startup simple.
			continue
		}
	}
}

func ensureInitialAdmin(logger *slog.Logger, users store.UserStore, cfg *config.Config) {
	ctx := context.Background()

	count, err := users.Count(ctx)
	if err != nil {
		logger.Error("counting users", "error", err)
		return
	}

	if count > 0 {
		return
	}

	hash, err := auth.HashPassword(cfg.Auth.InitialAdmin.Password)
	if err != nil {
		logger.Error("hashing initial admin password", "error", err)
		return
	}

	admin := &database.User{
		Username:   cfg.Auth.InitialAdmin.Username,
		Email:      "",
		Password:   &hash,
		AuthSource: "builtin",
		Role:       "admin",
	}

	if err := users.Create(ctx, admin); err != nil {
		logger.Error("creating initial admin", "error", err)
		return
	}

	logger.Info("created initial admin user", "username", admin.Username)
}
