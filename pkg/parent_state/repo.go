package parent_state

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

type State struct {
	ID     primitive.ObjectID `bson:"_id,omitempty"`
	UserID primitive.ObjectID `bson:"ui,omitempty"`
	Scene  Scene              `bson:"s,omitempty"`
}

type Scene uint32

const (
	None      Scene = 1
	CreateBot Scene = 2
	DeleteBot Scene = 3
)

type Repo struct {
	coll *mongo.Collection
}

func NewRepo(ctx context.Context, db *mongo.Database) (*Repo, error) {
	r := &Repo{
		coll: db.Collection("parent_state"),
	}

	err := r.createIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("r.createIndex: %w", err)
	}

	return r, nil
}

func (r *Repo) createIndex(ctx context.Context) error {
	_, err := r.coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.M{
			"ui": 1,
		},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("r.coll.Indexes().CreateOne: %w", err)
	}

	return nil
}

func (r *Repo) SetScene(c context.Context, userID primitive.ObjectID, sc Scene) error {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	_, err := r.coll.UpdateOne(ctx, bson.M{
		"ui": userID,
	}, bson.M{
		"$set": bson.M{
			"s": sc,
		},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("r.coll.UpdateOne: %w", err)
	}

	return nil
}

func (r *Repo) GetScene(c context.Context, userID primitive.ObjectID) (Scene, error) {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	var st State
	err := r.coll.FindOne(ctx, bson.M{
		"ui": userID,
	}).Decode(&st)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return None, nil
		}
		return 0, fmt.Errorf("r.coll.FindOne: %w", err)
	}

	return st.Scene, nil
}
