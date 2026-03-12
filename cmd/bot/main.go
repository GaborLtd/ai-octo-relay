package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/match/ai-octo-relay/internal/agent"
	"github.com/match/ai-octo-relay/internal/app"
	"github.com/match/ai-octo-relay/internal/config"
	"github.com/match/ai-octo-relay/internal/logx"
	"github.com/match/ai-octo-relay/internal/project"
	"github.com/match/ai-octo-relay/internal/slackbot"
	"github.com/match/ai-octo-relay/internal/store"
)

func main() {
	configPath := flag.String("config", "./config.json", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		panic("load config: " + err.Error())
	}
	logger := logx.New(cfg.LogLevel)
	logger.Infof("starting ai-octo-relay with config=%s log_level=%s", *configPath, cfg.LogLevel)

	stateStore, err := store.OpenStateStore(cfg.StateStore)
	if err != nil {
		logger.Errorf("create state store: %v", err)
		os.Exit(1)
	}

	eventStore, err := store.OpenEventStore(cfg.EventStore)
	if err != nil {
		logger.Errorf("create event store: %v", err)
		os.Exit(1)
	}

	registry, err := project.NewRegistry(cfg.Projects)
	if err != nil {
		logger.Errorf("create project registry: %v", err)
		os.Exit(1)
	}

	runners, err := agent.NewRegistry(cfg.Agents, logger)
	if err != nil {
		logger.Errorf("create agent registry: %v", err)
		os.Exit(1)
	}
	if err := runners.ValidateCommands(); err != nil {
		logger.Errorf("validate agent commands: %v", err)
		os.Exit(1)
	}

	service := app.NewService(cfg, registry, runners, stateStore, eventStore)
	bot, err := slackbot.New(cfg, service, logger)
	if err != nil {
		logger.Errorf("create slack bot: %v", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := bot.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Errorf("run slack bot: %v", err)
		os.Exit(1)
	}
}
