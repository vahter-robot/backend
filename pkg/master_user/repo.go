package master_user

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"time"
)

type User struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	TgUserID int64              `bson:"tui,omitempty"`
}

type Repo struct {
	coll *mongo.Collection
}

func NewRepo(ctx context.Context, db *mongo.Database, createIndex bool) (*Repo, error) {
	r := &Repo{
		coll: db.Collection("master_users"),
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
		Keys: bson.M{
			"ti": 1,
		},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("r.coll.Indexes().CreateOne: %w", err)
	}

	return nil
}

func (r *Repo) Create(
	c context.Context,
	tgID int64,
) (
	User,
	error,
) {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	z := User{}

	_, err := r.coll.UpdateOne(ctx, bson.M{
		"ti": tgID,
	}, bson.M{
		"$setOnInsert": User{
			TgUserID: tgID,
		},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return z, fmt.Errorf("r.coll.UpdateOne: %w", err)
	}

	var usr User
	err = r.coll.FindOne(ctx, bson.M{
		"ti": tgID,
	}).Decode(&usr)
	if err != nil {
		return z, fmt.Errorf("r.coll.FindOne: %w", err)
	}

	return usr, nil
}
