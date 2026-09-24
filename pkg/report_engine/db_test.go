/*
 * Copyright 2026 InfAI (CC SES)
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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	sb_config_types "github.com/SENERGY-Platform/go-service-base/config-hdl/types"
	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const replicaSetURL = "mongodb://mongo-0.mongo:27017,mongo-1.mongo:27017/?replicaSet=rs0&readPreference=primary"

func TestClientOptionsSetsAuthWhenUserGiven(t *testing.T) {
	opts, err := clientOptions(&config.Config{
		MongoUrl:        replicaSetURL,
		MongoUser:       "reporting-service",
		MongoPassword:   "s3cr3t",
		MongoAuthSource: "admin",
		MongoDatabase:   "reporting",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := &options.Credential{Username: "reporting-service", Password: "s3cr3t", AuthSource: "admin"}
	if !reflect.DeepEqual(opts.Auth, want) {
		t.Errorf("auth = %+v, want %+v", opts.Auth, want)
	}
}

func TestClientOptionsNoAuthWithoutUser(t *testing.T) {
	// A password without a user must not switch auth on.
	opts, err := clientOptions(&config.Config{
		MongoUrl:        "mongodb://localhost:27017",
		MongoPassword:   "s3cr3t",
		MongoAuthSource: "admin",
		MongoDatabase:   "reporting",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Auth != nil {
		t.Errorf("auth = %+v, want nil", opts.Auth)
	}
}

func TestClientOptionsConfiguredCredentialsReplaceURICredentials(t *testing.T) {
	opts, err := clientOptions(&config.Config{
		MongoUrl:        "mongodb://old:oldpw@localhost:27017/?authSource=other&authMechanism=SCRAM-SHA-1",
		MongoUser:       "reporting-service",
		MongoPassword:   "newpw",
		MongoAuthSource: "admin",
		MongoDatabase:   "reporting",
	})
	if err != nil {
		t.Fatal(err)
	}
	// An empty mechanism lets the driver negotiate it with the server.
	want := &options.Credential{Username: "reporting-service", Password: "newpw", AuthSource: "admin"}
	if !reflect.DeepEqual(opts.Auth, want) {
		t.Errorf("auth = %+v, want %+v", opts.Auth, want)
	}
}

func TestClientOptionsPassesURIUnchanged(t *testing.T) {
	opts, err := clientOptions(&config.Config{MongoUrl: replicaSetURL, MongoDatabase: "reporting"})
	if err != nil {
		t.Fatal(err)
	}
	if got := opts.GetURI(); got != replicaSetURL {
		t.Errorf("uri = %q, want %q", got, replicaSetURL)
	}
	if want := []string{"mongo-0.mongo:27017", "mongo-1.mongo:27017"}; !reflect.DeepEqual(opts.Hosts, want) {
		t.Errorf("hosts = %v, want %v", opts.Hosts, want)
	}
	if opts.ReplicaSet == nil || *opts.ReplicaSet != "rs0" {
		t.Errorf("replica set = %v, want rs0", opts.ReplicaSet)
	}
}

func TestClientOptionsDoesNotAddScheme(t *testing.T) {
	// MONGODB_URI used to carry the scheme too; a bare host must not be patched up.
	if _, err := clientOptions(&config.Config{MongoUrl: "localhost:27017", MongoDatabase: "reporting"}); err == nil {
		t.Fatal("expected an error for a url without scheme")
	}
}

// unusedAddr returns a loopback address that was just free, so nothing answers there.
func unusedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// The rejections run before connecting, so the unreachable server is never asked.
func TestInitDBRejectsInvalidConfig(t *testing.T) {
	const password = "pw-must-not-appear-4c1e"
	unreachable := "mongodb://" + unusedAddr(t) + "/?directConnection=true&serverSelectionTimeoutMS=200"
	cases := []struct {
		name string
		cfg  config.Config
		want error
	}{
		{"empty database", config.Config{MongoUrl: unreachable, MongoUser: "u", MongoPassword: password}, errEmptyDatabase},
		{"user without password", config.Config{MongoUrl: unreachable, MongoUser: "u", MongoDatabase: "reporting"}, errMissingPassword},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := DB
			err := InitDB(&c.cfg)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if strings.Contains(err.Error(), password) {
				t.Error("error text contains the password")
			}
			if DB != before {
				t.Error("DB changed after a rejected config")
			}
		})
	}
}

func TestInitDBFailsWhenStartupCheckFails(t *testing.T) {
	const password = "pw-must-not-appear-7f3a"
	before, beforeName := DB, dbName
	err := InitDB(&config.Config{
		MongoUrl:        "mongodb://" + unusedAddr(t) + "/?directConnection=true&serverSelectionTimeoutMS=200",
		MongoUser:       "reporting-service",
		MongoPassword:   password,
		MongoAuthSource: "admin",
		MongoDatabase:   "reporting_unreachable",
	})
	if err == nil {
		t.Fatal("expected an error when the server is unreachable")
	}
	if !strings.HasPrefix(err.Error(), "mongo startup check failed: ") {
		t.Errorf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), password) {
		t.Error("error text contains the password")
	}
	if DB != before || dbName != beforeName {
		t.Error("package state changed after a failed startup check")
	}
}

func TestConnectReturnsNoClientWithinTheTimeout(t *testing.T) {
	opts, err := clientOptions(&config.Config{MongoUrl: "mongodb://" + unusedAddr(t) + "/?directConnection=true", MongoDatabase: "reporting"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	client, err := connect(opts, "reporting", 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error")
	}
	if client != nil {
		t.Error("expected no client on failure")
	}
	if !strings.HasPrefix(err.Error(), "mongo startup check failed: ") {
		t.Errorf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("connect took %v, the timeout was not applied", elapsed)
	}
}

// TestInitDBAuthenticates needs a throwaway server with access control;
// MONGO_AUTH_TEST_USER and MONGO_AUTH_TEST_PASSWORD are root credentials, used to
// create and remove the test users and databases.
func TestInitDBAuthenticates(t *testing.T) {
	url, rootUser, rootPassword := os.Getenv("MONGO_AUTH_TEST_URL"), os.Getenv("MONGO_AUTH_TEST_USER"), os.Getenv("MONGO_AUTH_TEST_PASSWORD")
	if testing.Short() || url == "" || rootUser == "" || rootPassword == "" {
		t.Skip("needs MONGO_AUTH_TEST_URL, MONGO_AUTH_TEST_USER and MONGO_AUTH_TEST_PASSWORD, not in -short")
	}
	defaults, err := config.New("")
	if err != nil {
		t.Fatal(err)
	}
	retention, err := time.ParseDuration(defaults.ReportJobRetention)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	root, err := mongo.Connect(ctx, options.Client().ApplyURI(url).SetAuth(options.Credential{Username: rootUser, Password: rootPassword, AuthSource: "admin"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Disconnect(ctx) })

	suffix := randomHex(t)
	testDB, otherDB := "reporting_auth_test_"+suffix, "reporting_auth_other_"+suffix
	svcUser, svcPassword := "reporting-test-"+suffix, randomHex(t)
	otherUser, otherPassword := "reporting-other-"+suffix, randomHex(t)
	createUser(t, root, svcUser, svcPassword, testDB)
	createUser(t, root, otherUser, otherPassword, otherDB)

	cases := []struct {
		name, user, password string
		wantErr              bool
	}{
		{"correct credentials", svcUser, svcPassword, false},
		{"no credentials", "", "", true},
		{"user of another database", otherUser, otherPassword, true},
		{"wrong password", svcUser, svcPassword + "-wrong", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// openDB is InitDB without the package state, which other tests share.
			client, err := openDB(&config.Config{
				MongoUrl:        url,
				MongoUser:       c.user,
				MongoPassword:   sb_config_types.Secret(c.password),
				MongoAuthSource: "admin",
				MongoDatabase:   testDB,
			}, dbOpTimeout)
			if client != nil {
				t.Cleanup(func() { _ = client.Disconnect(ctx) })
			}
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err == nil {
				jobs := client.Database(testDB).Collection("report_jobs")
				if _, err = jobs.InsertOne(ctx, bson.M{"probe": true}); err != nil {
					t.Errorf("correct user cannot write its database: %v", err)
				}
				// Startup creates the indexes next, so readWrite has to cover that too.
				if err = ensureIndexes(jobs, retention); err != nil {
					t.Fatalf("correct user cannot create the indexes: %v", err)
				}
				assertJobIndexes(t, root.Database(testDB).Collection("report_jobs"), retention)
				return
			}
			if client != nil {
				t.Error("expected no client on failure")
			}
			if !strings.HasPrefix(err.Error(), "mongo startup check failed: ") {
				t.Errorf("unexpected error: %v", err)
			}
			for _, pw := range []string{svcPassword, otherPassword, rootPassword} {
				if strings.Contains(err.Error(), pw) {
					t.Error("error text contains a password")
				}
			}
		})
	}
}

// assertJobIndexes reads the indexes as root, so it does not depend on the rights under test.
func assertJobIndexes(t *testing.T, jobs *mongo.Collection, retention time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), dbOpTimeout)
	defer cancel()
	cur, err := jobs.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	var indexes []struct {
		Key                bson.D `bson:"key"`
		ExpireAfterSeconds *int64 `bson:"expireAfterSeconds"`
	}
	if err = cur.All(ctx, &indexes); err != nil {
		t.Fatalf("decode indexes: %v", err)
	}
	found := map[string]*int64{}
	for _, idx := range indexes {
		var parts []string
		for _, e := range idx.Key {
			parts = append(parts, fmt.Sprintf("%s:%v", e.Key, e.Value))
		}
		found[strings.Join(parts, ",")] = idx.ExpireAfterSeconds
	}
	for _, key := range []string{"userid:1,reportid:1,createdat:-1", "status:1,createdat:1", "status:1,heartbeat:1", "finishedat:1"} {
		if _, ok := found[key]; !ok {
			t.Errorf("index %s missing, have %v", key, found)
		}
	}
	if ttl := found["finishedat:1"]; ttl == nil {
		t.Error("index on finishedat has no ttl")
	} else if *ttl != int64(retention.Seconds()) {
		t.Errorf("ttl index on finishedat expires after %d seconds, want %d", *ttl, int64(retention.Seconds()))
	}
}

// createUser registers the cleanup first, so a user created by a call that then
// fails is still removed; dropping a missing user or database is harmless.
func createUser(t *testing.T, root *mongo.Client, user, password, db string) {
	t.Helper()
	admin := root.Database("admin")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), dbOpTimeout)
		defer cancel()
		_ = admin.RunCommand(ctx, bson.D{{Key: "dropUser", Value: user}}).Err()
		_ = root.Database(db).Drop(ctx)
	})
	cmd := bson.D{
		{Key: "createUser", Value: user},
		{Key: "pwd", Value: password},
		{Key: "roles", Value: bson.A{bson.D{{Key: "role", Value: "readWrite"}, {Key: "db", Value: db}}}},
	}
	if err := admin.RunCommand(context.Background(), cmd).Err(); err != nil {
		t.Fatalf("create user: %v", err)
	}
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
