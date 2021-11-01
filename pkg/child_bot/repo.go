package child_bot

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
	ID              primitive.ObjectID `bson:"_id,omitempty"`
	OwnerUserID     primitive.ObjectID `bson:"ui,omitempty"`
	OwnerUserChatID int64              `bson:"uci,omitempty"`
	Token           string             `bson:"t,omitempty"`
	SetupDone       bool               `bson:"sd,omitempty"`
	OnPeerStart     string             `bson:"ops,omitempty"`
	Keywords        []Keyword          `bson:"k,omitempty"`
	WebhookAt       time.Time          `bson:"wa,omitempty"`
	Mode            mode               `bson:"m,omitempty"`
}

type Keyword struct {
	In  []string `bson:"i,omitempty"`
	Out string   `bson:"o,omitempty"`
	Ban bool     `bson:"b,omitempty"`
}

type Repo struct {
	coll *mongo.Collection
}

type mode uint8

const (
	OnlyFirst mode = iota + 1
	Always
)

func NewRepo(ctx context.Context, db *mongo.Database) (*Repo, error) {
	r := &Repo{
		coll: db.Collection("child_bots"),
	}

	err := r.createIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("r.createIndex: %w", err)
	}

	return r, nil
}

func (r *Repo) createIndex(ctx context.Context) error {
	_, err := r.coll.Indexes().CreateMany(ctx, []mongo.IndexModel{{
		Keys: bson.M{
			"ui": 1,
		},
	}, {
		Keys: bson.M{
			"t": 1,
		},
		Options: options.Index().SetUnique(true),
	}, {
		Keys: bson.M{
			"wa": 1,
		},
	}})
	if err != nil {
		return fmt.Errorf("r.coll.Indexes().CreateOne: %w", err)
	}

	return nil
}

func (r *Repo) CountByUserID(c context.Context, userID primitive.ObjectID) (int64, error) {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	count, err := r.coll.CountDocuments(ctx, bson.M{
		"ui": userID,
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
		OwnerUserID: userID,
		Token:       token,
		SetupDone:   false,
		WebhookAt:   time.Now().UTC(),
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
		ID:          botID,
		OwnerUserID: userID,
	})
	if err != nil {
		return fmt.Errorf("r.coll.DeleteOne: %w", err)
	}

	return nil
}

func (r *Repo) GetByUserID(c context.Context, userID primitive.ObjectID) ([]Bot, error) {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	cur, err := r.coll.Find(ctx, bson.M{
		"ui": userID,
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

type Item struct {
	Doc Bot
	Err error
}

func (r *Repo) GetOldWebhooks(c context.Context, olderThan time.Duration) chan Item {
	res := make(chan Item)

	go func() {
		ctx, cancel := context.WithCancel(c)
		defer cancel()
		defer close(res)

		cur, err := r.coll.Find(ctx, bson.M{
			"wa": bson.M{
				"$lte": time.Now().UTC().Add(-olderThan),
			},
		})
		if err != nil {
			res <- Item{
				Err: fmt.Errorf("r.coll.Find: %w", err),
			}
			return
		}

		for cur.Next(ctx) {
			var doc Bot
			err = cur.Decode(&doc)
			if err != nil {
				res <- Item{
					Err: fmt.Errorf("cur.Decode: %w", err),
				}
				return
			}

			res <- Item{
				Doc: doc,
			}
		}
	}()

	return res
}

func (r *Repo) GetByToken(c context.Context, token string) (Bot, error) {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	var bot Bot
	err := r.coll.FindOne(ctx, bson.M{
		"t": token,
	}).Decode(&bot)
	if err != nil {
		return Bot{}, fmt.Errorf("r.coll.Find: %w", err)
	}

	return bot, nil
}

func (r *Repo) SetWebhookNow(c context.Context, id primitive.ObjectID) error {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	_, err := r.coll.UpdateOne(ctx, bson.M{
		"_id": id,
	}, bson.M{
		"$set": bson.M{
			"wa": time.Now().UTC(),
		},
	})
	if err != nil {
		return fmt.Errorf("r.coll.UpdateOne: %w", err)
	}

	return nil
}

func (r *Repo) SetUserChatID(c context.Context, id primitive.ObjectID, userChatID int64) error {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	_, err := r.coll.UpdateOne(ctx, bson.M{
		"_id": id,
	}, bson.M{
		"$set": bson.M{
			"uci": userChatID,
		},
	})
	if err != nil {
		return fmt.Errorf("r.coll.UpdateOne: %w", err)
	}

	return nil
}

func (r *Repo) SetOnPeerStart(c context.Context, id primitive.ObjectID, onPeerStart string) error {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	_, err := r.coll.UpdateOne(ctx, bson.M{
		"_id": id,
	}, bson.M{
		"$set": bson.M{
			"ops": onPeerStart,
		},
	})
	if err != nil {
		return fmt.Errorf("r.coll.UpdateOne: %w", err)
	}

	return nil
}

func (r *Repo) SetSetupDoneTrue(c context.Context, id primitive.ObjectID) error {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	_, err := r.coll.UpdateOne(ctx, bson.M{
		"_id": id,
	}, bson.M{
		"$set": bson.M{
			"sd": true,
		},
	})
	if err != nil {
		return fmt.Errorf("r.coll.UpdateOne: %w", err)
	}

	return nil
}

func (r *Repo) SetKeywordsAndMode(c context.Context, id primitive.ObjectID, keywords []Keyword, mode mode) error {
	ctx, cancel := context.WithTimeout(c, 10*time.Second)
	defer cancel()

	_, err := r.coll.UpdateOne(ctx, bson.M{
		"_id": id,
	}, bson.M{
		"$set": bson.M{
			"k": keywords,
			"m": mode,
		},
	})
	if err != nil {
		return fmt.Errorf("r.coll.UpdateOne: %w", err)
	}

	return nil
}
