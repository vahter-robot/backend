package slave_bot

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"time"
)

type Bot struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	MasterUserID primitive.ObjectID `bson:"mui,omitempty"`
	Token        string             `bson:"t,omitempty"`
	OnPeerStart  string             `bson:"ops,omitempty"`
	Keywords     []Keyword          `bson:"k,omitempty"`
}

type Keyword struct {
	In  string `bson:"i,omitempty"`
	Out string `bson:"o,omitempty"`
}

type Repo struct {
	coll *mongo.Collection
}

const maxKeywords = 50

func NewRepo(ctx context.Context, db *mongo.Database, createIndex bool) (*Repo, error) {
	r := &Repo{
		coll: db.Collection("slave_bots"),
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
	_, err := r.coll.Indexes().CreateMany(ctx, []mongo.IndexModel{{
		Keys: bson.M{
			"mui": 1,
		},
	}, {
		Keys: bson.M{
			"t": 1,
		},
		Options: options.Index().SetUnique(true),
	}})
	if err != nil {
		return fmt.Errorf("r.coll.Indexes().CreateOne: %w", err)
	}

	return nil
}

func (r *Repo) CountByMasterUserID(c context.Context, userID primitive.ObjectID) (int64, error) {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	count, err := r.coll.CountDocuments(ctx, bson.M{
		"mui": userID,
	})
	if err != nil {
		return 0, fmt.Errorf("r.coll.CountDocuments: %w", err)
	}

	return count, nil
}

func (r *Repo) Create(c context.Context, userID primitive.ObjectID, token string) error {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	_, err := r.coll.InsertOne(ctx, Bot{
		MasterUserID: userID,
		Token:        token,
	})
	if err != nil {
		return fmt.Errorf("r.coll.InsertOne: %w", err)
	}

	return nil
}

func (r *Repo) Delete(c context.Context, userID, botID primitive.ObjectID) error {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	_, err := r.coll.DeleteOne(ctx, Bot{
		ID:           botID,
		MasterUserID: userID,
	})
	if err != nil {
		return fmt.Errorf("r.coll.DeleteOne: %w", err)
	}

	return nil
}

type Item struct {
	Doc Bot
	Err error
}

func (r *Repo) Get(c context.Context, userID primitive.ObjectID) ([]Bot, error) {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	cur, err := r.coll.Find(ctx, Bot{
		MasterUserID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("r.coll.Find: %w", err)
	}

	var res []Bot
	err = cur.All(ctx, &res)
	if err != nil {
		return nil, fmt.Errorf("cur.All: %w", err)
	}

	return res, nil
}
