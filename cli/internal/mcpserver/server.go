package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/env"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/projection"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	mcp  *mcp.Server
	root string
	cfg  *config.Config

	mu      sync.Mutex
	session *activeSession
}

type activeSession struct {
	runtime    *gafferruntime.Session
	history    *history.Store
	info       projection.Info
	name       string
	stats      sessionStats
	partitions map[string]bool
	cancel     context.CancelFunc
	lastError  error

	// Debug state - set when paused at a breakpoint
	paused      bool
	feedDone    chan feedOutcome
	pausedEvent string
}

type sessionStats struct {
	Processed int64  `json:"processed"`
	Skipped   int64  `json:"skipped"`
	Errors    int64  `json:"errors"`
	Status    string `json:"status"`
}

func New(root string, cfg *config.Config) *Server {
	s := &Server{
		root: root,
		cfg:  cfg,
	}

	s.mcp = mcp.NewServer(
		&mcp.Implementation{
			Name:    "gaffer",
			Version: "0.1.0",
		},
		&mcp.ServerOptions{
			Instructions: "Gaffer is a projection toolkit for KurrentDB. " +
				"Read the projection-api and gotchas resources before writing projections. " +
				"Workflow: list_projections to see what exists, scaffold to create new ones, " +
				"run with fixture events to test, get_timeline/get_step to inspect results, " +
				"debug with break_at to pause and evaluate expressions. " +
				"Each run replaces the previous session.",
		},
	)

	s.registerTools()
	s.registerResources()
	s.registerPrompts()

	return s
}

func NewFromProjectRoot() (*Server, error) {
	root := project.FindRoot()
	if root == "" {
		return nil, fmt.Errorf("not in a gaffer project (no gaffer.toml found)")
	}

	cfg, err := config.Load(filepath.Join(root, "gaffer.toml"))
	if err != nil {
		return nil, err
	}

	return New(root, cfg), nil
}

func (s *Server) Run(ctx context.Context) error {
	err := s.mcp.Run(ctx, &mcp.StdioTransport{})
	s.mu.Lock()
	s.closeSession()
	s.mu.Unlock()
	return err
}

func (s *Server) connectToKurrentDB() (*kurrentdb.Client, error) {
	if s.cfg.Connection == "" {
		return nil, fmt.Errorf("no connection configured in gaffer.toml")
	}

	if err := env.Load(s.root, ""); err != nil {
		return nil, fmt.Errorf("loading .env: %w", err)
	}

	dbConfig, err := kurrentdb.ParseConnectionString(s.cfg.Connection)
	if err != nil {
		return nil, fmt.Errorf("invalid connection string: %w", err)
	}

	username, password := env.Credentials()
	if username != "" {
		dbConfig.Username = username
		dbConfig.Password = password
	}
	dbConfig.Logger = kurrentdb.NoopLogging()

	return kurrentdb.NewClient(dbConfig)
}

func (s *Server) closeSession() {
	if s.session != nil {
		if s.session.cancel != nil {
			s.session.cancel()
		}
		if s.session.paused {
			s.session.runtime.ClearBreakpoints()
			s.session.runtime.Continue()
			if s.session.feedDone != nil {
				<-s.session.feedDone
			}
			s.session.paused = false
		}
		s.session.runtime.Destroy()
		_ = s.session.history.Close()
		s.session = nil
	}
}

func (s *Server) createSession(name string, debug bool) (*activeSession, error) {
	s.closeSession()

	proj := s.cfg.FindProjection(name)
	if proj == nil {
		return nil, fmt.Errorf("projection %q not found in gaffer.toml", name)
	}

	source, err := os.ReadFile(filepath.Join(s.root, proj.Entry))
	if err != nil {
		return nil, fmt.Errorf("reading projection source: %w", err)
	}

	opts := projection.BuildSessionOptions(s.cfg, proj, debug)
	runtime, err := gafferruntime.NewSession(string(source), opts)
	if err != nil {
		return nil, err
	}

	store, err := history.New()
	if err != nil {
		runtime.Destroy()
		return nil, fmt.Errorf("creating history store: %w", err)
	}

	info := projection.GetInfo(runtime)

	status := "ready"
	if debug {
		status = "debugging"
	}

	s.session = &activeSession{
		runtime:    runtime,
		history:    store,
		info:       info,
		name:       name,
		partitions: make(map[string]bool),
		stats:      sessionStats{Status: status},
	}

	return s.session, nil
}
