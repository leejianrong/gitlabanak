package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
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
	store, err := gitstore.New(branch)
	if err != nil {
		return err
	}

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", env("LISTEN_ADDR", ":8080"))
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           api.New(token, &api.Project{ID: id, DefaultBranch: branch, Store: store}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("listening", "addr", ln.Addr().String(), "project", id, "branch", branch)
	return srv.Serve(ln)
}

type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
