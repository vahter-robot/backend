package peer

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"time"
)

type Peer struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	ChildBotID primitive.ObjectID `bson:"cbi,omitempty"`
	TgUserID   int64              `bson:"tui,omitempty"`
	TgChatID   int64              `bson:"tci,omitempty"`
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
			Key:   "cbi",
			Value: 1,
		}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("r.coll.Indexes().CreateOne: %w", err)
	}

	return nil
}

func (r *Repo) Create(c context.Context, childBotID primitive.ObjectID, tgUserID, tgChatID int64) error {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	_, err := r.coll.UpdateOne(ctx, bson.M{
		"cbi": childBotID,
		"tui": tgUserID,
	}, bson.M{
		"$set": bson.M{
			"tci": tgChatID,
		},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("r.coll.UpdateOne: %w", err)
	}

	return nil
}

func (r *Repo) CreateMuted(c context.Context, childBotID primitive.ObjectID, tgUserID, tgChatID int64) error {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	_, err := r.coll.UpdateOne(ctx, bson.M{
		"cbi": childBotID,
		"tui": tgUserID,
	}, bson.M{
		"$set": bson.M{
			"tci": tgChatID,
			"m":   true,
		},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("r.coll.UpdateOne: %w", err)
	}

	return nil
}

func (r *Repo) IsMuted(c context.Context, childBotID primitive.ObjectID, tgUserID int64) (bool, error) {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	var p Peer
	err := r.coll.FindOne(ctx, bson.M{
		"cbi": childBotID,
		"tui": tgUserID,
	}, options.FindOne().SetProjection(bson.M{
		"m": 1,
	})).Decode(&p)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, nil
		}

		return false, fmt.Errorf("r.coll.FindOne: %w", err)
	}

	return p.Muted, nil
}
