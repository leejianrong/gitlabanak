package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/leejianrong/gitlabanak/internal/api"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var level slog.Level
	_ = level.UnmarshalText([]byte(env("LOG_LEVEL", "info")))
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	token := os.Getenv("GITLAB_PRIVATE_TOKEN")
	if token == "" {
		return &configError{"GITLAB_PRIVATE_TOKEN is required"}
	}
	id, err := strconv.Atoi(env("GITLAB_DEFAULT_PROJECT_ID", "100"))
	if err != nil {
		return &configError{"GITLAB_DEFAULT_PROJECT_ID must be an integer"}
	}
	branch := env("GITLAB_DEFAULT_BRANCH", "main")
	cfg := api.Config{
		Token:    token,
		Default:  api.ProjectSpec{ID: id, DefaultBranch: branch},
		NewStore: func(_ int, branch string) (*gitstore.Store, error) { return gitstore.New(branch) },
	}
	if dir := os.Getenv("GITLAB_DATA_DIR"); dir != "" {
		if err := persist(&cfg, filepath.Join(dir, "projects")); err != nil {
			return err
		}
	}
	handler, err := api.New(cfg)
	if err != nil {
		return err
	}

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", env("LISTEN_ADDR", ":8080"))
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("listening", "addr", ln.Addr().String(), "project", id, "branch", branch)
	return srv.Serve(ln)
}

// persist keeps every project as a bare git repository under root, and reopens the ones
// already there so a restart loses nothing.
func persist(cfg *api.Config, root string) error {
	path := func(id int) string { return filepath.Join(root, strconv.Itoa(id)+".git") }
	cfg.NewStore = func(id int, branch string) (*gitstore.Store, error) { return gitstore.Open(path(id), branch) }
	cfg.WipeAll = func() error { return os.RemoveAll(root) }

	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		id, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".git"))
		if err != nil || id == cfg.Default.ID {
			continue
		}
		st, err := gitstore.Open(path(id), "")
		if err != nil {
			return err
		}
		b, err := st.DefaultBranch()
		if err != nil {
			return err
		}
		cfg.Extra = append(cfg.Extra, &api.Project{ID: id, DefaultBranch: b, Store: st})
	}
	return nil
}

type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
