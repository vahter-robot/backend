package mongo

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
	"time"
)

func NewConn(ctx context.Context, service string, url string) (*mongo.Database, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := mongo.Connect(ctx, options.Client().
		SetWriteConcern(writeconcern.New(
			writeconcern.WMajority(),
			writeconcern.J(true),
		)).
		SetReadConcern(readconcern.Majority()).
		SetReadPreference(readpref.SecondaryPreferred()).
		ApplyURI(url))
	if err != nil {
		return nil, fmt.Errorf("mongo.Connect: %w", err)
	}

	err = conn.Ping(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("conn.Ping: %w", err)
	}

	return conn.Database("vahter_robot_" + service), nil
}

type Doc struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`
}
