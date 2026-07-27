// Package handler exposes the application over HTTP using gin.
package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/config"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
)

// Pinger reports whether a backing service is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HTTPHandlers groups the resource handlers the server routes to.
type HTTPHandlers struct {
	Company      *HTTPCompanyHandler
	User         *HTTPUserHandler
	Membership   *HTTPMembershipHandler
	Onboarding   *HTTPOnboardingHandler
	Verification *HTTPVerificationHandler
	Report       *HTTPReportHandler
}

// HTTPServer owns the gin engine and the underlying http.Server, and bridges
// between gin and the application services.
type HTTPServer struct {
	server          *http.Server
	router          *gin.Engine
	logger          logger.Logger
	db              Pinger
	handlers        HTTPHandlers
	shutdownTimeout time.Duration
}

// NewHTTPServer builds the server and registers every route. Routes are wired
// here rather than in an exported method the caller has to remember to call.
func NewHTTPServer(cfg config.Server, log logger.Logger, db Pinger, handlers HTTPHandlers) *HTTPServer {
	gin.SetMode(ginMode(cfg.Mode))

	router := gin.New()
	router.Use(gin.Recovery(), requestLogger(log))

	s := &HTTPServer{
		router:          router,
		logger:          log,
		db:              db,
		handlers:        handlers,
		shutdownTimeout: cfg.ShutdownTimeout,
		server: &http.Server{
			Addr:              cfg.Addr(),
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	s.setupRoutes()

	return s
}

// setupRoutes registers every route. The comments record which transaction
// boundary each one ends up using — the whole map of the demo, in one place.
func (s *HTTPServer) setupRoutes() {
	s.router.GET("/healthz", s.health)

	api := s.router.Group("/api")

	// Plain CRUD. One statement each, so no transaction at all.
	companies := api.Group("/companies")
	{
		companies.POST("", s.handlers.Company.create)
		companies.GET("", s.handlers.Company.list)
		companies.GET("/:id", s.handlers.Company.get)
		companies.PUT("/:id", s.handlers.Company.update)
		companies.DELETE("/:id", s.handlers.Company.delete)

		// ✅ READ ONLY transaction: three reads that must agree.
		companies.GET("/:id/report", s.handlers.Report.get)

		// ✅ SERIALIZABLE with retry: the seat limit is decided from a count.
		companies.PUT("/:id/members/:userId", s.handlers.Membership.add)
		companies.DELETE("/:id/members/:userId", s.handlers.Membership.remove)

		// ❌ and ✅ — the same operation, written two ways.
		companies.POST("/:id/verify-bad", s.handlers.Verification.bad)
		companies.POST("/:id/verify-good", s.handlers.Verification.good)
	}

	users := api.Group("/users")
	{
		users.POST("", s.handlers.User.create)
		users.GET("", s.handlers.User.list)
		users.GET("/:id", s.handlers.User.get)
		users.PUT("/:id", s.handlers.User.update)
		users.DELETE("/:id", s.handlers.User.delete)
	}

	// ✅ Read-write transaction: one invariant spanning three tables.
	api.POST("/onboarding", s.handlers.Onboarding.execute)
}

// Run serves until ctx is cancelled, then drains in-flight requests.
func (s *HTTPServer) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		s.logger.InfoContext(ctx, "HTTP server listening", "addr", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("shutting down HTTP server", "timeout", s.shutdownTimeout)

		// Deliberately detached from ctx: ctx is already cancelled, and the
		// point of this context is to give in-flight requests time to finish.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
		defer cancel()

		if err := s.server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	}
}

// health reports whether the service can still reach its database.
func (s *HTTPServer) health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.Ping(ctx); err != nil {
		s.logger.ErrorContext(ctx, "health check failed", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// requestLogger emits one structured record per request through our logger,
// instead of gin's own line-oriented format.
func requestLogger(log logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		log.InfoContext(c.Request.Context(), "request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration", time.Since(start),
		)
	}
}

func ginMode(mode string) string {
	switch mode {
	case gin.ReleaseMode, gin.TestMode, gin.DebugMode:
		return mode
	default:
		return gin.ReleaseMode
	}
}
