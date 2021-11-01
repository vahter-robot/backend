package main

import (
	"context"
	"errors"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	graceful "github.com/leaq-ru/lib-graceful"
	"github.com/vahter-robot/backend/pkg/child_bot"
	"github.com/vahter-robot/backend/pkg/child_state"
	"github.com/vahter-robot/backend/pkg/config"
	"github.com/vahter-robot/backend/pkg/logger"
	"github.com/vahter-robot/backend/pkg/mongo"
	"github.com/vahter-robot/backend/pkg/parent_bot"
	"github.com/vahter-robot/backend/pkg/parent_state"
	"github.com/vahter-robot/backend/pkg/peer"
	"github.com/vahter-robot/backend/pkg/user"
	"golang.org/x/sync/errgroup"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	cfg, err := config.NewConfig()
	if err != nil {
		panic(err)
	}

	logg, err := logger.NewLogger(cfg.LogLevel)
	if err != nil {
		panic(err)
	}

	parentBot, err := tgbotapi.NewBotAPI(cfg.ParentBot.Token)
	if err != nil {
		panic(err)
	}

	db, err := mongo.NewConn(ctx, cfg.Service, cfg.MongoDB.URL)
	if err != nil {
		panic(err)
	}

	parentStateRepo, err := parent_state.NewRepo(ctx, db)
	if err != nil {
		panic(err)
	}

	userRepo, err := user.NewRepo(ctx, db)
	if err != nil {
		panic(err)
	}

	childBotRepo, err := child_bot.NewRepo(ctx, db)
	if err != nil {
		panic(err)
	}

	childStateRepo, err := child_state.NewRepo(ctx, db)
	if err != nil {
		panic(err)
	}

	peerRepo, err := peer.NewRepo(ctx, db)
	if err != nil {
		panic(err)
	}

	parentBotService, err := parent_bot.NewService(
		logg,
		cfg.ParentBot.Host,
		cfg.ParentBot.Port,
		cfg.ParentBot.TokenPathPrefix,
		cfg.ParentBot.Token,
		parentStateRepo,
		userRepo,
		peerRepo,
		childBotRepo,
		cfg.ChildBot.Host,
		cfg.ChildBot.TokenPathPrefix,
		cfg.ChildBot.BotsLimitPerUser,
	)
	if err != nil {
		panic(err)
	}

	childBotService := child_bot.NewService(
		logg,
		cfg.ChildBot.Host,
		cfg.ChildBot.Port,
		cfg.ChildBot.TokenPathPrefix,
		childStateRepo,
		userRepo,
		peerRepo,
		childBotRepo,
		cfg.ChildBot.KeywordsLimitPerBot,
		cfg.ChildBot.InLimitPerKeyword,
		cfg.ChildBot.InLimitChars,
		cfg.ChildBot.OutLimitChars,
		parentBot.Self.UserName,
	)

	go graceful.HandleSignals(cancel)
	eg, egc := errgroup.WithContext(ctx)
	eg.Go(func() error {
		parentBotService.Serve(egc)
		return nil
	})
	eg.Go(func() error {
		e := childBotService.Serve(egc, cfg.SetWebhooksOnStart)
		if e != nil {
			return fmt.Errorf("childBotService.Serve: %w", e)
		}
		return nil
	})
	err = eg.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		panic(err)
	}
}
