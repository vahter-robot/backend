package reply

import (
	"context"
	"fmt"
	m "github.com/vahter-robot/backend/pkg/mongo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"time"
)

type Reply struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	ChildBotID  primitive.ObjectID `bson:"cbi,omitempty"`
	TgUserID    int64              `bson:"tui,omitempty"`
	TgChatID    int64              `bson:"tci,omitempty"`
	TgMessageID int64              `bson:"tmi,omitempty"`
}

type Repo struct {
	coll *mongo.Collection
}

func NewRepo(ctx context.Context, db *mongo.Database) (*Repo, error) {
	r := &Repo{
		coll: db.Collection("replies"),
	}

	err := r.createIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("r.createIndex: %w", err)
	}

	return r, nil
}

func (r *Repo) createIndex(ctx context.Context) error {
	_, err := r.coll.Indexes().CreateMany(ctx, []mongo.IndexModel{{
		Keys: bson.D{{
			Key:   "tui",
			Value: 1,
		}, {
			Key:   "tci",
			Value: 1,
		}, {
			Key:   "tmi",
			Value: 1,
		}, {
			Key:   "cbi",
			Value: 1,
		}},
		Options: options.Index().SetUnique(true),
	}, {
		Keys: bson.M{
			"cbi": 1,
		},
	}})
	if err != nil {
		return fmt.Errorf("r.coll.Indexes().CreateMany: %w", err)
	}

	return nil
}

func (r *Repo) Create(
	c context.Context,
	childBotID primitive.ObjectID,
	tgUserID,
	tgChatID,
	tgMessageID int64,
) (
	primitive.ObjectID,
	error,
) {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	z := primitive.ObjectID{}

	ur, err := r.coll.UpdateOne(ctx, bson.M{
		"tui": tgUserID,
		"tci": tgChatID,
		"tmi": tgMessageID,
		"cbi": childBotID,
	}, bson.M{
		"$setOnInsert": bson.M{
			"tui": tgUserID,
			"tci": tgChatID,
			"tmi": tgMessageID,
			"cbi": childBotID,
		},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return z, fmt.Errorf("r.coll.UpdateOne: %w", err)
	}

	id, ok := ur.UpsertedID.(primitive.ObjectID)
	if ok {
		return id, nil
	}

	var doc m.Doc
	err = r.coll.FindOne(ctx, bson.M{
		"tui": tgUserID,
		"tci": tgChatID,
		"tmi": tgMessageID,
		"cbi": childBotID,
	}, options.FindOne().SetProjection(bson.M{
		"_id": 1,
	})).Decode(&doc)
	if err != nil {
		return z, fmt.Errorf("r.coll.FindOne: %w", err)
	}

	return doc.ID, nil
}

func (r *Repo) GetByID(c context.Context, id primitive.ObjectID) (Reply, error) {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	var reply Reply
	err := r.coll.FindOne(ctx, bson.M{
		"_id": id,
	}).Decode(&reply)
	if err != nil {
		return Reply{}, fmt.Errorf("r.coll.FindOne: %w", err)
	}

	return reply, nil
}

func (r *Repo) DeleteByChildBotID(c context.Context, childBotID primitive.ObjectID) error {
	_, err := r.coll.DeleteMany(c, bson.M{
		"cbi": childBotID,
	})
	if err != nil {
		return fmt.Errorf("r.coll.DeleteMany: %w", err)
	}

	return nil
}
