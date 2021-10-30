package main

import (
	"context"
	"errors"
	graceful "github.com/leaq-ru/lib-graceful"
	"github.com/vahter-robot/backend/pkg/config"
	"github.com/vahter-robot/backend/pkg/mongo"
	"golang.org/x/sync/errgroup"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	cfg, err := config.NewConfig()
	if err != nil {
		panic(err)
	}

	db, err := mongo.NewConn(ctx, cfg.Service, cfg.MongoDB.URL)
	if err != nil {
		panic(err)
	}

	var eg errgroup.Group
	eg.Go(func() error {
		graceful.HandleSignals(cancel)
		return nil
	})
	eg.Go(func() error {

	})
	err = eg.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		panic(err)
	}
}
