package peer

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Peer struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	SlaveBotID primitive.ObjectID `bson:"sbi,omitempty"`
	TgUserID   int64              `bson:"tui,omitempty"`
	Muted      bool               `bson:"m,omitempty"`
}

type Repo struct {
	coll *mongo.Collection
}

func NewRepo(ctx context.Context, db *mongo.Database, createIndex bool) (*Repo, error) {
	r := &Repo{
		coll: db.Collection("peers"),
	}

	if createIndex {
		err := r.createIndex(ctx)
		if err != nil {
			return nil, fmt.Errorf("r.createIndex: %w", err)
		}
	}

	return r, nil
}

func (r *Repo) createIndex(ctx context.Context) error {
	_, err := r.coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{
			Key:   "tui",
			Value: 1,
		}, {
			Key:   "sbi",
			Value: 1,
		}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("r.coll.Indexes().CreateOne: %w", err)
	}

	return nil
}
