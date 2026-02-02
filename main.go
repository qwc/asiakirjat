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
	"syscall"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
	"github.com/qwc/asiakirjat/internal/handler"
	"github.com/qwc/asiakirjat/internal/store"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/templates"
)

//go:embed static
var staticFiles embed.FS

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("loading config", "error", err)
		os.Exit(1)
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

	// Initialize storage
	storage := docs.NewFilesystemStorage(cfg.Storage.BasePath)

	// Ensure storage directory exists
	os.MkdirAll(cfg.Storage.BasePath, 0755)

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
	if cfg.Auth.LDAP.Enabled {
		if err := auth.ValidateLDAPConfig(cfg.Auth.LDAP); err != nil {
			logger.Error("invalid LDAP config", "error", err)
			os.Exit(1)
		}
		ldapAuth := auth.NewLDAPAuthenticator(cfg.Auth.LDAP, userStore, logger)
		authenticators = append(authenticators, ldapAuth)
		logger.Info("LDAP authentication enabled", "url", cfg.Auth.LDAP.URL)
	}

	// Create initial admin user if no users exist
	ensureInitialAdmin(logger, userStore, cfg)

	// Initialize templates
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
		Authenticators: authenticators,
		SessionMgr:     sessionMgr,
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
