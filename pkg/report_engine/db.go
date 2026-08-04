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

// InitDB connects to mongodb and verifies that the connection is usable.
func InitDB(url string, database string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(url))
	if err != nil {
		return fmt.Errorf("could not connect to database: %w", err)
	}
	// Connect does not talk to the server, so without a ping a wrong url would
	// only surface on the first report request.
	if err = client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("could not reach database: %w", err)
	}
	DB = client
	if database != "" {
		dbName = database
	}
	util.Logger.Info("connected to database", "database", dbName)
	return nil
}

// EnsureIndexes creates the indexes the report job queue relies on. Finished jobs
// expire jobRetention after they completed; unfinished jobs carry no finishedat
// and are therefore never removed by the ttl index.
func EnsureIndexes(jobRetention time.Duration) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := ReportJobs().Indexes().CreateMany(ctx, []mongo.IndexModel{
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
