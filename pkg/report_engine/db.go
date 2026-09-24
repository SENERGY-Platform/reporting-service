/*
 * Copyright 2024 InfAI (CC SES)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package report_engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/SENERGY-Platform/reporting-service/pkg/util"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// dbOpTimeout bounds a single database operation. The documents are small and all
// queries used here are indexed, so anything slower than this means the
// connection is broken rather than busy.
const dbOpTimeout = 10 * time.Second

// mongo error codes for an index that already exists with different options.
const (
	codeIndexOptionsConflict  = 85
	codeIndexKeySpecsConflict = 86
)

var DB *mongo.Client

// dbName is set by InitDB. It exists so the collection accessors can stay
// argument free and so tests can run against a scratch database.
var dbName = "reporting"

// dbCtx returns the context to use for a single database operation.
func dbCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), dbOpTimeout)
}

var (
	errEmptyDatabase   = errors.New("mongo database name must not be empty")
	errMissingPassword = errors.New("mongo password must not be empty when a mongo user is set")
)

// InitDB connects to mongodb and verifies that the configured credentials may
// use the configured database.
func InitDB(cfg *config.Config) error {
	client, err := openDB(cfg, dbOpTimeout)
	if err != nil {
		return err
	}
	DB = client
	dbName = cfg.MongoDatabase
	util.Logger.Info("connected to database", "database", dbName)
	return nil
}

// openDB returns a connected client or none at all; it leaves the package state alone.
func openDB(cfg *config.Config, timeout time.Duration) (*mongo.Client, error) {
	opts, err := clientOptions(cfg)
	if err != nil {
		return nil, err
	}
	return connect(opts, cfg.MongoDatabase, timeout)
}

// connect runs listCollections on the service database because Connect does not
// talk to the server and ping needs no authentication.
func connect(opts *options.ClientOptions, database string, timeout time.Duration) (*mongo.Client, error) {
	connectCtx, cancelConnect := context.WithTimeout(context.Background(), timeout)
	defer cancelConnect()
	client, err := mongo.Connect(connectCtx, opts)
	if err != nil {
		return nil, fmt.Errorf("could not connect to database: %w", err)
	}
	checkCtx, cancelCheck := context.WithTimeout(context.Background(), timeout)
	defer cancelCheck()
	listOpts := options.ListCollections().SetNameOnly(true).SetAuthorizedCollections(true)
	if _, err = client.Database(database).ListCollectionNames(checkCtx, bson.D{}, listOpts); err != nil {
		disconnectCtx, cancelDisconnect := context.WithTimeout(context.Background(), timeout)
		defer cancelDisconnect()
		_ = client.Disconnect(disconnectCtx)
		return nil, fmt.Errorf("mongo startup check failed: %w", err)
	}
	return client, nil
}

// clientOptions applies the credentials after the URI, so they replace user,
// password, authSource and authMechanism given there.
func clientOptions(cfg *config.Config) (*options.ClientOptions, error) {
	if cfg.MongoDatabase == "" {
		return nil, errEmptyDatabase
	}
	if cfg.MongoUser != "" && cfg.MongoPassword.Value() == "" {
		return nil, errMissingPassword
	}
	opts := options.Client().ApplyURI(cfg.MongoUrl)
	if cfg.MongoUser != "" {
		opts.SetAuth(options.Credential{
			Username:   cfg.MongoUser,
			Password:   cfg.MongoPassword.Value(),
			AuthSource: cfg.MongoAuthSource,
		})
	}
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid mongo client options: %w", err)
	}
	return opts, nil
}

// EnsureIndexes creates the indexes the report job queue relies on. Finished jobs
// expire jobRetention after they completed; unfinished jobs carry no finishedat
// and are therefore never removed by the ttl index.
func EnsureIndexes(jobRetention time.Duration) error {
	return ensureIndexes(ReportJobs(), jobRetention)
}

// ensureIndexes takes the collection so tests can use a client of their own.
func ensureIndexes(jobs *mongo.Collection, jobRetention time.Duration) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := jobs.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// listing a user's jobs, newest first
		{Keys: bson.D{{Key: "userid", Value: 1}, {Key: "reportid", Value: 1}, {Key: "createdat", Value: -1}}},
		// claiming the oldest pending job
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "createdat", Value: 1}}},
		// finding jobs whose worker stopped sending heartbeats
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "heartbeat", Value: 1}}},
		{
			Keys:    bson.D{{Key: "finishedat", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(int32(jobRetention.Seconds())),
		},
	})
	if err != nil {
		// An existing ttl index cannot be redefined by CreateMany, which would
		// otherwise turn a changed retention setting into a crash loop.
		if isIndexConflict(err) {
			util.Logger.Warn("report job indexes exist with different options, keeping the existing ones", "error", err)
			return nil
		}
		return fmt.Errorf("could not create report job indexes: %w", err)
	}
	return nil
}

func isIndexConflict(err error) bool {
	var serverErr mongo.ServerError
	if errors.As(err, &serverErr) {
		return serverErr.HasErrorCode(codeIndexOptionsConflict) || serverErr.HasErrorCode(codeIndexKeySpecsConflict)
	}
	return false
}

func Reports() *mongo.Collection {
	return DB.Database(dbName).Collection("reports")
}

func ReportJobs() *mongo.Collection {
	return DB.Database(dbName).Collection("report_jobs")
}

func CloseDB() {
	ctx, cancel := dbCtx()
	defer cancel()
	if err := DB.Disconnect(ctx); err != nil {
		util.Logger.Error("failed to disconnect database", "error", err)
	}
}
