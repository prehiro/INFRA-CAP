package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"infracap/internal/config"
	"infracap/internal/db"
	"infracap/internal/migrations"
	"infracap/internal/auth"
	"infracap/internal/modules/audit"
	"infracap/internal/modules/dashboard"
	"infracap/internal/modules/licenses"
	"infracap/internal/modules/notes"
	"infracap/internal/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// serverMode lets the Windows-service wrapper (Fase 4) reuse run() in-process.
var serverMode = struct{ foreground bool }{foreground: true}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	database, err := db.Open(cfg.DBDSN)
	if err != nil {
		return err
	}
	defer database.Close()

	if cfg.AutoMigrate {
		migFS := os.DirFS("migrations")
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := migrations.Run(ctx, database, migFS); err != nil {
			return err
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := database.PingContext(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"db": "error"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"db": "ok"})
	})

	mux.Handle("GET /static/", http.StripPrefix("/static/",
		http.FileServer(http.Dir("web/static"))))

	authSvc := &auth.Service{DB: database}
	if cfg.AutoMigrate {
		authSvc.SeedFirstAdmin(context.Background())
	}
	authMod := auth.NewModule(authSvc)
	usersMod := auth.NewAdminUsersModule(authSvc)
	permStore := &auth.PermissionStore{DB: database}

	// public routes
	authMod.RegisterRoutes(mux)

	// protected routes (auth required). Each module mux is wrapped with
	// a per-page permission check (and the users/audit modules keep
	// their existing admin-only gate on top of that). The layout
	// template also filters the sidebar nav using PageAccessForRole
	// so non-admins never see menu items they can't open.
	protected := http.NewServeMux()
	licStore := &licenses.Store{DB: database}
	dash := dashboard.NewWithStore(licStore)
	dashMux := http.NewServeMux()
	dash.RegisterRoutes(dashMux)
	protected.Handle("/",
		web.CSRFValidate(authSvc.Middleware(true, permStore)(auth.RequirePageAccess(permStore, auth.PageDashboard)(dashMux))))

	usersMux := http.NewServeMux()
	usersMod.RegisterRoutes(usersMux)
	protected.Handle("/users",
		web.CSRFValidate(authSvc.Middleware(true, permStore)(auth.RequireRole("admin")(auth.RequirePageAccess(permStore, auth.PageUsers)(usersMux)))))
	protected.Handle("/users/",
		web.CSRFValidate(authSvc.Middleware(true, permStore)(auth.RequireRole("admin")(auth.RequirePageAccess(permStore, auth.PageUsers)(usersMux)))))

	licMux := http.NewServeMux()
	licMod := licenses.New(licStore)
	licMod.RegisterRoutes(licMux)
	protected.Handle("/licenses",
		web.CSRFValidate(authSvc.Middleware(true, permStore)(auth.RequirePageAccess(permStore, auth.PageLicenses)(licMux))))
	protected.Handle("/licenses/",
		web.CSRFValidate(authSvc.Middleware(true, permStore)(auth.RequirePageAccess(permStore, auth.PageLicenses)(licMux))))

	notesMux := http.NewServeMux()
	notesMod := notes.New(&notes.Store{DB: database})
	notesMod.RegisterRoutes(notesMux)
	protected.Handle("/notes", web.CSRFValidate(authSvc.Middleware(true, permStore)(auth.RequirePageAccess(permStore, auth.PageNotes)(notesMux))))
	protected.Handle("/notes/", web.CSRFValidate(authSvc.Middleware(true, permStore)(auth.RequirePageAccess(permStore, auth.PageNotes)(notesMux))))

	// Admin Permissions page (admin only) — manage role page access
	permsMux := http.NewServeMux()
	permsMod := auth.NewPermissionsModule(permStore, authSvc)
	permsMod.RegisterRoutes(permsMux)
	protected.Handle("/admin/permissions",
		web.CSRFValidate(authSvc.Middleware(true, permStore)(auth.RequireRole("admin")(permsMux))))
	protected.Handle("/admin/permissions/",
		web.CSRFValidate(authSvc.Middleware(true, permStore)(auth.RequireRole("admin")(permsMux))))

	// Audit Trail (admin only) — Fase 2b
	auditMux := http.NewServeMux()
	auditMod := audit.New(&audit.Store{DB: database})
	auditMod.RegisterRoutes(auditMux)
	protected.Handle("/audit/",
		web.CSRFValidate(authSvc.Middleware(true, permStore)(auth.RequireRole("admin")(auth.RequirePageAccess(permStore, auth.PageAudit)(auditMux)))))

	mux.Handle("/", web.CSRFValidate(authSvc.Middleware(true, permStore)(protected)))

	handler := web.RequestLogger(web.CSRFCookieMiddleware(web.SecurityHeaders(mux)))
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("INFRA-CAP listening on %s", cfg.Addr)
		errCh <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-stop:
		log.Println("shutting down gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
	return nil
}
